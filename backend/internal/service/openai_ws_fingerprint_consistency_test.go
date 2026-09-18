package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 只连接本地 TLS 回显端，实际执行 WebSocket 握手和双向帧传输。
type fingerprintEchoDialer struct {
	url    string
	client *http.Client
}

func (d *fingerprintEchoDialer) Dial(ctx context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	conn, response, err := coderws.Dial(ctx, d.url, &coderws.DialOptions{HTTPClient: d.client, HTTPHeader: headers})
	if err != nil {
		return nil, 0, nil, err
	}
	return &coderOpenAIWSClientConn{conn: conn}, response.StatusCode, response.Header, nil
}

func TestNativeWSFingerprintMatchesHTTPAcrossTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, ingressMode := range []string{OpenAIWSIngressModePassthrough, OpenAIWSIngressModeCtxPool} {
		for _, mode := range []string{"off", "device", "session", "full"} {
			t.Run(ingressMode+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				headersSeen := make(chan http.Header, 2)
				framesSeen := make(chan []byte, 2)
				upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					conn, err := coderws.Accept(w, r, nil)
					if err != nil {
						return
					}
					defer conn.CloseNow()
					headersSeen <- r.Header.Clone()
					for turn := 1; turn <= 2; turn++ {
						_, body, err := conn.Read(ctx)
						if err != nil {
							return
						}
						framesSeen <- body
						completed := fmt.Sprintf(`{"type":"response.completed","response":{"id":"resp_identity_%d","status":"completed","model":%q,"output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`, turn, gjson.GetBytes(body, "model").String())
						if err := conn.Write(ctx, coderws.MessageText, []byte(completed)); err != nil {
							return
						}
					}
					_, _, _ = conn.Read(ctx)
				}))
				defer upstream.Close()

				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
				cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
				cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
				account := newTestOAuthAccount(951, map[string]any{
					codexFingerprintModeExtraKey:                mode,
					"openai_oauth_responses_websockets_v2_mode": ingressMode,
				})
				account.Concurrency = 1
				account.Credentials = map[string]any{"chatgpt_account_id": "local-identity-account"}
				dialer := &fingerprintEchoDialer{url: "wss" + strings.TrimPrefix(upstream.URL, "https"), client: upstream.Client()}
				svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
				svc.openaiWSPassthroughDialer = dialer
				svc.openaiWSPool = newOpenAIWSConnPool(cfg)
				svc.openaiWSPool.setClientDialerForTest(dialer)
				defer svc.openaiWSPool.Close()
				contexts := make(chan *gin.Context, 1)
				server, serverErr := startPassthroughLifecycleServerWithHooks(t, ctx, svc, account, func(c *gin.Context) *OpenAIWSIngressHooks {
					contexts <- c
					return nil
				})
				defer server.Close()
				clientHeaders := http.Header{}
				clientHeaders.Set("session-id", "client-session")
				clientHeaders.Set("x-codex-installation-id", "client-installation")
				clientHeaders.Set("thread-id", "client-thread")
				clientHeaders.Set(openAIWSTurnMetadataHeader, `{"installation_id":"client-installation","session_id":"client-session","thread_id":"client-thread","turn_id":"client-turn","sandbox":"test"}`)
				client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{HTTPHeader: clientHeaders})
				require.NoError(t, err)
				defer client.CloseNow()

				var handshake http.Header
				var observedContext *gin.Context
				var previousTurnID string
				for turn := 1; turn <= 2; turn++ {
					request := map[string]any{
						"type": "response.create", "model": "gpt-5.1", "input": []any{},
						"client_metadata": map[string]any{
							"installation_id": "client-installation", "x-codex-installation-id": "client-installation",
							"session_id": "client-session", "session-id": "client-session",
							"thread_id": "client-thread", "thread-id": "client-thread", "turn_id": "client-turn",
							openAIWSTurnMetadataHeader: clientHeaders.Get(openAIWSTurnMetadataHeader),
						},
					}
					if turn == 2 {
						request["previous_response_id"] = "resp_identity_1"
					}
					body, err := json.Marshal(request)
					require.NoError(t, err)
					require.NoError(t, client.Write(ctx, coderws.MessageText, body))
					_, completed, err := client.Read(ctx)
					require.NoError(t, err)
					require.Equal(t, "response.completed", gjson.GetBytes(completed, "type").String())
					var forwarded []byte
					select {
					case forwarded = <-framesSeen:
					case <-ctx.Done():
						t.Fatal("未收到本地上游请求")
					}
					if turn == 1 {
						handshake = <-headersSeen
						observedContext = <-contexts
					}
					value, exists := observedContext.Get(openAIRequestIntegrityReportKey)
					require.True(t, exists, "实际发送帧必须生成观察结果")
					report, ok := value.(openAIRequestIntegrityReport)
					require.True(t, ok)
					require.Equal(t, "unchanged", report.Status, "指纹变化不应误报正文变化：%v", report.Fields)
					if ingressMode == OpenAIWSIngressModeCtxPool {
						require.Equal(t, "responses_ws_ingress_final", report.Path)
					}
					// HTTP 采用相同的凭据隔离和既有收敛算法，稳定字段必须跨协议一致。
					expected, _, err := applyCodexAccountIdentityClientMetadataRaw(body, account, 0)
					require.NoError(t, err)
					expected, _, err = applyCodexFingerprintClientMetadataRaw(expected, resolveCodexFingerprintIDsFromRequest(account, clientHeaders))
					require.NoError(t, err)
					for _, field := range []string{"installation_id", "x-codex-installation-id", "session_id", "session-id", "thread_id", "thread-id"} {
						require.Equal(t, gjson.GetBytes(expected, "client_metadata."+field).String(), gjson.GetBytes(forwarded, "client_metadata."+field).String(), field)
					}
					require.Equal(t, handshake.Get("x-codex-installation-id"), gjson.GetBytes(forwarded, "client_metadata.installation_id").String())
					require.Equal(t, handshake.Get("session-id"), gjson.GetBytes(forwarded, "client_metadata.session_id").String())
					require.Equal(t, handshake.Get("thread-id"), gjson.GetBytes(forwarded, "client_metadata.thread_id").String())
					if mode == "session" || mode == "full" {
						turnID := gjson.GetBytes(forwarded, "client_metadata.turn_id").String()
						embedded := gjson.GetBytes(forwarded, "client_metadata."+openAIWSTurnMetadataHeader).String()
						require.Equal(t, turnID, gjson.Get(embedded, "turn_id").String())
						if turn == 1 {
							require.Equal(t, turnID, gjson.Get(handshake.Get(openAIWSTurnMetadataHeader), "turn_id").String())
						} else {
							require.NotEqual(t, previousTurnID, turnID, "每轮应生成新 turn ID")
						}
						previousTurnID = turnID
					}
				}
				_ = client.CloseNow()
				select {
				case <-serverErr:
				case <-ctx.Done():
					t.Fatal("本地 WS 转发未结束")
				}
			})
		}
	}
}

func TestWSFingerprintMissingHeadersReconnectAndAccountSwitch(t *testing.T) {
	body := []byte(`{"model":"gpt-5.1","input":[],"client_metadata":{"session_id":"body-only-session","installation_id":"client-device"}}`)
	svc := &OpenAIGatewayService{}
	for _, mode := range []string{"off", "device", "session", "full"} {
		t.Run(mode, func(t *testing.T) {
			account := newTestOAuthAccount(701, map[string]any{codexFingerprintModeExtraKey: mode})
			var previous []byte
			for connection := 0; connection < 2; connection++ {
				c := newFingerprintStageTestContext(t)
				_, err := svc.prepareCodexAccountIdentitySource(context.Background(), c, account)
				require.NoError(t, err)
				out, _, err := applyCodexAccountAndFingerprintIdentityRaw(c, account, body)
				require.NoError(t, err)
				for _, path := range []string{"client_metadata.installation_id", "client_metadata.session_id", "client_metadata.thread_id"} {
					if connection > 0 {
						require.Equal(t, gjson.GetBytes(previous, path).String(), gjson.GetBytes(out, path).String(), "重连不改变既有身份派生规则")
					}
				}
				previous = out
				if mode != "off" {
					require.NotNil(t, stagedCodexFingerprintIDs(c, account))
				}
				// 调度切到 API Key 后，不得残留前一个 OAuth 账号的收敛身份。
				next := &Account{ID: 702, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
				_, err = svc.prepareCodexAccountIdentitySource(context.Background(), c, next)
				require.NoError(t, err)
				out, changed, err := applyCodexAccountAndFingerprintIdentityRaw(c, next, body)
				require.NoError(t, err)
				require.False(t, changed)
				require.Equal(t, body, out)
				require.Nil(t, stagedCodexFingerprintIDs(c, account))
			}
		})
	}
}

func TestFingerprintEmbeddedIdentityAliasesRemainConsistent(t *testing.T) {
	account := newTestOAuthAccount(703, map[string]any{codexFingerprintModeExtraKey: "session"})
	ids := resolveCodexFingerprintIDs(account, "source-session", codexFingerprintSession)
	metadata := `{"installation_id":"old","x-codex-installation-id":"old","session_id":"old","session-id":"old","thread_id":"old","thread-id":"old","turn_id":"old","turn-id":"old","window_id":"old","x-codex-window-id":"old","sandbox":"test"}`
	headers := http.Header{}
	headers.Set(openAIWSTurnMetadataHeader, metadata)
	applyCodexFingerprintHeaders(headers, ids)
	body := map[string]any{"client_metadata": map[string]any{openAIWSTurnMetadataHeader: metadata}}
	require.True(t, applyCodexFingerprintClientMetadata(body, ids))
	out := body["client_metadata"].(map[string]any)[openAIWSTurnMetadataHeader].(string)
	require.JSONEq(t, headers.Get(openAIWSTurnMetadataHeader), out)
	for _, names := range [][2]string{{"installation_id", "x-codex-installation-id"}, {"session_id", "session-id"}, {"thread_id", "thread-id"}, {"turn_id", "turn-id"}, {"window_id", "x-codex-window-id"}} {
		require.Equal(t, gjson.Get(out, names[0]).String(), gjson.Get(out, names[1]).String())
		require.NotEqual(t, "old", gjson.Get(out, names[1]).String())
	}
	require.Equal(t, "test", gjson.Get(out, "sandbox").String())
}

func TestNativeWSFingerprintReconnectUsesCurrentTurnWithoutCacheKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type capturedRequest struct {
		headers http.Header
		body    []byte
	}
	captured := make(chan capturedRequest, 3)
	var connections atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		connection := connections.Add(1)
		for turn := 1; ; turn++ {
			_, body, err := conn.Read(ctx)
			if err != nil {
				return
			}
			captured <- capturedRequest{headers: r.Header.Clone(), body: body}
			if connection == 1 && turn == 2 {
				_ = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"error","error":{"type":"invalid_request_error","code":"previous_response_not_found","message":"previous response not found"}}`))
				return
			}
			responseID := "resp_reconnect_first"
			if connection > 1 {
				responseID = "resp_reconnect_second"
			}
			completed := fmt.Sprintf(`{"type":"response.completed","response":{"id":"%s","status":"completed","model":%q,"output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`, responseID, gjson.GetBytes(body, "model").String())
			if err := conn.Write(ctx, coderws.MessageText, []byte(completed)); err != nil {
				return
			}
		}
	}))
	defer upstream.Close()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.IngressPreviousResponseRecoveryEnabled = true
	account := newTestOAuthAccount(952, map[string]any{
		codexFingerprintModeExtraKey:                "session",
		"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModeCtxPool,
	})
	account.Concurrency = 1
	account.Credentials = map[string]any{"chatgpt_account_id": "local-reconnect-account"}
	svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
	svc.openaiWSPool = newOpenAIWSConnPool(cfg)
	svc.openaiWSPool.setClientDialerForTest(&fingerprintEchoDialer{url: "wss" + strings.TrimPrefix(upstream.URL, "https"), client: upstream.Client()})
	defer svc.openaiWSPool.Close()
	server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
	defer server.Close()
	headers := http.Header{}
	headers.Set("session-id", "client-session")
	headers.Set(openAIWSTurnMetadataHeader, `{"turn_id":"client-turn"}`)
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), &coderws.DialOptions{HTTPHeader: headers})
	require.NoError(t, err)
	defer client.CloseNow()
	firstBody := []byte(`{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"第一轮"}]}`)
	require.NoError(t, client.Write(ctx, coderws.MessageText, firstBody))
	_, firstResponse, err := client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "resp_reconnect_first", gjson.GetBytes(firstResponse, "response.id").String())
	secondBody := []byte(`{"type":"response.create","model":"gpt-5.1","previous_response_id":"resp_reconnect_first","input":[{"role":"user","content":"第二轮"}]}`)
	require.NoError(t, client.Write(ctx, coderws.MessageText, secondBody))
	_, secondResponse, err := client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, "resp_reconnect_second", gjson.GetBytes(secondResponse, "response.id").String())
	requests := make([]capturedRequest, 0, 3)
	for len(requests) < 3 {
		select {
		case request := <-captured:
			requests = append(requests, request)
		case <-ctx.Done():
			t.Fatal("未收到重连后的上游请求")
		}
	}
	require.Equal(t, int32(2), connections.Load())
	firstTurnID := gjson.GetBytes(requests[0].body, "client_metadata.turn_id").String()
	secondTurnID := gjson.GetBytes(requests[1].body, "client_metadata.turn_id").String()
	reconnectedTurnID := gjson.GetBytes(requests[2].body, "client_metadata.turn_id").String()
	require.NotEmpty(t, firstTurnID)
	require.NotEqual(t, firstTurnID, secondTurnID)
	require.Equal(t, secondTurnID, reconnectedTurnID, "同轮重试应保留当前轮的身份")
	require.Equal(t, firstTurnID, gjson.Get(requests[1].headers.Get(openAIWSTurnMetadataHeader), "turn_id").String(), "现有连接握手不随正文轮次改写")
	require.Equal(t, reconnectedTurnID, gjson.Get(requests[2].headers.Get(openAIWSTurnMetadataHeader), "turn_id").String(), "新连接握手必须使用当前轮身份")
	require.False(t, gjson.GetBytes(requests[2].body, "prompt_cache_key").Exists())
	_ = client.CloseNow()
	select {
	case <-serverErr:
	case <-ctx.Done():
		t.Fatal("重连测试未结束")
	}
}
