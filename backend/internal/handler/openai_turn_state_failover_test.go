//go:build unit

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func localTurnStateFailover(message string) *service.UpstreamFailoverError {
	return &service.UpstreamFailoverError{
		StatusCode:        http.StatusServiceUnavailable,
		Reason:            service.GatewayFailureReason("openai_turn_state_unavailable"),
		Scope:             service.GatewayFailureScopeAccount,
		NextAccountAction: service.NextAccountRetry,
		ClientStatusCode:  http.StatusServiceUnavailable,
		ClientMessage:     message,
	}
}

func TestTurnStateFailoverExhaustionPreserves503AcrossEntrypoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, failure := range []struct{ name, code, message string }{
		{"库存不足", "turn_state_unavailable", "当前账号模型暂时没有可用状态头，请稍后重试"},
		{"预算耗尽", "turn_state_budget_exhausted", service.ErrOpenAIHealthyTurnStateBudgetExhausted.Error()},
	} {
		t.Run(failure.name, func(t *testing.T) {
			for _, entry := range []struct {
				name, path, errorType string
				withCode              bool
				handle                func(*gin.Context, *service.UpstreamFailoverError, bool)
			}{
				{"OpenAI Responses", "/openai/v1/responses", "server_error", true, (&OpenAIGatewayHandler{}).handleFailoverExhausted},
				{"OpenAI Chat", "/openai/v1/chat/completions", "server_error", true, (&OpenAIGatewayHandler{}).handleFailoverExhausted},
				{"OpenAI Messages", "/openai/v1/messages", "api_error", false, (&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted},
				{"通用 Responses", "/v1/responses", "", true, (&GatewayHandler{}).handleResponsesFailoverExhausted},
				{"通用 Chat", "/v1/chat/completions", "server_error", true, (&GatewayHandler{}).handleCCFailoverExhausted},
				{"通用 Messages", "/v1/messages", "api_error", true, func(c *gin.Context, err *service.UpstreamFailoverError, started bool) {
					(&GatewayHandler{}).handleFailoverExhausted(c, err, service.PlatformOpenAI, started)
				}},
			} {
				t.Run(entry.name, func(t *testing.T) {
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest(http.MethodPost, entry.path, nil)
					entry.handle(c, localTurnStateFailover(failure.message), false)
					require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
					require.Equal(t, failure.message, gjson.Get(recorder.Body.String(), "error.message").String())
					require.Equal(t, entry.errorType, gjson.Get(recorder.Body.String(), "error.type").String())
					if entry.withCode {
						require.Equal(t, failure.code, gjson.Get(recorder.Body.String(), "error.code").String())
					}
				})
			}
		})
	}
}

func TestTurnStateFailoverAfterHeartbeatPreservesTerminalErrorAndOps503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range []struct {
		name, path, terminal string
		handle               func(*gin.Context, *service.UpstreamFailoverError, bool)
	}{
		{"OpenAI Responses", "/openai/v1/responses", "event: response.failed", (&OpenAIGatewayHandler{}).handleFailoverExhausted},
		{"OpenAI Messages", "/openai/v1/messages", "event: error", (&OpenAIGatewayHandler{}).handleAnthropicFailoverExhausted},
		{"通用 Responses", "/v1/responses", "event: response.failed", (&GatewayHandler{}).handleResponsesFailoverExhausted},
		{"通用 Messages", "/v1/messages", `"type":"error"`, func(c *gin.Context, err *service.UpstreamFailoverError, started bool) {
			(&GatewayHandler{}).handleFailoverExhausted(c, err, service.PlatformOpenAI, started)
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, entry.path, nil)
			heartbeat := ": keepalive\n\n"
			written, err := c.Writer.WriteString(heartbeat)
			require.NoError(t, err)
			recordGatewayStreamHeartbeat(c, written)
			c.Writer.Flush()
			message := service.ErrOpenAIHealthyTurnStateBudgetExhausted.Error()
			entry.handle(c, localTurnStateFailover(message), true)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.True(t, strings.HasPrefix(recorder.Body.String(), heartbeat))
			require.Contains(t, recorder.Body.String(), entry.terminal)
			require.Contains(t, recorder.Body.String(), message)
			streamErr, ok := service.GetOpsStreamError(c)
			require.True(t, ok)
			require.Equal(t, http.StatusServiceUnavailable, streamErr.IntendedStatus)
			require.Equal(t, message, streamErr.Message)
		})
	}
}

func TestTurnStateFailoverDoesNotDuplicateCommittedChatOrResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, entry := range []struct {
		name, payload string
		handle        func(*gin.Context, *service.UpstreamFailoverError, bool)
	}{
		{"Chat 已结束", "data: [DONE]\n\n", (&GatewayHandler{}).handleCCFailoverExhausted},
		{"Responses 已有语义", "event: response.created\ndata: {\"type\":\"response.created\"}\n\n", (&GatewayHandler{}).handleResponsesFailoverExhausted},
	} {
		t.Run(entry.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			_, err := c.Writer.WriteString(entry.payload)
			require.NoError(t, err)
			c.Writer.Flush()
			entry.handle(c, localTurnStateFailover(service.ErrOpenAIHealthyTurnStateBudgetExhausted.Error()), true)
			require.Equal(t, entry.payload, recorder.Body.String())
		})
	}
}

func TestTurnStateWebSocketFailoverRetainsLocalReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	message := service.ErrOpenAIHealthyTurnStateBudgetExhausted.Error()
	closeOpenAIWSFailoverExhausted(c, nil, localTurnStateFailover(message))
	streamErr, ok := service.GetOpsStreamError(c)
	require.True(t, ok)
	require.Equal(t, http.StatusServiceUnavailable, streamErr.IntendedStatus)
	require.Equal(t, "server_error", streamErr.ErrType)
	require.Equal(t, "turn_state_budget_exhausted", streamErr.Code)
	require.Equal(t, message, streamErr.Message)
}
