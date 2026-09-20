package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketLiveStatusTracksAttemptAndActualNextCycle(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"},
	}, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
			return nil, errors.New("proxyconnect http://secret:password@proxy.example:8080: connection refused")
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}})
	account := ticketTestAccount(41)
	account.Status = StatusActive
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("未开始采集")
	}
	status := svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0]
	require.Equal(t, "collecting", status.HarvestStatus)
	require.NotNil(t, status.LastAttemptAt)
	require.Equal(t, 1, status.AttemptIndex)
	require.Equal(t, 1, status.AttemptTotal)
	require.Nil(t, status.NextAttemptAt, "进行中的整轮没有确定下一轮时刻")
	require.Equal(t, 6, status.RetryIntervalSeconds)
	require.Equal(t, 25, status.AttemptTimeoutSeconds)
	finishedAfter := time.Now()
	close(release)
	require.Eventually(t, func() bool {
		return svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0].NextAttemptAt != nil
	}, time.Second, time.Millisecond, "本轮结束后应安排下一轮")
	status = svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0]
	require.Equal(t, "waiting", status.HarvestStatus)
	require.Equal(t, "proxy_error", status.LastResult)
	require.True(t, status.NextAttemptAt.After(finishedAfter.Add(6*time.Second)), "下次整轮应从本轮结束起等6秒")
	require.WithinDuration(t, time.Now().Add(6*time.Second), *status.NextAttemptAt, time.Second)
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	for _, secret := range []string{"password", "secret", "proxy.example"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestCodexTicketLiveStatusHonorsSwitchesAndRefreshWindow(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}, FailClosed: true,
	}, &httpUpstreamRecorder{})
	account := ticketTestAccount(41)
	account.Status = StatusActive
	now := time.Now()
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
		Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292, ExpiresAt: now.Add(time.Hour),
	})
	status := svc.OpenAICodexTicketStatuses(context.Background(), account, now)[0]
	require.True(t, status.Ready, "未持久化的内存门票也应立即显示")
	require.Equal(t, "ready", status.HarvestStatus)
	require.Equal(t, now.Add(50*time.Minute), *status.RefreshDueAt)
	require.Nil(t, status.NextAttemptAt)
	require.False(t, status.Blocked)
	svc.openaiCodexTicketHarvest.beginAttempt(account.ID, status.Model, 1, 2)
	account.Status = "disabled"
	status = svc.OpenAICodexTicketStatuses(context.Background(), account, now)[0]
	require.Equal(t, "paused", status.HarvestStatus)
	require.Equal(t, "account_inactive", status.PauseReason)
	require.Nil(t, status.NextAttemptAt)
	account.Status = StatusActive
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = ""
	require.Equal(t, "proxy_not_configured", svc.OpenAICodexTicketStatuses(context.Background(), account, now)[0].PauseReason)
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	require.Empty(t, svc.OpenAICodexTicketStatuses(context.Background(), account, now))
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = true
	account.Extra["openai_turn_state_mode"] = "off"
	require.Empty(t, svc.OpenAICodexTicketStatuses(context.Background(), account, now))
}

func TestCodexTicketProbeSafeResults(t *testing.T) {
	for _, tt := range []struct {
		status int
		result string
	}{
		{http.StatusOK, "invalid_format"}, {http.StatusUnauthorized, "auth_failed"},
		{http.StatusProxyAuthRequired, "proxy_error"}, {http.StatusForbidden, "upstream_error"}, {http.StatusTooManyRequests, "upstream_error"},
	} {
		t.Run(tt.result+http.StatusText(tt.status), func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				response := codexTicketResponse()
				response.StatusCode = tt.status
				response.Header.Set(openAICodexTurnStateHeader, "格式不符的秘密响应")
				return response, nil
			}})
			account := ticketTestAccount(41)
			account.Status = StatusActive
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			status := svc.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0]
			require.Equal(t, tt.result, status.LastResult)
			require.Equal(t, tt.status, status.LastHTTPStatus)
		})
	}
	require.Equal(t, "canceled", classifyOpenAICodexTicketProbeError(context.Canceled))
	require.Equal(t, "request_timeout", classifyOpenAICodexTicketProbeError(context.DeadlineExceeded))
}

type codexTicketBatchUpstream struct {
	HTTPUpstream
	active, peak atomic.Int64
	started      chan struct{}
	release      chan struct{}
	mu           sync.Mutex
	proxies      map[int64][]string
}

func (u *codexTicketBatchUpstream) Do(req *http.Request, proxy string, accountID int64, _ int) (*http.Response, error) {
	active := u.active.Add(1)
	defer u.active.Add(-1)
	for peak := u.peak.Load(); active > peak; peak = u.peak.Load() {
		if u.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	u.mu.Lock()
	u.proxies[accountID] = append(u.proxies[accountID], proxy)
	index := len(u.proxies[accountID])
	u.mu.Unlock()
	if index == 1 {
		u.started <- struct{}{}
		select {
		case <-u.release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	response := codexTicketResponse()
	if index == 1 {
		response.Header.Set(openAICodexTurnStateHeader, "无效门票")
	}
	return response, nil
}

func TestCodexTicketExtractBatchBoundsConcurrencyAndStopsAfterSuccess(t *testing.T) {
	upstream := &codexTicketBatchUpstream{started: make(chan struct{}, 8), release: make(chan struct{}), proxies: make(map[int64][]string)}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyMode: "extract", HarvestExtractURL: "https://supplier.example/list", Models: []string{"gpt-6-astra"}}, upstream)
	var targets []openAICodexTicketHarvestTarget
	for id := int64(1); id <= 8; id++ {
		account := ticketTestAccount(id)
		account.Status = StatusActive
		targets = append(targets, openAICodexTicketHarvestTarget{account: *account, model: "gpt-6-astra"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source := svc.openAICodexTicketHarvestSource(ctx)
	done := make(chan struct{})
	go func() {
		svc.runOpenAICodexTicketHarvestBatch(ctx, targets, []string{"http://1.1.1.1:8080", "http://8.8.8.8:8080", "http://9.9.9.9:8080"}, "extract", source)
		close(done)
	}()
	for i := 0; i < 4; i++ {
		select {
		case <-upstream.started:
		case <-ctx.Done():
			t.Fatal("未按并发启动采集")
		}
	}
	require.Equal(t, int64(4), upstream.active.Load())
	close(upstream.release)
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("批量采集超时")
	}
	require.Equal(t, int64(4), upstream.peak.Load())
	for _, target := range targets {
		require.Equal(t, []string{"http://1.1.1.1:8080", "http://8.8.8.8:8080"}, upstream.proxies[target.account.ID])
		status := svc.OpenAICodexTicketStatuses(ctx, &target.account, time.Now())[0]
		require.True(t, status.Ready)
		require.Equal(t, "success", status.LastResult)
		require.Equal(t, 2, status.AttemptIndex)
		require.Equal(t, 3, status.AttemptTotal)
	}
}

func TestCodexTicketBatchDropsResultsAfterSourceChanges(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://old.example:8080", Models: []string{"gpt-6-astra"}}
	svc := ticketTestService(t, cfg, nil)
	var calls int
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://new.example:8080"
		return codexTicketResponse(), nil
	}}
	account := ticketTestAccount(41)
	account.Status = StatusActive
	source := svc.openAICodexTicketHarvestSource(context.Background())
	svc.probeOpenAICodexTicketWithProxies(context.Background(), account, "gpt-6-astra", []string{source.ProxyURL, "http://another.example:8080"}, source)
	require.Equal(t, 1, calls)
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	require.Equal(t, "canceled", svc.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0].LastResult)
}
