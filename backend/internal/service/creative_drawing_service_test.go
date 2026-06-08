package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCreativeDrawingTaskTimeoutAllowsSlowImageGatewayButBoundsStuckTasks(t *testing.T) {
	require.Equal(t, 12*time.Minute, creativeDrawingTaskTimeout)
	require.Equal(t, 2, creativeDrawingMaxAttempts)
}

func TestNormalizeCreativeDrawingTaskErrorMapsDeadlineForEdit(t *testing.T) {
	got := normalizeCreativeDrawingTaskError(context.DeadlineExceeded, &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Contains(t, got, "参考图作画超时")
	require.NotContains(t, got, "context deadline exceeded")
}

func TestNormalizeCreativeDrawingTaskErrorMapsDeadlineForGenerate(t *testing.T) {
	got := normalizeCreativeDrawingTaskError(errors.New("creative drawing gateway request failed: context deadline exceeded"), &CreativeDrawingTask{Mode: CreativeDrawingModeGenerate})

	require.Equal(t, "图片生成超时，请重试", got)
}

func TestNormalizeCreativeDrawingTaskErrorKeepsGatewayMessage(t *testing.T) {
	got := normalizeCreativeDrawingTaskError(errors.New("上游图片接口返回 400"), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Equal(t, "上游图片接口返回 400", got)
}

func TestIsCreativeDrawingRetryableError(t *testing.T) {
	require.True(t, isCreativeDrawingRetryableError(newCreativeDrawingGatewayError(http.StatusBadGateway, "temporary upstream error")))
	require.True(t, isCreativeDrawingRetryableError(errors.New("upstream image stream idle for 15m0s")))
	require.True(t, isCreativeDrawingRetryableError(errors.New("image stream data interval timeout")))
	require.False(t, isCreativeDrawingRetryableError(newCreativeDrawingGatewayError(http.StatusBadRequest, "invalid image request")))
	require.False(t, isCreativeDrawingRetryableError(errors.New("invalid image request")))
}

func TestBuildCreativeDrawingEditMultipartBodyEnablesStreaming(t *testing.T) {
	body, contentType, err := buildCreativeDrawingEditMultipartBody(&CreativeDrawingTask{
		Mode:         CreativeDrawingModeEdit,
		Model:        "gpt-image-2",
		Prompt:       "画一座赛博客栈",
		Size:         "3840x2160",
		Count:        1,
		OutputFormat: "png",
		ReferenceImages: []CreativeDrawingReference{
			{Name: "reference.png", DataURL: "data:image/png;base64,ZmFrZQ=="},
		},
	})
	require.NoError(t, err)

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)

	fields := map[string]string{}
	reader := multipart.NewReader(bytes.NewReader(data), params["boundary"])
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if part.FileName() != "" {
			continue
		}
		value, err := io.ReadAll(part)
		require.NoError(t, err)
		fields[part.FormName()] = string(value)
	}

	require.Equal(t, "true", fields["stream"])
	require.Equal(t, "2", fields["partial_images"])
	require.Equal(t, "3840x2160", fields["size"])
}

func TestParseCreativeDrawingGatewayStreamImagesReadsCompletedEvent(t *testing.T) {
	images, err := parseCreativeDrawingGatewayStreamImages([]byte(
		"data: {\"type\":\"image_edit.partial_image\",\"b64_json\":\"cGFydGlhbA==\"}\n\n"+
			"data: {\"type\":\"image_edit.completed\",\"b64_json\":\"ZmluYWw=\",\"output_format\":\"webp\",\"size\":\"3840x2160\",\"created_at\":1710000000}\n\n"+
			"data: [DONE]\n\n",
	), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit, OutputFormat: "png", Size: "1024x1024"})

	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, "ZmluYWw=", images[0].B64JSON)
	require.Equal(t, "webp", images[0].OutputFormat)
	require.Equal(t, "3840x2160", images[0].Size)
	require.Equal(t, int64(1710000000), images[0].CreatedAt)
}

func TestParseCreativeDrawingGatewayStreamImagesReadsResponseOutputItemDone(t *testing.T) {
	images, err := parseCreativeDrawingGatewayStreamImages([]byte(
		"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"image_generation_call\",\"result\":\"UmVzcG9uc2VJbWFnZQ==\",\"revised_prompt\":\"画一张海报\",\"output_format\":\"png\",\"size\":\"1024x1024\"}}\n\n"+
			"data: [DONE]\n\n",
	), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit, OutputFormat: "webp", Size: "3840x2160"})

	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, "UmVzcG9uc2VJbWFnZQ==", images[0].B64JSON)
	require.Equal(t, "画一张海报", images[0].RevisedPrompt)
	require.Equal(t, "png", images[0].OutputFormat)
	require.Equal(t, "1024x1024", images[0].Size)
}

func TestParseCreativeDrawingGatewayImagesReadsResponsesOutputPayload(t *testing.T) {
	images, err := parseCreativeDrawingGatewayImages([]byte(
		`{"created_at":1710000001,"output":[{"type":"image_generation_call","result":"T25lSW1hZ2U=","revised_prompt":"画一张海报","output_format":"webp","size":"3840x2160"}]}`,
	), &CreativeDrawingTask{OutputFormat: "png", Size: "1024x1024"})

	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, "T25lSW1hZ2U=", images[0].B64JSON)
	require.Equal(t, "画一张海报", images[0].RevisedPrompt)
	require.Equal(t, "webp", images[0].OutputFormat)
	require.Equal(t, "3840x2160", images[0].Size)
	require.Equal(t, int64(1710000001), images[0].CreatedAt)
}

func TestParseCreativeDrawingGatewayImagesTreatsResultURLAsURL(t *testing.T) {
	images, err := parseCreativeDrawingGatewayImages([]byte(
		`{"created_at":1710000001,"output":[{"type":"image_generation_call","result":"http://192.0.2.10:3000/images/generated.png","output_format":"png"}]}`,
	), &CreativeDrawingTask{OutputFormat: "png"})

	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Empty(t, images[0].B64JSON)
	require.Equal(t, "http://192.0.2.10:3000/images/generated.png", images[0].URL)
	require.Equal(t, "http://192.0.2.10:3000/images/generated.png", images[0].SourceURL)
}

func TestParseCreativeDrawingGatewayImagesCleansURLFromB64JSON(t *testing.T) {
	images, err := parseCreativeDrawingGatewayImages([]byte(
		`{"created":1710000000,"data":[{"b64_json":"http://192.0.2.10:3000/images/generated.png"}]}`,
	), &CreativeDrawingTask{OutputFormat: "png"})

	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Empty(t, images[0].B64JSON)
	require.Equal(t, "http://192.0.2.10:3000/images/generated.png", images[0].URL)
}

func TestNormalizeCreativeDrawingImageResultsCleansPersistedURLFromB64JSON(t *testing.T) {
	images := NormalizeCreativeDrawingImageResults([]CreativeDrawingImageResult{
		{ID: "image-1", B64JSON: "http://192.0.2.10:3000/images/generated.png", OutputFormat: "png"},
	})

	require.Len(t, images, 1)
	require.Equal(t, "image-1", images[0].ID)
	require.Empty(t, images[0].B64JSON)
	require.Equal(t, "http://192.0.2.10:3000/images/generated.png", images[0].URL)
	require.Equal(t, "http://192.0.2.10:3000/images/generated.png", images[0].SourceURL)
}

func TestResolveCreativeDrawingPromptMarketAssetURLUsesWhitelistedRepositories(t *testing.T) {
	tests := []struct {
		name       string
		library    string
		assetPath  string
		wantPrefix string
		wantPath   string
	}{
		{
			name:       "library-a",
			library:    "library-a",
			assetPath:  "/images/example.png",
			wantPrefix: creativeDrawingPromptMarketBananaRawBaseURL,
			wantPath:   "images/example.png",
		},
		{
			name:       "library-a legacy alias",
			library:    "a",
			assetPath:  "images/example.png",
			wantPrefix: creativeDrawingPromptMarketBananaRawBaseURL,
			wantPath:   "images/example.png",
		},
		{
			name:       "library-b new repository",
			library:    "library-b",
			assetPath:  "api/images/poster_case151/output.jpg",
			wantPrefix: creativeDrawingPromptMarketAwesomeAPIBaseURL,
			wantPath:   "images/poster_case151/output.jpg",
		},
		{
			name:       "library-b legacy repository",
			library:    "b",
			assetPath:  "prompts/images/poster_case151/output.jpg",
			wantPrefix: creativeDrawingPromptMarketAwesomePromptsBaseURL,
			wantPath:   "images/poster_case151/output.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveCreativeDrawingPromptMarketAssetURL(tt.library, tt.assetPath)
			require.NoError(t, err)
			want, err := joinCreativeDrawingPromptMarketURL(tt.wantPrefix, tt.wantPath)
			require.NoError(t, err)
			require.Equal(t, want, got)
		})
	}
}

func TestResolveCreativeDrawingPromptMarketAssetURLRejectsUnsafePath(t *testing.T) {
	_, err := resolveCreativeDrawingPromptMarketAssetURL("library-b", "https://example.com/image.png")
	require.Error(t, err)

	_, err = resolveCreativeDrawingPromptMarketAssetURL("library-b", "images/output.jpg")
	require.Error(t, err)
}

func TestRewriteCreativeDrawingPromptMarketContentUsesLocalAssetProxy(t *testing.T) {
	raw := []byte(
		creativeDrawingPromptMarketAwesomeAPIBaseURL + "images/case/output.jpg\n" +
			creativeDrawingPromptMarketAwesomePromptsBaseURL + "images/legacy/output.jpg",
	)

	got := string(rewriteCreativeDrawingPromptMarketContent("library-b", raw))

	require.Contains(t, got, "/api/v1/creative-drawing/prompt-market/assets/library-b/api/images/case/output.jpg")
	require.Contains(t, got, "/api/v1/creative-drawing/prompt-market/assets/library-b/prompts/images/legacy/output.jpg")
	require.NotContains(t, got, "raw.githubusercontent.com")
}

func TestParseCreativeDrawingGatewayImagesReturnsSuccessErrorPayload(t *testing.T) {
	_, err := parseCreativeDrawingGatewayImages([]byte(
		`{"error":{"code":"upstream_error","message":"upstream returned Cloudflare challenge page"}}`,
	), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Error(t, err)
	require.Contains(t, err.Error(), "Cloudflare challenge")
}

func TestParseCreativeDrawingGatewayImagesKeepsNormalDataPayload(t *testing.T) {
	images, err := parseCreativeDrawingGatewayImages([]byte(
		`{"created":1710000000,"data":[{"b64_json":"ZmluYWw="}]}`,
	), &CreativeDrawingTask{OutputFormat: "png", Size: "3840x2160"})

	require.NoError(t, err)
	require.Len(t, images, 1)
	require.Equal(t, "ZmluYWw=", images[0].B64JSON)
	require.Equal(t, "png", images[0].OutputFormat)
	require.Equal(t, "3840x2160", images[0].Size)
}

func TestParseCreativeDrawingGatewayStreamImagesReturnsStreamError(t *testing.T) {
	_, err := parseCreativeDrawingGatewayStreamImages([]byte(
		"data: {\"type\":\"error\",\"error\":{\"message\":\"upstream image stream idle\"}}\n\n",
	), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream image stream idle")
}

func TestParseCreativeDrawingGatewayStreamImagesReturnsTopLevelError(t *testing.T) {
	_, err := parseCreativeDrawingGatewayStreamImages([]byte(
		"data: {\"error\":{\"code\":\"upstream_error\",\"message\":\"upstream returned Cloudflare challenge page\"}}\n\n"+
			"data: [DONE]\n\n",
	), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Error(t, err)
	require.Contains(t, err.Error(), "Cloudflare challenge")
}

func TestParseCreativeDrawingGatewayStreamImagesReturnsResponseFailedError(t *testing.T) {
	_, err := parseCreativeDrawingGatewayStreamImages([]byte(
		"data: {\"type\":\"response.failed\",\"response\":{\"error\":{\"message\":\"upstream did not return image output\"}}}\n\n",
	), &CreativeDrawingTask{Mode: CreativeDrawingModeEdit})

	require.Error(t, err)
	require.Contains(t, err.Error(), "upstream did not return image output")
}
