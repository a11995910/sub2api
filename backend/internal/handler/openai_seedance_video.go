package handler

import (
	"bytes"
	"io"
	"net/http"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// OpenAIVideoGeneration 复用 Chat Completions handler 的账号调度、故障切换和计费循环。
func (h *OpenAIGatewayHandler) OpenAIVideoGeneration(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
			return
		}
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	_, requestInfo, err := service.ParseOpenAIVideoCreateBody(body)
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
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	c.Request.ContentLength = int64(len(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.ChatCompletions(c)
}

func isModelTestVideoRequest(c *gin.Context) bool {
	mode, ok := service.TrustedModelTestMode(c)
	return ok && mode == service.ModelTestModeVideo
}

func shouldReserveOpenAIVideoBilling(c *gin.Context, apiKey *service.APIKey, subscription *service.UserSubscription) bool {
	meta, ok := service.OpenAIVideoContextFromGin(c)
	if !ok || apiKey == nil || meta.RecordModelTestTask {
		return false
	}
	return subscription == nil || apiKey.Group == nil || !apiKey.Group.IsSubscriptionType()
}

func (h *OpenAIGatewayHandler) validateOpenAIVideoRequestForAccount(c *gin.Context, account *service.Account, body []byte, streamStarted bool) bool {
	meta, ok := service.OpenAIVideoContextFromGin(c)
	if !ok {
		return true
	}
	prepared, err := service.PrepareOpenAIVideoCreateBodyForAccount(account, body)
	if err != nil {
		h.handleStreamingAwareError(c, http.StatusBadRequest, "invalid_request_error", err.Error(), streamStarted)
		return false
	}
	// ZYCA 始终按秒计费，必须在转发前确认客户价格；其他协议的 token 视频
	// 仍按上游实际 token 用量结算，高分辨率不应触发固定视频价格门禁。
	if service.ResolveOpenAIVideoRequestProfile(account) == service.OpenAIVideoRequestProfileZYCA {
		apiKey, _ := middleware2.GetAPIKeyFromContext(c)
		// body 可能已经过渠道映射；费用预留使用的仍是入口记录的原始模型名。
		if _, err := h.gatewayService.EstimateVideoCostForAccount(c.Request.Context(), account, apiKey, meta.Model, prepared.Resolution, prepared.DurationSeconds); err != nil {
			h.handleStreamingAwareError(c, http.StatusBadRequest, "invalid_request_error", err.Error(), streamStarted)
			return false
		}
	}
	meta.Resolution = prepared.Resolution
	meta.DurationSeconds = prepared.DurationSeconds
	meta.ReferenceImageCount = len(prepared.ImageURLs)
	service.SetOpenAIVideoContext(c, meta)
	return true
}

// SeedanceVideoGeneration 保留旧调用点，实际进入通用 OpenAI 视频实现。
func (h *OpenAIGatewayHandler) SeedanceVideoGeneration(c *gin.Context) {
	h.OpenAIVideoGeneration(c)
}
