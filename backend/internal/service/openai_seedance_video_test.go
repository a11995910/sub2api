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

func TestBuildSeedanceChatRequest_TextOnly(t *testing.T) {
	body, err := BuildSeedanceChatRequest(SeedanceVideoRequest{
		Model:      "dreamina-seedance-2-0-mini-ep",
		Prompt:     "纯黑背景",
		Resolution: "480p",
		Duration:   4,
	})
	require.NoError(t, err)
	require.Equal(t, "dreamina-seedance-2-0-mini-ep", gjson.GetBytes(body, "model").String())
	require.False(t, gjson.GetBytes(body, "stream").Bool())
	content := gjson.GetBytes(body, "messages.0.content").String()
	require.Contains(t, content, "纯黑背景")
	require.Contains(t, content, "480p")
	require.Contains(t, content, "4 秒")
}

func TestBuildSeedanceChatRequest_WithReferenceImages(t *testing.T) {
	body, err := BuildSeedanceChatRequest(SeedanceVideoRequest{
		Model:                  "dreamina-seedance-2-0-ep",
		Prompt:                 "让画面缓慢移动",
		Resolution:             "720p",
		Duration:               8,
		ReferenceImageDataURLs: []string{"data:image/png;base64,AAAA"},
	})
	require.NoError(t, err)
	require.Equal(t, "text", gjson.GetBytes(body, "messages.0.content.0.type").String())
	require.Equal(t, "image_url", gjson.GetBytes(body, "messages.0.content.1.type").String())
	require.Equal(t, "data:image/png;base64,AAAA", gjson.GetBytes(body, "messages.0.content.1.image_url.url").String())
}

func TestBuildSeedanceChatRequest_RejectsUnsupportedResolution(t *testing.T) {
	_, err := BuildSeedanceChatRequest(SeedanceVideoRequest{
		Model:      "dreamina-seedance-2-0-mini-ep",
		Prompt:     "测试",
		Resolution: "1080p",
		Duration:   4,
	})
	require.ErrorContains(t, err, "1080p")
}

func TestParseSeedanceChatResponse_TextVideoURL(t *testing.T) {
	result, err := ParseSeedanceChatResponse([]byte(`{
		"id":"chatcmpl_1",
		"choices":[{"message":{"content":"生成完成：https://cdn.test/video.mp4"}}],
		"usage":{"completion_tokens":1234}
	}`))
	require.NoError(t, err)
	require.Equal(t, "https://cdn.test/video.mp4", result.VideoURL)
	require.Equal(t, "chatcmpl_1", result.RequestID)
	require.Equal(t, 1234, result.OutputTokens)
}

func TestParseSeedanceChatResponse_MarkdownVideoURL(t *testing.T) {
	result, err := ParseSeedanceChatResponse([]byte(`{
		"choices":[{"message":{"content":"[下载视频](https://cdn.test/final.mp4?token=abc)"}}]
	}`))
	require.NoError(t, err)
	require.Equal(t, "https://cdn.test/final.mp4?token=abc", result.VideoURL)
}

func TestParseSeedanceChatResponse_JSONContent(t *testing.T) {
	result, err := ParseSeedanceChatResponse([]byte(`{
		"choices":[{"message":{"content":"{\"request_id\":\"task_123\",\"status\":\"processing\",\"video_url\":\"https://cdn.test/result.webm\"}"}}]
	}`))
	require.NoError(t, err)
	require.Equal(t, "task_123", result.RequestID)
	require.Equal(t, "processing", result.Status)
	require.Equal(t, "https://cdn.test/result.webm", result.VideoURL)
}

func TestParseSeedanceChatResponse_UpstreamError(t *testing.T) {
	_, err := ParseSeedanceChatResponse([]byte(`{
		"error":{"message":"upstream temporarily unavailable","type":"api_error"}
	}`))
	require.ErrorContains(t, err, "upstream temporarily unavailable")
}

func TestBufferRawChatCompletions_SeedanceTransformsVideoResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	SetSeedanceVideoContext(c, SeedanceVideoContext{
		Model:      "dreamina-seedance-2-0-mini-ep",
		Resolution: "480p",
		Duration:   4,
	})
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"id":"chatcmpl_seedance",
			"choices":[{"message":{"content":"https://cdn.test/result.mp4"}}],
			"usage":{"completion_tokens":1234}
		}`)),
	}

	result, err := (&OpenAIGatewayService{}).bufferRawChatCompletions(
		c,
		resp,
		"dreamina-seedance-2-0-mini-ep",
		"dreamina-seedance-2-0-mini-ep",
		"dreamina-seedance-2-0-mini-ep",
		nil,
		nil,
		time.Now(),
	)
	require.NoError(t, err)
	require.Equal(t, "https://cdn.test/result.mp4", gjson.Get(recorder.Body.String(), "video.url").String())
	require.Equal(t, 1, result.VideoCount)
	require.Equal(t, "480p", result.VideoResolution)
	require.Equal(t, 4, result.VideoDurationSeconds)
	require.True(t, isVideoUsageResult(result, []string{"dreamina-seedance-2-0-mini-ep"}))
}
