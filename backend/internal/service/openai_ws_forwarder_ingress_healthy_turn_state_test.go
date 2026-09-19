package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type healthyIngressHandshakeDialer struct {
	mu      sync.Mutex
	status  int
	headers []http.Header
	conn    *openAIWSCaptureConn
}

func (d *healthyIngressHandshakeDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.headers = append(d.headers, headers.Clone())
	if len(d.headers) == 1 {
		return nil, d.status, nil, errors.New("测试握手失败")
	}
	responseHeaders := http.Header{}
	responseHeaders.Set(openAICodexTurnStateHeader, "本账号握手健康头")
	return d.conn, http.StatusSwitchingProtocols, responseHeaders, nil
}

func TestOpenAIWSIngressHealthyTurnStateUsesMappedModelAcrossTurns(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			cfg := newOpenAIWSExecutionScopeTestConfig()
			upstream := &openAIWSCaptureConn{events: [][]byte{
				[]byte(healthyTurnStateDelta),
				[]byte(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`),
				[]byte(healthyTurnStateDelta),
				[]byte(`{"type":"response.completed","response":{"id":"resp_second","model":"gpt-5.4","usage":{"input_tokens":1,"output_tokens":1}}}`),
			}}
			dialer := &healthyIngressHandshakeDialer{status: status, conn: upstream}
			pool := newOpenAIWSConnPool(cfg)
			pool.setClientDialerForTest(dialer)
			defer pool.Close()
			svc := &OpenAIGatewayService{
				cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
				openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool,
			}
			account := &Account{
				ID: 512, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{
					"api_key":       "sk-test",
					"model_mapping": map[string]any{"group-first": "gpt-5.1", "group-second": "gpt-5.4"},
				},
				Extra: map[string]any{"responses_websockets_v2_enabled": true, openAIHealthyTurnStateRecordKey: true, openAIHealthyTurnStateReplaceKey: true},
			}
			originalScope := openAIHealthyTurnStateScope{accountID: account.ID, model: "client-model"}
			firstScope := openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-5.1"}
			secondScope := openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-5.4"}
			foreignScope := openAIHealthyTurnStateScope{accountID: account.ID + 1, model: firstScope.model}
			for scope, value := range map[openAIHealthyTurnStateScope]string{
				originalScope: "原始别名的健康头", firstScope: "映射后模型的健康头", foreignScope: "其他账号的健康头",
			} {
				require.True(t, svc.openaiHealthyTurnStates.store(scope, openAIHealthyTurnStateEntry{value: value, expiresAt: time.Now().Add(time.Minute)}))
			}
			hooks := &OpenAIWSIngressHooks{MapRequestModel: func(turn int, _ string) (string, error) {
				if turn == 1 {
					return "group-first", nil
				}
				return "group-second", nil
			}}
			serverErrCh := make(chan error, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					serverErrCh <- err
					return
				}
				defer conn.CloseNow()
				ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ginCtx.Request = r
				readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				_, firstMessage, readErr := conn.Read(readCtx)
				cancel()
				if readErr != nil {
					serverErrCh <- readErr
					return
				}
				serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "sk-test", firstMessage, hooks)
			}))
			defer server.Close()
			client := dialOpenAIWSExecutionScopeClient(t, server.URL, "健康头模型隔离")
			defer client.CloseNow()
			for _, responseID := range []string{"resp_first", "resp_second"} {
				writeOpenAIWSExecutionScopeRequest(t, client, `{"type":"response.create","model":"client-model","stream":true,"input":[{"role":"user","content":"hi"}]}`)
				readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				for {
					_, payload, err := client.Read(readCtx)
					require.NoError(t, err)
					if gjson.GetBytes(payload, "type").String() == "response.completed" {
						require.Equal(t, responseID, gjson.GetBytes(payload, "response.id").String())
						break
					}
				}
				cancel()
			}
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "完成"))
			select {
			case err := <-serverErrCh:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("等待模型隔离测试结束超时")
			}
			dialer.mu.Lock()
			headers := append([]http.Header(nil), dialer.headers...)
			dialer.mu.Unlock()
			require.Len(t, headers, 2)
			require.Equal(t, "映射后模型的健康头", headers[1].Get(openAICodexTurnStateHeader), "握手补试只领取最终发送模型的健康头")
			for scope, want := range map[openAIHealthyTurnStateScope]string{
				originalScope: "原始别名的健康头", foreignScope: "其他账号的健康头",
				firstScope: "映射后模型的健康头",
			} {
				entry, ok := svc.openaiHealthyTurnStates.get(scope)
				require.True(t, ok)
				require.Equal(t, want, entry.value, "各账号与实际模型保持独立，成功归还原健康头")
			}
			_, secondRecorded := svc.openaiHealthyTurnStates.get(secondScope)
			require.False(t, secondRecorded, "后续轮次不能自动记录握手状态头")
			require.Empty(t, svc.openaiHealthyTurnStates.held, "替换完成必须在原领取范围释放占用")
			upstream.mu.Lock()
			writes := append([]map[string]any(nil), upstream.writes...)
			upstream.mu.Unlock()
			require.Len(t, writes, 2)
			require.Equal(t, firstScope.model, writes[0]["model"])
			require.Equal(t, secondScope.model, writes[1]["model"])
		})
	}
}
