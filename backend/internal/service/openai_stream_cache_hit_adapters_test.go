package service

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type openAIAdapterFailingWriter struct {
	gin.ResponseWriter
}

func (w *openAIAdapterFailingWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *openAIAdapterFailingWriter) WriteString(string) (int, error) {
	return 0, errors.New("write failed")
}

func newOpenAIAdapterCacheHitContext(path string) (*gin.Context, *httptest.ResponseRecorder, *responsesCacheHitGatewayCache) {
	groupID := int64(1901)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Set("api_key", &APIKey{
		UserID:  2901,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
			UpdatedAt:                      time.Unix(1_700_000_000, 0),
		},
	})
	return c, recorder, &responsesCacheHitGatewayCache{}
}

func cacheHitAnthropicSSE(stopReason string, includeMessageStop bool) string {
	lines := []string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_cache_hit","type":"message","role":"assistant","content":[],"model":"test-model","usage":{"input_tokens":6,"cache_read_input_tokens":94}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"` + stopReason + `"},"usage":{"output_tokens":7}}`,
		"",
	}
	if includeMessageStop {
		lines = append(lines,
			"event: message_stop",
			`data: {"type":"message_stop"}`,
			"",
		)
	}
	return strings.Join(lines, "\n")
}

func cacheHitAnthropicTerminalOnlySSE() string {
	return strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_cache_hit_empty","type":"message","role":"assistant","content":[],"model":"test-model","usage":{"input_tokens":6,"cache_read_input_tokens":94}}}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
}

func cacheHitAnthropicAliasSSE() string {
	return strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_cache_alias","type":"message","role":"assistant","content":[],"model":"test-model","usage":{"input_tokens":0,"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":94}}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":7}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
}

func cacheHitAnthropicRepeatedTerminalSSE() string {
	return strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_cache_repeat","type":"message","role":"assistant","content":[],"model":"test-model","usage":{"input_tokens":6,"cache_read_input_tokens":94}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":12,"cache_read_input_tokens":188,"output_tokens":9}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")
}

func TestNativeAnthropicChatStreamForcesAdjustedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
	svc := &OpenAIGatewayService{cache: cache}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(cacheHitAnthropicSSE("end_turn", true))),
	}

	result, err := svc.handleCCStreamingFromNativeAnthropic(
		resp, c, "test-model", "test-model", "test-model", nil, time.Now(), false,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount())
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 90, result.Usage.CacheReadInputTokens)
	require.NotNil(t, result.CacheHitTargetAdjustment)
	require.Equal(t, 4, result.CacheHitTargetAdjustment.ShiftedTokens)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":100`)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":94`)
}

func TestAnthropicAdapterAliasUsageSyncsConverterState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("gateway chat completions", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicAliasSSE()))}

		result, err := (&GatewayService{cache: cache}).handleCCStreamingFromAnthropic(
			resp, c, "test-model", "test-model", nil, time.Now(), false,
		)

		require.NoError(t, err)
		require.Equal(t, 1, cache.callCount())
		require.Equal(t, 10, result.Usage.InputTokens)
		require.Equal(t, 90, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"prompt_tokens":100`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
	})

	t.Run("gateway responses", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicAliasSSE()))}

		result, err := (&GatewayService{cache: cache}).handleResponsesStreamingResponse(
			resp, c, "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.Equal(t, 1, cache.callCount())
		require.Equal(t, 10, result.Usage.InputTokens)
		require.Equal(t, 90, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"input_tokens":100`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
	})

	t.Run("native chat completions", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicAliasSSE()))}

		result, err := (&OpenAIGatewayService{cache: cache}).handleCCStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), false,
		)

		require.NoError(t, err)
		require.Equal(t, 1, cache.callCount())
		require.Equal(t, 100, result.Usage.InputTokens)
		require.Equal(t, 90, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"prompt_tokens":100`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
	})

	t.Run("native responses", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicAliasSSE()))}

		result, err := (&OpenAIGatewayService{cache: cache}).handleResponsesStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.Equal(t, 1, cache.callCount())
		require.Equal(t, 100, result.Usage.InputTokens)
		require.Equal(t, 90, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"input_tokens":100`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
	})
}

func TestAnthropicResponsesAdaptersFreezeUsageAfterFirstSuccessfulTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("通用网关", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(cacheHitAnthropicRepeatedTerminalSSE())),
		}

		result, err := (&GatewayService{cache: cache}).handleResponsesStreamingResponse(
			resp, c, "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 1, cache.callCount())
		require.Equal(t, ClaudeUsage{InputTokens: 10, OutputTokens: 2, CacheReadInputTokens: 90}, result.Usage)
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"response.completed"`))
		require.Contains(t, recorder.Body.String(), `"input_tokens":100`)
		require.Contains(t, recorder.Body.String(), `"output_tokens":2`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
		require.NotContains(t, recorder.Body.String(), `"input_tokens":200`)
		require.NotContains(t, recorder.Body.String(), `"output_tokens":9`)
		require.NotContains(t, recorder.Body.String(), `"cached_tokens":188`)
	})

	t.Run("原生 Anthropic", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(cacheHitAnthropicRepeatedTerminalSSE())),
		}

		result, err := (&OpenAIGatewayService{cache: cache}).handleResponsesStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 1, cache.callCount())
		require.Equal(t, OpenAIUsage{InputTokens: 100, OutputTokens: 2, CacheReadInputTokens: 90}, result.Usage)
		require.Equal(t, 1, strings.Count(recorder.Body.String(), `"type":"response.completed"`))
		require.Contains(t, recorder.Body.String(), `"input_tokens":100`)
		require.Contains(t, recorder.Body.String(), `"output_tokens":2`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
		require.NotContains(t, recorder.Body.String(), `"input_tokens":200`)
		require.NotContains(t, recorder.Body.String(), `"output_tokens":9`)
		require.NotContains(t, recorder.Body.String(), `"cached_tokens":188`)
	})
}

func TestAnthropicAdaptersTerminalOnlyStreamDoesNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("gateway chat completions", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicTerminalOnlySSE()))}

		result, err := (&GatewayService{cache: cache}).handleCCStreamingFromAnthropic(
			resp, c, "test-model", "test-model", nil, time.Now(), false,
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Zero(t, cache.callCount())
		require.Nil(t, result.CacheHitTargetAdjustment)
		require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
	})

	t.Run("gateway responses", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicTerminalOnlySSE()))}

		result, err := (&GatewayService{cache: cache}).handleResponsesStreamingResponse(
			resp, c, "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Zero(t, cache.callCount())
		require.Nil(t, result.CacheHitTargetAdjustment)
		require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
	})

	t.Run("native chat completions", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicTerminalOnlySSE()))}

		result, err := (&OpenAIGatewayService{cache: cache}).handleCCStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), false,
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Zero(t, cache.callCount())
		require.Nil(t, result.CacheHitTargetAdjustment)
		require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
	})

	t.Run("native responses", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(cacheHitAnthropicTerminalOnlySSE()))}

		result, err := (&OpenAIGatewayService{cache: cache}).handleResponsesStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Zero(t, cache.callCount())
		require.Nil(t, result.CacheHitTargetAdjustment)
		require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
	})
}

func TestNativeAnthropicResponsesStreamSkipsIncompleteAndDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("max tokens", func(t *testing.T) {
		c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		svc := &OpenAIGatewayService{cache: cache}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(cacheHitAnthropicSSE("max_tokens", true))),
		}

		result, err := svc.handleResponsesStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.Zero(t, cache.callCount())
		require.Nil(t, result.CacheHitTargetAdjustment)
		require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		require.Contains(t, recorder.Body.String(), `"type":"response.incomplete"`)
		require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
	})

	t.Run("client disconnected", func(t *testing.T) {
		c, _, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
		c.Writer = &failingGinWriter{ResponseWriter: c.Writer, failAfter: 0}
		svc := &OpenAIGatewayService{cache: cache}
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(cacheHitAnthropicSSE("end_turn", true))),
		}

		result, err := svc.handleResponsesStreamingFromNativeAnthropic(
			resp, c, "test-model", "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
		)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.True(t, result.ClientDisconnect)
		require.Zero(t, cache.callCount())
		require.Nil(t, result.CacheHitTargetAdjustment)
		require.Equal(t, 94, result.Usage.CacheReadInputTokens)
	})
}

func TestNativeAnthropicStreamsSkipNonSuccessfulTerminals(t *testing.T) {
	gin.SetMode(gin.TestMode)

	terminals := []struct {
		name               string
		stopReason         string
		includeMessageStop bool
	}{
		{name: "empty stop reason", includeMessageStop: true},
		{name: "refusal", stopReason: "refusal", includeMessageStop: true},
		{name: "unknown pause turn", stopReason: "pause_turn", includeMessageStop: true},
		{name: "abnormal eof synthetic terminal", stopReason: "end_turn"},
	}

	for _, terminal := range terminals {
		t.Run("chat completions "+terminal.name, func(t *testing.T) {
			c, _, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
			svc := &OpenAIGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(cacheHitAnthropicSSE(
					terminal.stopReason, terminal.includeMessageStop,
				))),
			}

			result, err := svc.handleCCStreamingFromNativeAnthropic(
				resp, c, "test-model", "test-model", "test-model", nil, time.Now(), false,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		})

		t.Run("responses "+terminal.name, func(t *testing.T) {
			c, _, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
			svc := &OpenAIGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body: io.NopCloser(strings.NewReader(cacheHitAnthropicSSE(
					terminal.stopReason, terminal.includeMessageStop,
				))),
			}

			result, err := svc.handleResponsesStreamingFromNativeAnthropic(
				resp, c, "test-model", "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		})
	}
}

func TestGatewayAnthropicStreamsSkipNonSuccessfulTerminals(t *testing.T) {
	gin.SetMode(gin.TestMode)

	terminals := []struct {
		name               string
		stopReason         string
		includeMessageStop bool
	}{
		{name: "empty stop reason", includeMessageStop: true},
		{name: "refusal", stopReason: "refusal", includeMessageStop: true},
		{name: "unknown pause turn", stopReason: "pause_turn", includeMessageStop: true},
		{name: "abnormal eof synthetic terminal", stopReason: "end_turn"},
	}

	for _, terminal := range terminals {
		t.Run("chat completions "+terminal.name, func(t *testing.T) {
			c, _, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
			svc := &GatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(cacheHitAnthropicSSE(
					terminal.stopReason, terminal.includeMessageStop,
				))),
			}

			result, err := svc.handleCCStreamingFromAnthropic(
				resp, c, "test-model", "test-model", nil, time.Now(), false,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		})

		t.Run("responses "+terminal.name, func(t *testing.T) {
			c, _, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
			svc := &GatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(cacheHitAnthropicSSE(
					terminal.stopReason, terminal.includeMessageStop,
				))),
			}

			result, err := svc.handleResponsesStreamingResponse(
				resp, c, "test-model", "test-model", nil, time.Now(), apicompat.ResponsesClientToolMapping{},
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		})
	}
}

func geminiCacheHitSSE(finishReason string) string {
	finish := ""
	if finishReason != "" {
		finish = `,"finishReason":"` + finishReason + `"`
	}
	return `data: {"candidates":[{"content":{"parts":[{"text":"ok"}]}` + finish + `}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":7,"cachedContentTokenCount":94}}` + "\n\n"
}

func geminiCacheHitTerminalOnlySSE() string {
	return `data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":7,"cachedContentTokenCount":94}}` + "\n\n"
}

func TestGeminiChatStreamAdjustsOnlyExplicitSuccessfulTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		finishReason  string
		wantCalls     int
		wantCacheRead int
		wantSnapshot  bool
	}{
		{name: "success", finishReason: "STOP", wantCalls: 1, wantCacheRead: 90, wantSnapshot: true},
		{name: "output limit", finishReason: "MAX_TOKENS", wantCacheRead: 94},
		{name: "safety terminal", finishReason: "SAFETY", wantCacheRead: 94},
		{name: "recitation terminal", finishReason: "RECITATION", wantCacheRead: 94},
		{name: "abnormal eof without finish reason", wantCacheRead: 94},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
			svc := &GeminiMessagesCompatService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(geminiCacheHitSSE(tt.finishReason))),
			}

			result, err := svc.handleChatCompletionsStreamingResponseFromGemini(
				c, resp, time.Now(), "test-model", false, false,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantCalls, cache.callCount())
			require.Equal(t, tt.wantCacheRead, result.usage.CacheReadInputTokens)
			adjustment := OpenAIStreamCacheHitAdjustmentFromContext(c)
			if tt.wantSnapshot {
				require.NotNil(t, adjustment)
				require.Equal(t, 4, adjustment.ShiftedTokens)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
			} else {
				require.Nil(t, adjustment)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
			}
		})
	}
}

func TestGeminiChatTerminalOnlyStreamDoesNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(geminiCacheHitTerminalOnlySSE())),
	}

	result, err := (&GeminiMessagesCompatService{cache: cache}).handleChatCompletionsStreamingResponseFromGemini(
		c, resp, time.Now(), "test-model", false, false,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, cache.callCount())
	require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
	require.Equal(t, 94, result.usage.CacheReadInputTokens)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
}

func TestGeminiChatStreamDisconnectDoesNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
	c.Writer = &openAIAdapterFailingWriter{ResponseWriter: c.Writer}
	svc := &GeminiMessagesCompatService{cache: cache}
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(geminiCacheHitSSE("STOP"))),
	}

	result, err := svc.handleChatCompletionsStreamingResponseFromGemini(
		c, resp, time.Now(), "test-model", false, false,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, cache.callCount())
	require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
}

func TestGeminiChatStreamImageModelsDoNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		requestedModel string
		mappedModel    string
	}{
		{name: "direct image model", requestedModel: "gemini-2.5-flash-image"},
		{name: "mapped image model", requestedModel: "text-alias", mappedModel: "gemini-2.5-flash-image"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
			upstream := &geminiCompatHTTPUpstreamStub{response: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(geminiCacheHitSSE("STOP"))),
			}}
			account := &Account{
				ID:          1902,
				Platform:    PlatformGemini,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "test-key"},
			}
			if tt.mappedModel != "" {
				account.Credentials["model_mapping"] = map[string]any{tt.requestedModel: tt.mappedModel}
			}
			svc := &GeminiMessagesCompatService{cache: cache, httpUpstream: upstream, cfg: &config.Config{}}
			body := []byte(`{"model":"` + tt.requestedModel + `","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"draw"}]}`)

			result, err := svc.ForwardAsChatCompletions(c.Request.Context(), c, account, body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
			require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
		})
	}
}

func antigravityCacheHitSSE(finishReason string, toolCall bool) string {
	part := `{"text":"ok"}`
	if toolCall {
		part = `{"functionCall":{"id":"call_cache_hit","name":"lookup","args":{"q":"ok"}}}`
	}
	finish := ""
	if finishReason != "" {
		finish = `,"finishReason":"` + finishReason + `"`
	}
	return `data: {"response":{"responseId":"resp_cache_hit","candidates":[{"content":{"parts":[` + part + `]}` + finish + `}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":7,"cachedContentTokenCount":94}}}` + "\n\n"
}

func antigravityCacheHitTerminalOnlySSE() string {
	return `data: {"response":{"responseId":"resp_cache_hit_terminal_only","candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":7,"cachedContentTokenCount":94}}}` + "\n\n"
}

func antigravityCacheHitConflictingPostTerminalSSE() string {
	return antigravityCacheHitSSE("STOP", false) +
		`data: {"response":{"responseId":"resp_cache_hit_late_usage","candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":200,"candidatesTokenCount":9,"cachedContentTokenCount":188}}}` + "\n\n"
}

func TestAntigravityCompatStreamAdjustsOnlyRealSuccessfulTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		finishReason  string
		toolCall      bool
		wantCalls     int
		wantCacheRead int
		wantSnapshot  bool
	}{
		{name: "success", finishReason: "STOP", wantCalls: 1, wantCacheRead: 90, wantSnapshot: true},
		{name: "max tokens with tool call", finishReason: "MAX_TOKENS", toolCall: true, wantCacheRead: 94},
		{name: "processor finish synthetic terminal", wantCacheRead: 94},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
			svc := &AntigravityGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(antigravityCacheHitSSE(tt.finishReason, tt.toolCall))),
			}

			result, err := svc.handleChatCompletionsStreamingFromAntigravity(
				c, resp, time.Now(), "test-model", false,
			)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantCalls, cache.callCount())
			require.Equal(t, tt.wantCacheRead, result.usage.CacheReadInputTokens)
			adjustment := OpenAIStreamCacheHitAdjustmentFromContext(c)
			if tt.wantSnapshot {
				require.NotNil(t, adjustment)
				require.Equal(t, 4, adjustment.ShiftedTokens)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
			} else {
				require.Nil(t, adjustment)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
			}
		})
	}
}

func TestAntigravityCompatResponsesStreamAdjustsOnlyRealSuccessfulTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		finishReason  string
		wantCalls     int
		wantCacheRead int
		wantSnapshot  bool
		terminalType  string
	}{
		{name: "success", finishReason: "STOP", wantCalls: 1, wantCacheRead: 90, wantSnapshot: true, terminalType: "response.completed"},
		{name: "max tokens", finishReason: "MAX_TOKENS", wantCacheRead: 94, terminalType: "response.incomplete"},
		{name: "processor finish synthetic terminal", wantCacheRead: 94, terminalType: "response.completed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
			svc := &AntigravityGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(antigravityCacheHitSSE(tt.finishReason, false))),
			}

			result, err := svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "test-model")

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantCalls, cache.callCount())
			require.Equal(t, tt.wantCacheRead, result.usage.CacheReadInputTokens)
			require.Contains(t, recorder.Body.String(), `"type":"`+tt.terminalType+`"`)
			adjustment := OpenAIStreamCacheHitAdjustmentFromContext(c)
			if tt.wantSnapshot {
				require.NotNil(t, adjustment)
				require.Equal(t, 4, adjustment.ShiftedTokens)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
			} else {
				require.Nil(t, adjustment)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
			}
		})
	}
}

func TestAntigravityCompatStreamsFreezeFirstSuccessfulTerminalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		call func(*AntigravityGatewayService, *gin.Context, *http.Response) (*antigravityStreamResult, error)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			call: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "test-model", true)
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			call: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "test-model")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext(tt.path)
			svc := &AntigravityGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(antigravityCacheHitConflictingPostTerminalSSE())),
			}

			result, err := tt.call(svc, c, resp)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, cache.callCount())
			require.Equal(t, 10, result.usage.InputTokens)
			require.Equal(t, 90, result.usage.CacheReadInputTokens)
			require.Equal(t, 100, result.usage.InputTokens+result.usage.CacheReadInputTokens)
			require.Equal(t, 7, result.usage.OutputTokens)
			require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
			require.NotContains(t, recorder.Body.String(), `"cached_tokens":188`)
			require.NotContains(t, recorder.Body.String(), `"output_tokens":9`)
		})
	}
}

func TestAntigravityCompatTerminalOnlyStreamDoesNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		call func(*AntigravityGatewayService, *gin.Context, *http.Response) (*antigravityStreamResult, error)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			call: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "test-model", false)
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			call: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "test-model")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext(tt.path)
			svc := &AntigravityGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(antigravityCacheHitTerminalOnlySSE())),
			}

			result, err := tt.call(svc, c, resp)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
			require.Equal(t, 94, result.usage.CacheReadInputTokens)
			require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
		})
	}
}

func TestAntigravityCompatStreamDisconnectDoesNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
		call func(*AntigravityGatewayService, *gin.Context, *http.Response) (*antigravityStreamResult, error)
	}{
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			call: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleChatCompletionsStreamingFromAntigravity(c, resp, time.Now(), "test-model", false)
			},
		},
		{
			name: "responses",
			path: "/v1/responses",
			call: func(svc *AntigravityGatewayService, c *gin.Context, resp *http.Response) (*antigravityStreamResult, error) {
				return svc.handleResponsesStreamingFromAntigravity(c, resp, time.Now(), "test-model")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _, cache := newOpenAIAdapterCacheHitContext(tt.path)
			c.Writer = &antigravityFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
			svc := &AntigravityGatewayService{cache: cache}
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(antigravityCacheHitSSE("STOP", false))),
			}

			result, err := tt.call(svc, c, resp)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, result.clientDisconnect)
			require.Zero(t, cache.callCount())
			require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
		})
	}
}

func TestAntigravityCompatImageModelsDoNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		requestedModel string
		mappedModel    string
	}{
		{name: "direct image model", requestedModel: "gemini-2.5-flash-image", mappedModel: "gemini-2.5-flash-image"},
		{name: "mapped image model", requestedModel: "text-alias", mappedModel: "gemini-2.5-flash-image"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
			upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(antigravityCacheHitSSE("STOP", false))),
			}}}
			svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
			svc.cache = cache
			account := newAntigravityCompatAccount(AccountTypeOAuth)
			account.Credentials["model_mapping"] = map[string]any{tt.requestedModel: tt.mappedModel}
			body := []byte(`{"model":"` + tt.requestedModel + `","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"draw"}]}`)

			result, err := svc.ForwardAsChatCompletions(c.Request.Context(), c, account, body, nil)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
			require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
		})
	}
}
