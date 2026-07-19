package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// SeedanceVideoGeneration 将统一视频请求转换为 Helix Chat Completions 请求，
// 后续账号调度、错误切换和用量记录复用现有 OpenAI Chat Completions 链路。
func (h *OpenAIGatewayHandler) SeedanceVideoGeneration(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	model := strings.TrimSpace(gjson.GetBytes(body, "model").String())
	if !service.IsSeedanceVideoModel(model) {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Videos API is not supported for this platform")
		return
	}
	resolution := strings.ToLower(strings.TrimSpace(gjson.GetBytes(body, "resolution").String()))
	if resolution == "" {
		resolution = service.VideoBillingResolution720P
	}
	duration := int(gjson.GetBytes(body, "duration").Int())
	if duration <= 0 {
		duration = service.VideoBillingDefaultDurationSeconds
	}
	referenceImages := seedanceReferenceImageURLs(body)
	chatBody, err := service.BuildSeedanceChatRequest(service.SeedanceVideoRequest{
		Model:                  model,
		Prompt:                 gjson.GetBytes(body, "prompt").String(),
		Resolution:             resolution,
		Duration:               duration,
		ReferenceImageDataURLs: referenceImages,
	})
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	service.SetSeedanceVideoContext(c, service.SeedanceVideoContext{
		Model:               model,
		Resolution:          resolution,
		Duration:            duration,
		ReferenceImageCount: len(referenceImages),
	})
	c.Request.Body = io.NopCloser(bytes.NewReader(chatBody))
	c.Request.ContentLength = int64(len(chatBody))
	c.Request.Header.Set("Content-Type", "application/json")
	h.ChatCompletions(c)
}

func seedanceReferenceImageURLs(body []byte) []string {
	urls := make([]string, 0, 4)
	if rawURL := strings.TrimSpace(gjson.GetBytes(body, "image.url").String()); rawURL != "" {
		urls = append(urls, rawURL)
	}
	for _, item := range gjson.GetBytes(body, "reference_images").Array() {
		if rawURL := strings.TrimSpace(item.Get("url").String()); rawURL != "" {
			urls = append(urls, rawURL)
		}
	}
	return urls
}
