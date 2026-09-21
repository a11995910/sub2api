package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func healthyTurnStateProbeAccount() *Account {
	return &Account{ID: 99, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2,
		Credentials: map[string]any{"access_token": "test-token"},
		Extra:       map[string]any{openAIHealthyTurnStateRecordKey: false, openAIHealthyTurnStateReplaceKey: true}}
}

func TestOpenAIHealthyTurnStateProbeHTTPOnlyRecordsCompleteResponses(t *testing.T) {
	for _, tc := range []struct {
		name, state, body, want, message string
		status                           int
	}{
		{"正常响应", "健康状态", healthyTurnStateSSE(), "recorded", "", 200},
		{"无状态头", "", healthyTurnStateSSE(), "no_header", "模型采集响应成功，但未返回有效的健康状态头", 200},
		{"拒绝访问", "无效状态", `{"error":"机密正文 https://private.example/token=secret"}`, "failed", "模型采集请求返回 HTTP 403", 403},
		{"限流", "无效状态", healthyTurnStateSSE(), "failed", "模型采集请求返回 HTTP 429", 429},
		{"过载", "无效状态", healthyTurnStateSSE(), "failed", "模型采集请求返回 HTTP 503", 503},
		{"服务错误", "无效状态", healthyTurnStateSSE(), "failed", "模型采集请求返回 HTTP 500", 500},
		{"首字后断流", "无效状态", "data: " + healthyTurnStateDelta + "\n\n", "unhealthy", "模型采集响应未完整结束", 200},
		{"流内限流", "无效状态", "data: " + healthyTurnStateDelta + "\n\ndata: {\"type\":\"error\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"机密正文 https://private.example/token=secret\"}}\n\n", "unhealthy", "模型采集响应包含错误或未完成状态", 200},
		{"无首字", "无效状态", "data: " + healthyTurnStateDone + "\n\n", "unhealthy", "模型采集响应未产生有效内容", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(tc.status, tc.state, tc.body)}}
			gateway := &OpenAIGatewayService{httpUpstream: upstream}
			svc := &AccountTestService{openaiGatewayService: gateway}
			account := healthyTurnStateProbeAccount()
			result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), account, "", "http")
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Status)
			require.Equal(t, tc.status, result.HTTPStatus)
			require.Contains(t, result.Message, tc.message)
			for _, secret := range []string{"机密正文", "private.example", "token=secret", "test-token"} {
				require.NotContains(t, result.Message, secret)
			}
			require.Len(t, upstream.requests, 1, "失败也只能发送一次")
			require.Equal(t, "hi", gjson.Get(upstream.bodies[0], "input.0.content.0.text").String())
			require.Equal(t, "gpt-6-astra", gjson.Get(upstream.bodies[0], "model").String())
			require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, false, account.Extra[openAIHealthyTurnStateRecordKey], "手动测试不能修改账号旧配置")
			if tc.want == "recorded" {
				require.Len(t, gateway.openaiHealthyTurnStates.entries, 1)
				require.NotNil(t, result.ExpiresAt)
				encoded, _ := json.Marshal(result)
				require.NotContains(t, string(encoded), tc.state)
			} else {
				require.Empty(t, gateway.openaiHealthyTurnStates.entries)
			}
		})
	}
}

type healthyTurnStateCheckingBody struct {
	io.Reader
	beforeRead func()
}

func TestHealthyTurnStateProbeFirstOutputLimit(t *testing.T) {
	for _, format := range []string{"SSE", "JSON"} {
		for _, tc := range []struct {
			name   string
			delay  time.Duration
			status string
		}{
			{"超过旧门槛仍接受", 6 * time.Second, "recorded"},
			{"十五秒内接受", 14 * time.Second, "recorded"},
			{"超过十五秒拒绝", 16 * time.Second, "unhealthy"},
		} {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				upstream := &healthyTurnStateUpstream{}
				gateway := &OpenAIGatewayService{httpUpstream: upstream}
				response := healthyTurnStateResponse(200, "首字时间测试头", "")
				body := healthyTurnStateSSE()
				if format == "JSON" {
					response.Header.Set("Content-Type", "application/json")
					body = `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"健康输出"}]}]}`
				}
				response.Body = &healthyTurnStateCheckingBody{Reader: strings.NewReader(body), beforeRead: func() {
					// 调整探测起点模拟首字等待，避免测试真实休眠十几秒。
					attempt := openAIHealthyTurnStateAttemptFromRequest(upstream.requests[0])
					attempt.startedAt = time.Now().Add(-tc.delay)
				}}
				upstream.responses = []*http.Response{response}
				svc := &AccountTestService{openaiGatewayService: gateway}
				result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), healthyTurnStateProbeAccount(), "gpt-5.4", "http")
				require.NoError(t, err)
				require.Equal(t, tc.status, result.Status)
				if tc.status == "recorded" {
					require.Len(t, gateway.openaiHealthyTurnStates.entries, 1)
				} else {
					require.Empty(t, gateway.openaiHealthyTurnStates.entries)
					require.Equal(t, "首字超过 15 秒，未记录状态头", result.Message)
				}
			})
		}
	}
}

func TestOpenAIHealthyTurnStateProbeHTTPMissingContentType(t *testing.T) {
	for _, tc := range []struct {
		name, state, body, want string
	}{
		{"完整 SSE", "测试状态头", healthyTurnStateSSE(), "recorded"},
		{"完整 SSE 无头", "", healthyTurnStateSSE(), "no_header"},
		{"首字后断流", "测试状态头", "data: " + healthyTurnStateDelta + "\n\n", "unhealthy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := healthyTurnStateResponse(200, tc.state, tc.body)
			response.Header.Del("Content-Type")
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{response}}
			gateway := &OpenAIGatewayService{httpUpstream: upstream}
			svc := &AccountTestService{openaiGatewayService: gateway}
			result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), healthyTurnStateProbeAccount(), "", "http")
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Status)
			require.Equal(t, 200, result.HTTPStatus)
			require.Len(t, upstream.requests, 1)
			if tc.want == "recorded" {
				require.Len(t, gateway.openaiHealthyTurnStates.entries, 1)
			} else {
				require.Empty(t, gateway.openaiHealthyTurnStates.entries)
			}
		})
	}
}

func (b *healthyTurnStateCheckingBody) Read(p []byte) (int, error) {
	b.beforeRead()
	return b.Reader.Read(p)
}
func (b *healthyTurnStateCheckingBody) Close() error { return nil }

func TestOpenAIHealthyTurnStateProbeDeferredCommitAndProxyIsolation(t *testing.T) {
	upstream := &healthyTurnStateUpstream{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	response := healthyTurnStateResponse(200, "健康状态", "")
	response.Body = &healthyTurnStateCheckingBody{
		Reader: strings.NewReader(healthyTurnStateSSE()),
		beforeRead: func() {
			require.Empty(t, gateway.openaiHealthyTurnStates.entries, "完整读取之前不能提交测试记录")
		},
	}
	upstream.responses = []*http.Response{response, healthyTurnStateResponse(200, "健康状态", healthyTurnStateSSE())}
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := healthyTurnStateProbeAccount()
	account.Proxy = &Proxy{ID: 3, Protocol: "http", Host: "127.0.0.1", Port: 8888}
	account.ProxyID = &account.Proxy.ID
	result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), account, "gpt-5.4", "http")
	require.NoError(t, err)
	require.Equal(t, "recorded", result.Status)
	require.Equal(t, []string{account.Proxy.URL()}, upstream.proxies)
	second, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), account, "gpt-5.4", "http")
	require.NoError(t, err)
	require.Equal(t, "already_recorded", second.Status)
	require.Equal(t, result.ExpiresAt, second.ExpiresAt, "同值不能续期")

	account.Extra[openAIHealthyTurnStateRecordKey] = true
	request := upstream.requests[0]
	foreign := gateway.newOpenAIHealthyTurnStateAttempt(nil, account, "gpt-5.4", "http:"+request.URL.String(), "", request.Header)
	_, claimed := gateway.openaiHealthyTurnStates.claim(foreign.scope, "")
	require.True(t, claimed, "共享池不按代理出口隔离")
	matching := gateway.newOpenAIHealthyTurnStateAttempt(nil, account, "gpt-5.4", "http:"+request.URL.String(), account.Proxy.URL(), request.Header)
	_, claimed = gateway.openaiHealthyTurnStates.claim(matching.scope, "")
	require.False(t, claimed, "同一条共享记录只能被领取一次")
}

func TestOpenAIHealthyTurnStateProbeIgnoresCooldown(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(200, "健康状态", healthyTurnStateSSE())}}
			dialer := &healthyTurnStateProbeWSDialer{status: 101, conn: &healthyTurnStateProbeWSConn{events: [][]byte{[]byte(healthyTurnStateDelta), []byte(healthyTurnStateDone)}}}
			gateway := &OpenAIGatewayService{httpUpstream: upstream, openaiWSPassthroughDialer: dialer}
			svc := &AccountTestService{openaiGatewayService: gateway}
			account := healthyTurnStateProbeAccount()
			until := time.Now().Add(time.Hour)
			account.RateLimitResetAt = &until
			account.OverloadUntil = &until
			account.TempUnschedulableUntil = &until
			setAccountModelRateLimitSnapshot(account, "gpt-5.4", until, "429", time.Now())
			result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), account, "gpt-5.4", transport)
			require.NoError(t, err)
			require.Equal(t, "recorded", result.Status)
			if transport == "http" {
				require.Len(t, upstream.requests, 1)
			} else {
				require.Equal(t, 1, dialer.calls)
			}
			require.Len(t, gateway.openaiHealthyTurnStates.entries, 1)
			require.Equal(t, &until, account.RateLimitResetAt, "状态头采集只记录结果，不修改账号状态")
		})
	}
}

func TestOpenAIHealthyTurnStateProbeCancellation(t *testing.T) {
	upstream := &healthyTurnStateUpstream{}
	gateway := &OpenAIGatewayService{httpUpstream: upstream}
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := healthyTurnStateProbeAccount()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp := healthyTurnStateResponse(200, "健康状态", "")
	resp.Body = &healthyTurnStateCheckingBody{Reader: strings.NewReader(healthyTurnStateSSE()), beforeRead: cancel}
	upstream.responses = []*http.Response{resp}
	result, err := svc.ProbeOpenAIHealthyTurnState(ctx, account, "gpt-5.4", "http")
	require.NoError(t, err)
	require.Equal(t, "unhealthy", result.Status)
	require.Equal(t, "模型采集已取消，未记录状态头", result.Message)
	require.Empty(t, gateway.openaiHealthyTurnStates.entries)
}

type healthyTurnStateProbeWSConn struct {
	openAIWSFakeConn
	events [][]byte
	sent   any
}

func (c *healthyTurnStateProbeWSConn) WriteJSON(_ context.Context, value any) error {
	c.sent = value
	return nil
}
func (c *healthyTurnStateProbeWSConn) ReadMessage(context.Context) ([]byte, error) {
	if len(c.events) == 0 {
		return nil, io.EOF
	}
	payload := c.events[0]
	c.events = c.events[1:]
	return payload, nil
}

type healthyTurnStateProbeWSDialer struct {
	conn   *healthyTurnStateProbeWSConn
	status int
	calls  int
}

func (d *healthyTurnStateProbeWSDialer) Dial(context.Context, string, http.Header, string) (openAIWSClientConn, int, http.Header, error) {
	d.calls++
	if d.status != 101 {
		return nil, d.status, nil, errors.New("测试握手失败")
	}
	return d.conn, 101, healthyTurnStateResponse(200, "WS健康状态", "").Header, nil
}

func TestOpenAIHealthyTurnStateProbeWS(t *testing.T) {
	for _, status := range []int{101, 403, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			conn := &healthyTurnStateProbeWSConn{events: [][]byte{[]byte(healthyTurnStateDelta), []byte(healthyTurnStateDone)}}
			dialer := &healthyTurnStateProbeWSDialer{conn: conn, status: status}
			gateway := &OpenAIGatewayService{openaiWSPassthroughDialer: dialer}
			svc := &AccountTestService{openaiGatewayService: gateway}
			result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), healthyTurnStateProbeAccount(), "gpt-5.4", "websocket")
			require.NoError(t, err)
			require.Equal(t, 1, dialer.calls)
			if status == 101 {
				require.Equal(t, "recorded", result.Status)
				data, _ := json.Marshal(conn.sent)
				require.Equal(t, "hi", gjson.GetBytes(data, "input.0.content.0.text").String())
				require.Equal(t, "response.create", gjson.GetBytes(data, "type").String())
				require.Len(t, gateway.openaiHealthyTurnStates.entries, 1)
			} else {
				require.Equal(t, "failed", result.Status)
				require.Equal(t, status, result.HTTPStatus)
				require.Contains(t, result.Message, "模型采集 WebSocket 握手返回 HTTP")
				require.Empty(t, gateway.openaiHealthyTurnStates.entries)
			}
		})
	}
}

func TestOpenAIHealthyTurnStateProbeRejectsMappedMediaBeforeRequest(t *testing.T) {
	for _, target := range []string{"gpt-image-1", "sora-2"} {
		t.Run(target, func(t *testing.T) {
			upstream := &healthyTurnStateUpstream{}
			svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{httpUpstream: upstream}}
			account := healthyTurnStateProbeAccount()
			account.Credentials["model_mapping"] = map[string]any{"文本别名": target}
			result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), account, "文本别名", "http")
			require.Error(t, err)
			require.Nil(t, result)
			require.Empty(t, upstream.requests, "文本采集不能向映射后的图片或视频模型发请求")
		})
	}
}
