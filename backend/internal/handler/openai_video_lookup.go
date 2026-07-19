package handler

import (
	"net/http"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
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
	if content {
		_, err = h.gatewayService.ForwardOpenAIVideoContent(c.Request.Context(), c, account, taskID)
	} else {
		_, err = h.gatewayService.ForwardOpenAIVideoStatus(c.Request.Context(), c, account, taskID)
	}
	if err != nil && !service.IsResponseCommitted(c) {
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "Video upstream request failed")
	}
}
