package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const healthyTurnStateDelta = `{"type":"response.output_text.delta","delta":"健康输出"}`
const healthyTurnStateDone = `{"type":"response.completed","response":{"status":"completed"}}`

func healthyTurnStateResponse(status int, state, body string) *http.Response {
	headers := make(http.Header)
	headers.Set("Content-Type", "text/event-stream")
	if state != "" {
		headers.Set(openAICodexTurnStateHeader, state)
	}
	return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}
}

func healthyTurnStateSSE() string {
	return "data: " + healthyTurnStateDelta + "\n\ndata: " + healthyTurnStateDone + "\n\n"
}

func healthyTurnStateRequest(t *testing.T, svc *OpenAIGatewayService, account *Account, session string) (*gin.Context, *http.Request) {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-test","input":"原始请求"}`))
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-test","input":"原始请求"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer 测试凭据")
	req.Header.Set("session_id", session)
	return c, svc.prepareOpenAIHealthyTurnStateRequest(c, account, req, "gpt-test")
}

type healthyTurnStateUpstream struct {
	responses []*http.Response
	requests  []*http.Request
	bodies    []string
	proxies   []string
}

func (u *healthyTurnStateUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.requests = append(u.requests, req.Clone(req.Context()))
	u.proxies = append(u.proxies, proxy)
	body, _ := io.ReadAll(req.Body)
	u.bodies = append(u.bodies, string(body))
	if len(u.responses) == 0 {
		return nil, errors.New("发生了未预期的重试")
	}
	response := u.responses[0]
	u.responses = u.responses[1:]
	return response, nil
}

func (u *healthyTurnStateUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestOpenAIHealthyTurnStateFlags(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI}
	require.False(t, a.OpenAIHealthyTurnStateRecordEnabled())
	require.False(t, a.OpenAIHealthyTurnStateReplaceEnabled())
	a.Extra = map[string]any{openAIHealthyTurnStateRecordKey: false, openAIHealthyTurnStateReplaceKey: true}
	require.False(t, a.OpenAIHealthyTurnStateRecordEnabled())
	require.True(t, a.OpenAIHealthyTurnStateReplaceEnabled())
	for _, key := range []string{openAIHealthyTurnStateRecordKey, openAIHealthyTurnStateReplaceKey} {
		for _, value := range []any{nil, "true", 1} {
			require.Error(t, ValidateOpenAIHealthyTurnStateExtra(map[string]any{key: value}))
		}
	}
	require.NoError(t, ValidateOpenAIHealthyTurnStateExtra(a.Extra))
	a.Platform = PlatformGrok
	require.False(t, a.OpenAIHealthyTurnStateRecordEnabled())
	require.False(t, a.OpenAIHealthyTurnStateReplaceEnabled())
}

func TestOpenAIHealthyTurnStateRecordsOnlyRealOutput(t *testing.T) {
	svc := &OpenAIGatewayService{}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true}}
	_, req := healthyTurnStateRequest(t, svc, a, "来源会话")
	attempt := openAIHealthyTurnStateAttemptFromRequest(req)
	observer := newOpenAIHealthyTurnStateObserver(attempt, healthyTurnStateResponse(200, "健康状态", "").Header)
	observer.observe([]byte(`{"type":"response.created"}`), "")
	observer.observe([]byte(`{"type":"response.output_text.delta","delta":""}`), "")
	require.Empty(t, svc.openaiHealthyTurnStates.entries)
	observer.observe([]byte(healthyTurnStateDelta), "")
	require.Empty(t, svc.openaiHealthyTurnStates.entries, "首字尚未完成，不能记录")
	observer.observe([]byte(`{"type":"error","error":{"code":"server_is_overloaded"}}`), "")
	require.Empty(t, svc.openaiHealthyTurnStates.entries)
	require.False(t, svc.openaiHealthyTurnStates.store(attempt.scope, observer.candidate), "失败旧值不能被延迟响应重新写入")
}

func TestOpenAIHealthyTurnStateHTTPReplaceAndEvict(t *testing.T) {
	for _, status := range []int{429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{
				healthyTurnStateResponse(200, "健康状态", healthyTurnStateSSE()),
				healthyTurnStateResponse(status, "错误状态", "错误"),
				healthyTurnStateResponse(status, "错误状态", "仍失败"),
				healthyTurnStateResponse(status, "", "后续失败"),
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			a := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: true}}
			_, donor := healthyTurnStateRequest(t, svc, a, "来源会话")
			resp, err := svc.doOpenAIUpstream(donor, "", a)
			require.NoError(t, err)
			payload, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, healthyTurnStateSSE(), string(payload))
			require.NoError(t, resp.Body.Close())
			_, target := healthyTurnStateRequest(t, svc, a, "问题会话")
			target.Header.Set(openAICodexTurnStateHeader, "原状态")
			resp, err = svc.doOpenAIUpstream(target, "", a)
			require.NoError(t, err)
			require.Equal(t, status, resp.StatusCode)
			require.Equal(t, "原状态", target.Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, "健康状态", upstream.requests[2].Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, "问题会话", upstream.requests[2].Header.Get("session_id"))
			require.Equal(t, upstream.bodies[1], upstream.bodies[2], "替换不能修改请求体")
			require.Empty(t, svc.openaiHealthyTurnStates.entries)
			_, next := healthyTurnStateRequest(t, svc, a, "后续会话")
			_, err = svc.doOpenAIUpstream(next, "", a)
			require.NoError(t, err)
			require.Len(t, upstream.requests, 4, "已淘汰值不能继续补试")
		})
	}
}

func TestOpenAIHealthyTurnStateHTTPSuccessKeepsIndependentSwitches(t *testing.T) {
	upstream := &healthyTurnStateUpstream{responses: []*http.Response{
		healthyTurnStateResponse(429, "", "错误"), healthyTurnStateResponse(200, "新状态", healthyTurnStateSSE()),
		healthyTurnStateResponse(503, "", "后续同请求错误"),
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	a := &Account{ID: 2, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: false, openAIHealthyTurnStateReplaceKey: true}}
	c, req := healthyTurnStateRequest(t, svc, a, "会话")
	attempt := openAIHealthyTurnStateAttemptFromRequest(req)
	entry := openAIHealthyTurnStateEntry{value: "已有健康状态", expiresAt: time.Now().Add(time.Minute)}
	require.True(t, svc.openaiHealthyTurnStates.store(attempt.scope, entry))
	resp, err := svc.doOpenAIUpstream(req, "", a)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Equal(t, entry, svc.openaiHealthyTurnStates.entries[attempt.scope], "关闭记录仍可使用旧记录，且不能延长寿命或新增值")
	req.Body, _ = req.GetBody()
	req = svc.prepareOpenAIHealthyTurnStateRequest(c, a, req, "gpt-test")
	_, err = svc.doOpenAIUpstream(req, "", a)
	require.NoError(t, err)
	require.Len(t, upstream.requests, 3, "外层普通重试不能重复触发状态替换")
}

func TestOpenAIHealthyTurnStateRetryStreamFailureEvicts(t *testing.T) {
	svc := &OpenAIGatewayService{}
	a := &Account{ID: 3, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true}}
	_, req := healthyTurnStateRequest(t, svc, a, "会话")
	attempt := openAIHealthyTurnStateAttemptFromRequest(req)
	attempt.borrowed = openAIHealthyTurnStateEntry{value: "旧健康状态", expiresAt: time.Now().Add(time.Minute)}
	observer := newOpenAIHealthyTurnStateObserver(attempt, healthyTurnStateResponse(200, "新状态", "").Header)
	observer.observe([]byte(healthyTurnStateDelta), "")
	require.Empty(t, svc.openaiHealthyTurnStates.entries, "替换试验期间不能提前归还状态")
	observer.observe([]byte(`{"type":"response.failed","response":{"error":{"code":"rate_limit_exceeded"}}}`), "")
	require.Empty(t, svc.openaiHealthyTurnStates.entries)
	require.Len(t, svc.openaiHealthyTurnStates.rejected, 2)
}

func TestOpenAIHealthyTurnStateBodyPreservesChunksAndRejectsEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		sse, healthy  bool
	}{
		{"逐字节 SSE", strings.ReplaceAll(healthyTurnStateSSE(), "\n", "\r\n"), true, true},
		{"仅终止事件", "data: " + healthyTurnStateDone + "\n\n", true, false},
		{"首字后断流", "data: " + healthyTurnStateDelta + "\n\n", true, false},
		{"空成功响应", "", false, false},
		{"完整 JSON", `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"健康输出"}]}]}`, false, true},
		{"空 JSON 输出", `{"status":"completed","output":[]}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			a := &Account{ID: 7, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true}}
			_, req := healthyTurnStateRequest(t, svc, a, "会话")
			attempt := openAIHealthyTurnStateAttemptFromRequest(req)
			entry := openAIHealthyTurnStateEntry{value: "旧健康状态", expiresAt: time.Now().Add(time.Minute)}
			require.True(t, svc.openaiHealthyTurnStates.store(attempt.scope, entry))
			var claimed bool
			attempt.borrowed, claimed = svc.openaiHealthyTurnStates.claim(attempt.scope, "")
			require.True(t, claimed)
			body := &openAIHealthyTurnStateBody{
				ReadCloser: io.NopCloser(iotest.OneByteReader(strings.NewReader(tc.payload))),
				observer:   newOpenAIHealthyTurnStateObserver(attempt, healthyTurnStateResponse(200, "新健康状态", "").Header),
				sse:        tc.sse,
			}
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			require.Equal(t, tc.payload, string(payload), "观察状态不能改变原始输出字节")
			require.NoError(t, body.Close())
			require.Empty(t, svc.openaiHealthyTurnStates.held)
			if tc.healthy {
				require.Equal(t, "新健康状态", svc.openaiHealthyTurnStates.entries[attempt.scope].value)
			} else {
				require.Empty(t, svc.openaiHealthyTurnStates.entries)
				require.False(t, svc.openaiHealthyTurnStates.store(attempt.scope, entry), "未成功输出的试验不能回填旧记录")
			}
		})
	}
}

func TestOpenAIHealthyTurnStateCacheIsolationExpiryAndConcurrency(t *testing.T) {
	svc := &OpenAIGatewayService{}
	a := &Account{ID: 4, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true}}
	_, req := healthyTurnStateRequest(t, svc, a, "会话")
	scope := openAIHealthyTurnStateAttemptFromRequest(req).scope
	entry := openAIHealthyTurnStateEntry{value: "健康状态", expiresAt: time.Now().Add(time.Minute)}
	require.True(t, svc.openaiHealthyTurnStates.store(scope, entry))
	for _, foreign := range []openAIHealthyTurnStateScope{{accountID: 5, model: scope.model, identity: scope.identity}, {accountID: 4, model: "不同模型", identity: scope.identity}, {accountID: 4, model: scope.model, identity: [32]byte{1}}} {
		_, ok := svc.openaiHealthyTurnStates.claim(foreign, "")
		require.False(t, ok)
	}
	var claims atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := svc.openaiHealthyTurnStates.claim(scope, ""); ok {
				claims.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 1, claims.Load())
	require.False(t, svc.openaiHealthyTurnStates.store(scope, entry), "已领取状态不能被并发来源重新入池")
	svc.openaiHealthyTurnStates.reject(scope, entry)
	require.False(t, svc.openaiHealthyTurnStates.store(scope, entry))
	entry.value = "过期记录"
	entry.expiresAt = time.Now().Add(-time.Second)
	require.False(t, svc.openaiHealthyTurnStates.store(scope, entry))
}

func TestOpenAIHealthyTurnStateRetryAfterAndCancellation(t *testing.T) {
	svc := &OpenAIGatewayService{}
	a := &Account{ID: 5, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: true}}
	_, req := healthyTurnStateRequest(t, svc, a, "会话")
	attempt := openAIHealthyTurnStateAttemptFromRequest(req)
	entry := openAIHealthyTurnStateEntry{value: "健康状态", expiresAt: time.Now().Add(time.Minute)}
	svc.openaiHealthyTurnStates.store(attempt.scope, entry)
	for _, status := range []int{400, 401, 500, 502} {
		retry, err := attempt.claimRetry(context.Background(), status, nil, "")
		require.NoError(t, err)
		require.False(t, retry)
	}
	retry, err := attempt.claimRetry(context.Background(), 429, http.Header{"Retry-After": []string{"3600"}}, "")
	require.NoError(t, err)
	require.False(t, retry)
	require.Len(t, svc.openaiHealthyTurnStates.entries, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	retry, err = attempt.claimRetry(ctx, 503, nil, "")
	require.NoError(t, err)
	require.False(t, retry)
	require.Len(t, svc.openaiHealthyTurnStates.entries, 1)
	ctx, cancel = context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	retry, err = attempt.claimRetry(ctx, 429, nil, "")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, retry)
	require.Len(t, svc.openaiHealthyTurnStates.entries, 1)
}

type healthyTurnStateWSDialer struct {
	headers []http.Header
	status  []int
}

func (d *healthyTurnStateWSDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.headers = append(d.headers, headers.Clone())
	status := d.status[0]
	d.status = d.status[1:]
	if status != 101 {
		return nil, status, http.Header{}, errors.New("握手失败")
	}
	return &openAIWSFakeConn{}, 101, healthyTurnStateResponse(200, "WS健康状态", "").Header, nil
}

func TestOpenAIHealthyTurnStateWSHandshakeRetriesOnFreshConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	defer svc.getOpenAIWSConnPool().Close()
	dialer := &healthyTurnStateWSDialer{status: []int{503, 101}}
	svc.getOpenAIWSConnPool().setClientDialerForTest(dialer)
	a := &Account{ID: 6, Platform: PlatformOpenAI, Concurrency: 2, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: true}}
	c, _ := healthyTurnStateRequest(t, svc, a, "会话")
	req := openAIWSAcquireRequest{Account: a, WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: http.Header{"Authorization": []string{"Bearer 测试凭据"}}}
	attempt := svc.newOpenAIHealthyTurnStateAttempt(c, a, "gpt-test", "ws:"+req.WSURL, "", req.Headers)
	svc.openaiHealthyTurnStates.store(attempt.scope, openAIHealthyTurnStateEntry{value: "已有健康状态", expiresAt: time.Now().Add(time.Minute)})
	lease, err := svc.acquireOpenAIWSWithHealthyTurnState(context.Background(), c, req, "gpt-test")
	require.NoError(t, err)
	require.NotNil(t, lease)
	require.Len(t, dialer.headers, 2)
	require.Empty(t, dialer.headers[0].Get(openAICodexTurnStateHeader))
	require.Equal(t, "已有健康状态", dialer.headers[1].Get(openAICodexTurnStateHeader))
	lease.observeHealthyTurnState([]byte(healthyTurnStateDelta), nil)
	lease.observeHealthyTurnState([]byte(healthyTurnStateDone), nil)
	lease.Release()
	require.Equal(t, "WS健康状态", svc.openaiHealthyTurnStates.entries[attempt.scope].value)
}

func TestOpenAIHealthyTurnStateWSFailedReplacementEvicts(t *testing.T) {
	svc := &OpenAIGatewayService{}
	dialer := &healthyTurnStateWSDialer{status: []int{429, 503}}
	a := &Account{ID: 8, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: true}}
	c, _ := healthyTurnStateRequest(t, svc, a, "会话")
	wsURL := "wss://chatgpt.com/backend-api/codex/responses"
	headers := http.Header{"Authorization": []string{"Bearer 测试凭据"}}
	attempt := svc.newOpenAIHealthyTurnStateAttempt(c, a, "gpt-test", "ws:"+wsURL, "", headers)
	entry := openAIHealthyTurnStateEntry{value: "已有健康状态", expiresAt: time.Now().Add(time.Minute)}
	require.True(t, svc.openaiHealthyTurnStates.store(attempt.scope, entry))
	conn, status, _, observer, err := svc.dialOpenAIWSWithHealthyTurnState(context.Background(), c, a, "gpt-test", wsURL, headers, "", dialer)
	require.Error(t, err)
	require.Nil(t, conn)
	require.Nil(t, observer)
	require.Equal(t, 503, status)
	require.Equal(t, "已有健康状态", dialer.headers[1].Get(openAICodexTurnStateHeader))
	require.Empty(t, headers.Get(openAICodexTurnStateHeader), "不能修改原始握手请求头")
	require.Empty(t, svc.openaiHealthyTurnStates.entries)
	require.Empty(t, svc.openaiHealthyTurnStates.held)
	require.False(t, svc.openaiHealthyTurnStates.store(attempt.scope, entry))
}
