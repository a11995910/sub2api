package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketRaceUpstream struct {
	HTTPUpstream
	do func(*http.Request, string, int64) (*http.Response, error)
}

func (u *codexTicketRaceUpstream) Do(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
	return u.do(req, proxy, id)
}

func waitCodexTicketJob(t *testing.T, svc *OpenAIGatewayService, account *Account, model string) {
	t.Helper()
	require.Eventually(t, func() bool {
		svc.openaiCodexTicketLifecycleMu.Lock()
		defer svc.openaiCodexTicketLifecycleMu.Unlock()
		return svc.openaiCodexTicketJobs[openAICodexTicketKey(account.ID, model)] != nil
	}, time.Second, time.Millisecond)
}

func TestCodexTicketJobInvalidationWakesImmediatelyAndDeduplicates(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	started, release := make(chan struct{}, 10), make(chan struct{})
	var calls atomic.Int64
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, HarvestProxyURL: "http://proxy.example:8080",
		Models: []string{"gpt-6-astra"}, HarvestProbeIntervalSeconds: 60,
	}, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return codexTicketResponse(), nil
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}})
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	ticket := boundTicket("gpt-6-astra", "http://proxy.example:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, ticket)
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	waitCodexTicketJob(t, svc, account, ticket.Model)
	require.Zero(t, calls.Load(), "有效票不得提前刷新")
	svc.invalidateOpenAICodexTicket(account, ticket, "model_mismatch")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("弃票后仍在等待发现周期")
	}
	for i := 0; i < 100; i++ {
		require.True(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
	}
	require.Equal(t, int64(1), calls.Load(), "并发缺票检查只唤醒同一任务")
	close(release)
	require.Eventually(t, func() bool {
		return svc.lookupOpenAICodexTicket(account, ticket.Model).valid(time.Now(), 292)
	}, time.Second, time.Millisecond)
	svc.StopOpenAICodexTicketHarvester()
	require.Equal(t, int64(1), calls.Load())
}

func TestCodexTicketJobExpiresWithoutWaitingForDiscovery(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	started := make(chan struct{}, 1)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}, HarvestProbeIntervalSeconds: 60,
	}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		started <- struct{}{}
		return codexTicketResponse(), nil
	}})
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	ticket := boundTicket("gpt-6-astra", "http://proxy.example:8080")
	ticket.ExpiresAt = time.Now().Add(100 * time.Millisecond)
	svc.storeOpenAICodexTicket(context.Background(), account, ticket)
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("到期后没有按目标独立计时采集")
	}
}

func TestCodexTicketJobRetriesWhileOtherTargetStillRunning(t *testing.T) {
	first, second := ticketTestAccount(41), ticketTestAccount(42)
	first.Status, second.Status = StatusActive, StatusActive
	var fastCalls atomic.Int64
	slowStarted, fastRetried := make(chan struct{}, 1), make(chan struct{}, 1)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}, HarvestProbeIntervalSeconds: 1,
	}, &codexTicketRaceUpstream{do: func(req *http.Request, _ string, id int64) (*http.Response, error) {
		if id == first.ID {
			slowStarted <- struct{}{}
			<-req.Context().Done()
			return nil, req.Context().Err()
		}
		if fastCalls.Add(1) == 1 {
			return nil, errors.New("探测失败")
		}
		fastRetried <- struct{}{}
		return codexTicketResponse(), nil
	}})
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*first, *second}}
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	select {
	case <-slowStarted:
	case <-time.After(time.Second):
		t.Fatal("慢目标未启动")
	}
	select {
	case <-fastRetried:
	case <-time.After(3 * time.Second):
		t.Fatal("快目标的重试被慢目标阻塞")
	}
	require.Equal(t, int64(2), fastCalls.Load())
}

func TestCodexTicketJobRepeatedMissingChecksRespectRetryDelay(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	var calls atomic.Int64
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}, HarvestProbeIntervalSeconds: 60,
	}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("探测失败")
	}})
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	require.Eventually(t, func() bool {
		_, next := svc.openaiCodexTicketHarvest.snapshot(account.ID, "gpt-6-astra")
		return !next.IsZero()
	}, time.Second, time.Millisecond)
	for i := 0; i < 100; i++ {
		require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
	}
	require.Never(t, func() bool { return calls.Load() > 1 }, 100*time.Millisecond, time.Millisecond)
}

func TestCodexTicketJobsBoundTargetsAndStopQueuedWork(t *testing.T) {
	var accounts []Account
	for id := int64(1); id <= 10; id++ {
		account := ticketTestAccount(id)
		account.Status = StatusActive
		accounts = append(accounts, *account)
	}
	var active, peak atomic.Int64
	started := make(chan struct{}, 10)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"},
	}, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		started <- struct{}{}
		<-req.Context().Done()
		return nil, req.Context().Err()
	}})
	svc.accountRepo = &codexTicketRefreshRepo{accounts: accounts}
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	for i := 0; i < 4; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("目标未启动")
		}
	}
	require.Equal(t, int64(4), active.Load())
	done := make(chan struct{})
	go func() { svc.StopOpenAICodexTicketHarvester(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("停止没有取消排队中的目标")
	}
	require.Zero(t, active.Load())
	require.LessOrEqual(t, peak.Load(), int64(4))
	svc.openaiCodexTicketLifecycleMu.Lock()
	jobs := len(svc.openaiCodexTicketJobs)
	svc.openaiCodexTicketLifecycleMu.Unlock()
	require.Zero(t, jobs)
}

type codexTicketBlockingBody struct {
	ctx     context.Context
	started chan struct{}
	closed  atomic.Bool
}

func (b *codexTicketBlockingBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (b *codexTicketBlockingBody) Close() error { b.closed.Store(true); return nil }

func TestCodexTicketRaceFastValidatedWinnerCancelsSlowHeader(t *testing.T) {
	started := make(chan struct{})
	var body *codexTicketBlockingBody
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyMode: "extract", HarvestExtractURL: "https://supplier.example/list", HarvestProxyConcurrency: 2},
		&codexTicketRaceUpstream{do: func(req *http.Request, proxy string, _ int64) (*http.Response, error) {
			resp := codexTicketResponse()
			if proxy == "http://1.1.1.1:8080" {
				body = &codexTicketBlockingBody{ctx: req.Context(), started: started}
				resp.Body = body
			} else {
				select {
				case <-started:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			return resp, nil
		}})
	account := ticketTestAccount(41)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	svc.probeOpenAICodexTicketWithProxies(ctx, account, "gpt-6-astra", []string{"http://1.1.1.1:8080", "http://8.8.8.8:8080"}, svc.openAICodexTicketHarvestSource(ctx))
	ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.Equal(t, "http://8.8.8.8:8080", ticket.ProxyURL)
	require.True(t, body.closed.Load(), "获胜后必须取消慢探测并关闭正文")
	require.Equal(t, "success", svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0].LastResult)
}

func TestCodexTicketRaceRejectsLunaAndContinuesOtherProxies(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyMode: "extract", HarvestExtractURL: "https://supplier.example/list", HarvestProxyConcurrency: 2},
		&codexTicketRaceUpstream{do: func(req *http.Request, proxy string, _ int64) (*http.Response, error) {
			if proxy == "http://1.1.1.1:8080" {
				<-req.Context().Done()
				return nil, req.Context().Err()
			}
			resp := codexTicketResponse()
			if proxy == "http://8.8.8.8:8080" {
				resp.Body = io.NopCloser(strings.NewReader(codexTicketSuccessSSE("gpt-5.6-luna")))
			}
			return resp, nil
		}})
	account := ticketTestAccount(41)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	svc.probeOpenAICodexTicketWithProxies(ctx, account, "gpt-6-astra", []string{"http://1.1.1.1:8080", "http://8.8.8.8:8080", "http://9.9.9.9:8080"}, svc.openAICodexTicketHarvestSource(ctx))
	ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.Equal(t, "http://9.9.9.9:8080", ticket.ProxyURL)
}

func TestCodexTicketRaceGlobalLimitAndCancellation(t *testing.T) {
	var active, peak atomic.Int64
	started := make(chan struct{}, 100)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyMode: "extract", HarvestExtractURL: "https://supplier.example/list", HarvestProxyConcurrency: 3, HarvestGlobalConcurrency: 2},
		&codexTicketRaceUpstream{do: func(req *http.Request, _ string, _ int64) (*http.Response, error) {
			n := active.Add(1)
			defer active.Add(-1)
			for old := peak.Load(); n > old; old = peak.Load() {
				if peak.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			<-req.Context().Done()
			return nil, req.Context().Err()
		}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for id := int64(1); id <= 5; id++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			svc.probeOpenAICodexTicketWithProxies(ctx, ticketTestAccount(id), "gpt-6-astra", []string{"http://1.1.1.1:8080", "http://8.8.8.8:8080", "http://9.9.9.9:8080"}, svc.openAICodexTicketHarvestSource(ctx))
		}(id)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("探测未启动")
		}
	}
	require.Equal(t, int64(2), peak.Load())
	cancel()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("取消后仍有探测或排队未退出")
	}
	require.Zero(t, active.Load())
	require.LessOrEqual(t, peak.Load(), int64(2))
	for id := int64(1); id <= 5; id++ {
		require.Nil(t, svc.lookupOpenAICodexTicket(ticketTestAccount(id), "gpt-6-astra"))
	}
}

func TestCodexTicketCollectionDeduplicatesExtractionAndReusesValidWinner(t *testing.T) {
	var fetches, calls atomic.Int64
	fetchStarted, release := make(chan struct{}), make(chan struct{})
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example:8080"},
		&codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { calls.Add(1); return codexTicketResponse(), nil }})
	loader := func(ctx context.Context) ([]string, OpenAICodexTicketHarvestSource, error) {
		if fetches.Add(1) == 1 {
			close(fetchStarted)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, OpenAICodexTicketHarvestSource{}, ctx.Err()
		}
		return []string{"http://proxy.example:8080"}, svc.openAICodexTicketHarvestSource(ctx), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc.collectOpenAICodexTicket(ctx, ticketTestAccount(41), "gpt-6-astra", loader)
		}()
	}
	select {
	case <-fetchStarted:
	case <-ctx.Done():
		t.Fatal("提取未开始")
	}
	close(release)
	wg.Wait()
	require.Equal(t, int64(1), fetches.Load(), "提取本身也必须去重")
	require.Equal(t, int64(1), calls.Load())
	svc.collectOpenAICodexTicket(ctx, ticketTestAccount(41), "gpt-6-astra", loader)
	require.Equal(t, int64(1), fetches.Load(), "已有有效票不再付费提取")
}

func TestCodexTicketRaceSimultaneousSuccessPersistsOnePair(t *testing.T) {
	var started, persisted atomic.Int64
	ready := make(chan struct{})
	account := ticketTestAccount(41)
	account.Status = StatusActive
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyMode: "extract", HarvestExtractURL: "https://supplier.example/list", HarvestProxyConcurrency: 2},
		&codexTicketRaceUpstream{do: func(req *http.Request, proxy string, _ int64) (*http.Response, error) {
			if started.Add(1) == 2 {
				close(ready)
			}
			select {
			case <-ready:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			response := codexTicketResponse()
			fill := "A"
			if proxy == "http://8.8.8.8:8080" {
				fill = "B"
			}
			response.Header.Set(openAICodexTurnStateHeader, openAICodexTicketStatePrefix+strings.Repeat(fill, 286))
			return response, nil
		}})
	svc.accountRepo = &codexTicketLifecycleRepo{account: *account, persist: func(context.Context) error { persisted.Add(1); return nil }}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	svc.probeOpenAICodexTicketWithProxies(ctx, account, "gpt-6-astra", []string{"http://1.1.1.1:8080", "http://8.8.8.8:8080"}, svc.openAICodexTicketHarvestSource(ctx))
	require.Equal(t, int64(1), persisted.Load())
	ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
	require.NotNil(t, ticket)
	fill := "A"
	if ticket.ProxyURL == "http://8.8.8.8:8080" {
		fill = "B"
	}
	require.Equal(t, openAICodexTicketStatePrefix+strings.Repeat(fill, 286), ticket.State)
}
