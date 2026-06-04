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

func TestCreativeDrawingTaskTimeoutAllowsImageGatewayRetries(t *testing.T) {
	require.Equal(t, 30*time.Minute, creativeDrawingTaskTimeout)
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
