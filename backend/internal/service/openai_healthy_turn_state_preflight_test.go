package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestHealthyTurnStateModeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name            string
		extra           map[string]any
		mode            string
		healthy, ticket bool
	}{
		{"默认关闭", nil, OpenAITurnStateOff, false, false},
		{"旧开关仍是异常补试", map[string]any{openAIHealthyTurnStateReplaceKey: true}, OpenAITurnStateHealthyRetry, true, false},
		{"首发模式", map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}, OpenAITurnStateHealthyPreflight, true, false},
		{"门票优先旧开关", map[string]any{OpenAITurnStateModeKey: OpenAITurnStateCodexTicket, openAIHealthyTurnStateReplaceKey: true}, OpenAITurnStateCodexTicket, false, true},
		{"非法存量保守关闭", map[string]any{OpenAITurnStateModeKey: "unknown", openAIHealthyTurnStateReplaceKey: true}, OpenAITurnStateOff, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Extra: tc.extra}
			require.Equal(t, tc.mode, account.OpenAITurnStateMode())
			require.Equal(t, tc.healthy, account.OpenAIHealthyTurnStateReplaceEnabled())
			require.Equal(t, tc.ticket, account.OpenAICodexTicketEnabled())
		})
	}
	for _, mode := range []string{OpenAITurnStateOff, OpenAITurnStateHealthyRetry, OpenAITurnStateHealthyPreflight, OpenAITurnStateCodexTicket} {
		extra := map[string]any{OpenAITurnStateModeKey: mode, openAIHealthyTurnStateReplaceKey: true}
		require.NoError(t, ValidateOpenAIHealthyTurnStateExtra(extra))
		require.Equal(t, mode == OpenAITurnStateHealthyRetry || mode == OpenAITurnStateHealthyPreflight, extra[openAIHealthyTurnStateReplaceKey])
	}
	legacy := map[string]any{openAIHealthyTurnStateReplaceKey: false}
	require.NoError(t, ValidateOpenAIHealthyTurnStateExtra(legacy))
	require.Equal(t, OpenAITurnStateOff, legacy[OpenAITurnStateModeKey], "旧客户端局部关闭必须覆盖已保存的新模式")
	require.Error(t, ValidateOpenAIHealthyTurnStateExtra(map[string]any{OpenAITurnStateModeKey: "unknown"}))
	require.Error(t, ValidateOpenAIHealthyTurnStateExtra(map[string]any{OpenAIHealthyTurnStateFailClosedKey: "true"}))
}

func TestHealthyTurnStatePreflightHTTPUsesOneLeaseAndPreservesRequest(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		body    string
		success bool
	}{
		{"完整成功归还", 200, healthyTurnStateSSE(), true},
		{"握手失败不再借第二张", 503, "忙", false},
		{"首字后断流淘汰", 200, "data: " + healthyTurnStateDelta + "\n\n", false},
		{"模型错误淘汰且不重放", 200, "data: {\"type\":\"response.created\",\"response\":{\"model\":\"错误模型\"}}\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &healthyStateStoreStub{}
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(tc.status, "", tc.body)}}
			svc := &OpenAIGatewayService{httpUpstream: upstream, openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: store}}
			account := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
			_, req := healthyTurnStateRequest(t, svc, account, "首发会话")
			req.Header.Set(openAICodexTurnStateHeader, "客户端原头")
			resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", account)
			require.NoError(t, err)
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "持久测试头", upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, "客户端原头", req.Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, `{"model":"gpt-test","input":"原始请求"}`, upstream.bodies[0])
			require.Equal(t, 1, store.starts)
			require.Equal(t, []bool{tc.success}, store.results)
			require.Equal(t, []int{tc.status}, store.statuses)
			require.Zero(t, store.saves)
		})
	}
}

func TestHealthyTurnStatePreflightMissingInventory(t *testing.T) {
	for _, tc := range []struct {
		closed     any
		wantClosed bool
	}{{nil, true}, {false, false}, {true, true}} {
		upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(200, "", healthyTurnStateSSE())}}
		svc := &OpenAIGatewayService{httpUpstream: upstream}
		account := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
		if tc.closed != nil {
			account.Extra[OpenAIHealthyTurnStateFailClosedKey] = tc.closed
		}
		_, req := healthyTurnStateRequest(t, svc, account, "无库存")
		req.Header.Set(openAICodexTurnStateHeader, "客户端原头")
		resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", account)
		if tc.wantClosed {
			require.ErrorIs(t, err, ErrOpenAIHealthyTurnStateUnavailable)
			require.Nil(t, resp)
			require.Empty(t, upstream.requests)
		} else {
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Len(t, upstream.requests, 1)
			require.Equal(t, "客户端原头", upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
		}
	}
}

func TestHealthyTurnStatePreflightBudgetExhaustionIsDistinctFromMissingInventory(t *testing.T) {
	svc := &OpenAIGatewayService{openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: &healthyStateStoreStub{}}}
	account := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight, OpenAIHealthyTurnStateFailClosedKey: true}}
	c, req := healthyTurnStateRequest(t, svc, account, "共享预算")
	first := openAIHealthyTurnStateAttemptFromRequest(req)
	used, err := first.claimPreflight(context.Background(), req.Header)
	require.NoError(t, err)
	require.True(t, used)
	first.failed()
	second := svc.newOpenAIHealthyTurnStateAttempt(c, account, "gpt-test", "http:测试上游", "", req.Header)
	used, err = second.claimPreflight(context.Background(), req.Header)
	require.False(t, used)
	require.ErrorIs(t, err, ErrOpenAIHealthyTurnStateBudgetExhausted)
	require.NotErrorIs(t, err, ErrOpenAIHealthyTurnStateUnavailable)
}

func TestHealthyTurnStatePreflightMissingInventoryDoesNotConsumeBudget(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
	c, req := healthyTurnStateRequest(t, svc, account, "空库存")
	first := openAIHealthyTurnStateAttemptFromRequest(req)
	used, err := first.claimPreflight(context.Background(), req.Header)
	require.False(t, used)
	require.ErrorIs(t, err, ErrOpenAIHealthyTurnStateUnavailable)
	require.True(t, svc.openaiHealthyTurnStates.store(first.scope, openAIHealthyTurnStateEntry{value: "补充健康头", expiresAt: time.Now().Add(time.Minute)}))
	next := svc.newOpenAIHealthyTurnStateAttempt(c, account, "gpt-test", "http:测试上游", "", nil)
	used, err = next.claimPreflight(context.Background(), req.Header)
	require.NoError(t, err)
	require.True(t, used, "缺库存未使用健康头，不应消耗该账号的次数")
	next.restore()
}

func TestHealthyTurnStateBudgetFollowsAccountAcrossModesAndTransports(t *testing.T) {
	for _, firstMode := range []string{OpenAITurnStateHealthyPreflight, OpenAITurnStateHealthyRetry} {
		for _, nextMode := range []string{OpenAITurnStateHealthyPreflight, OpenAITurnStateHealthyRetry} {
			t.Run(firstMode+"到"+nextMode, func(t *testing.T) {
				svc := &OpenAIGatewayService{}
				firstAccount := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: firstMode}}
				nextAccount := &Account{ID: 4, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: nextMode}}
				c, req := healthyTurnStateRequest(t, svc, firstAccount, "换号预算")
				claim := func(attempt *openAIHealthyTurnStateAttempt) {
					t.Helper()
					entry := openAIHealthyTurnStateEntry{value: fmt.Sprintf("账号%d健康头", attempt.scope.accountID), expiresAt: time.Now().Add(time.Minute)}
					require.True(t, svc.openaiHealthyTurnStates.store(attempt.scope, entry))
					var used bool
					var err error
					if attempt.preflight {
						used, err = attempt.claimPreflight(context.Background(), make(http.Header))
					} else {
						used, err = attempt.claimRetry(context.Background(), http.StatusServiceUnavailable, nil, "")
					}
					require.NoError(t, err)
					require.True(t, used, "切换账号后必须能领取该账号自己的健康头")
					attempt.failed()
				}
				claim(openAIHealthyTurnStateAttemptFromRequest(req))
				claim(svc.newOpenAIHealthyTurnStateAttempt(c, nextAccount, "gpt-test", "ws:测试上游", "", nil))

				// 更改模型、传输或模式都不能重置原账号已经使用的次数。
				firstAccount.Extra[OpenAITurnStateModeKey] = OpenAITurnStateHealthyPreflight
				back := svc.newOpenAIHealthyTurnStateAttempt(c, firstAccount, "其他模型", "ws:测试上游", "", nil)
				require.True(t, svc.openaiHealthyTurnStates.store(back.scope, openAIHealthyTurnStateEntry{value: "原账号补充库存", expiresAt: time.Now().Add(time.Minute)}))
				used, err := back.claimPreflight(context.Background(), make(http.Header))
				require.False(t, used)
				require.ErrorIs(t, err, ErrOpenAIHealthyTurnStateBudgetExhausted)
				used, err = back.claimRetry(context.Background(), http.StatusServiceUnavailable, nil, "")
				require.False(t, used)
				require.NoError(t, err)
				require.True(t, svc.HasOpenAIHealthyTurnState(firstAccount, "其他模型"), "被预算拦截不能消耗库存")
			})
		}
	}
}

func TestHealthyTurnStatePreflightHTTPSwitchAccountAfterFailure(t *testing.T) {
	upstream := &healthyTurnStateUpstream{responses: []*http.Response{
		healthyTurnStateResponse(503, "", "上游忙"),
		healthyTurnStateResponse(200, "", healthyTurnStateSSE()),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	firstAccount := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
	nextAccount := &Account{ID: 4, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
	c, req := healthyTurnStateRequest(t, svc, firstAccount, "HTTP换号")
	for _, account := range []*Account{firstAccount, nextAccount} {
		require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-test"}, openAIHealthyTurnStateEntry{value: fmt.Sprintf("账号%d健康头", account.ID), expiresAt: time.Now().Add(time.Minute)}))
	}
	resp, err := svc.doOpenAIUpstreamWithHealthyTurnState(req, "", firstAccount)
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.NoError(t, resp.Body.Close())
	req.Body, err = req.GetBody()
	require.NoError(t, err)
	req = svc.prepareOpenAIHealthyTurnStateRequest(c, nextAccount, req, "gpt-test")
	resp, err = svc.doOpenAIUpstreamWithHealthyTurnState(req, "", nextAccount)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, healthyTurnStateSSE(), string(payload))
	require.NoError(t, resp.Body.Close())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "账号3健康头", upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
	require.Equal(t, "账号4健康头", upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
	require.Equal(t, upstream.bodies[0], upstream.bodies[1])
	require.False(t, svc.HasOpenAIHealthyTurnState(firstAccount, "gpt-test"))
	require.True(t, svc.HasOpenAIHealthyTurnState(nextAccount, "gpt-test"))
}

func TestHealthyTurnStatePreflightWSUsesLeaseAndSkipsStrictContinuation(t *testing.T) {
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	defer svc.getOpenAIWSConnPool().Close()
	dialer := &healthyTurnStateWSDialer{status: []int{101}}
	svc.getOpenAIWSConnPool().setClientDialerForTest(dialer)
	account := &Account{ID: 6, Platform: PlatformOpenAI, Concurrency: 2, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
	c, _ := healthyTurnStateRequest(t, svc, account, "首发WS")
	scope := openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-test"}
	entry := openAIHealthyTurnStateEntry{value: "健康首发头", expiresAt: time.Now().Add(time.Minute)}
	require.True(t, svc.openaiHealthyTurnStates.store(scope, entry))
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: http.Header{"Authorization": []string{"Bearer 测试凭据"}}}
	lease, err := svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), c, req, "gpt-test")
	require.NoError(t, err)
	require.Len(t, dialer.headers, 1)
	require.Equal(t, entry.value, dialer.headers[0].Get(openAICodexTurnStateHeader))
	lease.observeHealthyTurnState([]byte(healthyTurnStateDelta), nil)
	lease.observeHealthyTurnState([]byte(healthyTurnStateDone), nil)
	connID := lease.ConnID()
	lease.Release()
	require.True(t, svc.HasOpenAIHealthyTurnState(account, "gpt-test"))
	c, _ = healthyTurnStateRequest(t, svc, account, "严格续链")
	req.ForcePreferredConn, req.PreferredConnID = true, connID
	lease, err = svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), c, req, "gpt-test")
	require.NoError(t, err)
	require.Len(t, dialer.headers, 1, "严格续链必须复用原连接")
	require.True(t, svc.HasOpenAIHealthyTurnState(account, "gpt-test"), "严格续链不占用新租约")
	lease.Release()
}

func TestHealthyTurnStatePreflightWSSwitchAccountAfterFailure(t *testing.T) {
	for _, pooled := range []bool{false, true} {
		t.Run(fmt.Sprintf("连接池%t", pooled), func(t *testing.T) {
			svc := &OpenAIGatewayService{cfg: &config.Config{}}
			defer svc.getOpenAIWSConnPool().Close()
			dialer := &healthyTurnStateWSDialer{status: []int{503, 101}}
			svc.getOpenAIWSConnPool().setClientDialerForTest(dialer)
			firstAccount := &Account{ID: 3, Platform: PlatformOpenAI, Concurrency: 2, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
			nextAccount := &Account{ID: 4, Platform: PlatformOpenAI, Concurrency: 2, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
			c, _ := healthyTurnStateRequest(t, svc, firstAccount, "WS换号")
			for i, account := range []*Account{firstAccount, nextAccount} {
				require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-test"}, openAIHealthyTurnStateEntry{value: fmt.Sprintf("账号%d健康头", account.ID), expiresAt: time.Now().Add(time.Minute)}))
				req := openAIWSAcquireRequest{Account: account, WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: http.Header{"Authorization": []string{"Bearer 测试凭据"}}}
				var err error
				if pooled {
					var lease *openAIWSConnLease
					lease, err = svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), c, req, "gpt-test")
					if lease != nil {
						lease.Release()
					}
				} else {
					var conn openAIWSClientConn
					var observer *openAIHealthyTurnStateObserver
					conn, _, _, observer, err = svc.dialOpenAIWSWithHealthyTurnState(context.Background(), c, account, "gpt-test", req.WSURL, req.Headers, "", dialer)
					if conn != nil {
						_ = conn.Close()
						observer.finish()
					}
				}
				if i == 0 {
					require.Error(t, err)
				} else {
					require.NoError(t, err, "第二个账号必须能使用自己的健康头建立连接")
				}
			}
			require.Len(t, dialer.headers, 2)
			require.Equal(t, "账号3健康头", dialer.headers[0].Get(openAICodexTurnStateHeader))
			require.Equal(t, "账号4健康头", dialer.headers[1].Get(openAICodexTurnStateHeader))
		})
	}
}

func TestHealthyTurnStateTicketModeSkipsHealthyObserver(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateCodexTicket}}
	_, req := healthyTurnStateRequest(t, svc, account, "门票会话")
	require.Nil(t, openAIHealthyTurnStateAttemptFromRequest(req))
	require.True(t, IsOpenAITurnStateProbe(WithOpenAITurnStateProbe(context.Background())))
}

func TestHealthyTurnStateProbeIgnoresPreflightPolicy(t *testing.T) {
	upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(200, "新采集状态", healthyTurnStateSSE())}}
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := healthyTurnStateProbeAccount()
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateHealthyPreflight
	account.Extra[OpenAIHealthyTurnStateFailClosedKey] = true
	result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), account, "gpt-6-astra", "http")
	require.NoError(t, err)
	require.Equal(t, "recorded", result.Status)
	require.Len(t, upstream.requests, 1)
	require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
	require.True(t, IsOpenAITurnStateProbe(upstream.requests[0].Context()))
	require.Equal(t, OpenAITurnStateHealthyPreflight, account.OpenAITurnStateMode(), "采集副本不能更改业务策略")
}

func TestHealthyTurnStatePreflightWSFailureDoesNotRetryTicket(t *testing.T) {
	for _, pooled := range []bool{false, true} {
		store := &healthyStateStoreStub{}
		svc := &OpenAIGatewayService{cfg: &config.Config{}, openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: store}}
		dialer := &healthyTurnStateWSDialer{status: []int{503}}
		account := &Account{ID: 6, Platform: PlatformOpenAI, Concurrency: 2, Extra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}}
		c, _ := healthyTurnStateRequest(t, svc, account, "首发失败")
		headers := http.Header{"Authorization": []string{"Bearer 测试凭据"}}
		if pooled {
			svc.getOpenAIWSConnPool().setClientDialerForTest(dialer)
			_, err := svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), c, openAIWSAcquireRequest{Account: account, WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: headers}, "gpt-test")
			require.Error(t, err)
			svc.getOpenAIWSConnPool().Close()
		} else {
			_, _, _, _, err := svc.dialOpenAIWSWithHealthyTurnState(context.Background(), c, account, "gpt-test", "wss://chatgpt.com/backend-api/codex/responses", headers, "", dialer)
			require.Error(t, err)
		}
		require.Len(t, dialer.headers, 1, "首发已借用后不能追加健康头补试")
		require.Equal(t, "持久测试头", dialer.headers[0].Get(openAICodexTurnStateHeader))
		require.Equal(t, []bool{false}, store.results)
		require.Equal(t, []int{503}, store.statuses)
	}
}

type healthyRefreshInventoryRepo struct{ HealthyTurnStateRepository }

func (*healthyRefreshInventoryRepo) Stats(context.Context, int64) (*HealthyTurnStateStats, error) {
	return &HealthyTurnStateStats{Models: []HealthyTurnStateModelStats{
		{Model: "临期空闲", Available: 3, RefreshDue: 2},
		{Model: "全部在用", InUse: 3},
	}}, nil
}

func TestHealthyTurnStateMaintenanceRefreshesBeforeExpiryWithoutDuplicatingLeases(t *testing.T) {
	svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: &healthyRefreshInventoryRepo{}}}}
	counts, err := svc.healthyDynamicInventory(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"临期空闲": 1, "全部在用": 3}, counts)
}

func TestHealthyTurnStatePreflightHTTPToWSPreservesPreviousResponseConnection(t *testing.T) {
	var connections atomic.Int64
	requests := make(chan string, 2)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("升级连接失败: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		for turn := 1; turn <= 2; turn++ {
			var request map[string]any
			if err := conn.ReadJSON(&request); err != nil {
				return
			}
			previous, _ := request["previous_response_id"].(string)
			requests <- previous
			if err := conn.WriteJSON(map[string]any{"type": "response.output_text.delta", "delta": "健康输出"}); err != nil {
				return
			}
			if err := conn.WriteJSON(map[string]any{"type": "response.completed", "response": map[string]any{"id": fmt.Sprintf("resp_preserved_%d", turn), "model": "gpt-5.1", "status": "completed", "usage": map[string]any{"input_tokens": 1, "output_tokens": 1}}}); err != nil {
				return
			}
		}
	}))
	defer server.Close()
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIWS.Enabled, cfg.Gateway.OpenAIWS.APIKeyEnabled, cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true, true, true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount, cfg.Gateway.OpenAIWS.QueueLimitPerConn = 2, 4
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds, cfg.Gateway.OpenAIWS.ReadTimeoutSeconds, cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 2, 3, 3
	svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{}, openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector()}
	defer svc.getOpenAIWSConnPool().Close()
	account := &Account{ID: 89, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Credentials: map[string]any{"api_key": "测试密钥", "base_url": server.URL}, Extra: map[string]any{"responses_websockets_v2_enabled": true}}
	requestContext := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/openai/v1/responses", nil)
		c.Request.Header.Set("session_id", "首发策略续链")
		groupID := int64(1)
		c.Set("api_key", &APIKey{ID: 3, GroupID: &groupID})
		return c
	}
	result, err := svc.Forward(context.Background(), requestContext(), account, []byte(`{"model":"gpt-5.1","stream":false,"input":"首次请求"}`))
	require.NoError(t, err)
	require.Equal(t, "resp_preserved_1", result.RequestID)
	_, bound := svc.getOpenAIWSStateStore().GetResponseConn(result.RequestID)
	require.True(t, bound)
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateHealthyPreflight
	account.Extra[OpenAIHealthyTurnStateFailClosedKey] = true
	require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-5.1"}, openAIHealthyTurnStateEntry{value: "不应占用的健康头", expiresAt: time.Now().Add(time.Minute)}))
	result, err = svc.Forward(context.Background(), requestContext(), account, []byte(`{"model":"gpt-5.1","stream":false,"previous_response_id":"resp_preserved_1","input":"继续请求"}`))
	require.NoError(t, err)
	require.Equal(t, "resp_preserved_2", result.RequestID)
	require.EqualValues(t, 1, connections.Load(), "HTTP 转 WS 续链必须保留已绑定连接，不能因健康首发另建连接")
	require.Equal(t, "", <-requests)
	require.Equal(t, "resp_preserved_1", <-requests)
	require.True(t, svc.HasOpenAIHealthyTurnState(account, "gpt-5.1"), "续链不领取健康头")
}
