package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type videoTestTaskGateway interface {
	ResolveVideoTestTaskStoredAccount(ctx context.Context, accountID int64, platform string) (*service.Account, error)
	ForwardOpenAIVideoStatus(ctx context.Context, c *gin.Context, account *service.Account, taskID string) (*service.OpenAIForwardResult, error)
	ForwardOpenAIVideoContent(ctx context.Context, c *gin.Context, account *service.Account, taskID string) (*service.OpenAIForwardResult, error)
	ForwardGrokMedia(ctx context.Context, c *gin.Context, account *service.Account, endpoint service.GrokMediaEndpoint, requestID string, body []byte, contentType string) (*service.OpenAIForwardResult, error)
}

type VideoTestTaskHandler struct {
	tasks        *service.VideoTestTaskService
	gateway      videoTestTaskGateway
	contentStore *service.VideoTestTaskContentStore
}

func NewVideoTestTaskHandler(
	tasks *service.VideoTestTaskService,
	gateway videoTestTaskGateway,
	contentStore *service.VideoTestTaskContentStore,
) *VideoTestTaskHandler {
	return &VideoTestTaskHandler{tasks: tasks, gateway: gateway, contentStore: contentStore}
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
	if h.serveStoredContent(c, task) {
		return
	}
	account, err := h.gateway.ResolveVideoTestTaskStoredAccount(c.Request.Context(), task.AccountID, task.Platform)
	if err != nil {
		videoTestTaskJSONError(c, http.StatusBadGateway, "video upstream account is unavailable")
		return
	}
	cacheWriter, cacheResponse := h.startContentCache(c, task.ID)
	if cacheWriter != nil {
		defer cacheWriter.Abort()
	}
	if task.Platform == service.PlatformGrok {
		_, err = h.gateway.ForwardGrokMedia(c.Request.Context(), c, account, service.GrokMediaEndpointVideoContent, task.UpstreamTaskID, nil, "")
	} else {
		_, err = h.gateway.ForwardOpenAIVideoContent(c.Request.Context(), c, account, task.UpstreamTaskID)
	}
	if err != nil && !c.Writer.Written() {
		videoTestTaskJSONError(c, http.StatusBadGateway, "video content is temporarily unavailable")
		return
	}
	if err == nil && cacheWriter != nil && cacheResponse.cacheComplete() {
		createdAt := task.UpdatedAt.UTC()
		if task.CompletedAt != nil {
			createdAt = task.CompletedAt.UTC()
		}
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		if _, commitErr := cacheWriter.Commit(createdAt); commitErr != nil {
			logger.L().Warn("video_test_task.content_cache_failed",
				zap.String("task_id", task.ID), zap.Error(commitErr))
		}
	}
}

func (h *VideoTestTaskHandler) Delete(c *gin.Context) {
	userID, ok := videoTestTaskUserID(c)
	if !ok || h == nil || h.tasks == nil {
		videoTestTaskJSONError(c, http.StatusUnauthorized, "authentication required")
		return
	}
	task, err := h.tasks.Get(c.Request.Context(), userID, c.Param("id"))
	if err != nil {
		h.writeTaskError(c, err)
		return
	}
	if err := h.tasks.Delete(c.Request.Context(), userID, task.ID); err != nil {
		h.writeTaskError(c, err)
		return
	}
	if h.contentStore != nil {
		if err := h.contentStore.Delete(task.ID); err != nil {
			logger.L().Warn("video_test_task.content_delete_failed",
				zap.String("task_id", task.ID), zap.Error(err))
		}
	}
	c.Status(http.StatusNoContent)
	c.Writer.WriteHeaderNow()
}

func (h *VideoTestTaskHandler) serveStoredContent(c *gin.Context, task *service.VideoTestTask) bool {
	if h == nil || h.contentStore == nil || c == nil || task == nil {
		return false
	}
	content, err := h.contentStore.Resolve(task.ID, time.Now().UTC())
	if err != nil {
		return false
	}
	file, err := os.Open(content.Path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()
	c.Header("Content-Type", "video/mp4")
	c.Header("Accept-Ranges", "bytes")
	c.Header("Cache-Control", "private, max-age=3600")
	http.ServeContent(c.Writer, c.Request, task.ID+".mp4", content.CreatedAt, file)
	return true
}

func (h *VideoTestTaskHandler) startContentCache(
	c *gin.Context,
	taskID string,
) (*service.VideoTestTaskContentWriter, *videoTestTaskContentCacheResponseWriter) {
	if h == nil || h.contentStore == nil || c == nil || strings.TrimSpace(c.GetHeader("Range")) != "" {
		return nil, nil
	}
	writer, err := h.contentStore.Begin(c.Request.Context(), taskID)
	if err != nil {
		logger.L().Warn("video_test_task.content_cache_start_failed",
			zap.String("task_id", taskID), zap.Error(err))
		return nil, nil
	}
	response := &videoTestTaskContentCacheResponseWriter{ResponseWriter: c.Writer, cache: writer}
	c.Writer = response
	return writer, response
}

type videoTestTaskContentCacheResponseWriter struct {
	gin.ResponseWriter
	cache      *service.VideoTestTaskContentWriter
	cacheable  bool
	cacheErr   error
	written    int64
	expected   int64
	statusCode int
	headerSeen bool
}

func (w *videoTestTaskContentCacheResponseWriter) WriteHeader(statusCode int) {
	if !w.headerSeen {
		w.headerSeen = true
		w.statusCode = statusCode
	}
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *videoTestTaskContentCacheResponseWriter) Write(data []byte) (int, error) {
	if !w.headerSeen {
		w.WriteHeader(http.StatusOK)
	}
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(w.Header().Get("Content-Type"), ";")[0]))
	w.cacheable = w.statusCode == http.StatusOK &&
		(contentType == "video/mp4" || contentType == "application/octet-stream") &&
		strings.TrimSpace(w.Header().Get("Content-Range")) == ""
	if w.expected == 0 {
		if length, parseErr := strconv.ParseInt(strings.TrimSpace(w.Header().Get("Content-Length")), 10, 64); parseErr == nil && length > 0 {
			w.expected = length
		}
	}
	n, err := w.ResponseWriter.Write(data)
	if w.cacheable && w.cacheErr == nil && n > 0 {
		cached, cacheErr := w.cache.Write(data[:n])
		w.written += int64(cached)
		if cacheErr != nil {
			w.cacheErr = cacheErr
		} else if cached != n {
			w.cacheErr = errors.New("video content cache short write")
		}
	}
	return n, err
}

func (w *videoTestTaskContentCacheResponseWriter) cacheComplete() bool {
	return w != nil && w.cacheable && w.cacheErr == nil && w.written > 0 && (w.expected == 0 || w.expected == w.written)
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
