package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/gin-gonic/gin"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type failingOpenAIImageWriter struct {
	gin.ResponseWriter
	failAfter int
	writes    int
}

func (w *failingOpenAIImageWriter) Write(p []byte) (int, error) {
	if w.writes >= w.failAfter {
		return 0, errors.New("write failed: client disconnected")
	}
	w.writes++
	return w.ResponseWriter.Write(p)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_JSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"1024x1024","quality":"high","stream":true}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, "/v1/images/generations", parsed.Endpoint)
	require.Equal(t, "gpt-image-2", parsed.Model)
	require.Equal(t, "draw a cat", parsed.Prompt)
	require.True(t, parsed.Stream)
	require.Equal(t, "1024x1024", parsed.Size)
	require.Equal(t, "1K", parsed.SizeTier)
	require.Equal(t, OpenAIImagesCapabilityNative, parsed.RequiredCapability)
	require.False(t, parsed.Multipart)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_MultipartEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "replace background"))
	require.NoError(t, writer.WriteField("size", "1536x1024"))
	part, err := writer.CreateFormFile("image", "source.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("fake-image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body.Bytes())
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, "/v1/images/edits", parsed.Endpoint)
	require.True(t, parsed.Multipart)
	require.Equal(t, "gpt-image-2", parsed.Model)
	require.Equal(t, "replace background", parsed.Prompt)
	require.Equal(t, "1536x1024", parsed.Size)
	require.Equal(t, "2K", parsed.SizeTier)
	require.Len(t, parsed.Uploads, 1)
	require.Equal(t, OpenAIImagesCapabilityNative, parsed.RequiredCapability)
}

func TestOpenAIImagesRequestModerationBody_JSONEditIncludesInputImageURLs(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint:       openAIImagesEditsEndpoint,
		Prompt:         "replace background",
		InputImageURLs: []string{"https://example.com/source.png"},
		MaskImageURL:   "https://example.com/mask.png",
	}

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, parsed.ModerationBody())

	require.Equal(t, "replace background", input.Text)
	require.Equal(t, []string{"https://example.com/source.png", "https://example.com/mask.png"}, input.Images)
}

func TestOpenAIImagesRequestModerationBody_MultipartEditIncludesUploadsInMemory(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesEditsEndpoint,
		Prompt:   "replace background",
		Uploads: []OpenAIImagesUpload{{
			FieldName:   "image",
			FileName:    "source.png",
			ContentType: "image/png",
			Data:        []byte("fake-image-bytes"),
		}},
		MaskUpload: &OpenAIImagesUpload{
			FieldName:   "mask",
			FileName:    "mask.png",
			ContentType: "image/png",
			Data:        []byte("fake-mask-bytes"),
		},
	}

	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIImages, parsed.ModerationBody())

	require.Equal(t, "replace background", input.Text)
	require.Equal(t, []string{
		"data:image/png;base64,ZmFrZS1pbWFnZS1ieXRlcw==",
		"data:image/png;base64,ZmFrZS1tYXNrLWJ5dGVz",
	}, input.Images)

	log := (&ContentModerationService{}).buildLog(ContentModerationCheckInput{}, defaultContentModerationConfig(), ContentModerationActionAllow, false, "", 0, nil, input.ExcerptText(), nil, nil, "")
	require.Equal(t, "replace background", log.InputExcerpt)
	require.NotContains(t, log.InputExcerpt, "ZmFrZS")
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_NormalizesOfficialAndCustomSizes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		size     string
		wantTier string
	}{
		{size: "1024x1024", wantTier: "1K"},
		{size: "1536x1024", wantTier: "2K"},
		{size: "1024x1536", wantTier: "2K"},
		{size: "2048x2048", wantTier: "2K"},
		{size: "2048x1152", wantTier: "2K"},
		{size: "3840x2160", wantTier: "4K"},
		{size: "2160x3840", wantTier: "4K"},
		{size: "1024X768", wantTier: "1K"},
		{size: "1280x768", wantTier: "2K"},
		{size: "2560x1440", wantTier: "4K"},
		{size: "2560x1600", wantTier: "4K"},
		{size: "auto", wantTier: "2K"},
	}

	svc := &OpenAIGatewayService{}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"` + tt.size + `"}`)

			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req

			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.NotNil(t, parsed)
			require.Equal(t, tt.size, parsed.Size)
			require.Equal(t, tt.wantTier, parsed.SizeTier)
		})
	}
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_UnknownSizesDoNotBlockPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		size     string
		wantTier string
	}{
		{size: "2048x1153", wantTier: "2K"},
		{size: "4096x1024", wantTier: "4K"},
		{size: "3840x1024", wantTier: "4K"},
		{size: "512x512", wantTier: "1K"},
		{size: "invalid", wantTier: "2K"},
		{size: "999999999999999999999999999x2", wantTier: "2K"},
	}

	svc := &OpenAIGatewayService{}
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"` + tt.size + `"}`)

			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req

			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.NotNil(t, parsed)
			require.Equal(t, tt.size, parsed.Size)
			require.Equal(t, tt.wantTier, parsed.SizeTier)
		})
	}
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_LegacyImageModelUnknownSizePassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-1.5","prompt":"draw a cat","size":"2048x1152"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, "2048x1152", parsed.Size)
	require.Equal(t, "2K", parsed.SizeTier)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_MultipartEditWithMaskAndNativeOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "replace foreground"))
	require.NoError(t, writer.WriteField("output_format", "png"))
	require.NoError(t, writer.WriteField("input_fidelity", "high"))
	require.NoError(t, writer.WriteField("output_compression", "80"))
	require.NoError(t, writer.WriteField("partial_images", "2"))

	imageHeader := make(textproto.MIMEHeader)
	imageHeader.Set("Content-Disposition", `form-data; name="image"; filename="source.png"`)
	imageHeader.Set("Content-Type", "image/png")
	imagePart, err := writer.CreatePart(imageHeader)
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("source-image-bytes"))
	require.NoError(t, err)

	maskHeader := make(textproto.MIMEHeader)
	maskHeader.Set("Content-Disposition", `form-data; name="mask"; filename="mask.png"`)
	maskHeader.Set("Content-Type", "image/png")
	maskPart, err := writer.CreatePart(maskHeader)
	require.NoError(t, err)
	_, err = maskPart.Write([]byte("mask-image-bytes"))
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body.Bytes())
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Len(t, parsed.Uploads, 1)
	require.NotNil(t, parsed.MaskUpload)
	require.True(t, parsed.HasMask)
	require.Equal(t, "png", parsed.OutputFormat)
	require.Equal(t, "high", parsed.InputFidelity)
	require.NotNil(t, parsed.OutputCompression)
	require.Equal(t, 80, *parsed.OutputCompression)
	require.NotNil(t, parsed.PartialImages)
	require.Equal(t, 2, *parsed.PartialImages)
	require.Equal(t, OpenAIImagesCapabilityNative, parsed.RequiredCapability)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_PromptOnlyDefaultsRemainBasic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"prompt":"draw a cat"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, "gpt-image-2", parsed.Model)
	require.Equal(t, OpenAIImagesCapabilityBasic, parsed.RequiredCapability)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_ExplicitSizeRequiresNativeCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"prompt":"draw a cat","size":"1024x1024"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, OpenAIImagesCapabilityNative, parsed.RequiredCapability)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_RejectsNonImageModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.4","prompt":"draw a cat"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	err = ValidateOpenAIImagesNativeModel(parsed.Model)
	require.ErrorContains(t, err, `images endpoint requires an image model, got "gpt-5.4"`)
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_AllowsGrokImageModels(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, model := range []string{"grok-imagine", "grok-imagine-image", "grok-imagine-image-quality", "grok-imagine-edit"} {
		t.Run(model, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"draw a cat","response_format":"b64_json"}`, model))
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req

			svc := &OpenAIGatewayService{}
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.NotNil(t, parsed)
			require.Equal(t, model, parsed.Model)
			require.Equal(t, OpenAIImagesCapabilityNative, parsed.RequiredCapability)
		})
	}
}

func TestOpenAIGatewayServiceParseOpenAIImagesRequest_JSONEditURLs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{
		"model":"gpt-image-2",
		"prompt":"replace the background",
		"images":[{"image_url":"https://example.com/source.png"}],
		"mask":{"image_url":"https://example.com/mask.png"},
		"input_fidelity":"high",
		"output_compression":90,
		"partial_images":2,
		"response_format":"url"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.Equal(t, []string{"https://example.com/source.png"}, parsed.InputImageURLs)
	require.Equal(t, "https://example.com/mask.png", parsed.MaskImageURL)
	require.Equal(t, "high", parsed.InputFidelity)
	require.NotNil(t, parsed.OutputCompression)
	require.Equal(t, 90, *parsed.OutputCompression)
	require.NotNil(t, parsed.PartialImages)
	require.Equal(t, 2, *parsed.PartialImages)
	require.True(t, parsed.HasMask)
	require.Equal(t, OpenAIImagesCapabilityNative, parsed.RequiredCapability)
}

func TestCollectOpenAIImagePointers_RecognizesDirectAssets(t *testing.T) {
	items := collectOpenAIImagePointers([]byte(`{
		"revised_prompt": "cat astronaut",
		"parts": [
			{"b64_json":"QUJD"},
			{"download_url":"https://files.example.com/image.png?sig=1"},
			{"asset_pointer":"file-service://file_123"}
		]
	}`))

	require.Len(t, items, 3)
	var sawBase64, sawURL, sawPointer bool
	for _, item := range items {
		if item.B64JSON == "QUJD" {
			sawBase64 = true
			require.Equal(t, "cat astronaut", item.Prompt)
		}
		if item.DownloadURL == "https://files.example.com/image.png?sig=1" {
			sawURL = true
		}
		if item.Pointer == "file-service://file_123" {
			sawPointer = true
		}
	}
	require.True(t, sawBase64)
	require.True(t, sawURL)
	require.True(t, sawPointer)
}

func TestResolveOpenAIImageBytes_PrefersInlineBase64(t *testing.T) {
	data, err := resolveOpenAIImageBytes(context.Background(), nil, nil, "", openAIImagePointerInfo{
		B64JSON: "data:image/png;base64,QUJD",
	}, openAIUpstreamErrorBodyReadLimit)
	require.NoError(t, err)
	require.Equal(t, []byte("ABC"), data)
}

func TestNewOpenAIImageStatusError_UsesProvidedReadLimit(t *testing.T) {
	padding := strings.Repeat("x", int(openAIUpstreamErrorBodyReadLimit)+1024)
	body := fmt.Sprintf(`{"error":{"padding":"%s","message":"diagnostic-marker"}}`, padding)
	resp := &req.Response{Response: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}}

	err := newOpenAIImageStatusError(resp, "download image bytes failed", int64(len(body)))
	require.Error(t, err)
	require.Equal(t, "diagnostic-marker", err.Error())

	var statusErr *openAIImageStatusError
	require.ErrorAs(t, err, &statusErr)
	require.Len(t, statusErr.ResponseBody, len(body))
}

func TestOpenAIUpstreamErrorBodyReadLimitForConfig_RespectsDiagnosticLimit(t *testing.T) {
	cfg := &config.Config{Gateway: config.GatewayConfig{
		LogUpstreamErrorBody:         true,
		LogUpstreamErrorBodyMaxBytes: int(openAIUpstreamErrorBodyReadLimit) + 1024,
	}}

	require.Equal(t, int64(cfg.Gateway.LogUpstreamErrorBodyMaxBytes), openAIUpstreamErrorBodyReadLimitForConfig(cfg))
}

func TestAccountSupportsOpenAIImageCapability_OAuthSupportsNative(t *testing.T) {
	account := &Account{
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
	}

	require.True(t, account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic))
	require.True(t, account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityNative))
}

func TestAccountSupportsOpenAIImageCapability_EmptyRequirementDoesNotRejectGrok(t *testing.T) {
	account := &Account{
		Platform: PlatformGrok,
		Type:     AccountTypeOAuth,
	}

	require.True(t, account.SupportsOpenAIImageCapability(""))
	require.False(t, account.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic))
}

func TestAccountSupportsOpenAIEndpointCapability(t *testing.T) {
	t.Run("OpenAI APIKey 默认兼容 chat、embeddings 和 alpha search", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityEmbeddings))
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
	})

	t.Run("OpenAI OAuth 默认仅兼容 chat", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityEmbeddings))
	})

	t.Run("alpha search 允许 OpenAI OAuth/PAT 与 APIKey 账号，拒绝 Grok", func(t *testing.T) {
		// OAuth/PAT 走 chatgpt.com Codex 端点，APIKey 走 {base_url}/v1/alpha/search，
		// 两类都能承接独立搜索（APIKey 被排除曾导致纯 APIKey 分组搜索失效的回归）。
		apiKey := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
		}
		oauth := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
		}
		grok := &Account{
			Platform: PlatformGrok,
			Type:     AccountTypeAPIKey,
		}

		require.True(t, apiKey.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
		require.True(t, oauth.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
		require.False(t, grok.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
	})

	t.Run("显式列表支持同时声明 chat 和 embeddings", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions", "embeddings"},
			},
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityEmbeddings))
	})

	t.Run("显式列表只声明 chat 时不支持 embeddings", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions"},
			},
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
		// chat 能力隐含放行 alpha search（OAuth/APIKey 语义一致）。
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityEmbeddings))
	})

	t.Run("OAuth 显式列表沿用 chat 能力放行 alpha search", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Credentials: map[string]any{
				"openai_capabilities": []any{"chat_completions"},
			},
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityAlphaSearch))
	})

	t.Run("显式 map 支持单独关闭 chat 并开启 embeddings", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"openai_capabilities": map[string]any{
					"chat_completions": false,
					"embeddings":       true,
				},
			},
		}

		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityEmbeddings))
	})

	t.Run("未知能力不应默认放行", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
		}

		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapability("unknown")))
	})

	t.Run("responses 能力：未探测的 APIKey 默认放行", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	})

	t.Run("responses 能力：探测确认不支持的 APIKey 被排除", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Extra:    map[string]any{"openai_responses_supported": false},
		}

		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
		// 非生图路径仍可选中（只要求 chat_completions）。
		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
	})

	t.Run("responses 能力：探测确认支持的 APIKey 放行", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Extra:    map[string]any{"openai_responses_supported": true},
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	})

	t.Run("responses 能力：force_chat_completions 覆盖排除 APIKey", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Extra:    map[string]any{"openai_responses_mode": "force_chat_completions"},
		}

		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	})

	t.Run("responses 能力：OAuth 账号不受探测标记影响", func(t *testing.T) {
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeOAuth,
			Extra:    map[string]any{"openai_responses_supported": false},
		}

		require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	})

	t.Run("responses 能力：仍需通过 chat_completions 配置集校验", func(t *testing.T) {
		// 未探测（默认支持 responses），但显式能力集未声明 chat_completions。
		account := &Account{
			Platform: PlatformOpenAI,
			Type:     AccountTypeAPIKey,
			Credentials: map[string]any{
				"openai_capabilities": []any{"embeddings"},
			},
		}

		require.False(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityResponses))
	})
}

func TestBuildOpenAIImagesURL_HandlesVersionedBaseURL(t *testing.T) {
	require.Equal(t,
		"https://image-upstream.example/v1/images/generations",
		buildOpenAIImagesURL("https://image-upstream.example/v1", openAIImagesGenerationsEndpoint),
	)
	require.Equal(t,
		"https://open.bigmodel.cn/api/paas/v4/images/generations",
		buildOpenAIImagesURL("https://open.bigmodel.cn/api/paas/v4", openAIImagesGenerationsEndpoint),
	)
	require.Equal(t,
		"https://image-upstream.example/v1/images/edits",
		buildOpenAIImagesURL("https://image-upstream.example/v1/", openAIImagesEditsEndpoint),
	)
	require.Equal(t,
		"https://image-upstream.example/v1/images/generations",
		buildOpenAIImagesURL("https://image-upstream.example", openAIImagesGenerationsEndpoint),
	)
	require.Equal(t,
		"https://image-upstream.example/v1/images/generations",
		buildOpenAIImagesURL("https://image-upstream.example/v1/images/generations", openAIImagesGenerationsEndpoint),
	)
}

type openAIImageTestSSEEvent struct {
	Name string
	Data string
}

func parseOpenAIImageTestSSEEvents(body string) []openAIImageTestSSEEvent {
	chunks := strings.Split(body, "\n\n")
	events := make([]openAIImageTestSSEEvent, 0, len(chunks))
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var event openAIImageTestSSEEvent
		for _, line := range strings.Split(chunk, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				event.Name = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
			case strings.HasPrefix(line, "data: "):
				event.Data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			}
		}
		if event.Name != "" || event.Data != "" {
			events = append(events, event)
		}
	}
	return events
}

func findOpenAIImageTestSSEEvent(events []openAIImageTestSSEEvent, name string) (openAIImageTestSSEEvent, bool) {
	for _, event := range events {
		if event.Name == name {
			return event, true
		}
	}
	return openAIImageTestSSEEvent{}, false
}

func TestOpenAIGatewayServiceForwardImages_OAuthSuperResolutionOnlyFor4K(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		requestSize       string
		wantSuperCalls    int
		wantResultBase64  string
		wantOutputFormat  string
		wantResponseSize  string
		wantImageSizeTier string
	}{
		{
			name:              "2K 请求不触发超分",
			requestSize:       "2048x2048",
			wantSuperCalls:    0,
			wantResultBase64:  "b3JpZ2luYWw=",
			wantOutputFormat:  "webp",
			wantResponseSize:  "2048x2048",
			wantImageSizeTier: "2K",
		},
		{
			name:              "4K 请求触发超分",
			requestSize:       "3840x2160",
			wantSuperCalls:    1,
			wantResultBase64:  "dXBzY2FsZWQtcG5n",
			wantOutputFormat:  "png",
			wantResponseSize:  "3840x2160",
			wantImageSizeTier: "4K",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			superCalls := 0
			superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				superCalls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write([]byte("upscaled-png"))
			}))
			defer superServer.Close()

			body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"` + tt.requestSize + `","response_format":"b64_json"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req
			c.Set("api_key", &APIKey{
				ID: 42,
				Group: &Group{
					ID:                          7,
					AllowImageGeneration:        true,
					ImageSuperResolutionEnabled: true,
				},
			})

			svc := &OpenAIGatewayService{
				cfg: &config.Config{Gateway: config.GatewayConfig{
					ImageSuperResolutionURL: superServer.URL,
				}},
				httpUpstream: &httpUpstreamRecorder{
					resp: &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"text/event-stream"},
							"X-Request-Id": []string{"req_img_super"},
						},
						Body: io.NopCloser(strings.NewReader(
							"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000000,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-2\",\"output_format\":\"webp\",\"size\":\"" + tt.requestSize + "\"}],\"output\":[{\"type\":\"image_generation_call\",\"result\":\"b3JpZ2luYWw=\",\"output_format\":\"webp\",\"size\":\"" + tt.requestSize + "\"}]}}\n\n" +
								"data: [DONE]\n\n",
						)),
					},
				},
			}

			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)

			account := &Account{
				ID:       1,
				Name:     "openai-oauth",
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
				Credentials: map[string]any{
					"access_token": "token-123",
				},
			}

			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantImageSizeTier, result.ImageSize)
			require.Equal(t, tt.requestSize, result.ImageInputSize)
			require.Equal(t, tt.wantSuperCalls, superCalls)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tt.wantResultBase64, gjson.Get(rec.Body.String(), "data.0.b64_json").String())
			require.Equal(t, tt.wantOutputFormat, gjson.Get(rec.Body.String(), "output_format").String())
			require.Equal(t, tt.wantResponseSize, gjson.Get(rec.Body.String(), "size").String())
		})
	}
}

func TestOpenAIGatewayServiceForwardImages_APIKeySuperResolutionSkipsNon4K(t *testing.T) {
	gin.SetMode(gin.TestMode)
	superCalls := 0
	superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		superCalls++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("upscaled-png"))
	}))
	defer superServer.Close()

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"2048x2048","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                          7,
			AllowImageGeneration:        true,
			ImageSuperResolutionEnabled: true,
		},
	})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ImageSuperResolutionURL: superServer.URL,
		}},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}]}`)),
			},
		},
	}

	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "openai-apikey",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-test"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, ImageBillingSize2K, result.ImageSize)
	require.Equal(t, "2048x2048", result.ImageInputSize)
	require.Equal(t, 0, superCalls)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "b3JpZ2luYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementUsesTargetImageGroupAndOriginalSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image4KEnhancementEnabled: true,
			Image4KEnhancementGroupID: &targetGroupID,
		},
	})

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_4k_enhance"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,dXBzY2FsZWQ="}}],
					"usage":{"prompt_tokens":9,"completion_tokens":2}
				}`)),
			},
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, targetGroupID, []Account{openAIImages4KEnhancementTargetAccount(targetGroupID)})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "dXBzY2FsZWQ=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, "3840x2160", gjson.Get(rec.Body.String(), "size").String())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.requests[1].URL.String())
	require.Equal(t, "banana-upstream-model", gjson.GetBytes(upstream.bodies[1], "model").String())
	require.Equal(t, "4K", gjson.GetBytes(upstream.bodies[1], "generationConfig.imageConfig.imageSize").String())
	require.Equal(t, "16:9", gjson.GetBytes(upstream.bodies[1], "generationConfig.imageConfig.aspectRatio").String())

	content := gjson.GetBytes(upstream.bodies[1], "messages.0.content")
	require.True(t, content.IsArray())
	promptText := content.Get("0.text").String()
	require.Contains(t, promptText, "3840x2160")
	require.Contains(t, strings.ToLower(promptText), "do not change")
	require.Contains(t, strings.ToLower(promptText), "composition")
	require.Contains(t, strings.ToLower(promptText), "text")
	require.Equal(t, "data:image/png;base64,b3JpZ2luYWw=", content.Get("1.image_url.url").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey2KEnhancementUpscalesLocallyToExplicitSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a product photo","size":"2048x2048","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image2KEnhancementEnabled: true,
		},
	})

	srcB64 := base64.StdEncoding.EncodeToString(encodeOpenAIImagesTestPNG(t, 320, 180))
	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"` + srcB64 + `"}],"model":"gpt-image-2","size":"2048x2048"}`)),
			},
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, 46, nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, ImageBillingSize2K, result.ImageSize)
	// 仅 1 次上游调用 = 原始生成；本地放大不产生第二段网络请求。
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "2048x2048", gjson.Get(rec.Body.String(), "size").String())

	outputB64 := gjson.Get(rec.Body.String(), "data.0.b64_json").String()
	require.NotEqual(t, srcB64, outputB64)
	outputBytes, err := base64.StdEncoding.DecodeString(outputB64)
	require.NoError(t, err)
	cfg, format, err := image.DecodeConfig(bytes.NewReader(outputBytes))
	require.NoError(t, err)
	require.Equal(t, "png", format)
	require.Equal(t, 2048, cfg.Width)
	require.Equal(t, 2048, cfg.Height)
}

func TestOpenAIGatewayServiceForwardImages_APIKey2KEnhancementSupportsPresetSizes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		size       string
		wantWidth  int
		wantHeight int
	}{
		{size: "1536x1024", wantWidth: 1536, wantHeight: 1024},
		{size: "1024x1536", wantWidth: 1024, wantHeight: 1536},
		{size: "2048x2048", wantWidth: 2048, wantHeight: 2048},
	}

	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			body := []byte(fmt.Sprintf(`{"model":"gpt-image-2","prompt":"draw a product photo","size":%q,"response_format":"b64_json"}`, tt.size))
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req
			c.Set("api_key", &APIKey{
				ID: 42,
				Group: &Group{
					ID:                        7,
					AllowImageGeneration:      true,
					Image2KEnhancementEnabled: true,
				},
			})

			srcB64 := base64.StdEncoding.EncodeToString(encodeOpenAIImagesTestPNG(t, 320, 180))
			upstream := &httpUpstreamRecorder{
				responses: []*http.Response{
					{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"` + srcB64 + `"}],"model":"gpt-image-2"}`)),
					},
				},
			}
			svc := newOpenAIImages4KEnhancementTestService(upstream, 46, nil)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			require.Equal(t, ImageBillingSize2K, parsed.SizeTier)

			account := &Account{
				ID:          1,
				Name:        "image2",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-image2"},
			}

			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, ImageBillingSize2K, result.ImageSize)
			// 2K 本地超分只使用原始生成结果，不会二次调用上游。
			require.Len(t, upstream.requests, 1)
			require.Equal(t, tt.size, gjson.Get(rec.Body.String(), "size").String())

			outputB64 := gjson.Get(rec.Body.String(), "data.0.b64_json").String()
			require.NotEqual(t, srcB64, outputB64)
			outputBytes, err := base64.StdEncoding.DecodeString(outputB64)
			require.NoError(t, err)
			cfg, format, err := image.DecodeConfig(bytes.NewReader(outputBytes))
			require.NoError(t, err)
			require.Equal(t, "png", format)
			require.Equal(t, tt.wantWidth, cfg.Width)
			require.Equal(t, tt.wantHeight, cfg.Height)
		})
	}
}

func TestOpenAIGatewayServiceForwardImages_APIKey2KEnhancementUpscalesLocallyByLongestEdge(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// size 用 "2K" 关键字（无具体像素）→ 长边放大到 2048，保持宽高比。
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a product photo","size":"2K","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image2KEnhancementEnabled: true,
		},
	})

	srcB64 := base64.StdEncoding.EncodeToString(encodeOpenAIImagesTestPNG(t, 512, 256))
	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"` + srcB64 + `"}],"model":"gpt-image-2"}`)),
			},
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, 46, nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	_, err = svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, upstream.requests, 1)

	outputBytes, err := base64.StdEncoding.DecodeString(gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.NoError(t, err)
	cfg, _, err := image.DecodeConfig(bytes.NewReader(outputBytes))
	require.NoError(t, err)
	// 512x256 长边 512 → 放大 4 倍 → 2048x1024。
	require.Equal(t, 2048, cfg.Width)
	require.Equal(t, 1024, cfg.Height)
}

func TestOpenAIGatewayServiceForwardImages_APIKey2KEnhancementSkipsWhenAlreadyAtLeast2K(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 无显式像素（"2K" 关键字），原图长边已 ≥ 2048 → 不放大，原样返回。
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a product photo","size":"2K","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image2KEnhancementEnabled: true,
		},
	})

	srcB64 := base64.StdEncoding.EncodeToString(encodeOpenAIImagesTestPNG(t, 2048, 1152))
	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"` + srcB64 + `"}],"model":"gpt-image-2"}`)),
			},
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, 46, nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	_, err = svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, upstream.requests, 1)
	// 原图未被放大，b64 保持不变。
	require.Equal(t, srcB64, gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey2KEnhancementSkipsImplicitDefaultSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a product photo","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image2KEnhancementEnabled: true,
			Image2KEnhancementGroupID: &targetGroupID,
		},
	})

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2"}`)),
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, targetGroupID, []Account{openAIImages4KEnhancementTargetAccount(targetGroupID)})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.False(t, parsed.ExplicitSize)
	require.Equal(t, ImageBillingSize2K, parsed.SizeTier)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "b3JpZ2luYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://api.openai.com/v1/images/generations", upstream.requests[0].URL.String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey2KEnhancementSkipsAutoSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a product photo","size":"auto","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image2KEnhancementEnabled: true,
			Image2KEnhancementGroupID: &targetGroupID,
		},
	})

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2"}`)),
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, targetGroupID, []Account{openAIImages4KEnhancementTargetAccount(targetGroupID)})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	// size="auto" 会让 ExplicitSize 为 true，但不应被当作显式 2K 触发计费的二段提升。
	require.True(t, parsed.ExplicitSize)
	require.Equal(t, ImageBillingSize2K, parsed.SizeTier)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "b3JpZ2luYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Len(t, upstream.requests, 1)
	require.Equal(t, "https://api.openai.com/v1/images/generations", upstream.requests[0].URL.String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementUsesTargetAccountImageModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image4KEnhancementEnabled: true,
			Image4KEnhancementGroupID: &targetGroupID,
		},
	})

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_4k_enhance"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,dXBzY2FsZWQ="}}],
					"usage":{"prompt_tokens":9,"completion_tokens":2}
				}`)),
			},
		},
	}
	targetAccount := openAIImages4KEnhancementTargetAccount(targetGroupID)
	targetAccount.Credentials["model_mapping"] = map[string]any{
		"nano-banana-2": "gemini-3.1-flash-image",
	}
	svc := newOpenAIImages4KEnhancementTestServiceWithChannelMapping(upstream, targetGroupID, []Account{targetAccount}, nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "dXBzY2FsZWQ=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.requests[1].URL.String())
	require.Equal(t, "gemini-3.1-flash-image", gjson.GetBytes(upstream.bodies[1], "model").String())
	require.Contains(t, gjson.GetBytes(upstream.bodies[1], "messages.0.content.0.text").String(), "3840x2160")
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementUsesConfiguredTargetModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	targetModel := "nano-banana-2"
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image4KEnhancementEnabled: true,
			Image4KEnhancementGroupID: &targetGroupID,
			Image4KEnhancementModel:   &targetModel,
		},
	})

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_4k_enhance"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,dXBzY2FsZWQ="}}],
					"usage":{"prompt_tokens":9,"completion_tokens":2}
				}`)),
			},
		},
	}
	targetAccount := openAIImages4KEnhancementTargetAccount(targetGroupID)
	targetAccount.Credentials["model_mapping"] = map[string]any{
		"nano-banana-2": "gemini-3.1-flash-image",
	}
	svc := newOpenAIImages4KEnhancementTestServiceWithChannelMapping(upstream, targetGroupID, []Account{targetAccount}, nil)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "dXBzY2FsZWQ=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.requests[1].URL.String())
	require.Equal(t, "gemini-3.1-flash-image", gjson.GetBytes(upstream.bodies[1], "model").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementAutoUsesChatCompletionsForNonNativeTargetModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	targetModel := "nano-banana-2"
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image4KEnhancementEnabled: true,
			Image4KEnhancementGroupID: &targetGroupID,
			Image4KEnhancementModel:   &targetModel,
		},
	})

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_4k_enhance_auto_cc"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,dXBzY2FsZWQ="}}],
					"usage":{"prompt_tokens":9,"completion_tokens":2}
				}`)),
			},
		},
	}
	targetAccount := openAIImages4KEnhancementTargetAccount(targetGroupID)
	targetAccount.Credentials["model_mapping"] = map[string]any{
		"nano-banana-2": "gemini-3.1-flash-image",
	}
	svc := newOpenAIImages4KEnhancementTestServiceWithChannel(upstream, targetGroupID, []Account{targetAccount}, Channel{
		ID:       100,
		Name:     "nano-Banana2 香蕉生图",
		Status:   StatusActive,
		GroupIDs: []int64{targetGroupID},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: nil,
		},
	})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "dXBzY2FsZWQ=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.requests[1].URL.String())
	require.Equal(t, "gemini-3.1-flash-image", gjson.GetBytes(upstream.bodies[1], "model").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementKeepsNativeImagesForNativeMappedTargetModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	targetModel := "custom-image-alias"
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image4KEnhancementEnabled: true,
			Image4KEnhancementGroupID: &targetGroupID,
			Image4KEnhancementModel:   &targetModel,
		},
	})

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000001,"data":[{"b64_json":"dXBzY2FsZWQ="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
		},
	}
	targetAccount := openAIImages4KEnhancementTargetAccount(targetGroupID)
	targetAccount.Credentials["model_mapping"] = map[string]any{
		"custom-image-alias": "gpt-image-2",
	}
	svc := newOpenAIImages4KEnhancementTestServiceWithChannel(upstream, targetGroupID, []Account{targetAccount}, Channel{
		ID:       100,
		Name:     "native image",
		Status:   StatusActive,
		GroupIDs: []int64{targetGroupID},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: nil,
		},
	})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://banana-upstream.example/v1/images/edits", upstream.requests[1].URL.String())
	require.Contains(t, upstream.requests[1].Header.Get("Content-Type"), "multipart/form-data")
	require.Contains(t, string(upstream.bodies[1]), `name="model"`)
	require.Contains(t, string(upstream.bodies[1]), "gpt-image-2")
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementResizesEnhancedImageToRequestedSize(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		mimeType   string
		imageBytes []byte
		wantFormat string
	}{
		{
			name:       "PNG",
			mimeType:   "image/png",
			imageBytes: encodeOpenAIImagesTestPNG(t, 320, 180),
			wantFormat: "png",
		},
		{
			name:       "JPEG",
			mimeType:   "image/jpeg",
			imageBytes: encodeOpenAIImagesTestJPEG(t, 320, 180),
			wantFormat: "png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = req
			targetGroupID := int64(46)
			c.Set("api_key", &APIKey{
				ID: 42,
				Group: &Group{
					ID:                        7,
					AllowImageGeneration:      true,
					Image4KEnhancementEnabled: true,
					Image4KEnhancementGroupID: &targetGroupID,
				},
			})

			targetB64 := base64.StdEncoding.EncodeToString(tt.imageBytes)
			upstream := &httpUpstreamRecorder{
				responses: []*http.Response{
					{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
					},
					{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"application/json"},
							"X-Request-Id": []string{"req_img_4k_enhance"},
						},
						Body: io.NopCloser(strings.NewReader(`{
							"choices":[{"message":{"role":"assistant","content":"data:` + tt.mimeType + `;base64,` + targetB64 + `"}}],
							"usage":{"prompt_tokens":9,"completion_tokens":2}
						}`)),
					},
				},
			}
			svc := newOpenAIImages4KEnhancementTestService(upstream, targetGroupID, []Account{openAIImages4KEnhancementTargetAccount(targetGroupID)})
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)

			account := &Account{
				ID:          1,
				Name:        "image2",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "sk-image2"},
			}

			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, "3840x2160", gjson.Get(rec.Body.String(), "size").String())
			require.Equal(t, "3840x2160", gjson.Get(rec.Body.String(), "data.0.size").String())
			require.Equal(t, tt.wantFormat, gjson.Get(rec.Body.String(), "output_format").String())
			require.Equal(t, []string{"3840x2160"}, result.ImageOutputSizes)

			outputB64 := gjson.Get(rec.Body.String(), "data.0.b64_json").String()
			outputBytes, err := base64.StdEncoding.DecodeString(outputB64)
			require.NoError(t, err)
			cfg, format, err := image.DecodeConfig(bytes.NewReader(outputBytes))
			require.NoError(t, err)
			require.Equal(t, tt.wantFormat, format)
			require.Equal(t, 3840, cfg.Width)
			require.Equal(t, 2160, cfg.Height)
			require.NotEqual(t, targetB64, outputB64)
		})
	}
}

func TestBuildOpenAIImagesAPIResponseUsesInlineImageDimensionsForDataSize(t *testing.T) {
	img := encodeOpenAIImagesTestPNG(t, 4, 3)
	body, err := buildOpenAIImagesAPIResponse([]openAIResponsesImageResult{{
		Result: base64.StdEncoding.EncodeToString(img),
		Size:   "3840x2160",
	}}, 1710000000, nil, openAIResponsesImageResult{
		Size: "3840x2160",
	}, "b64_json")
	require.NoError(t, err)

	require.Equal(t, "4x3", gjson.GetBytes(body, "data.0.size").String())
	require.Equal(t, "3840x2160", gjson.GetBytes(body, "size").String())
	require.Equal(t, []string{"4x3"}, collectOpenAIResponseImageOutputSizesFromJSONBytes(body))
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementFallsBackAfterThreeTargetFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                        7,
			AllowImageGeneration:      true,
			Image4KEnhancementEnabled: true,
			Image4KEnhancementGroupID: &targetGroupID,
		},
	})

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
			{
				StatusCode: http.StatusBadGateway,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"banana temporarily unavailable"}}`)),
			},
			{
				StatusCode: http.StatusBadGateway,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"banana temporarily unavailable"}}`)),
			},
			{
				StatusCode: http.StatusBadGateway,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"banana temporarily unavailable"}}`)),
			},
		},
	}
	svc := newOpenAIImages4KEnhancementTestService(upstream, targetGroupID, []Account{openAIImages4KEnhancementTargetAccount(targetGroupID)})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "b3JpZ2luYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Len(t, upstream.requests, 4)
	require.Equal(t, "https://api.openai.com/v1/images/generations", upstream.requests[0].URL.String())
	for i := 1; i <= 3; i++ {
		require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.requests[i].URL.String())
		require.Contains(t, gjson.GetBytes(upstream.bodies[i], "messages.0.content.0.text").String(), "3840x2160")
	}
}

func TestOpenAIGatewayServiceForwardImages_APIKey4KEnhancementBlocksLegacySuperResolutionWhenTargetMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	superCalls := 0
	superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		superCalls++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("legacy-upscaled-png"))
	}))
	defer superServer.Close()

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a city skyline","size":"3840x2160","response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                          7,
			AllowImageGeneration:        true,
			Image4KEnhancementEnabled:   true,
			ImageSuperResolutionEnabled: true,
		},
	})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ImageSuperResolutionURL: superServer.URL,
		}},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"created":1710000000,"data":[{"b64_json":"b3JpZ2luYWw="}],"model":"gpt-image-2","size":"3840x2160"}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:          1,
		Name:        "image2",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-image2"},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "b3JpZ2luYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, 0, superCalls)
}

func newOpenAIImages4KEnhancementTestService(upstream *httpUpstreamRecorder, targetGroupID int64, targetAccounts []Account) *OpenAIGatewayService {
	return newOpenAIImages4KEnhancementTestServiceWithChannelMapping(upstream, targetGroupID, targetAccounts, map[string]string{
		"gpt-image-2": "banana-upstream-model",
	})
}

func newOpenAIImages4KEnhancementTestServiceWithChannelMapping(upstream *httpUpstreamRecorder, targetGroupID int64, targetAccounts []Account, mapping map[string]string) *OpenAIGatewayService {
	return newOpenAIImages4KEnhancementTestServiceWithChannel(upstream, targetGroupID, targetAccounts, Channel{
		ID:       100,
		Name:     "nano-Banana2 香蕉生图",
		Status:   StatusActive,
		GroupIDs: []int64{targetGroupID},
		FeaturesConfig: map[string]any{
			featureKeyOpenAIImagesUpstream: map[string]any{"mode": openAIImagesUpstreamModeChatCompletions},
		},
		ModelMapping: map[string]map[string]string{
			PlatformOpenAI: mapping,
		},
	})
}

func newOpenAIImages4KEnhancementTestServiceWithChannel(upstream *httpUpstreamRecorder, targetGroupID int64, targetAccounts []Account, channel Channel) *OpenAIGatewayService {
	channelRepo := &openAIImages4KEnhancementChannelRepo{
		listAllFn: func(ctx context.Context) ([]Channel, error) {
			return []Channel{channel}, nil
		},
		getGroupPlatformsFn: func(ctx context.Context, groupIDs []int64) (map[int64]string, error) {
			return map[int64]string{targetGroupID: PlatformOpenAI}, nil
		},
	}
	return &OpenAIGatewayService{
		cfg:            &config.Config{},
		httpUpstream:   upstream,
		accountRepo:    &openAIImages4KEnhancementAccountRepo{accountsByGroup: map[int64][]Account{targetGroupID: targetAccounts}},
		channelService: NewChannelService(channelRepo, nil, nil, nil),
	}
}

type openAIImages4KEnhancementChannelRepo struct {
	listAllFn           func(ctx context.Context) ([]Channel, error)
	getGroupPlatformsFn func(ctx context.Context, groupIDs []int64) (map[int64]string, error)
}

func (r *openAIImages4KEnhancementChannelRepo) Create(ctx context.Context, channel *Channel) error {
	panic("unexpected Create call")
}

func (r *openAIImages4KEnhancementChannelRepo) GetByID(ctx context.Context, id int64) (*Channel, error) {
	panic("unexpected GetByID call")
}

func (r *openAIImages4KEnhancementChannelRepo) Update(ctx context.Context, channel *Channel) error {
	panic("unexpected Update call")
}

func (r *openAIImages4KEnhancementChannelRepo) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}

func (r *openAIImages4KEnhancementChannelRepo) List(ctx context.Context, params pagination.PaginationParams, status, search string) ([]Channel, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (r *openAIImages4KEnhancementChannelRepo) ListAll(ctx context.Context) ([]Channel, error) {
	if r.listAllFn != nil {
		return r.listAllFn(ctx)
	}
	return nil, nil
}

func (r *openAIImages4KEnhancementChannelRepo) ExistsByName(ctx context.Context, name string) (bool, error) {
	panic("unexpected ExistsByName call")
}

func (r *openAIImages4KEnhancementChannelRepo) ExistsByNameExcluding(ctx context.Context, name string, excludeID int64) (bool, error) {
	panic("unexpected ExistsByNameExcluding call")
}

func (r *openAIImages4KEnhancementChannelRepo) GetGroupIDs(ctx context.Context, channelID int64) ([]int64, error) {
	panic("unexpected GetGroupIDs call")
}

func (r *openAIImages4KEnhancementChannelRepo) SetGroupIDs(ctx context.Context, channelID int64, groupIDs []int64) error {
	panic("unexpected SetGroupIDs call")
}

func (r *openAIImages4KEnhancementChannelRepo) GetChannelIDByGroupID(ctx context.Context, groupID int64) (int64, error) {
	panic("unexpected GetChannelIDByGroupID call")
}

func (r *openAIImages4KEnhancementChannelRepo) GetGroupsInOtherChannels(ctx context.Context, channelID int64, groupIDs []int64) ([]int64, error) {
	panic("unexpected GetGroupsInOtherChannels call")
}

func (r *openAIImages4KEnhancementChannelRepo) GetGroupPlatforms(ctx context.Context, groupIDs []int64) (map[int64]string, error) {
	if r.getGroupPlatformsFn != nil {
		return r.getGroupPlatformsFn(ctx, groupIDs)
	}
	return nil, nil
}

func (r *openAIImages4KEnhancementChannelRepo) ListModelPricing(ctx context.Context, channelID int64) ([]ChannelModelPricing, error) {
	return nil, nil
}

func (r *openAIImages4KEnhancementChannelRepo) CreateModelPricing(ctx context.Context, pricing *ChannelModelPricing) error {
	panic("unexpected CreateModelPricing call")
}

func (r *openAIImages4KEnhancementChannelRepo) UpdateModelPricing(ctx context.Context, pricing *ChannelModelPricing) error {
	panic("unexpected UpdateModelPricing call")
}

func (r *openAIImages4KEnhancementChannelRepo) DeleteModelPricing(ctx context.Context, id int64) error {
	panic("unexpected DeleteModelPricing call")
}

func (r *openAIImages4KEnhancementChannelRepo) ReplaceModelPricing(ctx context.Context, channelID int64, pricingList []ChannelModelPricing) error {
	panic("unexpected ReplaceModelPricing call")
}

func openAIImages4KEnhancementTargetAccount(groupID int64) Account {
	return Account{
		ID:          46,
		Name:        "nano-Banana2 香蕉生图",
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Status:      StatusActive,
		Schedulable: true,
		Concurrency: 1,
		GroupIDs:    []int64{groupID},
		Credentials: map[string]any{
			"api_key":  "sk-banana",
			"base_url": "https://banana-upstream.example/v1",
		},
	}
}

func encodeOpenAIImagesTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x * 255) / maxInt(width, 1)),
				G: uint8((y * 255) / maxInt(height, 1)),
				B: 120,
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func encodeOpenAIImagesTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: 180,
				G: uint8((x * 255) / maxInt(width, 1)),
				B: uint8((y * 255) / maxInt(height, 1)),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

type openAIImages4KEnhancementAccountRepo struct {
	accountsByGroup map[int64][]Account
}

func (r *openAIImages4KEnhancementAccountRepo) Create(ctx context.Context, account *Account) error {
	panic("unexpected Create call")
}

func (r *openAIImages4KEnhancementAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	panic("unexpected GetByID call")
}

func (r *openAIImages4KEnhancementAccountRepo) GetByIDs(ctx context.Context, ids []int64) ([]*Account, error) {
	panic("unexpected GetByIDs call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListShadowsByParent(ctx context.Context, parentID int64) ([]*Account, error) {
	panic("unexpected ListShadowsByParent call")
}

func (r *openAIImages4KEnhancementAccountRepo) ExistsByID(ctx context.Context, id int64) (bool, error) {
	panic("unexpected ExistsByID call")
}

func (r *openAIImages4KEnhancementAccountRepo) GetByCRSAccountID(ctx context.Context, crsAccountID string) (*Account, error) {
	panic("unexpected GetByCRSAccountID call")
}

func (r *openAIImages4KEnhancementAccountRepo) FindByExtraField(ctx context.Context, key string, value any) ([]Account, error) {
	panic("unexpected FindByExtraField call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListCRSAccountIDs(ctx context.Context) (map[string]int64, error) {
	panic("unexpected ListCRSAccountIDs call")
}

func (r *openAIImages4KEnhancementAccountRepo) Update(ctx context.Context, account *Account) error {
	panic("unexpected Update call")
}

func (r *openAIImages4KEnhancementAccountRepo) Delete(ctx context.Context, id int64) error {
	panic("unexpected Delete call")
}

func (r *openAIImages4KEnhancementAccountRepo) List(ctx context.Context, params pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected List call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListWithFilters(ctx context.Context, params pagination.PaginationParams, platform, accountType, status, search string, groupID int64, privacyMode string) ([]Account, *pagination.PaginationResult, error) {
	panic("unexpected ListWithFilters call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListByGroup(ctx context.Context, groupID int64) ([]Account, error) {
	panic("unexpected ListByGroup call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListActive(ctx context.Context) ([]Account, error) {
	panic("unexpected ListActive call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListOAuthRefreshCandidates(ctx context.Context) ([]Account, error) {
	panic("unexpected ListOAuthRefreshCandidates call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	panic("unexpected ListByPlatform call")
}

func (r *openAIImages4KEnhancementAccountRepo) UpdateLastUsed(ctx context.Context, id int64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) BatchUpdateLastUsed(ctx context.Context, updates map[int64]time.Time) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) SetError(ctx context.Context, id int64, errorMsg string) error {
	panic("unexpected SetError call")
}

func (r *openAIImages4KEnhancementAccountRepo) ClearError(ctx context.Context, id int64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) SetSchedulable(ctx context.Context, id int64, schedulable bool) error {
	panic("unexpected SetSchedulable call")
}

func (r *openAIImages4KEnhancementAccountRepo) AutoPauseExpiredAccounts(ctx context.Context, now time.Time) (int64, error) {
	return 0, nil
}

func (r *openAIImages4KEnhancementAccountRepo) BindGroups(ctx context.Context, accountID int64, groupIDs []int64) error {
	panic("unexpected BindGroups call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulable(ctx context.Context) ([]Account, error) {
	panic("unexpected ListSchedulable call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupID call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableByPlatform(ctx context.Context, platform string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatform call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableByGroupIDAndPlatform(ctx context.Context, groupID int64, platform string) ([]Account, error) {
	accounts := r.accountsByGroup[groupID]
	out := make([]Account, 0, len(accounts))
	for _, account := range accounts {
		if normalizeOpenAICompatiblePlatform(account.Platform) == normalizeOpenAICompatiblePlatform(platform) {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	panic("unexpected ListSchedulableByPlatforms call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableByGroupIDAndPlatforms(ctx context.Context, groupID int64, platforms []string) ([]Account, error) {
	panic("unexpected ListSchedulableByGroupIDAndPlatforms call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableUngroupedByPlatform(ctx context.Context, platform string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatform call")
}

func (r *openAIImages4KEnhancementAccountRepo) ListSchedulableUngroupedByPlatforms(ctx context.Context, platforms []string) ([]Account, error) {
	panic("unexpected ListSchedulableUngroupedByPlatforms call")
}

func (r *openAIImages4KEnhancementAccountRepo) SetRateLimited(ctx context.Context, id int64, resetAt time.Time) error {
	panic("unexpected SetRateLimited call")
}

func (r *openAIImages4KEnhancementAccountRepo) SetModelRateLimit(ctx context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	panic("unexpected SetModelRateLimit call")
}

func (r *openAIImages4KEnhancementAccountRepo) SetOverloaded(ctx context.Context, id int64, until time.Time) error {
	panic("unexpected SetOverloaded call")
}

func (r *openAIImages4KEnhancementAccountRepo) SetTempUnschedulable(ctx context.Context, id int64, until time.Time, reason string) error {
	panic("unexpected SetTempUnschedulable call")
}

func (r *openAIImages4KEnhancementAccountRepo) ClearTempUnschedulable(ctx context.Context, id int64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) ClearRateLimit(ctx context.Context, id int64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) ClearAntigravityQuotaScopes(ctx context.Context, id int64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) UpdateSessionWindow(ctx context.Context, id int64, start, end *time.Time, status string) error {
	panic("unexpected UpdateSessionWindow call")
}

func (r *openAIImages4KEnhancementAccountRepo) UpdateSessionWindowEnd(ctx context.Context, id int64, end time.Time) error {
	panic("unexpected UpdateSessionWindowEnd call")
}

func (r *openAIImages4KEnhancementAccountRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) BulkUpdate(ctx context.Context, ids []int64, updates AccountBulkUpdate) (int64, error) {
	panic("unexpected BulkUpdate call")
}

func (r *openAIImages4KEnhancementAccountRepo) IncrementQuotaUsed(ctx context.Context, id int64, amount float64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) ResetQuotaUsed(ctx context.Context, id int64) error {
	return nil
}

func (r *openAIImages4KEnhancementAccountRepo) RevertProxyFallback(ctx context.Context, accountID int64) error {
	panic("unexpected RevertProxyFallback call")
}

func (r *openAIImages4KEnhancementAccountRepo) ClearModelRateLimits(ctx context.Context, id int64) error {
	return nil
}

func TestOpenAIGatewayServiceForwardImages_OAuthPassesNAndReturnsAllImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"1024x1024","quality":"high","n":3}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{ID: 42})

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_123"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000000,\"usage\":{\"input_tokens\":11,\"output_tokens\":22,\"input_tokens_details\":{\"cached_tokens\":3},\"output_tokens_details\":{\"image_tokens\":7}},\"tool_usage\":{\"image_gen\":{\"input_tokens\":46,\"output_tokens\":2459,\"output_tokens_details\":{\"image_tokens\":2459},\"images\":3}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"aW1hZ2UtMQ==\",\"revised_prompt\":\"draw a cat 1\",\"output_format\":\"png\",\"quality\":\"high\",\"size\":\"1024x1024\"},{\"type\":\"image_generation_call\",\"result\":\"aW1hZ2UtMg==\",\"revised_prompt\":\"draw a cat 2\",\"output_format\":\"png\",\"quality\":\"high\",\"size\":\"1024x1024\"},{\"type\":\"image_generation_call\",\"result\":\"aW1hZ2UtMw==\",\"revised_prompt\":\"draw a cat 3\",\"output_format\":\"png\",\"quality\":\"high\",\"size\":\"1024x1024\"}]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc.httpUpstream = upstream

	account := &Account{
		ID:       1,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token":       "token-123",
			"chatgpt_account_id": "acct-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "gpt-image-2", result.Model)
	require.Equal(t, "gpt-image-2", result.UpstreamModel)
	require.Equal(t, 3, result.ImageCount)
	require.Equal(t, 46, result.Usage.InputTokens)
	require.Equal(t, 2459, result.Usage.OutputTokens)
	require.Equal(t, 2459, result.Usage.ImageOutputTokens)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, chatgptCodexURL, upstream.lastReq.URL.String())
	require.Equal(t, "chatgpt.com", upstream.lastReq.Host)
	require.Equal(t, HTTPUpstreamProfileOpenAI, HTTPUpstreamProfileFromContext(upstream.lastReq.Context()))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.Equal(t, "text/event-stream", upstream.lastReq.Header.Get("Accept"))
	require.Equal(t, "acct-123", upstream.lastReq.Header.Get("chatgpt-account-id"))
	require.Equal(t, "responses=experimental", upstream.lastReq.Header.Get("OpenAI-Beta"))

	require.Equal(t, openAIImagesResponsesMainModel, gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, gjson.GetBytes(upstream.lastBody, "stream").Bool())
	require.Equal(t, "image_generation", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
	require.Equal(t, "generate", gjson.GetBytes(upstream.lastBody, "tools.0.action").String())
	require.Equal(t, "gpt-image-2", gjson.GetBytes(upstream.lastBody, "tools.0.model").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(upstream.lastBody, "tools.0.size").String())
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "tools.0.quality").String())
	require.Equal(t, int64(3), gjson.GetBytes(upstream.lastBody, "tools.0.n").Int())
	require.Equal(t, "draw a cat", gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gpt-image-2", gjson.Get(rec.Body.String(), "model").String())
	require.Len(t, gjson.Get(rec.Body.String(), "data").Array(), 3)
	require.Equal(t, "aW1hZ2UtMQ==", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, "aW1hZ2UtMg==", gjson.Get(rec.Body.String(), "data.1.b64_json").String())
	require.Equal(t, "aW1hZ2UtMw==", gjson.Get(rec.Body.String(), "data.2.b64_json").String())
	require.Equal(t, "draw a cat 1", gjson.Get(rec.Body.String(), "data.0.revised_prompt").String())
	require.Equal(t, "draw a cat 3", gjson.Get(rec.Body.String(), "data.2.revised_prompt").String())
}

func TestParseOpenAIImagesSSEUsageBytes_ToolUsagePrecedenceAndFallback(t *testing.T) {
	svc := &OpenAIGatewayService{}
	fallback := OpenAIUsage{InputTokens: 3, OutputTokens: 4, ImageOutputTokens: 2}
	tests := []struct {
		name      string
		toolUsage string
		want      OpenAIUsage
	}{
		{
			name:      "valid tool usage takes atomic precedence",
			toolUsage: `{"input_tokens":4.6e1,"output_tokens":2459e0,"output_tokens_details":{"image_tokens":24590e-1}}`,
			want:      OpenAIUsage{InputTokens: 46, OutputTokens: 2459, ImageOutputTokens: 2459},
		},
		{name: "absent", want: fallback},
		{name: "malformed field", toolUsage: `{"input_tokens":"46","output_tokens":2459,"output_tokens_details":{"image_tokens":2459}}`, want: fallback},
		{name: "fractional field", toolUsage: `{"input_tokens":46,"output_tokens":2459.5,"output_tokens_details":{"image_tokens":2459}}`, want: fallback},
		{name: "negative field", toolUsage: `{"input_tokens":46,"output_tokens":2459,"output_tokens_details":{"image_tokens":-1}}`, want: fallback},
		{name: "overflow field", toolUsage: `{"input_tokens":46,"output_tokens":9223372036854775808,"output_tokens_details":{"image_tokens":2459}}`, want: fallback},
		{name: "incomplete object", toolUsage: `{"input_tokens":46,"output_tokens":2459}`, want: fallback},
		{name: "hostile huge exponent", toolUsage: `{"input_tokens":1e1000000000,"output_tokens":2459,"output_tokens_details":{"image_tokens":2459}}`, want: fallback},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			toolUsageField := ""
			if tt.toolUsage != "" {
				toolUsageField = `,"tool_usage":{"image_gen":` + tt.toolUsage + `}`
			}
			payload := []byte(`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":4,"output_tokens_details":{"image_tokens":2}}` + toolUsageField + `}}`)
			var got OpenAIUsage
			svc.parseOpenAIImagesSSEUsageBytes(payload, &got)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestParseOpenAIImagesSSEUsageBytes_MalformedCompletedDoesNotOverrideUsage(t *testing.T) {
	svc := &OpenAIGatewayService{}
	var usage OpenAIUsage

	svc.parseOpenAIImagesSSEUsageBytes([]byte(`{"type":"response.output_item.done","item":{"type":"image_generation_call","result":"aW1hZ2U="}}`), &usage)
	svc.parseOpenAIImagesSSEUsageBytes([]byte(`{"type":"response.completed","response":{"usage":{"input_tokens":3,"output_tokens":4,"output_tokens_details":{"image_tokens":2}}}}`), &usage)
	svc.parseOpenAIImagesSSEUsageBytes([]byte(`{"type":"response.completed","response":{"tool_usage":{"image_gen":{"input_tokens":46,"output_tokens":2459,"output_tokens_details":{"image_tokens":2459}}}}} trailing`), &usage)

	require.Equal(t, OpenAIUsage{InputTokens: 3, OutputTokens: 4, ImageOutputTokens: 2}, usage)
}

func TestBoundedJSONNonNegativeInt(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
		ok   bool
	}{
		{name: "scale reduction before accumulation", raw: `10000000000000000000e-19`, want: 1, ok: true},
		{name: "decimal scale reduction", raw: `10000000000000000000.0e-19`, want: 1, ok: true},
		{name: "fractional after scale reduction", raw: `10000000000000000001e-19`, ok: false},
		{name: "overflow after scale reduction", raw: `92233720368547758080e-1`, ok: false},
		{name: "zero with negative exponent", raw: `0e-100`, want: 0, ok: true},
		{name: "zero beyond exponent bound", raw: `0e101`, want: 0, ok: true},
		{name: "zero padded decimal beyond exponent bound", raw: `0.000000e+000000000000000000000000000000000000000000000000101`, want: 0, ok: true},
		{name: "zero padded exponent", raw: `1e0000`, want: 1, ok: true},
		{name: "negative zero syntax", raw: `-0e101`, ok: false},
		{name: "hostile exponent", raw: `1e-1000`, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := boundedJSONNonNegativeInt(gjson.Parse(tt.raw))
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestOpenAIGatewayServiceForwardImages_OAuthUpstreamHTTPErrorSurfacesRealError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)

	// The non-failover upstream error path is shared by /generations and /edits;
	// use /generations here so the request parses without an uploaded image.
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{ID: 42})

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	svc.httpUpstream = &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusBadRequest,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_img_badreq"},
			},
			Body: io.NopCloser(strings.NewReader(
				`{"error":{"message":"Invalid value for 'size': expected one of 1024x1024, 1536x1024.","type":"invalid_request_error","param":"size","code":"unknown_parameter"}}`,
			)),
		},
	}

	account := &Account{
		ID:       1,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.Nil(t, result)

	var upstreamErr *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusBadRequest, upstreamErr.StatusCode)
	require.Equal(t, "invalid_request_error", upstreamErr.ErrorType)
	require.Equal(t, "unknown_parameter", upstreamErr.Code)

	// The client must receive the actual upstream status code and message instead
	// of a generic 502 "Upstream request failed".
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "invalid_request_error", gjson.Get(rec.Body.String(), "error.type").String())
	require.Equal(t, "unknown_parameter", gjson.Get(rec.Body.String(), "error.code").String())
	require.Equal(t, "size", gjson.Get(rec.Body.String(), "error.param").String())
	require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "Invalid value for 'size'")
}

func TestOpenAIGatewayServiceForwardImages_OAuthNonStreamModerationBlockedReturnsClientError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw blocked image","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{ID: 42})

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	svc.httpUpstream = &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_blocked"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000020}}\n\n" +
					"data: {\"type\":\"error\",\"error\":{\"type\":\"image_generation_user_error\",\"code\":\"moderation_blocked\",\"message\":\"Your request was rejected by the safety system. safety_violations=[sexual].\"}}\n\n" +
					"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_blocked\",\"status\":\"failed\",\"error\":{\"type\":\"image_generation_user_error\",\"code\":\"moderation_blocked\",\"message\":\"Your request was rejected by the safety system. safety_violations=[sexual].\"}}}\n\n",
			)),
		},
	}

	account := &Account{
		ID:       1,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.Nil(t, result)
	var upstreamErr *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.Equal(t, http.StatusBadRequest, upstreamErr.StatusCode)
	require.Equal(t, "moderation_blocked", upstreamErr.Code)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "image_generation_user_error", gjson.Get(rec.Body.String(), "error.type").String())
	require.Equal(t, "moderation_blocked", gjson.Get(rec.Body.String(), "error.code").String())
	require.Contains(t, gjson.Get(rec.Body.String(), "error.message").String(), "safety system")
}

func TestOpenAIGatewayServiceForwardImages_OAuthNonStreamServerErrorReturnsFailoverBeforeFlush(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_server_error"},
				},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000021}}\n\n" +
						"data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"The image service is temporarily unavailable.\"}}\n\n",
				)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := &Account{
		ID:       21,
		Name:     "openai-oauth-server-error",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.Contains(t, string(failoverErr.ResponseBody), "temporarily unavailable")
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "failover", events[0].Kind)
	require.Equal(t, account.ID, events[0].AccountID)
	require.Equal(t, http.StatusBadGateway, events[0].UpstreamStatusCode)
}

func TestOpenAIGatewayServiceForwardImages_OAuthStreamServerErrorAfterFlushDoesNotFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_server_error_after_partial"},
				},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"cGFydGlhbA==\",\"partial_image_index\":0,\"output_format\":\"png\"}\n\n" +
						"data: {\"type\":\"error\",\"error\":{\"type\":\"server_error\",\"code\":\"server_error\",\"message\":\"The image service failed after partial output.\"}}\n\n",
				)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := &Account{
		ID:       22,
		Name:     "openai-oauth-partial-server-error",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.False(t, errors.As(err, &failoverErr))
	var upstreamErr *OpenAIImagesUpstreamError
	require.ErrorAs(t, err, &upstreamErr)
	require.True(t, IsOpenAIImagesRetryableUpstreamError(upstreamErr))
	require.True(t, c.Writer.Written())
	require.Contains(t, rec.Body.String(), "event: image_generation.partial_image")
	require.Contains(t, rec.Body.String(), "event: error")
	require.Contains(t, rec.Body.String(), "failed after partial output")

	rawEvents, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events, ok := rawEvents.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, events, 1)
	require.Equal(t, "retry_exhausted_failover", events[0].Kind)
	require.Equal(t, account.ID, events[0].AccountID)
}

func TestOpenAIImagesSSEClientErrorsAreNotRetryable(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		wantStatus int
	}{
		{
			name:       "invalid request",
			payload:    `{"type":"error","error":{"type":"invalid_request_error","code":"invalid_value","message":"bad size"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "content policy",
			payload:    `{"type":"error","error":{"type":"image_generation_user_error","code":"content_policy_violation","message":"blocked"}}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "rate limit remains distinct from server error",
			payload:    `{"type":"error","error":{"type":"rate_limit_exceeded","code":"rate_limit_exceeded","message":"try again"}}`,
			wantStatus: http.StatusTooManyRequests,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstreamErr := openAIImagesUpstreamErrorFromSSEPayload([]byte(tt.payload))
			require.NotNil(t, upstreamErr)
			require.Equal(t, tt.wantStatus, upstreamErr.StatusCode)
			require.False(t, IsOpenAIImagesRetryableUpstreamError(upstreamErr))
		})
	}
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationUsesConfiguredV1BaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_apikey"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000007,"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       6,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "gpt-image-2", result.Model)
	require.Equal(t, "gpt-image-2", result.UpstreamModel)

	upstream, ok := svc.httpUpstream.(*httpUpstreamRecorder)
	require.True(t, ok)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "application/json", upstream.lastReq.Header.Get("Content-Type"))
	require.Equal(t, "gpt-image-2", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "aGVsbG8=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationViaChatCompletions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"nano-banana-2","prompt":"draw a banana","response_format":"url"}`)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_img_cc"},
			},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"id":"chatcmpl_1",
				"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,%s"}}],
				"usage":{"prompt_tokens":12,"completion_tokens":3}
			}`, encodedImage))),
		},
	}
	svc := &OpenAIGatewayService{
		cfg:                 &config.Config{},
		httpUpstream:        upstream,
		generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()}),
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       7,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://banana-upstream.example/v1",
			"model_mapping": map[string]any{
				"nano-banana-2": "banana-upstream-model",
			},
		},
	}
	channel := &Channel{FeaturesConfig: map[string]any{
		featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
	}}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "", channel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "nano-banana-2", result.Model)
	require.Equal(t, "banana-upstream-model", result.UpstreamModel)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 3, result.Usage.OutputTokens)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "banana-upstream-model", gjson.GetBytes(upstream.lastBody, "model").String())
	require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "messages.0.role").String())
	require.Contains(t, gjson.GetBytes(upstream.lastBody, "messages.0.content").String(), "draw a banana")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(rec.Body.String(), "data.0.url").String())
	require.Equal(t, "nano-banana-2", gjson.Get(rec.Body.String(), "model").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationViaChatCompletionsDownloadsURLForB64(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"nano-banana-2","prompt":"draw a banana","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_cc_b64"},
				},
				Body: io.NopCloser(strings.NewReader(`{
					"choices":[{"message":{"role":"assistant","content":"https://cdn.example.com/banana.png"}}],
					"usage":{"prompt_tokens":9,"completion_tokens":2}
				}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader("png-bytes")),
			},
		},
	}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       9,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://banana-upstream.example/v1",
		},
	}
	channel := &Channel{FeaturesConfig: map[string]any{
		featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
	}}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "", channel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.requests[0].URL.String())
	require.Equal(t, "https://cdn.example.com/banana.png", upstream.requests[1].URL.String())
	require.Equal(t, "cG5nLWJ5dGVz", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationViaChatCompletionsReadsMessageImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"nano-banana","prompt":"draw a banana","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_img_cc_message_images"},
			},
			Body: io.NopCloser(strings.NewReader(`{
				"choices":[{"message":{"role":"assistant","content":null,"images":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,aW1hZ2UtYnl0ZXM="}}]}}],
				"usage":{"prompt_tokens":9,"completion_tokens":2}
			}`)),
		},
	}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       9,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":       "test-api-key",
			"base_url":      "https://banana-upstream.example/v1",
			"model_mapping": map[string]any{"nano-banana": "gemini-3.1-flash-image"},
		},
	}
	channel := &Channel{FeaturesConfig: map[string]any{
		featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
	}}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "", channel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "nano-banana", result.Model)
	require.Equal(t, "gemini-3.1-flash-image", result.UpstreamModel)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("image-bytes")), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationViaChatCompletionsForwardsImageConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"nano-banana","prompt":"draw a banana","size":"3840x2160","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_img_cc_image_config"},
			},
			Body: io.NopCloser(strings.NewReader(`{
				"choices":[{"message":{"role":"assistant","content":null,"images":[{"type":"image_url","image_url":{"url":"data:image/jpeg;base64,aW1hZ2UtYnl0ZXM="}}]}}],
				"usage":{"prompt_tokens":9,"completion_tokens":2}
			}`)),
		},
	}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       9,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":       "test-api-key",
			"base_url":      "https://banana-upstream.example/v1",
			"model_mapping": map[string]any{"nano-banana": "gemini-3.1-flash-image"},
		},
	}
	channel := &Channel{FeaturesConfig: map[string]any{
		featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
	}}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "", channel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "4K", result.ImageSize)
	require.Equal(t, "3840x2160", result.ImageInputSize)
	require.Equal(t, "4K", gjson.GetBytes(upstream.lastBody, "generationConfig.imageConfig.imageSize").String())
	require.Equal(t, "16:9", gjson.GetBytes(upstream.lastBody, "generationConfig.imageConfig.aspectRatio").String())
	require.Contains(t, gjson.GetBytes(upstream.lastBody, "messages.0.content").String(), "Requested image size: 3840x2160")
}

func TestOpenAIGatewayServiceForwardImages_APIKeyJSONEditViaChatCompletionsIncludesImages(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))
	body := []byte(`{
		"model":"nano-banana-2",
		"prompt":"edit this banana",
		"images":[{"image_url":"data:image/png;base64,c291cmNlLWltYWdl"}],
		"response_format":"url"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_img_cc_edit"},
			},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,%s"}}],
				"usage":{"prompt_tokens":18,"completion_tokens":4}
			}`, encodedImage))),
		},
	}
	svc := &OpenAIGatewayService{
		cfg:                 &config.Config{},
		httpUpstream:        upstream,
		generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()}),
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://banana-upstream.example/v1",
		},
	}
	channel := &Channel{FeaturesConfig: map[string]any{
		featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
	}}
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "", channel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "nano-banana-2", result.Model)
	require.Equal(t, "nano-banana-2", result.UpstreamModel)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "user", gjson.GetBytes(upstream.lastBody, "messages.0.role").String())
	require.Equal(t, "text", gjson.GetBytes(upstream.lastBody, "messages.0.content.0.type").String())
	require.Contains(t, gjson.GetBytes(upstream.lastBody, "messages.0.content.0.text").String(), "edit this banana")
	require.Equal(t, "image_url", gjson.GetBytes(upstream.lastBody, "messages.0.content.1.type").String())
	require.Equal(t, "data:image/png;base64,c291cmNlLWltYWdl", gjson.GetBytes(upstream.lastBody, "messages.0.content.1.image_url.url").String())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(rec.Body.String(), "data.0.url").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyMultipartEditViaChatCompletionsIncludesUpload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "nano-banana-2"))
	require.NoError(t, writer.WriteField("prompt", "make it brighter"))
	require.NoError(t, writer.WriteField("response_format", "url"))
	imageHeader := make(textproto.MIMEHeader)
	imageHeader.Set("Content-Disposition", `form-data; name="image"; filename="source.png"`)
	imageHeader.Set("Content-Type", "image/png")
	imagePart, err := writer.CreatePart(imageHeader)
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("source-image"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Request-Id": []string{"req_img_cc_edit_multipart"},
			},
			Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{
				"choices":[{"message":{"role":"assistant","content":"data:image/png;base64,%s"}}],
				"usage":{"prompt_tokens":20,"completion_tokens":5}
			}`, encodedImage))),
		},
	}
	svc := &OpenAIGatewayService{
		cfg:                 &config.Config{},
		httpUpstream:        upstream,
		generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()}),
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body.Bytes())
	require.NoError(t, err)

	account := &Account{
		ID:       10,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://banana-upstream.example/v1",
		},
	}
	channel := &Channel{FeaturesConfig: map[string]any{
		featureKeyOpenAIImagesUpstream: map[string]any{"mode": "chat_completions"},
	}}

	result, err := svc.ForwardImages(context.Background(), c, account, body.Bytes(), parsed, "", channel)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "nano-banana-2", result.Model)

	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://banana-upstream.example/v1/chat/completions", upstream.lastReq.URL.String())
	require.Equal(t, "image_url", gjson.GetBytes(upstream.lastBody, "messages.0.content.1.type").String())
	require.Equal(t, "data:image/png;base64,c291cmNlLWltYWdl", gjson.GetBytes(upstream.lastBody, "messages.0.content.1.image_url.url").String())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(rec.Body.String(), "data.0.url").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGeneration502RetryableOnSameAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg:              &config.Config{},
		rateLimitService: &RateLimitService{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusBadGateway,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_502"},
				},
				Body: io.NopCloser(strings.NewReader(`{"error":{"message":"temporary upstream error"}}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       6,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.Nil(t, result)
	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.True(t, failoverErr.RetryableOnSameAccount)
}

func TestShouldRetryOpenAIImagesSameAccount(t *testing.T) {
	regularAPIKeyAccount := &Account{
		ID:          6,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test-api-key"},
	}
	poolModeAPIKeyAccount := &Account{
		ID:       7,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":   "test-api-key",
			"pool_mode": true,
		},
	}

	tests := []struct {
		name       string
		statusCode int
		account    *Account
		want       bool
	}{
		{name: "nil account", statusCode: http.StatusBadGateway, account: nil, want: false},
		{name: "regular account retries 502", statusCode: http.StatusBadGateway, account: regularAPIKeyAccount, want: true},
		{name: "regular account retries 503", statusCode: http.StatusServiceUnavailable, account: regularAPIKeyAccount, want: true},
		{name: "regular account retries 504", statusCode: http.StatusGatewayTimeout, account: regularAPIKeyAccount, want: true},
		{name: "regular account does not retry 400", statusCode: http.StatusBadRequest, account: regularAPIKeyAccount, want: false},
		{name: "regular account does not retry 429", statusCode: http.StatusTooManyRequests, account: regularAPIKeyAccount, want: false},
		{name: "pool mode keeps 429 retry behavior", statusCode: http.StatusTooManyRequests, account: poolModeAPIKeyAccount, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, shouldRetryOpenAIImagesSameAccount(tt.statusCode, tt.account))
		})
	}
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationInlinesProtectedImageURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_apikey_url"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000007,"data":[{"url":"/images/generated.png","revised_prompt":"draw a cat"}]}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader("png-image-bytes")),
			},
		},
	}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       6,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://image-upstream.example/v1/images/generations", upstream.requests[0].URL.String())
	require.Equal(t, "https://image-upstream.example/images/generated.png", upstream.requests[1].URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.requests[1].Header.Get("Authorization"))
	require.Equal(t, "image/*,*/*;q=0.8", upstream.requests[1].Header.Get("Accept"))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-image-bytes")), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, "/images/generated.png", gjson.Get(rec.Body.String(), "data.0.url").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyGenerationInlinesURLMisplacedInB64JSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	upstream := &httpUpstreamRecorder{
		responses: []*http.Response{
			{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_apikey_bad_b64_url"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000007,"data":[{"b64_json":"/images/generated.png"}]}`)),
			},
			{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader("png-image-bytes")),
			},
		},
	}
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       6,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, upstream.requests, 2)
	require.Equal(t, "https://image-upstream.example/images/generated.png", upstream.requests[1].URL.String())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-image-bytes")), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamJSONResponseBillsImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_stream_json"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000008,"usage":{"input_tokens":12,"output_tokens":21,"output_tokens_details":{"image_tokens":9}},"data":[{"b64_json":"aGVsbG8=","revised_prompt":"draw a cat"}]}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       7,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 12, result.Usage.InputTokens)
	require.Equal(t, 21, result.Usage.OutputTokens)
	require.Equal(t, 9, result.Usage.ImageOutputTokens)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "aGVsbG8=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamRawJSONEventStreamFallbackBillsImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_json_mislabeled"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000009,"usage":{"input_tokens":10,"output_tokens":18,"output_tokens_details":{"image_tokens":8}},"data":[{"b64_json":"ZmluYWw="}]}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 18, result.Usage.OutputTokens)
	require.Equal(t, 8, result.Usage.ImageOutputTokens)
	require.Equal(t, "ZmluYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamMultilineSSEDataBillsImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_multiline"},
				},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"image_generation.completed\",\n" +
						"data: \"usage\":{\"input_tokens\":10,\"output_tokens\":18,\"output_tokens_details\":{\"image_tokens\":8}},\n" +
						"data: \"b64_json\":\"ZmluYWw=\",\"output_format\":\"png\"}\n\n" +
						"data: [DONE]\n\n",
				)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 18, result.Usage.OutputTokens)
	require.Equal(t, 8, result.Usage.ImageOutputTokens)
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamingSuperResolutionFor4KCompletedEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upscaledImage := generatedImageTestPNG(t)
	superCalls := 0
	superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		superCalls++
		require.Equal(t, http.MethodPost, r.Method)
		require.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(upscaledImage)
	}))
	defer superServer.Close()

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"3840x2160","stream":true,"response_format":"url"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                          7,
			AllowImageGeneration:        true,
			ImageSuperResolutionEnabled: true,
		},
	})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ImageSuperResolutionURL: superServer.URL,
		}},
		generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()}),
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_super_apikey"},
				},
				Body: io.NopCloser(strings.NewReader(
					"event: image_generation.partial_image\n" +
						"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cGFydGlhbA==\",\"partial_image_index\":0,\"output_format\":\"webp\"}\n\n" +
						"event: image_generation.completed\n" +
						"data: {\"type\":\"image_generation.completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":18,\"output_tokens_details\":{\"image_tokens\":8}},\"b64_json\":\"b3JpZ2luYWw=\",\"output_format\":\"webp\",\"size\":\"3840x2160\"}\n\n" +
						"data: [DONE]\n\n",
				)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 18, result.Usage.OutputTokens)
	require.Equal(t, 8, result.Usage.ImageOutputTokens)
	require.Equal(t, 1, superCalls)

	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	partial, ok := findOpenAIImageTestSSEEvent(events, "image_generation.partial_image")
	require.True(t, ok)
	require.Equal(t, "cGFydGlhbA==", gjson.Get(partial.Data, "b64_json").String())

	completed, ok := findOpenAIImageTestSSEEvent(events, "image_generation.completed")
	require.True(t, ok)
	require.False(t, gjson.Get(completed.Data, "b64_json").Exists())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(completed.Data, "url").String())
	require.Equal(t, "png", gjson.Get(completed.Data, "output_format").String())
	require.Equal(t, "image/png", gjson.Get(completed.Data, "mime_type").String())
	require.Equal(t, "3840x2160", gjson.Get(completed.Data, "size").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreaming4KEnhancementDoesNotFallbackToLegacySuperResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))
	superCalls := 0
	superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		superCalls++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("legacy-upscaled-stream-png"))
	}))
	defer superServer.Close()

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"3840x2160","stream":true,"response_format":"url"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                          7,
			AllowImageGeneration:        true,
			Image4KEnhancementEnabled:   true,
			Image4KEnhancementGroupID:   &targetGroupID,
			ImageSuperResolutionEnabled: true,
		},
	})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ImageSuperResolutionURL: superServer.URL,
		}},
		generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()}),
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_4k_enhancement_no_legacy"},
				},
				Body: io.NopCloser(strings.NewReader(
					"event: image_generation.completed\n" +
						"data: {\"type\":\"image_generation.completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":18,\"output_tokens_details\":{\"image_tokens\":8}},\"b64_json\":\"" + encodedImage + "\",\"output_format\":\"png\",\"size\":\"3840x2160\"}\n\n" +
						"data: [DONE]\n\n",
				)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "image2",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 0, superCalls)

	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	completed, ok := findOpenAIImageTestSSEEvent(events, "image_generation.completed")
	require.True(t, ok)
	require.False(t, gjson.Get(completed.Data, "b64_json").Exists())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(completed.Data, "url").String())
	require.Equal(t, "png", gjson.Get(completed.Data, "output_format").String())
	require.Equal(t, "3840x2160", gjson.Get(completed.Data, "size").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamJSONFallback4KEnhancementDoesNotFallbackToLegacySuperResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	superCalls := 0
	superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		superCalls++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("legacy-json-fallback-upscaled-png"))
	}))
	defer superServer.Close()

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"3840x2160","stream":true,"response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	targetGroupID := int64(46)
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                          7,
			AllowImageGeneration:        true,
			Image4KEnhancementEnabled:   true,
			Image4KEnhancementGroupID:   &targetGroupID,
			ImageSuperResolutionEnabled: true,
		},
	})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ImageSuperResolutionURL: superServer.URL,
		}},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_json_fallback_4k_enhancement_no_legacy"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000009,"usage":{"input_tokens":10,"output_tokens":18,"output_tokens_details":{"image_tokens":8}},"data":[{"b64_json":"b3JpZ2luYWw="}],"size":"3840x2160"}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "image2",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 0, superCalls)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "b3JpZ2luYWw=", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.False(t, gjson.Get(rec.Body.String(), "output_format").Exists())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamJSONFallbackURLStorageFailureReturnsError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-1","prompt":"draw a cat","stream":true,"response_format":"url"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Host = "attacker.example"
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{ID: 42, Group: &Group{ID: 7, AllowImageGeneration: true}})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(`{"created":1710000009,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(generatedImageTestPNG(t)) + `"}]}`)),
		}},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := &Account{ID: 8, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-api-key"}}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "error", gjson.Get(rec.Body.String(), "type").String())
	require.False(t, gjson.Get(rec.Body.String(), "data.0.b64_json").Exists())
	require.NotContains(t, rec.Body.String(), "attacker.example")
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamJSONFallbackSuperResolutionFor4K(t *testing.T) {
	gin.SetMode(gin.TestMode)
	superCalls := 0
	superServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		superCalls++
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("fallback-upscaled-png"))
	}))
	defer superServer.Close()

	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","size":"3840x2160","stream":true,"response_format":"b64_json"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{
		ID: 42,
		Group: &Group{
			ID:                          7,
			AllowImageGeneration:        true,
			ImageSuperResolutionEnabled: true,
		},
	})

	svc := &OpenAIGatewayService{
		cfg: &config.Config{Gateway: config.GatewayConfig{
			ImageSuperResolutionURL: superServer.URL,
		}},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_json_fallback_super"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000009,"usage":{"input_tokens":10,"output_tokens":18,"output_tokens_details":{"image_tokens":8}},"data":[{"b64_json":"b3JpZ2luYWw="}]}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 10, result.Usage.InputTokens)
	require.Equal(t, 18, result.Usage.OutputTokens)
	require.Equal(t, 8, result.Usage.ImageOutputTokens)
	require.Equal(t, 1, superCalls)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("fallback-upscaled-png")), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, "png", gjson.Get(rec.Body.String(), "output_format").String())
}

func TestExtractOpenAIImagesBillableCountFromJSONBytes_CompletedEvent(t *testing.T) {
	body := []byte(`{"type":"image_generation.completed","b64_json":"ZmluYWw=","usage":{"input_tokens":10,"output_tokens":18}}`)

	require.Equal(t, 1, extractOpenAIImagesBillableCountFromJSONBytes(body))
}

func TestOpenAIGatewayServiceForwardImages_APIKeyEditUsesConfiguredV1BaseURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "replace background"))
	imagePart, err := writer.CreateFormFile("image", "source.png")
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("png-image-content"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_edit_apikey"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000008,"data":[{"b64_json":"ZWRpdGVk","revised_prompt":"replace background"}]}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body.Bytes())
	require.NoError(t, err)

	account := &Account{
		ID:       7,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1/",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body.Bytes(), parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)

	upstream, ok := svc.httpUpstream.(*httpUpstreamRecorder)
	require.True(t, ok)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://image-upstream.example/v1/images/edits", upstream.lastReq.URL.String())
	require.Equal(t, "Bearer test-api-key", upstream.lastReq.Header.Get("Authorization"))
	require.Contains(t, upstream.lastReq.Header.Get("Content-Type"), "multipart/form-data")
	require.Contains(t, string(upstream.lastBody), `name="model"`)
	require.Contains(t, string(upstream.lastBody), "gpt-image-2")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ZWRpdGVk", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyJSONEditConvertsDataURLToMultipart(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{
		"model":"gpt-image-2",
		"prompt":"replace background",
		"images":[{"image_url":"data:image/png;base64,c291cmNlLWltYWdl"}],
		"response_format":"b64_json",
		"output_format":"png"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{
		cfg: &config.Config{},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/json"},
					"X-Request-Id": []string{"req_img_edit_json_apikey"},
				},
				Body: io.NopCloser(strings.NewReader(`{"created":1710000010,"data":[{"b64_json":"ZWRpdGVk"}]}`)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.False(t, parsed.Multipart)

	account := &Account{
		ID:       9,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-api-key",
			"base_url": "https://image-upstream.example/v1",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)

	upstream, ok := svc.httpUpstream.(*httpUpstreamRecorder)
	require.True(t, ok)
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://image-upstream.example/v1/images/edits", upstream.lastReq.URL.String())
	require.Contains(t, upstream.lastReq.Header.Get("Content-Type"), "multipart/form-data")
	require.Contains(t, string(upstream.lastBody), `name="model"`)
	require.Contains(t, string(upstream.lastBody), "gpt-image-2")
	require.Contains(t, string(upstream.lastBody), `name="image"; filename="reference-1.png"`)
	require.Contains(t, string(upstream.lastBody), "source-image")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ZWRpdGVk", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
}

func TestBuildOpenAIImagesMultipartEditBody_RemoteImageURLPrivateAddressReturnsInputError(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer imageServer.Close()

	parsed := &OpenAIImagesRequest{
		Endpoint:       openAIImagesEditsEndpoint,
		Model:          "gpt-image-2",
		Prompt:         "replace background",
		N:              1,
		ResponseFormat: "b64_json",
		InputImageURLs: []string{imageServer.URL + "/ref.png"},
	}

	_, _, err := buildOpenAIImagesMultipartEditBody(context.Background(), parsed, "gpt-image-2")
	require.Error(t, err)

	var inputErr *OpenAIImagesInputError
	require.ErrorAs(t, err, &inputErr)
	require.Equal(t, "images[0].image_url", inputErr.Field)
	require.Contains(t, inputErr.Error(), "image_url must point to a public address")
}

func TestApplyDefaultOpenAIImagesResponseFormat(t *testing.T) {
	tests := []struct {
		name          string
		requestFormat string
		groupFormat   string
		want          string
	}{
		{name: "客户 Base64 覆盖 URL 分组", requestFormat: ImageResponseFormatB64JSON, groupFormat: ImageResponseFormatURL, want: ImageResponseFormatB64JSON},
		{name: "客户 URL 覆盖 Base64 分组", requestFormat: ImageResponseFormatURL, groupFormat: ImageResponseFormatB64JSON, want: ImageResponseFormatURL},
		{name: "缺省使用分组 URL", groupFormat: ImageResponseFormatURL, want: ImageResponseFormatURL},
		{name: "旧分组空值回退 Base64", want: ImageResponseFormatB64JSON},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := &OpenAIImagesRequest{Model: "gpt-image-2", N: 1, ResponseFormat: tt.requestFormat}
			err := ApplyDefaultOpenAIImagesResponseFormat(parsed, &Group{ImageResponseFormat: tt.groupFormat})
			require.NoError(t, err)
			require.Equal(t, tt.want, parsed.ResponseFormat)
			require.Equal(t, classifyOpenAIImagesCapability(parsed), parsed.RequiredCapability)
		})
	}
}

func TestApplyDefaultOpenAIImagesResponseFormatRejectsInvalidExplicitValue(t *testing.T) {
	parsed := &OpenAIImagesRequest{ResponseFormat: "base64", N: 1}
	require.Error(t, ApplyDefaultOpenAIImagesResponseFormat(parsed, &Group{ImageResponseFormat: ImageResponseFormatURL}))
}

func TestOpenAIGatewayServiceLocalizeOpenAIImagesJSONResponse(t *testing.T) {
	dir := t.TempDir()
	store := NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: dir})
	svc := &OpenAIGatewayService{generatedImageStore: store}
	imageBytes := generatedImageTestPNG(t)
	body := []byte(`{"created":1,"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(imageBytes) + `"}]}`)
	req := httptest.NewRequest(http.MethodPost, "https://api.example.com/v1/images/generations", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	localized, err := svc.localizeOpenAIImagesJSONResponse(context.Background(), c, body, openAIImagesNonStreamingResponseOptions{
		parsed: &OpenAIImagesRequest{ResponseFormat: ImageResponseFormatURL},
	})

	require.NoError(t, err)
	resultURL := gjson.GetBytes(localized, "data.0.url").String()
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, resultURL)
	require.False(t, gjson.GetBytes(localized, "data.0.b64_json").Exists())
	name := strings.TrimPrefix(resultURL, "/generated-images/")
	_, err = store.Resolve(name, time.Now().UTC())
	require.NoError(t, err)
}

func TestOpenAIImagesStreamingURLStoresOnlyCompletedImage(t *testing.T) {
	store := NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})
	svc := &OpenAIGatewayService{generatedImageStore: store}
	encoded := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))
	req := httptest.NewRequest(http.MethodPost, "https://api.example.com/v1/images/generations", nil)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	opts := openAIImagesStreamingResponseOptions{
		ctx:    context.Background(),
		parsed: &OpenAIImagesRequest{ResponseFormat: ImageResponseFormatURL},
	}

	partial := buildOpenAIImagesStreamPartialPayload("image_generation.partial_image", encoded, 0, ImageResponseFormatURL, 1, openAIResponsesImageResult{OutputFormat: "png"})
	require.False(t, gjson.GetBytes(partial, "url").Exists())
	require.Equal(t, encoded, gjson.GetBytes(partial, "b64_json").String())

	completed := []byte(`{"type":"image_generation.completed","b64_json":"` + encoded + `","output_format":"png"}`)
	rewritten, err := svc.rewriteOpenAIImagesStreamingCompletedPayload(context.Background(), c, completed, opts)
	require.NoError(t, err)
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.GetBytes(rewritten, "url").String())
	require.False(t, gjson.GetBytes(rewritten, "b64_json").Exists())
}

func TestOpenAIGatewayServiceForwardImages_OAuthStreamingTransformsEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"url"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_stream"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000001,\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-2\",\"background\":\"auto\",\"output_format\":\"png\",\"quality\":\"high\",\"size\":\"1024x1024\"}]}}\n\n" +
					"data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"cGFydGlhbA==\",\"partial_image_index\":0,\"output_format\":\"png\",\"background\":\"auto\"}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000001,\"usage\":{\"input_tokens\":5,\"output_tokens\":9,\"output_tokens_details\":{\"image_tokens\":4}},\"tool_usage\":{\"image_gen\":{\"input_tokens\":46,\"output_tokens\":2459,\"output_tokens_details\":{\"image_tokens\":2459},\"images\":1}},\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-2\",\"background\":\"auto\",\"output_format\":\"png\",\"quality\":\"high\",\"size\":\"1024x1024\"}],\"output\":[{\"type\":\"image_generation_call\",\"result\":\"" + encodedImage + "\",\"output_format\":\"png\"}]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc.httpUpstream = upstream

	account := &Account{
		ID:       2,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, OpenAIUsage{InputTokens: 46, OutputTokens: 2459, ImageOutputTokens: 2459}, result.Usage)
	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	partial, ok := findOpenAIImageTestSSEEvent(events, "image_generation.partial_image")
	require.True(t, ok)
	require.Equal(t, "image_generation.partial_image", gjson.Get(partial.Data, "type").String())
	require.Equal(t, int64(1710000001), gjson.Get(partial.Data, "created_at").Int())
	require.Equal(t, "cGFydGlhbA==", gjson.Get(partial.Data, "b64_json").String())
	require.False(t, gjson.Get(partial.Data, "url").Exists())
	require.Equal(t, "gpt-image-2", gjson.Get(partial.Data, "model").String())
	require.Equal(t, "png", gjson.Get(partial.Data, "output_format").String())
	require.Equal(t, "high", gjson.Get(partial.Data, "quality").String())
	require.Equal(t, "1024x1024", gjson.Get(partial.Data, "size").String())
	require.Equal(t, "auto", gjson.Get(partial.Data, "background").String())

	completed, ok := findOpenAIImageTestSSEEvent(events, "image_generation.completed")
	require.True(t, ok)
	require.Equal(t, "image_generation.completed", gjson.Get(completed.Data, "type").String())
	require.Equal(t, int64(1710000001), gjson.Get(completed.Data, "created_at").Int())
	require.False(t, gjson.Get(completed.Data, "b64_json").Exists())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(completed.Data, "url").String())
	require.Equal(t, "gpt-image-2", gjson.Get(completed.Data, "model").String())
	require.Equal(t, "png", gjson.Get(completed.Data, "output_format").String())
	require.Equal(t, "high", gjson.Get(completed.Data, "quality").String())
	// 已落到本地图片存储时，完成事件应反映实际图片尺寸而非上游声明尺寸。
	require.Equal(t, "2x2", gjson.Get(completed.Data, "size").String())
	require.Equal(t, "auto", gjson.Get(completed.Data, "background").String())
	require.JSONEq(t, `{"input_tokens":46,"output_tokens":2459,"output_tokens_details":{"image_tokens":2459},"images":1}`, gjson.Get(completed.Data, "usage").Raw)
	require.False(t, gjson.Get(completed.Data, "revised_prompt").Exists())
}

func TestOpenAIGatewayServiceForwardImages_APIKeyStreamingDrainsAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Writer = &failingOpenAIImageWriter{ResponseWriter: c.Writer, failAfter: 1}

	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				ImageStreamDataIntervalTimeout: 1,
				ImageStreamKeepaliveInterval:   0,
			},
		},
		httpUpstream: &httpUpstreamRecorder{
			resp: &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"text/event-stream"},
					"X-Request-Id": []string{"req_img_stream_disconnect_apikey"},
				},
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cGFydGlhbA==\"}\n\n" +
						"data: {\"type\":\"image_generation.completed\",\"usage\":{\"input_tokens\":3,\"output_tokens\":4,\"output_tokens_details\":{\"image_tokens\":2}},\"b64_json\":\"ZmluYWw=\",\"output_format\":\"png\"}\n\n" +
						"data: [DONE]\n\n",
				)),
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       8,
		Name:     "openai-apikey",
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "test-api-key",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 3, result.Usage.InputTokens)
	require.Equal(t, 4, result.Usage.OutputTokens)
	require.Equal(t, 2, result.Usage.ImageOutputTokens)
}

func TestOpenAIGatewayServiceForwardImages_OAuthEditsMultipartUsesResponsesAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "gpt-image-2"))
	require.NoError(t, writer.WriteField("prompt", "replace background with aurora"))
	require.NoError(t, writer.WriteField("input_fidelity", "high"))
	require.NoError(t, writer.WriteField("output_format", "webp"))
	require.NoError(t, writer.WriteField("quality", "high"))

	imageHeader := make(textproto.MIMEHeader)
	imageHeader.Set("Content-Disposition", `form-data; name="image"; filename="source.png"`)
	imageHeader.Set("Content-Type", "image/png")
	imagePart, err := writer.CreatePart(imageHeader)
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("png-image-content"))
	require.NoError(t, err)

	maskHeader := make(textproto.MIMEHeader)
	maskHeader.Set("Content-Disposition", `form-data; name="mask"; filename="mask.png"`)
	maskHeader.Set("Content-Type", "image/png")
	maskPart, err := writer.CreatePart(maskHeader)
	require.NoError(t, err)
	_, err = maskPart.Write([]byte("png-mask-content"))
	require.NoError(t, err)

	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set("api_key", &APIKey{ID: 100})

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body.Bytes())
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_edit_123"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000002,\"usage\":{\"input_tokens\":13,\"output_tokens\":21,\"output_tokens_details\":{\"image_tokens\":8}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"ZWRpdGVk\",\"revised_prompt\":\"replace background with aurora\",\"output_format\":\"webp\",\"quality\":\"high\"}]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc.httpUpstream = upstream

	account := &Account{
		ID:       3,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body.Bytes(), parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "gpt-image-2", gjson.GetBytes(upstream.lastBody, "tools.0.model").String())
	require.Equal(t, "edit", gjson.GetBytes(upstream.lastBody, "tools.0.action").String())
	require.False(t, gjson.GetBytes(upstream.lastBody, "tools.0.input_fidelity").Exists())
	require.Equal(t, "webp", gjson.GetBytes(upstream.lastBody, "tools.0.output_format").String())
	require.True(t, strings.HasPrefix(gjson.GetBytes(upstream.lastBody, "input.0.content.1.image_url").String(), "data:image/png;base64,"))
	require.True(t, strings.HasPrefix(gjson.GetBytes(upstream.lastBody, "tools.0.input_image_mask.image_url").String(), "data:image/png;base64,"))
	require.Equal(t, "replace background with aurora", gjson.GetBytes(upstream.lastBody, "input.0.content.0.text").String())
	require.Equal(t, "ZWRpdGVk", gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Equal(t, "replace background with aurora", gjson.Get(rec.Body.String(), "data.0.revised_prompt").String())
}

func TestOpenAIGatewayServiceForwardImages_OAuthEditsStreamingTransformsEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))
	body := []byte(`{
		"model":"gpt-image-2",
		"prompt":"replace background with aurora",
		"images":[{"image_url":"data:image/png;base64,c291cmNl"}],
		"mask":{"image_url":"data:image/png;base64,bWFzaw=="},
		"stream":true,
		"response_format":"url"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000003,\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-2\",\"background\":\"transparent\",\"output_format\":\"webp\",\"quality\":\"high\",\"size\":\"1024x1024\"}]}}\n\n" +
					"data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"cGFydGlhbA==\",\"partial_image_index\":0,\"output_format\":\"webp\",\"background\":\"transparent\"}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000003,\"usage\":{\"input_tokens\":7,\"output_tokens\":10,\"output_tokens_details\":{\"image_tokens\":5}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"tools\":[{\"type\":\"image_generation\",\"model\":\"gpt-image-2\",\"background\":\"transparent\",\"output_format\":\"webp\",\"quality\":\"high\",\"size\":\"1024x1024\"}],\"output\":[{\"type\":\"image_generation_call\",\"result\":\"" + encodedImage + "\",\"revised_prompt\":\"replace background with aurora\",\"output_format\":\"webp\"}]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc.httpUpstream = upstream

	account := &Account{
		ID:       4,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, "edit", gjson.GetBytes(upstream.lastBody, "tools.0.action").String())
	require.True(t, strings.HasPrefix(gjson.GetBytes(upstream.lastBody, "input.0.content.1.image_url").String(), "data:image/png;base64,"))
	require.True(t, strings.HasPrefix(gjson.GetBytes(upstream.lastBody, "tools.0.input_image_mask.image_url").String(), "data:image/png;base64,"))
	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	partial, ok := findOpenAIImageTestSSEEvent(events, "image_edit.partial_image")
	require.True(t, ok)
	require.Equal(t, "image_edit.partial_image", gjson.Get(partial.Data, "type").String())
	require.Equal(t, int64(1710000003), gjson.Get(partial.Data, "created_at").Int())
	require.Equal(t, "cGFydGlhbA==", gjson.Get(partial.Data, "b64_json").String())
	require.False(t, gjson.Get(partial.Data, "url").Exists())
	require.Equal(t, "gpt-image-2", gjson.Get(partial.Data, "model").String())
	require.Equal(t, "webp", gjson.Get(partial.Data, "output_format").String())
	require.Equal(t, "high", gjson.Get(partial.Data, "quality").String())
	require.Equal(t, "1024x1024", gjson.Get(partial.Data, "size").String())
	require.Equal(t, "transparent", gjson.Get(partial.Data, "background").String())

	completed, ok := findOpenAIImageTestSSEEvent(events, "image_edit.completed")
	require.True(t, ok)
	require.Equal(t, "image_edit.completed", gjson.Get(completed.Data, "type").String())
	require.Equal(t, int64(1710000003), gjson.Get(completed.Data, "created_at").Int())
	require.False(t, gjson.Get(completed.Data, "b64_json").Exists())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(completed.Data, "url").String())
	require.Equal(t, "gpt-image-2", gjson.Get(completed.Data, "model").String())
	require.Equal(t, "webp", gjson.Get(completed.Data, "output_format").String())
	require.Equal(t, "high", gjson.Get(completed.Data, "quality").String())
	// 完成事件已引用本地落盘的真实 PNG，尺寸应以图片内容而非上游声明为准。
	require.Equal(t, "2x2", gjson.Get(completed.Data, "size").String())
	require.Equal(t, "transparent", gjson.Get(completed.Data, "background").String())
	require.JSONEq(t, `{"images":1}`, gjson.Get(completed.Data, "usage").Raw)
	require.False(t, gjson.Get(completed.Data, "revised_prompt").Exists())
}

func TestBuildOpenAIImagesResponsesRequest_PassesThroughNForMultiImageModels(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesGenerationsEndpoint,
		Model:    "gpt-image-2",
		Prompt:   "draw a cat",
		N:        2,
	}

	body, err := buildOpenAIImagesResponsesRequest(context.Background(), parsed, "gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.Equal(t, int64(2), gjson.GetBytes(body, "tools.0.n").Int())
	require.Equal(t, "gpt-image-2", gjson.GetBytes(body, "tools.0.model").String())
	require.Equal(t, "draw a cat", gjson.GetBytes(body, "input.0.content.0.text").String())
}

func TestBuildOpenAIImagesResponsesRequest_DoesNotPassNForDallE3(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint: openAIImagesGenerationsEndpoint,
		Model:    "dall-e-3",
		Prompt:   "draw a cat",
		N:        2,
	}

	body, err := buildOpenAIImagesResponsesRequest(context.Background(), parsed, "dall-e-3")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.False(t, gjson.GetBytes(body, "tools.0.n").Exists())
	require.Equal(t, "dall-e-3", gjson.GetBytes(body, "tools.0.model").String())
}

func TestBuildOpenAIImagesResponsesRequest_StripsInputFidelity(t *testing.T) {
	parsed := &OpenAIImagesRequest{
		Endpoint:      openAIImagesEditsEndpoint,
		Model:         "gpt-image-2",
		Prompt:        "replace background",
		InputFidelity: "high",
		InputImageURLs: []string{
			"data:image/png;base64,c291cmNl",
		},
	}

	body, err := buildOpenAIImagesResponsesRequest(context.Background(), parsed, "gpt-image-2")
	require.NoError(t, err)
	require.NotNil(t, body)
	require.False(t, gjson.GetBytes(body, "tools.0.input_fidelity").Exists())
	require.Equal(t, "edit", gjson.GetBytes(body, "tools.0.action").String())
}

func TestCollectOpenAIImagesFromResponsesBody_FallsBackToOutputItemDone(t *testing.T) {
	body := []byte(
		"data: {\"type\":\"response.created\",\"response\":{\"created_at\":1710000004}}\n\n" +
			"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"aGVsbG8=\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\",\"quality\":\"high\"}}\n\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000004,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
			"data: [DONE]\n\n",
	)

	results, createdAt, usageRaw, firstMeta, foundFinal, err := collectOpenAIImagesFromResponsesBody(body)
	require.NoError(t, err)
	require.True(t, foundFinal)
	require.Equal(t, int64(1710000004), createdAt)
	require.Len(t, results, 1)
	require.Equal(t, "aGVsbG8=", results[0].Result)
	require.Equal(t, "draw a cat", results[0].RevisedPrompt)
	require.Equal(t, "png", firstMeta.OutputFormat)
	require.JSONEq(t, `{"images":1}`, string(usageRaw))
}

func TestCollectOpenAIImagesFromResponsesBody_MultilineSSE(t *testing.T) {
	body := []byte(
		"data: {\"type\":\"response.completed\",\n" +
			"data: \"response\":{\"created_at\":1710000010,\"usage\":{\"input_tokens\":5,\"output_tokens\":9,\"output_tokens_details\":{\"image_tokens\":4}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"ZmluYWw=\",\"output_format\":\"png\"}]}}\n\n" +
			"data: [DONE]\n\n",
	)

	results, createdAt, usageRaw, firstMeta, foundFinal, err := collectOpenAIImagesFromResponsesBody(body)
	require.NoError(t, err)
	require.True(t, foundFinal)
	require.Equal(t, int64(1710000010), createdAt)
	require.Len(t, results, 1)
	require.Equal(t, "ZmluYWw=", results[0].Result)
	require.Equal(t, "png", firstMeta.OutputFormat)
	require.JSONEq(t, `{"images":1}`, string(usageRaw))
}

func TestCollectOpenAIImagesFromResponsesBody_ExtractsNestedInlineImage(t *testing.T) {
	body := []byte(
		"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000012,\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"id\":\"ig_nested\",\"type\":\"image_generation_call\",\"content\":[{\"type\":\"output_image\",\"b64_json\":\"data:image/webp;base64,TmVzdGVk\"}],\"revised_prompt\":\"draw nested\",\"output_format\":\"webp\"}]}}\n\n" +
			"data: [DONE]\n\n",
	)

	results, createdAt, usageRaw, firstMeta, foundFinal, err := collectOpenAIImagesFromResponsesBody(body)
	require.NoError(t, err)
	require.True(t, foundFinal)
	require.Equal(t, int64(1710000012), createdAt)
	require.Len(t, results, 1)
	require.Equal(t, "TmVzdGVk", results[0].Result)
	require.Equal(t, "draw nested", results[0].RevisedPrompt)
	require.Equal(t, "webp", firstMeta.OutputFormat)
	require.JSONEq(t, `{"images":1}`, string(usageRaw))
}

func TestCollectOpenAIImagesFromResponsesBody_ExtractsNestedDownloadURL(t *testing.T) {
	body := []byte(
		"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000013,\"output\":[{\"id\":\"ig_url\",\"type\":\"image_generation_call\",\"result\":\"\",\"image\":{\"download_url\":\"https://files.example.com/generated.webp?sig=1\",\"mime_type\":\"image/webp\"}}]}}\n\n" +
			"data: [DONE]\n\n",
	)

	results, createdAt, _, firstMeta, foundFinal, err := collectOpenAIImagesFromResponsesBody(body)
	require.NoError(t, err)
	require.True(t, foundFinal)
	require.Equal(t, int64(1710000013), createdAt)
	require.Len(t, results, 1)
	require.Equal(t, "https://files.example.com/generated.webp?sig=1", results[0].URL)
	require.Equal(t, "webp", firstMeta.OutputFormat)
}

func TestOpenAIGatewayServiceMaterializeOpenAIResponsesImageURLs_DownloadsURLAsBase64(t *testing.T) {
	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/webp"}},
			Body:       io.NopCloser(strings.NewReader("webp-bytes")),
		},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	results, err := svc.materializeOpenAIResponsesImageURLs(
		context.Background(),
		[]openAIResponsesImageResult{{URL: "https://files.example.com/generated.webp?sig=1"}},
		http.Header{"User-Agent": []string{"test-agent"}},
		"socks5://proxy.example:1080",
		42,
		3,
	)

	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("webp-bytes")), results[0].Result)
	require.Empty(t, results[0].URL)
	require.Equal(t, "image/webp", results[0].MimeType)
	require.Equal(t, "webp", results[0].OutputFormat)
	require.Len(t, upstream.requests, 1)
	require.Equal(t, http.MethodGet, upstream.requests[0].Method)
	require.Equal(t, "test-agent", upstream.requests[0].Header.Get("User-Agent"))
	require.Equal(t, "image/*,*/*;q=0.8", upstream.requests[0].Header.Get("Accept"))
}

func TestOpenAIGatewayServiceMaterializeOpenAIResponsesImageURLsRejectsNonImageBody(t *testing.T) {
	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
			Body:       io.NopCloser(strings.NewReader("<html>upstream error</html>")),
		},
	}
	svc := &OpenAIGatewayService{httpUpstream: upstream}

	_, err := svc.materializeOpenAIResponsesImageURLs(
		context.Background(),
		[]openAIResponsesImageResult{{URL: "https://files.example.com/generated.webp?sig=1"}},
		http.Header{"User-Agent": []string{"test-agent"}},
		"",
		42,
		3,
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "response is not an image")
}

func TestOpenAIGatewayServiceHandleOpenAIImagesOAuthNonStreamingResponse_MaterializesDownloadURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	responseBody := "data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000014,\"output\":[{\"id\":\"ig_url\",\"type\":\"image_generation_call\",\"result\":\"\",\"image\":{\"download_url\":\"https://files.example.com/generated.webp?sig=1\",\"mime_type\":\"image/webp\"}}]}}\n\n" +
		"data: [DONE]\n\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(responseBody)),
	}
	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/webp"}},
			Body:       io.NopCloser(strings.NewReader("webp-bytes")),
		},
	}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	svc := &OpenAIGatewayService{
		cfg:          &config.Config{},
		httpUpstream: upstream,
	}

	_, imageCount, _, err := svc.handleOpenAIImagesOAuthNonStreamingResponse(
		resp,
		c,
		"b64_json",
		"gpt-image-2",
		ImageBillingSize2K,
		nil,
		context.Background(),
		http.Header{"User-Agent": []string{"test-agent"}},
		"",
		42,
		3,
	)

	require.NoError(t, err)
	require.Equal(t, 1, imageCount)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, base64.StdEncoding.EncodeToString([]byte("webp-bytes")), gjson.Get(rec.Body.String(), "data.0.b64_json").String())
	require.Empty(t, gjson.Get(rec.Body.String(), "data.0.url").String())
}

func TestOpenAIGatewayServiceForwardImages_OAuthStreamingHandlesOutputItemDoneFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	encodedImage := base64.StdEncoding.EncodeToString(generatedImageTestPNG(t))
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"url"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{generatedImageStore: NewGeneratedImageStore(GeneratedImageStoreConfig{Directory: t.TempDir()})}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_stream_output_item_done"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"ig_123\",\"type\":\"image_generation_call\",\"result\":\"" + encodedImage + "\",\"revised_prompt\":\"draw a cat\",\"output_format\":\"png\"}}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000005,\"usage\":{\"input_tokens\":5,\"output_tokens\":9,\"output_tokens_details\":{\"image_tokens\":4}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc.httpUpstream = upstream

	account := &Account{
		ID:       5,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	completed, ok := findOpenAIImageTestSSEEvent(events, "image_generation.completed")
	require.True(t, ok)
	require.Equal(t, "image_generation.completed", gjson.Get(completed.Data, "type").String())
	require.Equal(t, int64(1710000005), gjson.Get(completed.Data, "created_at").Int())
	require.False(t, gjson.Get(completed.Data, "b64_json").Exists())
	require.Regexp(t, `^/generated-images/[a-f0-9]{32}\.png$`, gjson.Get(completed.Data, "url").String())
	require.Equal(t, "gpt-image-2", gjson.Get(completed.Data, "model").String())
	require.JSONEq(t, `{"images":1}`, gjson.Get(completed.Data, "usage").Raw)
	require.NotContains(t, rec.Body.String(), "event: error")
}

func TestOpenAIGatewayServiceForwardImages_OAuthStreamingHandlesMultilineSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"b64_json"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req

	svc := &OpenAIGatewayService{}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	svc.httpUpstream = &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_stream_multiline_oauth"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.completed\",\n" +
					"data: \"response\":{\"created_at\":1710000011,\"usage\":{\"input_tokens\":6,\"output_tokens\":10,\"output_tokens_details\":{\"image_tokens\":5}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"TXVsdGlsaW5l\",\"output_format\":\"png\"}]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}

	account := &Account{
		ID:       11,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 6, result.Usage.InputTokens)
	require.Equal(t, 10, result.Usage.OutputTokens)
	require.Equal(t, 5, result.Usage.ImageOutputTokens)
	events := parseOpenAIImageTestSSEEvents(rec.Body.String())
	completed, ok := findOpenAIImageTestSSEEvent(events, "image_generation.completed")
	require.True(t, ok)
	require.Equal(t, "TXVsdGlsaW5l", gjson.Get(completed.Data, "b64_json").String())
	require.JSONEq(t, `{"images":1}`, gjson.Get(completed.Data, "usage").Raw)
	require.NotContains(t, rec.Body.String(), "event: error")
}

func TestOpenAIGatewayServiceForwardImages_OAuthStreamingDrainsAfterClientDisconnect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw a cat","stream":true,"response_format":"url"}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Writer = &failingOpenAIImageWriter{ResponseWriter: c.Writer, failAfter: 1}

	svc := &OpenAIGatewayService{
		cfg: &config.Config{
			Gateway: config.GatewayConfig{
				ImageStreamDataIntervalTimeout: 1,
				ImageStreamKeepaliveInterval:   0,
			},
		},
	}
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	upstream := &httpUpstreamRecorder{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"text/event-stream"},
				"X-Request-Id": []string{"req_img_stream_disconnect_oauth"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"type\":\"response.image_generation_call.partial_image\",\"partial_image_b64\":\"cGFydGlhbA==\",\"partial_image_index\":0,\"output_format\":\"png\"}\n\n" +
					"data: {\"type\":\"response.completed\",\"response\":{\"created_at\":1710000009,\"usage\":{\"input_tokens\":5,\"output_tokens\":9,\"output_tokens_details\":{\"image_tokens\":4}},\"tool_usage\":{\"image_gen\":{\"images\":1}},\"output\":[{\"type\":\"image_generation_call\",\"result\":\"ZmluYWw=\",\"output_format\":\"png\"}]}}\n\n" +
					"data: [DONE]\n\n",
			)),
		},
	}
	svc.httpUpstream = upstream

	account := &Account{
		ID:       9,
		Name:     "openai-oauth",
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"access_token": "token-123",
		},
	}

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 5, result.Usage.InputTokens)
	require.Equal(t, 9, result.Usage.OutputTokens)
	require.Equal(t, 4, result.Usage.ImageOutputTokens)
}
