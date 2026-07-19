//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIVideoCreateBodyUsesMappedModelAndStringSeconds(t *testing.T) {
	body, req, err := NormalizeOpenAIVideoCreateBody([]byte(`{
		"model":"dreamina-seedance-2-0-ep",
		"prompt":"雨夜城市",
		"resolution":"720p",
		"duration":5,
		"reference_images":[{"url":"https://cdn.test/a.png"}]
	}`), "jing-video-2-pro")
	require.NoError(t, err)
	require.Equal(t, "dreamina-seedance-2-0-ep", req.Model)
	require.Equal(t, "雨夜城市", req.Prompt)
	require.Equal(t, "720p", req.Resolution)
	require.Equal(t, 5, req.DurationSeconds)
	require.Equal(t, []string{"https://cdn.test/a.png"}, req.ImageURLs)
	require.Equal(t, "jing-video-2-pro", gjson.GetBytes(body, "model").String())
	require.Equal(t, "5", gjson.GetBytes(body, "seconds").String())
	require.Equal(t, "https://cdn.test/a.png", gjson.GetBytes(body, "image_urls.0").String())
	require.False(t, gjson.GetBytes(body, "duration").Exists())
	require.False(t, gjson.GetBytes(body, "reference_images").Exists())
}

func TestNormalizeOpenAIVideoCreateBodyMergesCompatibleImageFields(t *testing.T) {
	body, req, err := NormalizeOpenAIVideoCreateBody([]byte(`{
		"model":"future-motion-pro",
		"prompt":"角色转身",
		"seconds":"6",
		"image":{"url":"https://cdn.test/start.png"},
		"image_urls":["https://cdn.test/a.png"],
		"reference_image_urls":["https://cdn.test/b.png"],
		"reference_images":[{"url":"https://cdn.test/a.png"},{"url":"https://cdn.test/c.png"}]
	}`), "future-motion-upstream")
	require.NoError(t, err)
	require.Equal(t, 6, req.DurationSeconds)
	require.Equal(t, []string{
		"https://cdn.test/a.png",
		"https://cdn.test/b.png",
		"https://cdn.test/start.png",
		"https://cdn.test/c.png",
	}, req.ImageURLs)
	require.Equal(t, int64(4), gjson.GetBytes(body, "image_urls.#").Int())
}

func TestNormalizeOpenAIVideoCreateBodyValidatesRequiredFields(t *testing.T) {
	_, _, err := NormalizeOpenAIVideoCreateBody([]byte(`{"prompt":"x"}`), "mapped")
	require.ErrorContains(t, err, "model is required")

	_, _, err = NormalizeOpenAIVideoCreateBody([]byte(`{"model":"video"}`), "mapped")
	require.ErrorContains(t, err, "prompt is required")
}

func TestParseOpenAIVideoResultNormalizesTaskStatusProgressAndURL(t *testing.T) {
	result, err := ParseOpenAIVideoResult([]byte(`{
		"task_id":"task-1",
		"model":"jing-video-2-pro",
		"status":"processing",
		"progress":"42%",
		"metadata":{"url":"https://cdn.test/result.mp4"}
	}`))
	require.NoError(t, err)
	require.Equal(t, "task-1", result.TaskID)
	require.Equal(t, "jing-video-2-pro", result.Model)
	require.Equal(t, "in_progress", result.Status)
	require.Equal(t, 42, result.Progress)
	require.Equal(t, "https://cdn.test/result.mp4", result.VideoURL)
}

func TestParseOpenAIVideoResultSupportsAliasesAndClampsProgress(t *testing.T) {
	result, err := ParseOpenAIVideoResult([]byte(`{
		"data":{"id":"task-2","status":"done","progress":140},
		"videos":[{"url":"https://cdn.test/result.webm"}]
	}`))
	require.NoError(t, err)
	require.Equal(t, "task-2", result.TaskID)
	require.Equal(t, "completed", result.Status)
	require.Equal(t, 100, result.Progress)
	require.Equal(t, "https://cdn.test/result.webm", result.VideoURL)
}

func TestNormalizeOpenAIVideoStatus(t *testing.T) {
	tests := map[string]string{
		"pending":    "queued",
		"RUNNING":    "in_progress",
		"succeeded":  "completed",
		"cancelled":  "failed",
		"unexpected": "unexpected",
	}
	for input, expected := range tests {
		require.Equal(t, expected, NormalizeOpenAIVideoStatus(input))
	}
}

func TestIsOpenAIVideoEndpointUnsupported(t *testing.T) {
	require.True(t, IsOpenAIVideoEndpointUnsupported(404, []byte(`{"error":{"code":"not_found"}}`)))
	require.True(t, IsOpenAIVideoEndpointUnsupported(405, nil))
	require.True(t, IsOpenAIVideoEndpointUnsupported(400, []byte(`{"error":{"code":"unsupported_endpoint"}}`)))
	require.False(t, IsOpenAIVideoEndpointUnsupported(400, []byte(`{"code":"invalid_request","message":"prompt is required"}`)))
	require.False(t, IsOpenAIVideoEndpointUnsupported(401, []byte(`{"message":"invalid api key"}`)))
	require.False(t, IsOpenAIVideoEndpointUnsupported(429, []byte(`{"message":"rate limited"}`)))
	require.False(t, IsOpenAIVideoEndpointUnsupported(502, []byte(`{"message":"temporarily unavailable"}`)))
}
