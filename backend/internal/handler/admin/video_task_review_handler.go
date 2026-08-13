package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type videoTaskReviewHandlerService interface {
	List(context.Context, service.VideoTaskReviewFilter) ([]service.VideoTaskReviewItem, int64, error)
	ConfirmFailed(context.Context, int64, string) error
	ConfirmSucceeded(context.Context, int64, string) error
	Recheck(context.Context, int64) error
}

type VideoTaskReviewHandler struct {
	reviewService videoTaskReviewHandlerService
}

type videoTaskReviewResponse struct {
	ID                  int64      `json:"id"`
	RequestID           string     `json:"request_id"`
	UpstreamTaskID      string     `json:"upstream_task_id"`
	Platform            string     `json:"platform"`
	UserID              int64      `json:"user_id"`
	UserEmail           string     `json:"user_email"`
	Username            string     `json:"username"`
	APIKeyID            int64      `json:"api_key_id"`
	APIKeyName          string     `json:"api_key_name"`
	GroupID             *int64     `json:"group_id,omitempty"`
	AccountID           int64      `json:"account_id"`
	Model               string     `json:"model"`
	UpstreamModel       string     `json:"upstream_model"`
	Resolution          string     `json:"resolution"`
	DurationSeconds     int        `json:"duration_seconds"`
	ReferenceImageCount int        `json:"reference_image_count"`
	EstimatedCost       float64    `json:"estimated_cost"`
	ActualCost          *float64   `json:"actual_cost,omitempty"`
	TaskStatus          string     `json:"task_status"`
	BillingStatus       string     `json:"billing_status"`
	PollCount           int        `json:"poll_count"`
	LastPolledAt        *time.Time `json:"last_polled_at,omitempty"`
	NextPollAt          time.Time  `json:"next_poll_at"`
	LastPollError       string     `json:"last_poll_error"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func NewVideoTaskReviewHandler(reviewService *service.VideoTaskReviewService) *VideoTaskReviewHandler {
	return &VideoTaskReviewHandler{reviewService: reviewService}
}

func (h *VideoTaskReviewHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	filter := service.VideoTaskReviewFilter{
		Page: page, PageSize: pageSize, Search: strings.TrimSpace(c.Query("search")),
		Platform: strings.TrimSpace(c.Query("platform")), Model: strings.TrimSpace(c.Query("model")),
		TaskStatus: strings.TrimSpace(c.Query("task_status")), BillingStatus: strings.TrimSpace(c.Query("billing_status")),
	}
	var err error
	if filter.UserID, err = parseOptionalReviewID(c, "user_id"); err != nil {
		return
	}
	if filter.APIKeyID, err = parseOptionalReviewID(c, "api_key_id"); err != nil {
		return
	}
	if filter.AccountID, err = parseOptionalReviewID(c, "account_id"); err != nil {
		return
	}
	if filter.StartTime, err = parseOptionalReviewTime(c, "start_time"); err != nil {
		return
	}
	if filter.EndTime, err = parseOptionalReviewTime(c, "end_time"); err != nil {
		return
	}
	items, total, err := h.reviewService.List(c.Request.Context(), filter)
	if err != nil {
		response.InternalError(c, "failed to list video task reviews")
		return
	}
	out := make([]videoTaskReviewResponse, 0, len(items))
	for _, item := range items {
		if item.Task == nil {
			continue
		}
		task := item.Task
		out = append(out, videoTaskReviewResponse{
			ID: task.ID, RequestID: task.RequestID, UpstreamTaskID: task.UpstreamTaskID, Platform: task.Platform,
			UserID: task.UserID, UserEmail: item.UserEmail, Username: item.Username,
			APIKeyID: task.APIKeyID, APIKeyName: item.APIKeyName, GroupID: task.GroupID, AccountID: task.AccountID,
			Model: task.Model, UpstreamModel: task.UpstreamModel, Resolution: task.Resolution,
			DurationSeconds: task.DurationSeconds, ReferenceImageCount: task.ReferenceImageCount,
			EstimatedCost: task.EstimatedCost, ActualCost: task.ActualCost, TaskStatus: task.TaskStatus,
			BillingStatus: task.BillingStatus, PollCount: task.PollCount, LastPolledAt: task.LastPolledAt,
			NextPollAt: task.NextPollAt, LastPollError: task.LastPollError, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt,
		})
	}
	response.Paginated(c, out, total, page, pageSize)
}

func (h *VideoTaskReviewHandler) Recheck(c *gin.Context) {
	id, ok := parseReviewTaskID(c)
	if !ok {
		return
	}
	h.handleActionError(c, h.reviewService.Recheck(c.Request.Context(), id))
}

func (h *VideoTaskReviewHandler) ConfirmFailed(c *gin.Context) {
	h.confirm(c, h.reviewService.ConfirmFailed)
}

func (h *VideoTaskReviewHandler) ConfirmSucceeded(c *gin.Context) {
	h.confirm(c, h.reviewService.ConfirmSucceeded)
}

func (h *VideoTaskReviewHandler) confirm(c *gin.Context, action func(context.Context, int64, string) error) {
	id, ok := parseReviewTaskID(c)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Reason) == "" {
		response.BadRequest(c, "review reason is required")
		return
	}
	h.handleActionError(c, action(c.Request.Context(), id, strings.TrimSpace(req.Reason)))
}

func (h *VideoTaskReviewHandler) handleActionError(c *gin.Context, err error) {
	switch {
	case err == nil:
		response.Success(c, gin.H{"success": true})
	case errors.Is(err, service.ErrVideoTaskReviewCannotRecheck):
		response.BadRequest(c, "upstream task id is required for recheck")
	case errors.Is(err, service.ErrVideoTaskBillingNotFound):
		response.NotFound(c, "video task review not found")
	case errors.Is(err, service.ErrVideoTaskBillingInvalidState):
		response.Error(c, http.StatusConflict, "video task review state changed; refresh and retry")
	default:
		response.InternalError(c, "video task review action failed")
	}
}

func parseReviewTaskID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid video task id")
		return 0, false
	}
	return id, true
}

func parseOptionalReviewID(c *gin.Context, key string) (int64, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid "+key)
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func parseOptionalReviewTime(c *gin.Context, key string) (*time.Time, error) {
	value := strings.TrimSpace(c.Query(key))
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		response.BadRequest(c, "invalid "+key+", use RFC3339")
		return nil, err
	}
	return &parsed, nil
}
