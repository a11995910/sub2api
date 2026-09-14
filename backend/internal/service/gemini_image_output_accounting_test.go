//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const geminiTestPNG = "iVBORw0KGgoAAAANSUhEUg=="
const geminiTestPNG2 = "iVBORw0KGgoAAAANSUhEUh=="

func newGeminiImageTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost,
		"/v1beta/models/nana-banana-2:generateContent", strings.NewReader("{}"))
	return c
}

func geminiImageResponse(parts string) string {
	return `{"candidates":[{"content":{"role":"model","parts":[` + parts + `]},"finishReason":"STOP"}]}`
}

func TestCountGeminiInlineImageOutputs(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    int
	}{
		{
			name:    "camelCase inlineData",
			payload: geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`),
			want:    1,
		},
		{
			// 官方 SDK 与部分中转把字段回成 snake_case。
			name:    "snake_case inline_data",
			payload: geminiImageResponse(`{"inline_data":{"mime_type":"image/png","data":"` + geminiTestPNG + `"}}`),
			want:    1,
		},
		{
			name: "text and image mixed",
			payload: geminiImageResponse(`{"text":"here you go"},` +
				`{"inlineData":{"mimeType":"image/jpeg","data":"` + geminiTestPNG + `"}}`),
			want: 1,
		},
		{
			name: "multiple images",
			payload: geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/webp","data":"` + geminiTestPNG2 + `"}}`),
			want: 2,
		},
		{
			name: "identical images count once",
			payload: geminiImageResponse(`{"inlineData":{"mimeType":"image/jpeg","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/jpeg","data":"` + geminiTestPNG + `"}}`),
			want: 1,
		},
		{
			name: "thought image is not billable",
			payload: geminiImageResponse(`{"thought":true,"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG2 + `"}}`),
			want: 1,
		},
		{
			name:    "uppercase mime type",
			payload: geminiImageResponse(`{"inlineData":{"mimeType":"IMAGE/PNG","data":"` + geminiTestPNG + `"}}`),
			want:    1,
		},
		{
			name:    "text only",
			payload: geminiImageResponse(`{"text":"no image here"}`),
			want:    0,
		},
		{
			// 非图片的内联附件（例如音频）不能按图片计费。
			name:    "non image mime type",
			payload: geminiImageResponse(`{"inlineData":{"mimeType":"audio/mpeg","data":"` + geminiTestPNG + `"}}`),
			want:    0,
		},
		{
			name:    "empty data is not billable",
			payload: geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":""}}`),
			want:    0,
		},
		{name: "empty payload", payload: "", want: 0},
		{name: "invalid json", payload: "not-json", want: 0},
		{name: "error response", payload: `{"error":{"code":429,"message":"quota"}}`, want: 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, countGeminiInlineImageOutputs([]byte(tc.payload)))
		})
	}
}

func TestDeduplicateGeminiInlineImageOutputs(t *testing.T) {
	t.Run("removes byte-identical final image", func(t *testing.T) {
		payload := []byte(geminiImageResponse(
			`{"inlineData":{"mimeType":"image/jpeg","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/jpeg","data":"` + geminiTestPNG + `"}}`))

		got, removed := deduplicateGeminiInlineImageOutputs(payload, nil)

		require.Equal(t, 1, removed)
		require.Len(t, gjson.GetBytes(got, "candidates.0.content.parts").Array(), 1)
		require.Equal(t, 1, countGeminiInlineImageOutputs(got))
	})

	t.Run("keeps distinct and thought images", func(t *testing.T) {
		payload := []byte(geminiImageResponse(
			`{"thought":true,"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG2 + `"}}`))

		got, removed := deduplicateGeminiInlineImageOutputs(payload, nil)

		require.Zero(t, removed)
		require.Equal(t, string(payload), string(got))
		require.Len(t, gjson.GetBytes(got, "candidates.0.content.parts").Array(), 3)
		require.Equal(t, 2, countGeminiInlineImageOutputs(got))
	})

	t.Run("deduplicates across streaming payloads", func(t *testing.T) {
		seen := make(map[geminiInlineImageDigest]struct{})
		first := []byte(geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`))
		second := []byte(geminiImageResponse(
			`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
				`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG2 + `"}}`))

		firstOut, firstRemoved := deduplicateGeminiInlineImageOutputs(first, seen)
		secondOut, secondRemoved := deduplicateGeminiInlineImageOutputs(second, seen)

		require.Zero(t, firstRemoved)
		require.Equal(t, string(first), string(firstOut))
		require.Equal(t, 1, secondRemoved)
		require.Len(t, gjson.GetBytes(secondOut, "candidates.0.content.parts").Array(), 1)
		require.Equal(t, geminiTestPNG2, gjson.GetBytes(secondOut, "candidates.0.content.parts.0.inlineData.data").String())
	})
}

// 累积式 SSE 会把同一张图在后续 chunk 里整段重发，内容摘要必须挡住重复计费。
func TestObserveGeminiImageOutputs_CumulativeChunksDoNotDoubleCount(t *testing.T) {
	c := newGeminiImageTestContext(t)
	beginGeminiImageOutputObservation(c)

	oneImage := geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`)
	for range 4 {
		observeGeminiImageOutputs(c, []byte(oneImage))
	}

	require.Equal(t, 1, observedGeminiImageOutputs(c))
}

func TestObserveGeminiImageOutputs_TracksDistinctImagesInPayload(t *testing.T) {
	c := newGeminiImageTestContext(t)
	beginGeminiImageOutputObservation(c)

	observeGeminiImageOutputs(c, []byte(geminiImageResponse(`{"text":"working"}`)))
	observeGeminiImageOutputs(c, []byte(geminiImageResponse(
		`{"inlineData":{"mimeType":"image/png","data":"`+geminiTestPNG+`"}},`+
			`{"inlineData":{"mimeType":"image/png","data":"`+geminiTestPNG2+`"}}`)))
	// 收尾 chunk 只带 usageMetadata，不能把已数到的张数抹掉。
	observeGeminiImageOutputs(c, []byte(`{"usageMetadata":{"promptTokenCount":9}}`))

	require.Equal(t, 2, observedGeminiImageOutputs(c))
}

// failover 会拿同一个 gin.Context 重跑 Forward，计数器必须按次重置，
// 否则失败账号已经回吐的图会被叠加到成功账号的账单上。
func TestBeginGeminiImageOutputObservation_ResetsPerForward(t *testing.T) {
	c := newGeminiImageTestContext(t)
	oneImage := []byte(geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`))

	beginGeminiImageOutputObservation(c)
	observeGeminiImageOutputs(c, oneImage)
	require.Equal(t, 1, observedGeminiImageOutputs(c))

	beginGeminiImageOutputObservation(c)
	require.Equal(t, 0, observedGeminiImageOutputs(c))
	observeGeminiImageOutputs(c, oneImage)
	require.Equal(t, 1, observedGeminiImageOutputs(c))
}

// issue #5358：自定义模型名（客户端名与上游映射名都不在白名单里）走 Gemini 原生
// generateContent 生图，改动前 ImageCount 恒为 0，calculateRecordUsageCost 的按次
// 计费分支整条不触发，四次生图全部记 $0。
func TestResolveGeminiImageCount(t *testing.T) {
	oneImage := []byte(geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`))
	textOnly := []byte(geminiImageResponse(`{"text":"hello"}`))

	t.Run("custom model name bills by observed images", func(t *testing.T) {
		c := newGeminiImageTestContext(t)
		beginGeminiImageOutputObservation(c)
		observeGeminiImageOutputs(c, oneImage)

		require.False(t, isImageGenerationModel("nana-banana-2"), "前置条件：白名单判不出自定义名")
		require.Equal(t, 1, resolveGeminiImageCount(c, "nana-banana-2", "nana-banana-2"))
	})

	t.Run("falls back to requested model name", func(t *testing.T) {
		c := newGeminiImageTestContext(t)
		beginGeminiImageOutputObservation(c)
		observeGeminiImageOutputs(c, textOnly)

		require.Equal(t, 1, resolveGeminiImageCount(c, "gemini-3-pro-image-preview", "gemini-3-pro-image-preview"))
	})

	t.Run("falls back to mapped upstream model name", func(t *testing.T) {
		c := newGeminiImageTestContext(t)
		beginGeminiImageOutputObservation(c)
		observeGeminiImageOutputs(c, textOnly)

		require.Equal(t, 1, resolveGeminiImageCount(c, "my-image-alias", "gemini-2.5-flash-image"))
	})

	t.Run("text model stays unbilled", func(t *testing.T) {
		c := newGeminiImageTestContext(t)
		beginGeminiImageOutputObservation(c)
		observeGeminiImageOutputs(c, textOnly)

		require.Equal(t, 0, resolveGeminiImageCount(c, "gemini-2.5-pro", "gemini-2.5-pro"))
	})

	t.Run("thought-only response does not fall back to one image", func(t *testing.T) {
		c := newGeminiImageTestContext(t)
		beginGeminiImageOutputObservation(c)
		observeGeminiImageOutputs(c, []byte(geminiImageResponse(
			`{"thought":true,"inlineData":{"mimeType":"image/png","data":"`+geminiTestPNG+`"}}`)))

		require.Equal(t, 0, resolveGeminiImageCount(c, "gemini-3.1-flash-image", "gemini-3.1-flash-image"))
	})

	t.Run("no counter on context degrades to name heuristic", func(t *testing.T) {
		c := newGeminiImageTestContext(t)
		require.Equal(t, 0, resolveGeminiImageCount(c, "nana-banana-2", "nana-banana-2"))
		require.Equal(t, 1, resolveGeminiImageCount(c, "gemini-3-pro-image", "gemini-3-pro-image"))
	})
}

// 端到端守住接线：/v1beta/models/{model}:generateContent 的非流式响应体
// 必须真的喂进计数器，否则上面的单测全绿而线上依然记 $0。
func TestHandleNativeNonStreamingResponse_FeedsImageCounter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/v1beta/models/nana-banana-2:generateContent", strings.NewReader("{}"))
	beginGeminiImageOutputObservation(c)

	body := geminiImageResponse(
		`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
			`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}

	svc := &GeminiMessagesCompatService{}
	usage, err := svc.handleNativeNonStreamingResponse(c, resp, false)
	require.NoError(t, err)
	require.NotNil(t, usage)

	require.Equal(t, 1, observedGeminiImageOutputs(c))
	require.Equal(t, 1, resolveGeminiImageCount(c, "nana-banana-2", "nana-banana-2"))
	require.Len(t, gjson.GetBytes(recorder.Body.Bytes(), "candidates.0.content.parts").Array(), 1)
}

func TestHandleNativeStreamingResponse_DeduplicatesImagesAcrossChunks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	beginGeminiImageOutputObservation(c)

	first := geminiImageResponse(`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}}`)
	second := geminiImageResponse(
		`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG + `"}},` +
			`{"inlineData":{"mimeType":"image/png","data":"` + geminiTestPNG2 + `"}}`)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(
			"data: " + first + "\n\n" +
				"data: " + second + "\n\n" +
				"data: [DONE]\n\n")),
	}

	_, err := (&GeminiMessagesCompatService{}).handleNativeStreamingResponse(c, resp, time.Now(), false)
	require.NoError(t, err)
	require.Equal(t, 2, observedGeminiImageOutputs(c))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), geminiTestPNG))
	require.Equal(t, 1, strings.Count(recorder.Body.String(), geminiTestPNG2))
}
