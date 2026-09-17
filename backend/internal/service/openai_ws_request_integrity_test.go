package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestHTTPToWSFinalIntegrityObservesRetryPayload(t *testing.T) {
	for _, tt := range []struct {
		name           string
		include        bool
		changed        bool
		expectedFields []string
	}{
		{name: "传输字段变化不误报"},
		{name: "重试裁剪推理回放选项可观察", include: true, expectedFields: []string{"include"}},
		{name: "最终正文丢失可观察且不阻断", include: true, changed: true, expectedFields: []string{"input", "previous_response_id", "include"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			received := make(chan []byte, 1)
			upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := coderws.Accept(w, r, nil)
				if err != nil {
					return
				}
				defer conn.CloseNow()
				_, body, err := conn.Read(ctx)
				if err != nil {
					return
				}
				received <- body
				_ = conn.Write(ctx, coderws.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_final_integrity","status":"completed","model":"gpt-5.1","output":[],"usage":{"input_tokens":1,"output_tokens":1}}}`))
			}))
			defer upstream.Close()
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			svc := newPassthroughLifecycleService(cfg, newStagedPassthroughConn())
			svc.openaiWSPool = newOpenAIWSConnPool(cfg)
			svc.openaiWSPool.setClientDialerForTest(&fingerprintEchoDialer{url: "wss" + strings.TrimPrefix(upstream.URL, "https"), client: upstream.Client()})
			defer svc.openaiWSPool.Close()
			account := &Account{ID: 971, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Concurrency: 1}
			c := newFingerprintStageTestContext(t)
			original := []byte(`{"model":"gpt-5.1","input":[{"role":"user","content":"必须保留的正文"}],"previous_response_id":"resp_previous","stream":true,"store":true}`)
			var request map[string]any
			require.NoError(t, json.Unmarshal(original, &request))
			if tt.include {
				request["include"] = []string{"reasoning.encrypted_content"}
			}
			original, err := json.Marshal(request)
			require.NoError(t, err)
			stageOpenAIRequestIntegrity(c, account, original)
			request["stream"], request["store"] = false, false
			if tt.changed {
				request["input"] = []any{}
				delete(request, "previous_response_id")
			}
			result, err := svc.forwardOpenAIWSV2(ctx, c, account, request, "", "", "test-token",
				OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2},
				false, false, "gpt-5.1", "gpt-5.1", time.Now(), 2, "", nil)
			require.NoError(t, err, "观察不得阻断请求")
			require.NotNil(t, result)
			var body []byte
			select {
			case body = <-received:
			case <-ctx.Done():
				t.Fatal("未收到最终 WS 请求")
			}
			require.False(t, gjson.GetBytes(body, "include").Exists(), "第二次尝试会移除 include，观察应保留这一差异")
			value, ok := c.Get(openAIRequestIntegrityReportKey)
			require.True(t, ok)
			report, ok := value.(openAIRequestIntegrityReport)
			require.True(t, ok)
			require.Equal(t, "responses_http_to_ws_final", report.Path)
			if len(tt.expectedFields) > 0 {
				require.Equal(t, "changed", report.Status)
				require.ElementsMatch(t, tt.expectedFields, report.Fields)
			} else {
				require.Equal(t, "unchanged", report.Status)
				require.Empty(t, report.Fields)
			}
		})
	}
}
