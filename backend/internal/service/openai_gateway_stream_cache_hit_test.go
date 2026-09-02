package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type responsesCacheHitGatewayCache struct {
	mu      sync.Mutex
	calls   int
	err     error
	tracker memoryCacheHitTargetTracker
}

func (c *responsesCacheHitGatewayCache) AdjustCacheHitToTarget(
	ctx context.Context,
	operationID string,
	userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion, promptTokens, cacheReadTokens int64,
) (CacheHitTargetAdjustment, error) {
	c.mu.Lock()
	c.calls++
	err := c.err
	c.mu.Unlock()
	if err != nil {
		return CacheHitTargetAdjustment{}, err
	}
	return c.tracker.AdjustCacheHitToTarget(ctx, operationID, userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion, promptTokens, cacheReadTokens)
}

func (c *responsesCacheHitGatewayCache) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *responsesCacheHitGatewayCache) GetSessionAccountID(context.Context, int64, string) (int64, error) {
	return 0, ErrStickySessionNotFound
}

func (c *responsesCacheHitGatewayCache) SetSessionAccountID(context.Context, int64, string, int64, time.Duration) error {
	return nil
}

func (c *responsesCacheHitGatewayCache) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (c *responsesCacheHitGatewayCache) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

func (c *responsesCacheHitGatewayCache) SetGrokVideoPendingBilling(context.Context, string, []byte, time.Duration) error {
	return nil
}

func (c *responsesCacheHitGatewayCache) GetGrokVideoPendingBilling(context.Context, string) ([]byte, error) {
	return nil, nil
}

func (c *responsesCacheHitGatewayCache) ClaimGrokVideoBilled(context.Context, string, time.Duration) (bool, error) {
	return true, nil
}

func (c *responsesCacheHitGatewayCache) ReleaseGrokVideoBilled(context.Context, string) error {
	return nil
}

func (c *responsesCacheHitGatewayCache) SetReasoningContent(context.Context, string, string, time.Duration) error {
	return nil
}

func (c *responsesCacheHitGatewayCache) GetReasoningContent(context.Context, string) (string, error) {
	return "", ErrReasoningContentNotFound
}

func TestRewriteOpenAIStreamUsagePayload_ReclassifiesResponsesCacheDetailOnly(t *testing.T) {
	t.Parallel()

	body := []byte(`{"type":"response.completed","response":{"id":"resp_1","usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`)
	rewritten, err := rewriteOpenAIStreamUsagePayload(body, OpenAIUsage{
		InputTokens:              100,
		OutputTokens:             7,
		CacheCreationInputTokens: 2,
		CacheReadInputTokens:     90,
	})

	require.NoError(t, err)
	require.Equal(t, int64(100), gjson.GetBytes(rewritten, "response.usage.input_tokens").Int())
	require.Equal(t, int64(7), gjson.GetBytes(rewritten, "response.usage.output_tokens").Int())
	require.Equal(t, int64(107), gjson.GetBytes(rewritten, "response.usage.total_tokens").Int())
	require.Equal(t, int64(90), gjson.GetBytes(rewritten, "response.usage.input_tokens_details.cached_tokens").Int())
	require.Equal(t, int64(2), gjson.GetBytes(rewritten, "response.usage.input_tokens_details.cache_write_tokens").Int())
}

func TestRewriteOpenAIStreamUsagePayload_PreservesCompatibleUsageShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		body           string
		cacheReadPath  string
		cacheWritePath string
	}{
		{
			name:           "response done prompt token details",
			body:           `{"type":"response.done","response":{"usage":{"prompt_tokens":100,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":94}}}}`,
			cacheReadPath:  "response.usage.prompt_tokens_details.cached_tokens",
			cacheWritePath: "response.usage.prompt_tokens_details.cache_write_tokens",
		},
		{
			name:           "root usage legacy cache aliases",
			body:           `{"type":"response.completed","usage":{"input_tokens":100,"output_tokens":3,"cache_read_input_tokens":94,"cache_creation_input_tokens":9}}`,
			cacheReadPath:  "usage.cache_read_input_tokens",
			cacheWritePath: "usage.cache_creation_input_tokens",
		},
		{
			name:          "wrapped response usage",
			body:          `{"data":{"response":{"usage":{"input_tokens":100,"output_tokens":3,"cached_tokens":94}}}}`,
			cacheReadPath: "data.response.usage.cached_tokens",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rewritten, err := rewriteOpenAIStreamUsagePayload([]byte(tt.body), OpenAIUsage{
				InputTokens:              100,
				OutputTokens:             3,
				CacheCreationInputTokens: 2,
				CacheReadInputTokens:     90,
			})

			require.NoError(t, err)
			require.Equal(t, int64(90), gjson.GetBytes(rewritten, tt.cacheReadPath).Int())
			if tt.cacheWritePath != "" {
				require.Equal(t, int64(2), gjson.GetBytes(rewritten, tt.cacheWritePath).Int())
			}
			originalUsage, ok := extractOpenAIUsageFromJSONBytes([]byte(tt.body))
			require.True(t, ok)
			adjustedUsage, ok := extractOpenAIUsageFromJSONBytes(rewritten)
			require.True(t, ok)
			require.Equal(t, originalUsage.InputTokens, adjustedUsage.InputTokens)
			require.Equal(t, originalUsage.OutputTokens, adjustedUsage.OutputTokens)
			require.Equal(t, 90, adjustedUsage.CacheReadInputTokens)
		})
	}
}

func TestRewriteOpenAIStreamUsagePayload_UpdatesDuplicateCacheReadAliases(t *testing.T) {
	t.Parallel()

	body := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":1,"input_tokens_details":{"cached_tokens":94},"cache_read_input_tokens":94}}}`)
	rewritten, err := rewriteOpenAIStreamUsagePayload(body, OpenAIUsage{CacheReadInputTokens: 90})

	require.NoError(t, err)
	require.Equal(t, int64(90), gjson.GetBytes(rewritten, "response.usage.input_tokens_details.cached_tokens").Int())
	require.Equal(t, int64(90), gjson.GetBytes(rewritten, "response.usage.cache_read_input_tokens").Int())
}

func TestRewriteOpenAIStreamUsagePayload_UpdatesDuplicateUsageObjects(t *testing.T) {
	t.Parallel()

	body := []byte(`{"type":"response.completed","usage":{"input_tokens":100,"output_tokens":1,"input_tokens_details":{"cached_tokens":94}},"response":{"usage":{"input_tokens":100,"output_tokens":1,"prompt_tokens_details":{"cached_tokens":94}}}}`)
	rewritten, err := rewriteOpenAIStreamUsagePayload(body, OpenAIUsage{CacheReadInputTokens: 90})

	require.NoError(t, err)
	require.Equal(t, int64(90), gjson.GetBytes(rewritten, "usage.input_tokens_details.cached_tokens").Int())
	require.Equal(t, int64(90), gjson.GetBytes(rewritten, "response.usage.prompt_tokens_details.cached_tokens").Int())
}

func TestRewriteOpenAIStreamUsagePayload_BackfillsCompleteUsageWithoutCacheRead(t *testing.T) {
	t.Parallel()

	body := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":10}}}`)
	rewritten, err := rewriteOpenAIStreamUsagePayload(
		body,
		OpenAIUsage{InputTokens: 10, OutputTokens: 1, CacheCreationInputTokens: 2},
	)

	require.NoError(t, err)
	require.Equal(t, int64(10), gjson.GetBytes(rewritten, "response.usage.input_tokens").Int())
	require.Equal(t, int64(1), gjson.GetBytes(rewritten, "response.usage.output_tokens").Int())
	require.Equal(t, int64(11), gjson.GetBytes(rewritten, "response.usage.total_tokens").Int())
	require.Equal(t, int64(2), gjson.GetBytes(rewritten, "response.usage.input_tokens_details.cache_write_tokens").Int())
}

func TestRewriteOpenAIStreamUsagePayload_RejectsPositiveCacheWithoutDetail(t *testing.T) {
	t.Parallel()

	_, err := rewriteOpenAIStreamUsagePayload(
		[]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":10,"output_tokens":1}}}`),
		OpenAIUsage{InputTokens: 10, OutputTokens: 1, CacheReadInputTokens: 1},
	)

	require.ErrorContains(t, err, "no cache-read field")
}

func TestAdjustOpenAIStreamUsage_FreezesFirstAdjustedOpenAIUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
	svc := &OpenAIGatewayService{cache: cache}
	first := OpenAIUsage{
		InputTokens:          100,
		ImageInputTokens:     3,
		OutputTokens:         7,
		CacheReadInputTokens: 94,
		ImageOutputTokens:    5,
	}

	firstAdjustment, err := svc.AdjustOpenAIStreamUsage(c.Request.Context(), c, &first)

	require.NoError(t, err)
	require.NotNil(t, firstAdjustment)
	require.Equal(t, 4, firstAdjustment.ShiftedTokens)
	want := OpenAIUsage{
		InputTokens:          100,
		ImageInputTokens:     3,
		OutputTokens:         7,
		CacheReadInputTokens: 90,
		ImageOutputTokens:    5,
	}
	require.Equal(t, want, first)

	second := OpenAIUsage{
		InputTokens:          200,
		ImageInputTokens:     11,
		OutputTokens:         9,
		CacheReadInputTokens: 188,
		ImageOutputTokens:    13,
	}
	secondAdjustment, err := svc.AdjustOpenAIStreamUsage(c.Request.Context(), c, &second)

	require.NoError(t, err)
	require.Equal(t, firstAdjustment, secondAdjustment)
	require.Equal(t, want, second)
	require.Equal(t, 1, cache.callCount())
	snapshot, ok := openAIStreamCacheHitOpenAIUsageFromContext(c)
	require.True(t, ok)
	require.Equal(t, want, snapshot)
}

func TestGatewayAdjustOpenAIStreamUsage_FreezesFirstAdjustedClaudeUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
	svc := &GatewayService{cache: cache}
	first := ClaudeUsage{
		InputTokens:           6,
		OutputTokens:          7,
		CacheReadInputTokens:  94,
		CacheCreation5mTokens: 3,
		CacheCreation1hTokens: 5,
		ImageOutputTokens:     2,
	}

	firstAdjustment, err := svc.AdjustOpenAIStreamUsage(c.Request.Context(), c, &first)

	require.NoError(t, err)
	require.NotNil(t, firstAdjustment)
	require.Equal(t, 4, firstAdjustment.ShiftedTokens)
	want := ClaudeUsage{
		InputTokens:           10,
		OutputTokens:          7,
		CacheReadInputTokens:  90,
		CacheCreation5mTokens: 3,
		CacheCreation1hTokens: 5,
		ImageOutputTokens:     2,
	}
	require.Equal(t, want, first)

	second := ClaudeUsage{
		InputTokens:           12,
		OutputTokens:          9,
		CacheReadInputTokens:  188,
		CacheCreation5mTokens: 17,
		CacheCreation1hTokens: 19,
		ImageOutputTokens:     23,
	}
	secondAdjustment, err := svc.AdjustOpenAIStreamUsage(c.Request.Context(), c, &second)

	require.NoError(t, err)
	require.Equal(t, firstAdjustment, secondAdjustment)
	require.Equal(t, want, second)
	require.Equal(t, 1, cache.callCount())
	snapshot, ok := openAIStreamCacheHitClaudeUsageFromContext(c)
	require.True(t, ok)
	require.Equal(t, want, snapshot)
}

func TestAdjustOpenAIStreamUsage_TrackerErrorDoesNotPublishUsageSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _, cache := newOpenAIAdapterCacheHitContext("/v1/responses")
	cache.err = errors.New("tracker unavailable")
	svc := &OpenAIGatewayService{cache: cache}
	first := OpenAIUsage{InputTokens: 100, OutputTokens: 7, CacheReadInputTokens: 94}

	adjustment, err := svc.AdjustOpenAIStreamUsage(c.Request.Context(), c, &first)

	require.ErrorContains(t, err, "tracker unavailable")
	require.Nil(t, adjustment)
	require.Equal(t, OpenAIUsage{InputTokens: 100, OutputTokens: 7, CacheReadInputTokens: 94}, first)
	_, ok := openAIStreamCacheHitOpenAIUsageFromContext(c)
	require.False(t, ok)

	second := OpenAIUsage{InputTokens: 200, OutputTokens: 9, CacheReadInputTokens: 188}
	adjustment, err = svc.AdjustOpenAIStreamUsage(c.Request.Context(), c, &second)

	require.ErrorContains(t, err, "tracker unavailable")
	require.Nil(t, adjustment)
	require.Equal(t, OpenAIUsage{InputTokens: 200, OutputTokens: 9, CacheReadInputTokens: 188}, second)
	require.Equal(t, 1, cache.callCount())
}

func TestHandleStreamingResponse_AlignsTerminalUsageWithSingleAdjustment(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(91)
	apiKey := &APIKey{
		ID:      101,
		UserID:  201,
		GroupID: &groupID,
		User:    &User{ID: 201},
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
			UpdatedAt:                      time.Unix(1_700_000_000, 0),
		},
	}
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", apiKey)

	completed := `{"type":"response.completed","response":{"id":"resp_cache_hit","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`
	done := strings.Replace(completed, `"response.completed"`, `"response.done"`, 1)
	stream := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`event: response.completed`,
		`data: ` + completed,
		``,
		`event: response.done`,
		`data: ` + done,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 301, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount(), "completed 与 done 必须复用同一个请求级快照")
	require.Equal(t, 100, result.usage.InputTokens)
	require.Equal(t, 90, result.usage.CacheReadInputTokens)
	require.NotNil(t, result.cacheHitAdjustment)
	require.Equal(t, 4, result.cacheHitAdjustment.ShiftedTokens)
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"cached_tokens":90`))
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":94`)
}

func TestResponsesStreamingHandlersFreezeCanonicalAdjustedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handlers := []struct {
		name string
		run  func(*OpenAIGatewayService, *gin.Context, *http.Response, *Account) (OpenAIUsage, *CacheHitTargetAdjustment, error)
	}{
		{
			name: "native",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response, account *Account) (OpenAIUsage, *CacheHitTargetAdjustment, error) {
				result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, account, time.Now(), "gpt-test", "gpt-test")
				if result == nil {
					return OpenAIUsage{}, nil, err
				}
				return *result.usage, result.cacheHitAdjustment, err
			},
		},
		{
			name: "passthrough",
			run: func(svc *OpenAIGatewayService, c *gin.Context, resp *http.Response, account *Account) (OpenAIUsage, *CacheHitTargetAdjustment, error) {
				result, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, account, time.Now(), "gpt-test", "gpt-test")
				if result == nil {
					return OpenAIUsage{}, nil, err
				}
				return *result.usage, result.cacheHitAdjustment, err
			},
		},
	}

	scenarios := []struct {
		name              string
		stream            string
		wantCacheCount    int
		wantTerminalCount int
		forbidden         []string
	}{
		{
			name: "progressive usage with partial terminal",
			stream: strings.Join([]string{
				`event: response.output_text.delta`,
				`data: {"type":"response.output_text.delta","delta":"ok","usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94,"cache_write_tokens":2}}}`,
				``,
				`event: response.completed`,
				`data: {"type":"response.completed","response":{"id":"resp_partial_terminal","status":"completed","usage":{"input_tokens":100,"input_tokens_details":{"cached_tokens":94}}}}`,
				``,
			}, "\n"),
			wantCacheCount:    1,
			wantTerminalCount: 1,
		},
		{
			name: "conflicting duplicate terminal",
			stream: strings.Join([]string{
				`event: response.output_text.delta`,
				`data: {"type":"response.output_text.delta","delta":"ok"}`,
				``,
				`event: response.completed`,
				`data: {"type":"response.completed","response":{"id":"resp_conflicting_terminal","status":"completed","usage":{"attribution":{"request_fields":{"instructions":{"input_tokens":100,"cached_tokens":94}}},"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`,
				``,
				`event: response.done`,
				`data: {"type":"response.done","response":{"id":"resp_conflicting_terminal","status":"completed","usage":{"attribution":{"request_fields":{"instructions":{"input_tokens":200,"cached_tokens":188}}},"input_tokens":200,"output_tokens":9,"total_tokens":209,"input_tokens_details":{"cached_tokens":188}}}}`,
				``,
			}, "\n"),
			wantCacheCount:    2,
			wantTerminalCount: 2,
			forbidden:         []string{`"input_tokens":200`, `"output_tokens":9`, `"total_tokens":209`, `"cached_tokens":188`, `"attribution"`},
		},
		{
			name: "unsuccessful terminal after completed",
			stream: strings.Join([]string{
				`event: response.output_text.delta`,
				`data: {"type":"response.output_text.delta","delta":"ok"}`,
				``,
				`event: response.completed`,
				`data: {"type":"response.completed","response":{"id":"resp_mixed_terminal","status":"completed","usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`,
				``,
				`event: response.done`,
				`data: {"type":"response.done","response":{"id":"resp_mixed_terminal","status":"incomplete","usage":{"input_tokens":200,"output_tokens":9,"total_tokens":209,"input_tokens_details":{"cached_tokens":188}}}}`,
				``,
			}, "\n"),
			wantCacheCount:    2,
			wantTerminalCount: 2,
			forbidden:         []string{`"input_tokens":200`, `"output_tokens":9`, `"total_tokens":209`, `"cached_tokens":188`},
		},
	}

	for handlerIndex, handler := range handlers {
		for scenarioIndex, scenario := range scenarios {
			t.Run(handler.name+"/"+scenario.name, func(t *testing.T) {
				groupID := int64(9100 + handlerIndex*10 + scenarioIndex)
				cache := &responsesCacheHitGatewayCache{}
				svc := &OpenAIGatewayService{cache: cache}
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				c.Set("api_key", &APIKey{
					UserID:  9200 + int64(handlerIndex*10+scenarioIndex),
					GroupID: &groupID,
					Group: &Group{
						ID:                             groupID,
						CacheHitQuarterToInput:         true,
						CacheHitTargetPercent:          90,
						CacheHitTargetTolerancePercent: 0.5,
					},
				})
				resp := &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(scenario.stream)),
				}
				account := &Account{ID: 9300 + int64(handlerIndex), Platform: PlatformOpenAI, Type: AccountTypeOAuth}

				usage, adjustment, err := handler.run(svc, c, resp, account)

				require.NoError(t, err)
				require.Equal(t, 1, cache.callCount())
				require.NotNil(t, adjustment)
				require.Equal(t, 4, adjustment.ShiftedTokens)
				require.Equal(t, 100, usage.InputTokens)
				require.Equal(t, 7, usage.OutputTokens)
				if scenario.name == "progressive usage with partial terminal" {
					require.Equal(t, 2, usage.CacheCreationInputTokens)
				}
				require.Equal(t, 90, usage.CacheReadInputTokens)
				require.Equal(t, scenario.wantCacheCount, strings.Count(recorder.Body.String(), `"cached_tokens":90`))
				terminalCount := 0
				for _, line := range strings.Split(recorder.Body.String(), "\n") {
					payload := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
					if !gjson.Valid(payload) || !openAIStreamEventTypeIsTerminal(gjson.Get(payload, "type").String()) {
						continue
					}
					usageObject := extractOpenAIStreamUsageObject([]byte(payload))
					require.NotEmpty(t, usageObject)
					parsedUsage := gjson.ParseBytes(usageObject)
					require.Equal(t, int64(100), parsedUsage.Get("input_tokens").Int())
					require.Equal(t, int64(7), parsedUsage.Get("output_tokens").Int())
					require.Equal(t, int64(107), parsedUsage.Get("total_tokens").Int())
					require.Equal(t, int64(90), parsedUsage.Get("input_tokens_details.cached_tokens").Int())
					if scenario.name == "progressive usage with partial terminal" {
						require.Equal(t, int64(2), parsedUsage.Get("input_tokens_details.cache_write_tokens").Int())
					}
					terminalCount++
				}
				require.Equal(t, scenario.wantTerminalCount, terminalCount)
				for _, forbidden := range scenario.forbidden {
					require.NotContains(t, recorder.Body.String(), forbidden)
				}
			})
		}
	}
}

func TestHandleStreamingResponse_TrackerErrorKeepsOriginalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(102)
	cache := &responsesCacheHitGatewayCache{err: errors.New("tracker unavailable")}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{
		UserID:  212,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	stream := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`event: response.completed`,
		`data: {"type":"response.completed","response":{"id":"resp_tracker_error","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 307, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount())
	require.Equal(t, 94, result.usage.CacheReadInputTokens)
	require.Nil(t, result.cacheHitAdjustment)
	require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
	require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":90`)
}

func TestHandleStreamingResponse_FailedTerminalDoesNotAdjustUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	groupID := int64(92)
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{
		UserID:  202,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	stream := "event: response.failed\n" +
		`data: {"type":"response.failed","response":{"id":"resp_failed","usage":{"input_tokens":100,"output_tokens":0,"input_tokens_details":{"cached_tokens":94}},"error":{"message":"failed"}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	_, err := svc.handleStreamingResponse(c.Request.Context(), resp, c, &Account{ID: 302, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.Error(t, err)
	require.Zero(t, cache.callCount())
	require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
}

func TestHandleChatStreamingResponse_AlignsCompletedUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(99)
	cache := &responsesCacheHitGatewayCache{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("api_key", &APIKey{
		UserID:  209,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_chat_cache","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := (&OpenAIGatewayService{cache: cache}).handleChatStreamingResponse(
		resp, c, &Account{ID: 307, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", time.Now(), 0,
	)

	require.NoError(t, err)
	require.Equal(t, 1, cache.callCount())
	require.NotNil(t, result.CacheHitTargetAdjustment)
	require.Equal(t, 4, result.CacheHitTargetAdjustment.ShiftedTokens)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 90, result.Usage.CacheReadInputTokens)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":100`)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
}

func TestHandleChatStreamingResponse_BackfillsProgressiveUsageIntoEmptyTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(991)
	cache := &responsesCacheHitGatewayCache{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("api_key", &APIKey{
		UserID:  2091,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok","usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_chat_progressive","status":"completed","usage":{}}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := (&OpenAIGatewayService{cache: cache}).handleChatStreamingResponse(
		resp, c, &Account{ID: 3071, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", time.Now(), 0,
	)

	require.NoError(t, err)
	require.Equal(t, 1, cache.callCount())
	require.NotNil(t, result.CacheHitTargetAdjustment)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 90, result.Usage.CacheReadInputTokens)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":100`)
	require.Contains(t, recorder.Body.String(), `"completion_tokens":7`)
	require.Contains(t, recorder.Body.String(), `"total_tokens":107`)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
}

func TestHandleChatStreamingResponse_BackfillsProgressiveUsageIntoPartialTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(992)
	cache := &responsesCacheHitGatewayCache{}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	c.Set("api_key", &APIKey{
		UserID:  2092,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	stream := strings.Join([]string{
		`data: {"type":"response.output_text.delta","delta":"ok","usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}`,
		``,
		`data: {"type":"response.completed","response":{"id":"resp_chat_progressive_partial","status":"completed","usage":{"input_tokens":100}}}`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := (&OpenAIGatewayService{cache: cache}).handleChatStreamingResponse(
		resp, c, &Account{ID: 3072, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", time.Now(), 0,
	)

	require.NoError(t, err)
	require.Equal(t, 1, cache.callCount())
	require.NotNil(t, result.CacheHitTargetAdjustment)
	require.Equal(t, 100, result.Usage.InputTokens)
	require.Equal(t, 7, result.Usage.OutputTokens)
	require.Equal(t, 90, result.Usage.CacheReadInputTokens)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens":100`)
	require.Contains(t, recorder.Body.String(), `"completion_tokens":7`)
	require.Contains(t, recorder.Body.String(), `"total_tokens":107`)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
}

func TestHandleChatStreamingResponse_IncompleteAndDisconnectDoNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		eventType  string
		disconnect bool
	}{
		{name: "incomplete", eventType: "response.incomplete"},
		{name: "client disconnected", eventType: "response.completed", disconnect: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(100)
			cache := &responsesCacheHitGatewayCache{}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			if tc.disconnect {
				c.Writer = &openAIChatFailingWriter{ResponseWriter: c.Writer, failAfter: 0}
			}
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("api_key", &APIKey{
				UserID:  210,
				GroupID: &groupID,
				Group: &Group{
					ID:                             groupID,
					CacheHitQuarterToInput:         true,
					CacheHitTargetPercent:          90,
					CacheHitTargetTolerancePercent: 0.5,
				},
			})
			status := "completed"
			if tc.eventType == "response.incomplete" {
				status = "incomplete"
			}
			stream := strings.Join([]string{
				`data: {"type":"response.output_text.delta","delta":"ok"}`,
				``,
				`data: {"type":"` + tc.eventType + `","response":{"id":"resp_chat_skip","status":"` + status + `","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`,
				``,
			}, "\n")
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(stream)),
			}

			result, _ := (&OpenAIGatewayService{cache: cache}).handleChatStreamingResponse(
				resp, c, &Account{ID: 308, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", time.Now(), 0,
			)

			require.NotNil(t, result)
			require.Zero(t, cache.callCount())
			require.Nil(t, result.CacheHitTargetAdjustment)
			require.Equal(t, 94, result.Usage.CacheReadInputTokens)
		})
	}
}

func TestStreamRawChatCompletions_AdjustsOnlySuccessfulFinishReason(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		lines      []string
		wantAdjust bool
	}{
		{
			name: "stop and usage in separate chunks",
			lines: []string{
				`data: {"id":"chatcmpl_stop","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
				``,
				`data: {"id":"chatcmpl_stop","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
			wantAdjust: true,
		},
		{
			name: "tool calls and usage in same chunk",
			lines: []string{
				`data: {"id":"chatcmpl_tools","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},"finish_reason":null}]}`,
				``,
				`data: {"id":"chatcmpl_tools","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
			wantAdjust: true,
		},
		{
			name: "first terminal usage protects first output",
			lines: []string{
				`data: {"id":"chatcmpl_first_terminal","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
		},
		{
			name: "length and usage in separate chunks",
			lines: []string{
				`data: {"id":"chatcmpl_length","choices":[{"index":0,"delta":{},"finish_reason":"length"}]}`,
				``,
				`data: {"id":"chatcmpl_length","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
		},
		{
			name: "length and usage in same chunk",
			lines: []string{
				`data: {"id":"chatcmpl_length","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
		},
		{
			name: "content filter does not adjust",
			lines: []string{
				`data: {"id":"chatcmpl_filter","choices":[{"index":0,"delta":{},"finish_reason":"content_filter"}]}`,
				``,
				`data: {"id":"chatcmpl_filter","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":0,"total_tokens":100,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
		},
		{
			name: "unknown finish reason does not adjust",
			lines: []string{
				`data: {"id":"chatcmpl_unknown","choices":[{"index":0,"delta":{},"finish_reason":"provider_limit"}],"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
		},
		{
			name: "usage without finish reason does not adjust",
			lines: []string{
				`data: {"id":"chatcmpl_no_finish","choices":[],"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101,"prompt_tokens_details":{"cached_tokens":94}}}`,
				``,
				`data: [DONE]`,
				``,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupID := int64(102)
			cache := &responsesCacheHitGatewayCache{}
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("api_key", &APIKey{
				UserID:  212,
				GroupID: &groupID,
				Group: &Group{
					ID:                             groupID,
					CacheHitQuarterToInput:         true,
					CacheHitTargetPercent:          90,
					CacheHitTargetTolerancePercent: 0.5,
				},
			})
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(strings.Join(tc.lines, "\n"))),
			}

			result, err := (&OpenAIGatewayService{cache: cache}).streamRawChatCompletions(
				c, resp, &Account{ID: 309, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", nil, nil, time.Now(), 0,
			)

			require.NoError(t, err)
			require.Equal(t, 100, result.Usage.InputTokens)
			if tc.wantAdjust {
				require.Equal(t, 1, cache.callCount())
				require.NotNil(t, result.CacheHitTargetAdjustment)
				require.Equal(t, 4, result.CacheHitTargetAdjustment.ShiftedTokens)
				require.Equal(t, 90, result.Usage.CacheReadInputTokens)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
				require.NotContains(t, recorder.Body.String(), `"cached_tokens":94`)
			} else {
				require.Zero(t, cache.callCount())
				require.Nil(t, result.CacheHitTargetAdjustment)
				require.Equal(t, 94, result.Usage.CacheReadInputTokens)
				require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
			}
		})
	}
}

func TestStreamRawChatCompletions_FreezesConflictingSuccessfulTerminalUsage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_duplicate","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl_duplicate","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":94}}}`,
		``,
		`data: {"id":"chatcmpl_duplicate","choices":[],"usage":{"prompt_tokens":200,"completion_tokens":9,"total_tokens":209,"prompt_tokens_details":{"cached_tokens":188}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := (&OpenAIGatewayService{cache: cache}).streamRawChatCompletions(
		c, resp, &Account{ID: 310, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", nil, nil, time.Now(), 0,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount())
	require.Equal(t, OpenAIUsage{InputTokens: 100, OutputTokens: 7, CacheReadInputTokens: 90}, result.Usage)
	require.NotNil(t, result.CacheHitTargetAdjustment)
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"prompt_tokens":100`))
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"completion_tokens":7`))
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"cached_tokens":90`))
	require.NotContains(t, recorder.Body.String(), `"prompt_tokens":200`)
	require.NotContains(t, recorder.Body.String(), `"completion_tokens":9`)
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":188`)
}

func TestStreamRawChatCompletions_FreezesUsageAfterLaterUnsuccessfulTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, recorder, cache := newOpenAIAdapterCacheHitContext("/v1/chat/completions")
	stream := strings.Join([]string{
		`data: {"id":"chatcmpl_late_failure","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":null}]}`,
		``,
		`data: {"id":"chatcmpl_late_failure","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":7,"total_tokens":107,"prompt_tokens_details":{"cached_tokens":94}}}`,
		``,
		`data: {"id":"chatcmpl_late_failure","choices":[{"index":0,"delta":{},"finish_reason":"length"}],"usage":{"prompt_tokens":200,"completion_tokens":9,"total_tokens":209,"prompt_tokens_details":{"cached_tokens":188}}}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := (&OpenAIGatewayService{cache: cache}).streamRawChatCompletions(
		c, resp, &Account{ID: 311, Platform: PlatformOpenAI}, "gpt-test", "gpt-test", "gpt-test", nil, nil, time.Now(), 0,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount())
	require.Equal(t, OpenAIUsage{InputTokens: 100, OutputTokens: 7, CacheReadInputTokens: 90}, result.Usage)
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"prompt_tokens":100`))
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"completion_tokens":7`))
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"cached_tokens":90`))
	require.NotContains(t, recorder.Body.String(), `"prompt_tokens":200`)
	require.NotContains(t, recorder.Body.String(), `"completion_tokens":9`)
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":188`)
}

func TestOpenAIStreamCacheHitSuccessfulTerminalClassification(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		eventType string
		body      string
		want      bool
	}{
		{name: "completed", eventType: "response.completed", body: `{"type":"response.completed","response":{"status":"completed"}}`, want: true},
		{name: "done without status", eventType: "response.done", body: `{"type":"response.done","response":{}}`, want: true},
		{name: "incomplete status", eventType: "response.completed", body: `{"type":"response.completed","response":{"status":"incomplete"}}`},
		{name: "failed status", eventType: "response.done", body: `{"type":"response.done","response":{"status":"failed"}}`},
		{name: "non-null error", eventType: "response.completed", body: `{"type":"response.completed","response":{"status":"completed","error":{"message":"blocked"}}}`},
		{name: "null error", eventType: "response.completed", body: `{"type":"response.completed","response":{"status":"completed","error":null}}`, want: true},
		{name: "wrong event", eventType: "response.incomplete", body: `{"type":"response.incomplete","response":{"status":"completed"}}`},
		{name: "invalid payload", eventType: "response.completed", body: `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isSuccessfulOpenAIResponsesTerminalPayload(tc.eventType, []byte(tc.body)))
		})
	}

	for _, reason := range []string{"stop", "tool_calls", "function_call", " STOP "} {
		require.True(t, isSuccessfulOpenAIChatFinishReason(reason), reason)
	}
	for _, reason := range []string{"", "length", "content_filter", "provider_limit"} {
		require.False(t, isSuccessfulOpenAIChatFinishReason(reason), reason)
	}
}

func TestHandleStreamingResponsePassthrough_AlignsTerminalUsageAndBillingSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(93)
	apiKey := &APIKey{
		UserID:  203,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
			UpdatedAt:                      time.Unix(1_700_000_000, 0),
		},
	}
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", apiKey)
	completed := `{"type":"response.completed","response":{"id":"resp_pass_cache_hit","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107,"input_tokens_details":{"cached_tokens":94}}}}`
	stream := strings.Join([]string{
		`event: response.output_text.delta`,
		`data: {"type":"response.output_text.delta","delta":"ok"}`,
		``,
		`event: response.completed`,
		`data: ` + completed,
		``,
	}, "\n")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 303, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount())
	require.Equal(t, 100, result.usage.InputTokens)
	require.Equal(t, 90, result.usage.CacheReadInputTokens)
	require.NotNil(t, result.cacheHitAdjustment)
	require.Equal(t, 4, result.cacheHitAdjustment.ShiftedTokens)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":90`)
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":94`)
}

func TestHandleStreamingResponsePassthrough_FirstTerminalSkipsAdjustment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(931)
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{
		UserID:  2031,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	stream := "event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_first_terminal","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":100,"output_tokens":1,"total_tokens":101,"input_tokens_details":{"cached_tokens":94}}}}` + "\n\n" +
		"event: response.done\n" +
		`data: {"type":"response.done","response":{"id":"resp_first_terminal","status":"completed","usage":{"input_tokens":100,"output_tokens":1,"total_tokens":101,"input_tokens_details":{"cached_tokens":94}}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 3031, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, cache.callCount())
	require.Nil(t, result.cacheHitAdjustment)
	require.Equal(t, 94, result.usage.CacheReadInputTokens)
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"cached_tokens":94`))
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":90`)
}

func TestHandleStreamingResponsePassthrough_DuplicateTerminalUsesOneAdjustment(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(94)
	apiKey := &APIKey{
		UserID:  204,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
			UpdatedAt:                      time.Unix(1_700_000_000, 0),
		},
	}
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", apiKey)
	terminal := func(eventType string) string {
		return `event: ` + eventType + "\n" +
			`data: {"type":"` + eventType + `","response":{"id":"resp_pass_duplicate","usage":{"input_tokens":100,"output_tokens":7,"input_tokens_details":{"cached_tokens":94}}}}` + "\n\n"
	}
	stream := "event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"ok"}` + "\n\n" +
		terminal("response.completed") + terminal("response.done")
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 304, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result.cacheHitAdjustment)
	require.Equal(t, 1, cache.callCount())
	require.Equal(t, 2, strings.Count(recorder.Body.String(), `"cached_tokens":90`))
	require.NotContains(t, recorder.Body.String(), `"cached_tokens":94`)
}

func TestHandleStreamingResponsePassthrough_TracksZeroCacheWithoutDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(95)
	apiKey := &APIKey{
		UserID:  205,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	}
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", apiKey)
	stream := "event: response.output_text.delta\n" +
		`data: {"type":"response.output_text.delta","delta":"ok"}` + "\n\n" +
		"event: response.completed\n" +
		`data: {"type":"response.completed","response":{"id":"resp_pass_no_detail","usage":{"input_tokens":100,"output_tokens":7,"total_tokens":107}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 305, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, cache.callCount())
	require.NotNil(t, result.cacheHitAdjustment)
	require.True(t, result.cacheHitAdjustment.Enabled)
	require.Zero(t, result.cacheHitAdjustment.ShiftedTokens)
	require.Contains(t, recorder.Body.String(), `"input_tokens":100`)
	require.NotContains(t, recorder.Body.String(), `"cached_tokens"`)
}

func TestHandleStreamingResponsePassthrough_IncompleteTerminalDoesNotAdjust(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(96)
	apiKey := &APIKey{
		UserID:  206,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	}
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", apiKey)
	stream := "event: response.incomplete\n" +
		`data: {"type":"response.incomplete","response":{"id":"resp_pass_incomplete","usage":{"input_tokens":100,"output_tokens":0,"input_tokens_details":{"cached_tokens":94}}}}` + "\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(stream)),
	}

	result, err := svc.handleStreamingResponsePassthrough(c.Request.Context(), resp, c, &Account{ID: 306, Platform: PlatformOpenAI}, time.Now(), "gpt-test", "gpt-test")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Zero(t, cache.callCount())
	require.Nil(t, result.cacheHitAdjustment)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":94`)
}

func TestIsOpenAIStreamCacheHitAdjustmentEligible_Scope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name      string
		path      string
		method    string
		upgrade   string
		transport OpenAIClientTransport
		want      bool
	}{
		{name: "responses http", path: "/v1/responses", transport: OpenAIClientTransportHTTP, want: true},
		{name: "responses bare alias", path: "/responses", transport: OpenAIClientTransportHTTP, want: true},
		{name: "responses codex alias", path: "/backend-api/codex/responses", transport: OpenAIClientTransportHTTP, want: true},
		{name: "responses openai alias", path: "/openai/v1/responses", transport: OpenAIClientTransportHTTP, want: true},
		{name: "chat completions http", path: "/v1/chat/completions", transport: OpenAIClientTransportHTTP, want: true},
		{name: "chat completions bare alias", path: "/chat/completions", transport: OpenAIClientTransportHTTP, want: true},
		{name: "chat completions openai alias", path: "/openai/v1/chat/completions", transport: OpenAIClientTransportHTTP, want: true},
		{name: "trailing slash normalized", path: "/v1/responses///", transport: OpenAIClientTransportHTTP, want: true},
		{name: "compact excluded", path: "/v1/responses/compact", transport: OpenAIClientTransportHTTP, want: false},
		{name: "responses input tokens excluded", path: "/v1/responses/input_tokens", transport: OpenAIClientTransportHTTP, want: false},
		{name: "responses input tokens bare alias excluded", path: "/responses/input_tokens", transport: OpenAIClientTransportHTTP, want: false},
		{name: "responses input tokens codex alias excluded", path: "/backend-api/codex/responses/input_tokens", transport: OpenAIClientTransportHTTP, want: false},
		{name: "responses input tokens openai alias excluded", path: "/openai/v1/responses/input_tokens", transport: OpenAIClientTransportHTTP, want: false},
		{name: "messages excluded", path: "/v1/messages", transport: OpenAIClientTransportHTTP, want: false},
		{name: "tenant suffix excluded", path: "/tenant/v1/responses", transport: OpenAIClientTransportHTTP, want: false},
		{name: "unknown chat suffix excluded", path: "/proxy/chat/completions", transport: OpenAIClientTransportHTTP, want: false},
		{name: "get excluded", path: "/v1/responses", method: http.MethodGet, transport: OpenAIClientTransportHTTP, want: false},
		{name: "upgrade excluded", path: "/v1/responses", upgrade: "websocket", transport: OpenAIClientTransportHTTP, want: false},
		{name: "websocket excluded", path: "/v1/responses", transport: OpenAIClientTransportWS, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			method := tc.method
			if method == "" {
				method = http.MethodPost
			}
			c.Request = httptest.NewRequest(method, tc.path, nil)
			if tc.upgrade != "" {
				c.Request.Header.Set("Upgrade", tc.upgrade)
			}
			SetOpenAIClientTransport(c, tc.transport)
			require.Equal(t, tc.want, isOpenAIStreamCacheHitAdjustmentEligible(c))
		})
	}

	t.Run("native compaction v2 excluded", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		MarkOpenAINativeCompactionV2(c)
		require.False(t, isOpenAIStreamCacheHitAdjustmentEligible(c))
	})

	t.Run("image intent excluded", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		SetOpenAIClientTransport(c, OpenAIClientTransportHTTP)
		SetOpenAIImageIntentHint(c, true)
		require.False(t, isOpenAIStreamCacheHitAdjustmentEligible(c))
	})

	t.Run("generic image context excluded", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		request := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		c.Request = request.WithContext(WithOpenAIImageGenerationIntent(request.Context()))
		require.False(t, isOpenAIStreamCacheHitAdjustmentEligible(c))
	})

	t.Run("video context excluded", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		SetOpenAIVideoContext(c, OpenAIVideoContext{Model: "video-test"})
		require.False(t, isOpenAIStreamCacheHitAdjustmentEligible(c))
	})

	t.Run("seedance context excluded", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		SetSeedanceVideoContext(c, SeedanceVideoContext{Model: "dreamina-seedance-test"})
		require.False(t, isOpenAIStreamCacheHitAdjustmentEligible(c))
	})
}

func TestAdjustAndRewriteOpenAIStreamUsagePayload_PreflightBeforeTracker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(97)
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{
		UserID:  207,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	body := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":100,"output_tokens":1}}}`)

	rewritten, adjustedUsage, adjustment, err := svc.adjustAndRewriteOpenAIStreamUsagePayload(
		c.Request.Context(), c, body, OpenAIUsage{InputTokens: 100, OutputTokens: 1, CacheReadInputTokens: 94},
	)

	require.NoError(t, err)
	require.Nil(t, adjustment)
	require.Zero(t, cache.callCount())
	require.Equal(t, 94, adjustedUsage.CacheReadInputTokens)
	require.Equal(t, body, rewritten)
}

func TestAdjustAndRewriteOpenAIStreamUsagePayload_StripsNonstandardAttributionAfterShift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(971)
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{
		UserID:  2071,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	body := []byte(`{"type":"response.completed","response":{"usage":{"attribution":{"items":{"msg_1":{"input_tokens":6,"cached_tokens":0}},"request_fields":{"instructions":{"input_tokens":94,"cached_tokens":94}}},"input_tokens":100,"output_tokens":1,"total_tokens":101,"input_tokens_details":{"cached_tokens":94}},"service_tier":"default"}}`)

	rewritten, adjustedUsage, adjustment, err := svc.adjustAndRewriteOpenAIStreamUsagePayload(
		c.Request.Context(), c, body, OpenAIUsage{InputTokens: 100, OutputTokens: 1, CacheReadInputTokens: 94},
	)

	require.NoError(t, err)
	require.NotNil(t, adjustment)
	require.Equal(t, 4, adjustment.ShiftedTokens)
	require.Equal(t, 90, adjustedUsage.CacheReadInputTokens)
	require.Equal(t, int64(90), gjson.GetBytes(rewritten, "response.usage.input_tokens_details.cached_tokens").Int())
	require.False(t, gjson.GetBytes(rewritten, "response.usage.attribution").Exists())
	require.Equal(t, "default", gjson.GetBytes(rewritten, "response.service_tier").String())
}

func TestAdjustAndRewriteOpenAIStreamUsagePayload_PreservesAttributionWithoutShift(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(972)
	cache := &responsesCacheHitGatewayCache{}
	svc := &OpenAIGatewayService{cache: cache}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{
		UserID:  2072,
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	})
	body := []byte(`{"type":"response.completed","response":{"usage":{"attribution":{"request_fields":{"instructions":{"input_tokens":80,"cached_tokens":80}}},"input_tokens":100,"output_tokens":1,"total_tokens":101,"input_tokens_details":{"cached_tokens":80}}}}`)

	rewritten, adjustedUsage, adjustment, err := svc.adjustAndRewriteOpenAIStreamUsagePayload(
		c.Request.Context(), c, body, OpenAIUsage{InputTokens: 100, OutputTokens: 1, CacheReadInputTokens: 80},
	)

	require.NoError(t, err)
	require.NotNil(t, adjustment)
	require.Zero(t, adjustment.ShiftedTokens)
	require.Equal(t, 80, adjustedUsage.CacheReadInputTokens)
	require.Equal(t, int64(80), gjson.GetBytes(rewritten, "response.usage.input_tokens_details.cached_tokens").Int())
	require.Equal(t, int64(80), gjson.GetBytes(rewritten, "response.usage.attribution.request_fields.instructions.cached_tokens").Int())
}

func TestAdjustOpenAIStreamUsage_SkipsCanceledContexts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, cancelRequest := range []bool{false, true} {
		t.Run(map[bool]string{false: "调用上下文取消", true: "请求上下文取消"}[cancelRequest], func(t *testing.T) {
			groupID := int64(98)
			cache := &responsesCacheHitGatewayCache{}
			svc := &OpenAIGatewayService{cache: cache}
			requestCtx, cancelRequestContext := context.WithCancel(context.Background())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(requestCtx)
			c.Set("api_key", &APIKey{
				UserID:  208,
				GroupID: &groupID,
				Group: &Group{
					ID:                             groupID,
					CacheHitQuarterToInput:         true,
					CacheHitTargetPercent:          90,
					CacheHitTargetTolerancePercent: 0.5,
				},
			})
			callCtx, cancelCallContext := context.WithCancel(context.Background())
			if cancelRequest {
				cancelRequestContext()
			} else {
				cancelCallContext()
			}
			defer cancelRequestContext()
			defer cancelCallContext()
			usage := OpenAIUsage{InputTokens: 100, CacheReadInputTokens: 94}

			adjustment, err := svc.AdjustOpenAIStreamUsage(callCtx, c, &usage)

			require.NoError(t, err)
			require.Nil(t, adjustment)
			require.Zero(t, cache.callCount())
			require.Equal(t, 94, usage.CacheReadInputTokens)
		})
	}
}

func TestAdjustOpenAIStreamUsage_DisabledGroupReturnsNoSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cache := &responsesCacheHitGatewayCache{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", &APIKey{UserID: 211, Group: &Group{ID: 101}})
	usage := OpenAIUsage{InputTokens: 100, CacheReadInputTokens: 94}

	adjustment, err := (&OpenAIGatewayService{cache: cache}).AdjustOpenAIStreamUsage(c.Request.Context(), c, &usage)

	require.NoError(t, err)
	require.Nil(t, adjustment)
	require.Nil(t, OpenAIStreamCacheHitAdjustmentFromContext(c))
	require.Zero(t, cache.callCount())
	require.Equal(t, 94, usage.CacheReadInputTokens)
}
