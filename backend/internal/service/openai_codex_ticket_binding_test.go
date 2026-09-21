package service

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func boundTicket(model, proxy string) *openAICodexTicket {
	return &openAICodexTicket{Model: model, ProxyURL: proxy, State: fakeCodexTicketState(292), Length: 292, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
}

func TestCodexTicketBindingHarvestPersistsSuccessfulProxyAndRoutesAfterRestart(t *testing.T) {
	for _, length := range []int{292, 332} {
		t.Run(strconv.Itoa(length), func(t *testing.T) {
			response := codexTicketResponse()
			response.Header.Set(openAICodexTurnStateHeader, fakeCodexTicketState(length))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))},
				response,
			}}
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://192.0.2.1:8080"}
			svc := ticketTestService(t, cfg, upstream)
			account := ticketTestAccount(41)
			account.Status = StatusActive
			repo := &codexTicketRefreshRepo{accounts: []Account{*account}}
			svc.accountRepo = repo
			source := svc.openAICodexTicketHarvestSource(context.Background())
			svc.probeOpenAICodexTicketWithProxies(context.Background(), account, "gpt-6-astra", []string{cfg.HarvestProxyURL, "http://192.0.2.2:8080"}, source)
			ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
			require.NotNil(t, ticket)
			require.Equal(t, length, ticket.Length)
			require.Equal(t, "http://192.0.2.2:8080", ticket.ProxyURL)
			require.Equal(t, ticket.ProxyURL, upstream.lastProxyURL)

			// 用真实 JSON 往返模拟数据库与进程重启，不沿用原内存缓存。
			encoded, err := json.Marshal(repo.updates)
			require.NoError(t, err)
			var persisted map[string]any
			require.NoError(t, json.Unmarshal(encoded, &persisted))
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = persisted[openAICodexTicketExtraKey(ticket.Model)]
			business := &httpUpstreamRecorder{responses: []*http.Response{codexTicketResponse()}}
			restarted := ticketTestService(t, cfg, business)
			require.False(t, restarted.openAICodexTicketBlocksAccount(account, ticket.Model))
			status := restarted.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0]
			require.True(t, status.Ready)
			require.False(t, status.Blocked)
			require.Equal(t, length, status.Length)
			req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra"}`))
			require.NoError(t, err)
			require.NoError(t, restarted.bindOpenAICodexTicketRequest(req, account, ticket.Model))
			resp, err := restarted.doOpenAIUpstream(req, "http://192.0.2.99:8080", account)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, ticket.ProxyURL, business.lastProxyURL)
			require.Equal(t, ticket.State, business.requests[0].Header.Get(openAICodexTurnStateHeader))
		})
	}
}

func TestCodexTicketBindingUsesAtomicSnapshotAndModelIsolation(t *testing.T) {
	upstream := &httpUpstreamRecorder{responses: []*http.Response{codexTicketResponse(), codexTicketResponse()}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
	account := ticketTestAccount(41)
	first := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
	second := boundTicket("gpt-5.6-sol", "http://192.0.2.2:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, first)
	svc.storeOpenAICodexTicket(context.Background(), account, second)
	req, _ := http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
	require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, first.Model))
	newer := boundTicket(first.Model, "http://192.0.2.3:8080")
	newer.State = openAICodexTicketStatePrefix + strings.Repeat("C", 286)
	svc.storeOpenAICodexTicket(context.Background(), account, newer)
	_, err := svc.doOpenAIUpstream(req, "", account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable, "已构建请求不能把旧票和新代理拼接")
	require.Empty(t, upstream.requests)
	require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, first.Model))
	resp, err := svc.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, newer.ProxyURL, upstream.lastProxyURL)
	require.Equal(t, newer.State, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
	req, _ = http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
	require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, second.Model))
	resp, err = svc.doOpenAIUpstream(req, "", account)
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, second.ProxyURL, upstream.lastProxyURL)
	require.ErrorIs(t, svc.bindOpenAICodexTicketRequest(req, ticketTestAccount(42), second.Model), ErrOpenAICodexTicketUnavailable)
}

func TestCodexTicketBindingFailureRetiresPairWithoutRevivingStaleSnapshot(t *testing.T) {
	for _, status := range []int{403, 407, 502, 504} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{{StatusCode: status, Body: io.NopCloser(strings.NewReader(""))}}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
			account := ticketTestAccount(41)
			ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
			repo := &codexTicketRefreshRepo{}
			svc.accountRepo = repo
			req, _ := http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
			require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, ticket.Model))
			resp, err := svc.doOpenAIUpstream(req, "", account)
			require.NoError(t, err)
			resp.Body.Close()
			require.True(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
			require.True(t, svc.lookupOpenAICodexTicket(account, ticket.Model).Invalidated)
			require.True(t, repo.updates[openAICodexTicketExtraKey(ticket.Model)].(*openAICodexTicket).Invalidated)
			_, err = svc.doOpenAIUpstream(req, "", account)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable, "已构建的失败请求不能继续复用失效绑定")
			require.Len(t, upstream.requests, 1)
			newer := boundTicket(ticket.Model, "http://192.0.2.2:8080")
			svc.storeOpenAICodexTicket(context.Background(), account, newer)
			svc.invalidateOpenAICodexTicket(account, ticket)
			require.Equal(t, newer.ProxyURL, svc.lookupOpenAICodexTicket(account, ticket.Model).ProxyURL)
			require.False(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model), "旧失败不能淘汰新票")
		})
	}
}

func TestCodexTicketBindingFailureClassification(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{200, 400, 401, 429, 500, 503} {
		require.False(t, openAICodexTicketBindingFailed(ctx, status, nil, nil))
	}
	require.True(t, openAICodexTicketBindingFailed(ctx, 0, &net.OpError{Op: "dial", Err: io.ErrUnexpectedEOF}, nil))
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	require.False(t, openAICodexTicketBindingFailed(canceled, 0, context.Canceled, nil))
	require.False(t, openAICodexTicketBindingFailed(ctx, 0, nil, []byte(`{"type":"response.output_text.delta","delta":"invalid_turn_state"}`)))
	require.True(t, openAICodexTicketBindingFailed(ctx, 0, nil, []byte(`{"type":"response.failed","response":{"error":{"code":"invalid_turn_state"}}}`)))
}

func TestCodexTicketBindingObservesSSEWithoutChangingBytes(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	account := ticketTestAccount(41)
	ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, ticket)
	payload := "data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"turn_state_expired\"}}}\n\n"
	body := &openAICodexTicketBody{ReadCloser: io.NopCloser(strings.NewReader(payload)), ctx: context.Background(), observe: svc.observeOpenAICodexTicketBinding(account, ticket)}
	var output strings.Builder
	buf := make([]byte, 7)
	for {
		n, err := body.Read(buf)
		output.Write(buf[:n])
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, payload, output.String())
	require.True(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
}

func TestCodexTicketBindingLegacyTicketsNeedRecaptureAndValidPairIsNotRefreshedEarly(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://192.0.2.1:8080", Models: []string{"gpt-6-astra"}}
	upstream := &httpUpstreamRecorder{}
	svc := ticketTestService(t, cfg, upstream)
	account := ticketTestAccount(41)
	account.Status = StatusActive
	ticket := boundTicket("gpt-6-astra", "")
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	require.True(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
	ready := boundTicket(ticket.Model, cfg.HarvestProxyURL)
	ready.ExpiresAt = time.Now().Add(time.Minute)
	svc.storeOpenAICodexTicket(context.Background(), account, ready)
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	svc.refreshOpenAICodexTickets(context.Background())
	require.Empty(t, upstream.requests, "到期前继续保留原门票与出口")
	require.Equal(t, ready.ProxyURL, svc.lookupOpenAICodexTicket(account, ticket.Model).ProxyURL)
}

func TestCodexTicketBindingWebSocketPairsProxyAndRetiresExplicitFailure(t *testing.T) {
	pool, _ := newCodexTicketPoolTest(t)
	dialer := &codexTicketIngressReconnectDialer{queue: openAIWSQueueDialer{conns: []openAIWSClientConn{
		&openAIWSCaptureConn{},
		&openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.failed","response":{"error":{"code":"turn_state_expired"}}}`)}},
	}}}
	pool.setClientDialerForTest(dialer)
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	svc.openaiWSPool = pool
	account := ticketTestAccount(41)
	first := boundTicket("gpt-6-astra", "http://192.0.2.10:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, first)
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: make(http.Header), ProxyURL: "http://192.0.2.99:8080"}
	lease, err := svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), nil, req, first.Model)
	require.NoError(t, err)
	firstID := lease.ConnID()
	lease.Release()
	second := boundTicket(first.Model, "http://192.0.2.11:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, second)
	lease, err = svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), nil, req, second.Model)
	require.NoError(t, err)
	defer lease.Release()
	require.NotEqual(t, firstID, lease.ConnID(), "即使票文本相同，代理改变也必须重建握手")
	require.Equal(t, []string{first.ProxyURL, second.ProxyURL}, dialer.proxies)
	require.Equal(t, second.State, dialer.headers[1].Get(openAICodexTurnStateHeader))
	_, err = lease.ReadMessage(time.Second)
	require.NoError(t, err)
	require.True(t, svc.openAICodexTicketBlocksAccount(account, second.Model))
}
