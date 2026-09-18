//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIHealthyTurnStateHTTPRetryCancelsOnlyDiscardedAttempt(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			first := newContextBoundBlockingReadCloser([]byte("data: {\"type\":\"response.created\",\"response\":{\"model\":\"错误模型\"}}\n\n"))
			t.Cleanup(first.forceUnblock)
			var requestContexts []context.Context
			upstream := &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				requestContexts = append(requestContexts, req.Context())
				if len(requestContexts) == 1 {
					first.ctx = req.Context()
					response := healthyTurnStateResponse(status, "", "")
					response.Body = first
					return response, nil
				}
				return healthyTurnStateResponse(http.StatusOK, "", healthyTurnStateSSE()), nil
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{openAIHealthyTurnStateReplaceKey: true}}
			_, req := healthyTurnStateRequest(t, svc, account, "")
			require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateAttemptFromRequest(req).scope, openAIHealthyTurnStateEntry{
				value: "可替换头", expiresAt: time.Now().Add(time.Minute),
			}))
			type result struct {
				response *http.Response
				err      error
			}
			results := make(chan result, 1)
			go func() {
				response, err := svc.doOpenAIUpstream(req, "", account)
				results <- result{response, err}
			}()
			select {
			case got := <-results:
				require.NoError(t, got.err)
				require.NotNil(t, got.response)
				defer got.response.Body.Close()
				require.Len(t, requestContexts, 2)
				require.ErrorIs(t, requestContexts[0].Err(), context.Canceled, "关闭错误响应先取消对应的上游读取")
				require.NoError(t, requestContexts[1].Err(), "首个尝试取消不能打断补试")
				require.NoError(t, req.Context().Err(), "不取消调用方上下文")
				payload, err := io.ReadAll(got.response.Body)
				require.NoError(t, err)
				require.Equal(t, healthyTurnStateSSE(), string(payload))
				require.NoError(t, got.response.Body.Close())
				require.ErrorIs(t, requestContexts[1].Err(), context.Canceled, "消费结束关闭响应释放本次上游请求")
				require.NoError(t, req.Context().Err())
			case <-time.After(3 * time.Second):
				t.Fatal("关闭旧响应前必须取消其上游请求，不能阻塞补试")
			}
		})
	}
}
