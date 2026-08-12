package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *OpenAIGatewayHandler) OpenAIVideoStatus(c *gin.Context) {
	h.openAIVideoLookup(c, false)
}

func (h *OpenAIGatewayHandler) OpenAIVideoContent(c *gin.Context) {
	h.openAIVideoLookup(c, true)
}

func (h *OpenAIGatewayHandler) openAIVideoLookup(c *gin.Context, content bool) {
	taskID := strings.TrimSpace(c.Param("task_id"))
	if taskID == "" {
		taskID = strings.TrimSpace(c.Param("request_id"))
	}
	apiKey, apiKeyOK := middleware2.GetAPIKeyFromContext(c)
	subject, subjectOK := middleware2.GetAuthSubjectFromContext(c)
	if !apiKeyOK || apiKey == nil || !subjectOK || taskID == "" || h == nil || h.gatewayService == nil {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video task not found")
		return
	}
	account, err := h.gatewayService.ResolveOpenAIVideoTaskAccount(
		c.Request.Context(), apiKey.GroupID, taskID, subject.UserID, apiKey.ID,
	)
	if err != nil || account == nil {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Video task not found")
		return
	}
	var billingTask *service.VideoTaskBilling
	if h.videoTaskBilling != nil {
		billingTask, err = h.videoTaskBilling.ResolveOwnedTask(c.Request.Context(), service.PlatformOpenAI, taskID, subject.UserID, apiKey.ID)
		if err != nil && !errors.Is(err, service.ErrVideoTaskBillingNotFound) {
			h.errorResponse(c, http.StatusInternalServerError, "api_error", "Video task billing lookup failed")
			return
		}
	}
	var result *service.OpenAIForwardResult
	if content {
		result, err = h.gatewayService.ForwardOpenAIVideoContent(c.Request.Context(), c, account, taskID)
	} else {
		result, err = h.gatewayService.ForwardOpenAIVideoStatus(c.Request.Context(), c, account, taskID)
		if billingTask != nil {
			outcome := service.ClassifyVideoTaskResult(result)
			if err != nil {
				outcome = service.VideoTaskOutcome{Status: service.VideoTaskStatusUnknown, ErrorMessage: err.Error()}
			}
			ctx, cancel := detachedVideoBillingContext(c)
			billingErr := h.videoTaskBilling.ApplyOutcome(ctx, billingTask, outcome)
			cancel()
			if billingErr != nil {
				logger.L().Error("openai_video.billing_lookup_observation_failed", zap.Int64("billing_task_id", billingTask.ID), zap.Error(billingErr))
			}
		}
	}
	if err != nil && !service.IsResponseCommitted(c) {
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Video upstream request failed")
	}
}
