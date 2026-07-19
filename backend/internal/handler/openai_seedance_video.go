package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OpenAIVideoGeneration 复用 Chat Completions handler 的账号调度、故障切换和计费循环。
func (h *OpenAIGatewayHandler) OpenAIVideoGeneration(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	normalizedBody, requestInfo, err := service.NormalizeOpenAIVideoCreateBody(body, "")
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	videoContext := service.OpenAIVideoContext{
		Model:               requestInfo.Model,
		Prompt:              requestInfo.Prompt,
		Resolution:          requestInfo.Resolution,
		DurationSeconds:     requestInfo.DurationSeconds,
		ReferenceImageCount: len(requestInfo.ImageURLs),
		RecordModelTestTask: isModelTestVideoRequest(c),
	}
	if apiKey, ok := middleware2.GetAPIKeyFromContext(c); ok && apiKey != nil {
		videoContext.APIKeyID = apiKey.ID
		if apiKey.GroupID != nil {
			videoContext.GroupID = *apiKey.GroupID
		}
	}
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		videoContext.UserID = subject.UserID
	}
	videoContext.BindTask = videoContext.UserID > 0 && videoContext.APIKeyID > 0
	service.SetOpenAIVideoContext(c, videoContext)
	c.Request.Body = io.NopCloser(bytes.NewReader(normalizedBody))
	c.Request.ContentLength = int64(len(normalizedBody))
	c.Request.Header.Set("Content-Type", "application/json")
	h.ChatCompletions(c)
}

func isModelTestVideoRequest(c *gin.Context) bool {
	return c != nil && strings.TrimSpace(c.GetHeader("X-Sub2API-Model-Test")) == "video"
}

// SeedanceVideoGeneration 保留旧调用点，实际进入通用 OpenAI 视频实现。
func (h *OpenAIGatewayHandler) SeedanceVideoGeneration(c *gin.Context) {
	h.OpenAIVideoGeneration(c)
}
