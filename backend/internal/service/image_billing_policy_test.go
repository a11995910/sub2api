//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 同一个图片请求无论调度到什么 Token 渠道，都应保留分组原单价并按真实张数收费。
func TestImageBillingPolicy_PreservesGroupPriceAndActualCount(t *testing.T) {
	for _, unitPrice := range []float64{0.02, 0.04, 0.06, 0.13, 0.16, 0.20, 0.40, 0.53} {
		for _, count := range []int{0, 1, 2, 3} {
			groupID := int64(84)
			group := &Group{ID: groupID, ImagePrice1K: &unitPrice, ImagePrice2K: &unitPrice, ImagePrice4K: &unitPrice}
			key := &APIKey{GroupID: &groupID, Group: group}
			billing := NewBillingService(&config.Config{}, nil)
			geminiModel := "gemini-nano-banana-2.1"
			gateway := &GatewayService{billingService: billing, resolver: newOpenAITokenImageChannelPricingResolverForTest(t, groupID, geminiModel)}
			cost := gateway.calculateRecordUsageCost(context.Background(), &ForwardResult{
				Model: geminiModel, ImageGeneration: true, ImageCount: count, ImageSize: "4K",
				Usage: ClaudeUsage{InputTokens: 14734, OutputTokens: 16896},
			}, key, geminiModel, 0.3, 1, time.Time{})
			require.Equal(t, string(BillingModeImage), cost.BillingMode)
			require.InDelta(t, unitPrice*float64(count), cost.ActualCost, 1e-12)

			openaiModel := "gpt-image-2"
			openai := &OpenAIGatewayService{billingService: billing, resolver: newOpenAITokenImageChannelPricingResolverForTest(t, groupID, openaiModel)}
			openaiCost, err := openai.calculateOpenAIRecordUsageCost(context.Background(),
				&OpenAIForwardResult{Model: openaiModel, ImageCount: count, ImageSize: "4K"}, key,
				[]string{openaiModel}, 0.3, 1, 1, 1, UsageTokens{InputTokens: 14734, OutputTokens: 16896}, "", nil, false, time.Time{})
			require.NoError(t, err)
			require.Equal(t, string(BillingModeImage), openaiCost.BillingMode)
			require.InDelta(t, unitPrice*float64(count), openaiCost.ActualCost, 1e-12)
		}
	}
}

func TestImageBillingPolicy_GeminiReferencedImages(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"图片引用", geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}}`), 1},
		{"下划线字段", geminiImageResponse(`{"file_data":{"mime_type":"image/webp","file_uri":"gs://images/one.webp"}}`), 1},
		{"MIME参数", geminiImageResponse(`{"inlineData":{"mimeType":"IMAGE/JPG; charset=binary","data":"` + geminiTestPNG + `"}}`), 1},
		{"重复引用去重", geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}},{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}}`), 1},
		{"多张引用", geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}},{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/two.png"}}`), 2},
		{"包装响应", `{"response":` + geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}}`) + `}`, 1},
		{"数组响应去重", `[` + geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}}`) + `,` + geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}}`) + `]`, 1},
		{"空引用", geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":""}}`), 0},
		{"非图片附件", geminiImageResponse(`{"fileData":{"mimeType":"application/pdf","fileUri":"https://example.com/one.pdf"}}`), 0},
		{"普通文本链接", geminiImageResponse(`{"text":"https://example.com/one.png"}`), 0},
		{"思考图片", geminiImageResponse(`{"thought":true,"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}}`), 0},
		{"空响应", "", 0},
		{"损坏响应", "not-json", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newGeminiImageTestContext(t)
			beginGeminiImageOutputObservation(c)
			observeGeminiImageOutputs(c, []byte(tc.body))
			require.Equal(t, tc.want, resolveGeminiImageCount(c, "gemini-nano-banana-2.1", "gemini-nano-banana-2.1"))
		})
	}
}

func TestImageBillingPolicy_CustomModelIntentDoesNotFallBackToToken(t *testing.T) {
	groupID := int64(84)
	unitPrice := 0.53
	svc := &GatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	for _, count := range []int{0, 1, 2} {
		cost := svc.calculateRecordUsageCost(context.Background(), &ForwardResult{
			Model: "custom-image-alias", ImageGeneration: true, ImageCount: count, ImageSize: "4K",
			Usage: ClaudeUsage{InputTokens: 10000, OutputTokens: 20000},
		}, &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, ImagePrice4K: &unitPrice}}, "custom-image-alias", 1, 1, time.Time{})
		require.Equal(t, string(BillingModeImage), cost.BillingMode)
		require.InDelta(t, unitPrice*float64(count), cost.ActualCost, 1e-12)
	}
	require.True(t, isGeminiImageGenerationRequest([]byte(`{"generationConfig":{"responseModalities":["TEXT","IMAGE"]}}`), "custom-image-alias", "vendor-alias"))
	require.False(t, isGeminiImageGenerationRequest([]byte(`{"contents":[{"parts":[{"inlineData":{"mimeType":"image/png","data":"input-image"}}]}]}`), "gemini-2.5-pro", "gemini-2.5-pro"))
}

func TestImageBillingPolicy_EmptyNativeStreamHasNoBillableImage(t *testing.T) {
	for _, body := range []string{"", "data: [DONE]\n\n"} {
		t.Run(body, func(t *testing.T) {
			c := newGeminiImageTestContext(t)
			beginGeminiImageOutputObservation(c)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}
			_, err := (&GeminiMessagesCompatService{}).handleNativeStreamingResponse(c, resp, time.Now(), false, nil, "")
			require.NoError(t, err)
			require.Equal(t, 0, resolveGeminiImageCount(c, "gemini-nano-banana-2.1", "gemini-nano-banana-2.1"))
		})
	}
}

func TestImageBillingPolicy_OpenAIEmptyJSONDoesNotBillRequestedCount(t *testing.T) {
	for _, model := range []string{"gpt-image-2", "grok-imagine-image"} {
		t.Run(model, func(t *testing.T) {
			body := []byte(`{"model":"` + model + `","prompt":"draw a cat","n":3}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			svc := newOpenAIImagesTestService(&httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"data":[],"usage":{"input_tokens":100,"output_tokens":200}}`)),
			}})
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			result, err := svc.ForwardImages(context.Background(), c, &Account{
				ID: 6, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test-api-key", "base_url": "https://image-upstream.example/v1"},
			}, body, parsed, "")
			require.NoError(t, err)
			require.Zero(t, result.ImageCount, "请求 n 不能冒充实际出图张数")
			groupID, price := int64(84), 0.53
			svc.billingService = NewBillingService(&config.Config{}, nil)
			svc.resolver = newOpenAITokenImageChannelPricingResolverForTest(t, groupID, model)
			cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result,
				&APIKey{GroupID: &groupID, Group: &Group{ID: groupID, ImagePrice1K: &price, ImagePrice2K: &price, ImagePrice4K: &price}},
				[]string{model}, 1, 1, 1, 1, UsageTokens{InputTokens: 100, OutputTokens: 200}, "", nil, false, time.Time{})
			require.NoError(t, err)
			require.Equal(t, string(BillingModeImage), cost.BillingMode)
			require.Zero(t, cost.ActualCost)
		})
	}
}

func TestImageBillingPolicy_NativeResponseCountsWhatClientReceives(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	beginGeminiImageOutputObservation(c)
	body := geminiImageResponse(`{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}},{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/one.png"}},{"fileData":{"mimeType":"image/png","fileUri":"https://example.com/two.png"}}`)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	_, err := (&GeminiMessagesCompatService{}).handleNativeNonStreamingResponse(c, resp, false, nil, "")
	require.NoError(t, err)
	require.Equal(t, 2, resolveGeminiImageCount(c, "gemini-nano-banana-2.1", "gemini-nano-banana-2.1"))
	require.Len(t, gjson.Get(recorder.Body.String(), "candidates.0.content.parts").Array(), 2)
}
