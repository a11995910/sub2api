package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

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
