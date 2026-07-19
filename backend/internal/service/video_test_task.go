package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const videoTestTaskPollMinInterval = 5 * time.Second

const (
	VideoTestTaskStatusQueued     = "queued"
	VideoTestTaskStatusInProgress = "in_progress"
	VideoTestTaskStatusCompleted  = "completed"
	VideoTestTaskStatusFailed     = "failed"
)

var ErrVideoTestTaskNotFound = errors.New("video test task not found")

type VideoTestTask struct {
	ID                  string          `json:"id"`
	UserID              int64           `json:"-"`
	APIKeyID            int64           `json:"api_key_id"`
	GroupID             int64           `json:"group_id"`
	AccountID           int64           `json:"-"`
	UpstreamTaskID      string          `json:"upstream_task_id"`
	Platform            string          `json:"platform"`
	Model               string          `json:"model"`
	Prompt              string          `json:"prompt"`
	Resolution          string          `json:"resolution,omitempty"`
	DurationSeconds     int             `json:"duration_seconds,omitempty"`
	ReferenceImageCount int             `json:"reference_image_count"`
	Status              string          `json:"status"`
	Progress            *float64        `json:"progress,omitempty"`
	ResponseJSON        json.RawMessage `json:"response,omitempty"`
	ErrorMessage        string          `json:"error_message,omitempty"`
	LastPollError       string          `json:"last_poll_error,omitempty"`
	LastPolledAt        *time.Time      `json:"last_polled_at,omitempty"`
	CompletedAt         *time.Time      `json:"completed_at,omitempty"`
	FailedAt            *time.Time      `json:"failed_at,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type VideoTestTaskStore interface {
	Create(ctx context.Context, task *VideoTestTask) error
	GetByOwner(ctx context.Context, userID int64, id string) (*VideoTestTask, error)
	GetByUpstreamOwner(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string) (*VideoTestTask, error)
	ListByUser(ctx context.Context, userID int64, offset, limit int) ([]VideoTestTask, int64, error)
	UpdatePollResult(ctx context.Context, task *VideoTestTask) error
	DeleteByOwner(ctx context.Context, userID int64, id string) error
	DeleteExpiredTerminal(ctx context.Context, cutoff time.Time) (int64, error)
}

type VideoTestTaskAcceptedInput struct {
	UserID              int64
	APIKeyID            int64
	GroupID             int64
	AccountID           int64
	UpstreamTaskID      string
	Platform            string
	Model               string
	Prompt              string
	Resolution          string
	DurationSeconds     int
	ReferenceImageCount int
	Status              string
	Progress            *float64
	ResponseJSON        json.RawMessage
}

type VideoTestTaskPollResult struct {
	Status       string
	Progress     *float64
	ResponseJSON json.RawMessage
	ErrorMessage string
}

type VideoTestTaskPage struct {
	Items    []VideoTestTask `json:"items"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type VideoTestTaskService struct {
	store VideoTestTaskStore
	now   func() time.Time
}

func NewVideoTestTaskService(store VideoTestTaskStore) *VideoTestTaskService {
	return NewVideoTestTaskServiceWithClock(store, func() time.Time { return time.Now().UTC() })
}

func NewVideoTestTaskServiceWithClock(store VideoTestTaskStore, now func() time.Time) *VideoTestTaskService {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &VideoTestTaskService{store: store, now: now}
}

func (s *VideoTestTaskService) RecordAccepted(ctx context.Context, input VideoTestTaskAcceptedInput) (*VideoTestTask, error) {
	if s == nil || s.store == nil {
		return nil, fmt.Errorf("video test task store is unavailable")
	}
	input.UpstreamTaskID = strings.TrimSpace(input.UpstreamTaskID)
	input.Model = strings.TrimSpace(input.Model)
	input.Platform = strings.TrimSpace(input.Platform)
	if input.UserID <= 0 || input.APIKeyID <= 0 || input.GroupID <= 0 || input.AccountID <= 0 || input.UpstreamTaskID == "" || input.Model == "" {
		return nil, fmt.Errorf("video test task input is invalid")
	}
	status := normalizeVideoTestTaskStatus(input.Status)
	if status == "" {
		status = VideoTestTaskStatusQueued
	}
	now := s.now().UTC()
	task := &VideoTestTask{
		ID:                  uuid.NewString(),
		UserID:              input.UserID,
		APIKeyID:            input.APIKeyID,
		GroupID:             input.GroupID,
		AccountID:           input.AccountID,
		UpstreamTaskID:      input.UpstreamTaskID,
		Platform:            input.Platform,
		Model:               input.Model,
		Prompt:              input.Prompt,
		Resolution:          strings.TrimSpace(input.Resolution),
		DurationSeconds:     max(input.DurationSeconds, 0),
		ReferenceImageCount: max(input.ReferenceImageCount, 0),
		Status:              status,
		Progress:            input.Progress,
		ResponseJSON:        input.ResponseJSON,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if status == VideoTestTaskStatusCompleted {
		task.CompletedAt = &now
	}
	if status == VideoTestTaskStatusFailed {
		task.FailedAt = &now
	}
	if err := s.store.Create(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *VideoTestTaskService) Get(ctx context.Context, userID int64, id string) (*VideoTestTask, error) {
	if s == nil || s.store == nil || userID <= 0 || strings.TrimSpace(id) == "" {
		return nil, ErrVideoTestTaskNotFound
	}
	return s.store.GetByOwner(ctx, userID, strings.TrimSpace(id))
}

func (s *VideoTestTaskService) List(ctx context.Context, userID int64, page, pageSize int) (*VideoTestTaskPage, error) {
	if s == nil || s.store == nil || userID <= 0 {
		return nil, ErrVideoTestTaskNotFound
	}
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := s.store.ListByUser(ctx, userID, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, err
	}
	return &VideoTestTaskPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *VideoTestTaskService) ShouldPoll(task *VideoTestTask) bool {
	if s == nil || task == nil || isVideoTestTaskTerminal(task.Status) {
		return false
	}
	return task.LastPolledAt == nil || !task.LastPolledAt.Add(videoTestTaskPollMinInterval).After(s.now())
}

func (s *VideoTestTaskService) ApplyPollResult(ctx context.Context, userID int64, id string, result VideoTestTaskPollResult) (*VideoTestTask, error) {
	task, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	task.LastPolledAt = &now
	task.LastPollError = ""
	if len(result.ResponseJSON) > 0 {
		task.ResponseJSON = append(json.RawMessage(nil), result.ResponseJSON...)
	}
	if result.Progress != nil {
		task.Progress = result.Progress
	}
	if !isVideoTestTaskTerminal(task.Status) {
		next := normalizeVideoTestTaskStatus(result.Status)
		if next != "" {
			task.Status = next
		}
		if next == VideoTestTaskStatusCompleted {
			task.CompletedAt = &now
			task.ErrorMessage = ""
		} else if next == VideoTestTaskStatusFailed {
			task.FailedAt = &now
			task.ErrorMessage = strings.TrimSpace(result.ErrorMessage)
		}
	}
	task.UpdatedAt = now
	if err := s.store.UpdatePollResult(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *VideoTestTaskService) RecordPollError(ctx context.Context, userID int64, id, message string) (*VideoTestTask, error) {
	task, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	task.LastPolledAt = &now
	if !isVideoTestTaskTerminal(task.Status) {
		task.LastPollError = strings.TrimSpace(message)
	}
	task.UpdatedAt = now
	if err := s.store.UpdatePollResult(ctx, task); err != nil {
		return nil, err
	}
	return task, nil
}

func (s *VideoTestTaskService) ResolveAccountID(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string) (int64, error) {
	if s == nil || s.store == nil {
		return 0, ErrVideoTestTaskNotFound
	}
	task, err := s.store.GetByUpstreamOwner(ctx, userID, apiKeyID, strings.TrimSpace(upstreamTaskID))
	if err != nil {
		return 0, err
	}
	return task.AccountID, nil
}

func (s *VideoTestTaskService) Delete(ctx context.Context, userID int64, id string) error {
	if s == nil || s.store == nil {
		return ErrVideoTestTaskNotFound
	}
	return s.store.DeleteByOwner(ctx, userID, strings.TrimSpace(id))
}

func normalizeVideoTestTaskStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending", "submitted":
		return VideoTestTaskStatusQueued
	case "running", "processing", "in_progress", "in-progress":
		return VideoTestTaskStatusInProgress
	case "succeeded", "success", "completed", "complete", "done":
		return VideoTestTaskStatusCompleted
	case "failed", "error", "cancelled", "canceled":
		return VideoTestTaskStatusFailed
	default:
		return ""
	}
}

func isVideoTestTaskTerminal(status string) bool {
	return status == VideoTestTaskStatusCompleted || status == VideoTestTaskStatusFailed
}
