package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type videoTestTaskGateway interface {
	ResolveVideoTestTaskStoredAccount(ctx context.Context, accountID int64, platform string) (*service.Account, error)
	ForwardOpenAIVideoStatus(ctx context.Context, c *gin.Context, account *service.Account, taskID string) (*service.OpenAIForwardResult, error)
	ForwardOpenAIVideoContent(ctx context.Context, c *gin.Context, account *service.Account, taskID string) (*service.OpenAIForwardResult, error)
	ForwardGrokMedia(ctx context.Context, c *gin.Context, account *service.Account, endpoint service.GrokMediaEndpoint, requestID string, body []byte, contentType string) (*service.OpenAIForwardResult, error)
}

type VideoTestTaskHandler struct {
	tasks   *service.VideoTestTaskService
	gateway videoTestTaskGateway
}

func NewVideoTestTaskHandler(tasks *service.VideoTestTaskService, gateway videoTestTaskGateway) *VideoTestTaskHandler {
	return &VideoTestTaskHandler{tasks: tasks, gateway: gateway}
}

func (h *VideoTestTaskHandler) List(c *gin.Context) {
	userID, ok := videoTestTaskUserID(c)
	if !ok || h == nil || h.tasks == nil {
		videoTestTaskJSONError(c, http.StatusUnauthorized, "authentication required")
		return
	}
	page := positiveQueryInt(c, "page", 1)
	pageSize := positiveQueryInt(c, "page_size", 20)
	result, err := h.tasks.List(c.Request.Context(), userID, page, pageSize)
	if err != nil {
		videoTestTaskJSONError(c, http.StatusInternalServerError, "failed to list video test tasks")
		return
	}
	c.JSON(http.StatusOK, result)
}

func (h *VideoTestTaskHandler) Refresh(c *gin.Context) {
	userID, ok := videoTestTaskUserID(c)
	if !ok || h == nil || h.tasks == nil || h.gateway == nil {
		videoTestTaskJSONError(c, http.StatusUnauthorized, "authentication required")
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeTaskError(c, err)
		return
	}
	if !h.tasks.ShouldPoll(task) {
		c.JSON(http.StatusOK, task)
		return
	}
	account, err := h.gateway.ResolveVideoTestTaskStoredAccount(c.Request.Context(), task.AccountID, task.Platform)
	if err != nil {
		h.writePollError(c, userID, task.ID, err)
		return
	}
	service.SuppressOpenAIVideoResponse(c)
	var result *service.OpenAIForwardResult
	if task.Platform == service.PlatformGrok {
		result, err = h.gateway.ForwardGrokMedia(c.Request.Context(), c, account, service.GrokMediaEndpointVideoStatus, task.UpstreamTaskID, nil, "")
	} else {
		result, err = h.gateway.ForwardOpenAIVideoStatus(c.Request.Context(), c, account, task.UpstreamTaskID)
	}
	if err != nil {
		h.writePollError(c, userID, task.ID, err)
		return
	}
	if result == nil {
		h.writePollError(c, userID, task.ID, errors.New("empty upstream status response"))
		return
	}
	updated, err := h.tasks.ApplyPollResult(c.Request.Context(), userID, task.ID, service.VideoTestTaskPollResult{
		Status: result.VideoStatus, Progress: result.VideoProgress, ResponseJSON: result.VideoResponseJSON, ErrorMessage: result.VideoErrorMessage,
	})
	if err != nil {
		h.writeTaskError(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *VideoTestTaskHandler) Content(c *gin.Context) {
	userID, ok := videoTestTaskUserID(c)
	if !ok || h == nil || h.tasks == nil || h.gateway == nil {
		videoTestTaskJSONError(c, http.StatusUnauthorized, "authentication required")
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeTaskError(c, err)
		return
	}
	if task.Status != service.VideoTestTaskStatusCompleted {
		videoTestTaskJSONError(c, http.StatusConflict, "video test task is not completed")
		return
	}
	account, err := h.gateway.ResolveVideoTestTaskStoredAccount(c.Request.Context(), task.AccountID, task.Platform)
	if err != nil {
		videoTestTaskJSONError(c, http.StatusBadGateway, "video upstream account is unavailable")
		return
	}
	if task.Platform == service.PlatformGrok {
		_, err = h.gateway.ForwardGrokMedia(c.Request.Context(), c, account, service.GrokMediaEndpointVideoContent, task.UpstreamTaskID, nil, "")
	} else {
		_, err = h.gateway.ForwardOpenAIVideoContent(c.Request.Context(), c, account, task.UpstreamTaskID)
	}
	if err != nil && !c.Writer.Written() {
		videoTestTaskJSONError(c, http.StatusBadGateway, "video content is temporarily unavailable")
	}
}

func (h *VideoTestTaskHandler) Delete(c *gin.Context) {
	userID, ok := videoTestTaskUserID(c)
	if !ok || h == nil || h.tasks == nil {
		videoTestTaskJSONError(c, http.StatusUnauthorized, "authentication required")
		return
	}
	if err := h.tasks.Delete(c.Request.Context(), userID, c.Param("id")); err != nil {
		h.writeTaskError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

func (h *VideoTestTaskHandler) writePollError(c *gin.Context, userID int64, id string, pollErr error) {
	updated, err := h.tasks.RecordPollError(c.Request.Context(), userID, id, pollErr.Error())
	if err != nil {
		h.writeTaskError(c, err)
		return
	}
	if !c.Writer.Written() {
		c.JSON(http.StatusOK, updated)
	}
}

func (h *VideoTestTaskHandler) writeTaskError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrVideoTestTaskNotFound) {
		videoTestTaskJSONError(c, http.StatusNotFound, "video test task not found")
		return
	}
	videoTestTaskJSONError(c, http.StatusInternalServerError, "video test task operation failed")
}

func videoTestTaskUserID(c *gin.Context) (int64, bool) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	return subject.UserID, ok && subject.UserID > 0
}

func positiveQueryInt(c *gin.Context, key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(c.Query(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func videoTestTaskJSONError(c *gin.Context, status int, message string) {
	if c.Writer.Written() {
		return
	}
	c.JSON(status, gin.H{"error": gin.H{"message": message}})
}
