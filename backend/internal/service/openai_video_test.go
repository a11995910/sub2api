//go:build unit

package service

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIVideoContextCanBeDetectedByHandler(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	require.False(t, HasOpenAIVideoContext(c))

	SetOpenAIVideoContext(c, OpenAIVideoContext{Model: "future-motion-pro"})
	require.True(t, HasOpenAIVideoContext(c))
}

func TestSetOpenAIVideoBillingTaskKeepsExistingRequestMetadata(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetOpenAIVideoContext(c, OpenAIVideoContext{Model: "future-motion-pro", UserID: 7, APIKeyID: 11})

	require.True(t, SetOpenAIVideoBillingTask(c, 19))
	meta, ok := OpenAIVideoContextFromGin(c)

	require.True(t, ok)
	require.Equal(t, int64(19), meta.BillingTaskID)
	require.Equal(t, int64(7), meta.UserID)
}

func TestAccountSupportsOpenAIVideoEndpointCapability(t *testing.T) {
	apiKey := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"openai_capabilities": []any{"embeddings"},
		},
	}
	oauth := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	grok := &Account{Platform: PlatformGrok, Type: AccountTypeAPIKey}

	require.True(t, apiKey.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
	require.False(t, oauth.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
	require.False(t, grok.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityVideos))
}

func TestResolveOpenAIVideoRequestProfile(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		configured string
		expected   OpenAIVideoRequestProfile
	}{
		{name: "auto cangyuan", baseURL: "https://ai.cangyuansuanli.cn/v1", expected: OpenAIVideoRequestProfileUnifiedJSON},
		{name: "auto vip cangyuan", baseURL: "https://vip-api.cangyuansuanli.cn/v1", configured: "auto", expected: OpenAIVideoRequestProfileUnifiedJSON},
		{name: "hostname is case insensitive", baseURL: "https://AI.CANGYUANSUANLI.CN/v1", expected: OpenAIVideoRequestProfileUnifiedJSON},
		{name: "similar hostname stays legacy", baseURL: "https://ai.cangyuansuanli.cn.evil.example/v1", expected: OpenAIVideoRequestProfileLegacy},
		{name: "unknown host stays legacy", baseURL: "https://video.example.com/v1", expected: OpenAIVideoRequestProfileLegacy},
		{name: "invalid url stays legacy", baseURL: "://bad-url", expected: OpenAIVideoRequestProfileLegacy},
		{name: "explicit unified overrides host", baseURL: "https://video.example.com/v1", configured: "unified_json", expected: OpenAIVideoRequestProfileUnifiedJSON},
		{name: "explicit legacy overrides cangyuan", baseURL: "https://ai.cangyuansuanli.cn/v1", configured: "legacy", expected: OpenAIVideoRequestProfileLegacy},
		{name: "unknown configured value stays legacy", baseURL: "https://ai.cangyuansuanli.cn/v1", configured: "unified-json", expected: OpenAIVideoRequestProfileLegacy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform: PlatformOpenAI,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"base_url": tt.baseURL,
				},
			}
			if tt.configured != "" {
				account.Credentials["video_request_profile"] = tt.configured
			}
			require.Equal(t, tt.expected, ResolveOpenAIVideoRequestProfile(account))
		})
	}
}

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

func TestParseOpenAIVideoCreateBodyKeepsUnifiedFields(t *testing.T) {
	payload, req, err := ParseOpenAIVideoCreateBody([]byte(`{
		"model":"dreamina-seedance-2-0-ep",
		"prompt":"雨夜城市",
		"duration":5,
		"resolution":"720p",
		"aspect_ratio":"16:9",
		"reference_image_urls":["https://cdn.test/a.png"]
	}`))

	require.NoError(t, err)
	require.Equal(t, "16:9", req.AspectRatio)
	require.Equal(t, 5, req.DurationSeconds)
	require.Equal(t, []string{"https://cdn.test/a.png"}, req.ImageURLs)
	require.Equal(t, float64(5), payload["duration"])
}

func TestBuildUnifiedOpenAIVideoCreateBodyUsesOnlyStandardFields(t *testing.T) {
	payload, req, err := ParseOpenAIVideoCreateBody([]byte(`{
		"model":"public-model",
		"prompt":"角色转身",
		"seconds":"6",
		"resolution":"720p",
		"size":"9:16",
		"image":{"url":"https://cdn.test/start.png"},
		"image_urls":["https://cdn.test/a.png"],
		"reference_image_urls":["https://cdn.test/a.png","https://cdn.test/b.png"],
		"reference_images":[{"url":"https://cdn.test/c.png"}]
	}`))
	require.NoError(t, err)

	body, err := BuildUnifiedOpenAIVideoCreateBody(payload, req, "upstream-model")

	require.NoError(t, err)
	require.JSONEq(t, `{
		"model":"upstream-model",
		"prompt":"角色转身",
		"duration":6,
		"resolution":"720p",
		"aspect_ratio":"9:16",
		"reference_image_urls":[
			"https://cdn.test/a.png",
			"https://cdn.test/b.png",
			"https://cdn.test/start.png",
			"https://cdn.test/c.png"
		]
	}`, string(body))
	for _, field := range []string{"seconds", "size", "image_urls", "reference_images", "image"} {
		require.False(t, gjson.GetBytes(body, field).Exists(), field)
	}
}

func TestBuildUnifiedOpenAIVideoCreateBodyRejectsUnknownTopLevelField(t *testing.T) {
	payload, req, err := ParseOpenAIVideoCreateBody([]byte(`{"model":"video","prompt":"x","duration":5,"watermark":true}`))
	require.NoError(t, err)

	_, err = BuildUnifiedOpenAIVideoCreateBody(payload, req, "video")

	require.ErrorContains(t, err, `unsupported video field "watermark"`)
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

func TestParseOpenAIVideoResultReadsDocumentedDataArrayArtifact(t *testing.T) {
	result, err := ParseOpenAIVideoResult([]byte(`{
		"id":"task-3",
		"status":"completed",
		"data":[{"url":"https://api.test/v1/videos/task-3/content"}]
	}`))

	require.NoError(t, err)
	require.Equal(t, "completed", result.Status)
	require.Equal(t, "https://api.test/v1/videos/task-3/content", result.VideoURL)
}

func TestNormalizeOpenAIVideoStatus(t *testing.T) {
	tests := map[string]string{
		"pending":    "queued",
		"RUNNING":    "in_progress",
		"succeeded":  "completed",
		"cancelled":  "failed",
		"expired":    "failed",
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
