package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	UpstreamRateMonitorStatusUnknown = "unknown"
	UpstreamRateMonitorStatusSuccess = "success"
	UpstreamRateMonitorStatusFailed  = "failed"

	upstreamRateMonitorMaxPageSize = 100
	upstreamRateMonitorHTTPTimeout = 20 * time.Second
	upstreamRateMonitorMaxBodySize = 4 * 1024 * 1024
)

var (
	ErrUpstreamRateMonitorNotFound = infraerrors.NotFound(
		"UPSTREAM_RATE_MONITOR_NOT_FOUND", "upstream rate monitor not found",
	)
	ErrUpstreamRateMonitorInvalidURL = infraerrors.BadRequest(
		"UPSTREAM_RATE_MONITOR_INVALID_URL", "upstream url must be a valid http or https base url",
	)
	ErrUpstreamRateMonitorInvalidURLPath = infraerrors.BadRequest(
		"UPSTREAM_RATE_MONITOR_INVALID_URL_PATH", "upstream url must not contain query or fragment",
	)
	ErrUpstreamRateMonitorPrivateHost = infraerrors.BadRequest(
		"UPSTREAM_RATE_MONITOR_PRIVATE_HOST", "upstream host must not be localhost or private network",
	)
	ErrUpstreamRateMonitorMissingName = infraerrors.BadRequest(
		"UPSTREAM_RATE_MONITOR_MISSING_NAME", "name is required",
	)
	ErrUpstreamRateMonitorMissingUsername = infraerrors.BadRequest(
		"UPSTREAM_RATE_MONITOR_MISSING_USERNAME", "username is required",
	)
	ErrUpstreamRateMonitorMissingPassword = infraerrors.BadRequest(
		"UPSTREAM_RATE_MONITOR_MISSING_PASSWORD", "password is required",
	)
	ErrUpstreamRateMonitorPasswordDecryptFailed = infraerrors.InternalServer(
		"UPSTREAM_RATE_MONITOR_PASSWORD_DECRYPT_FAILED", "stored password cannot be decrypted; please update it",
	)
)

type UpstreamRateMonitorRepository interface {
	Create(ctx context.Context, m *UpstreamRateMonitor) error
	GetByID(ctx context.Context, id int64) (*UpstreamRateMonitor, error)
	Update(ctx context.Context, m *UpstreamRateMonitor) error
	Delete(ctx context.Context, id int64) error
	List(ctx context.Context, params UpstreamRateMonitorListParams) ([]*UpstreamRateMonitor, int64, error)
	UpdateSnapshot(ctx context.Context, id int64, snapshot UpstreamRateSnapshot, checkedAt time.Time) error
	MarkRefreshFailed(ctx context.Context, id int64, message string, checkedAt time.Time) error
}

type UpstreamRateMonitor struct {
	ID                int64
	Name              string
	BaseURL           string
	Username          string
	PasswordEncrypted string
	Password          string
	Enabled           bool
	LastCheckedAt     *time.Time
	LastStatus        string
	LastError         string
	LastGroupCount    int
	LastSnapshot      UpstreamRateSnapshot
	CreatedBy         int64
	CreatedAt         time.Time
	UpdatedAt         time.Time

	PasswordDecryptFailed bool
}

type UpstreamRateGroupSnapshot struct {
	ID                          int64    `json:"id"`
	Name                        string   `json:"name"`
	Description                 string   `json:"description,omitempty"`
	Platform                    string   `json:"platform,omitempty"`
	RateMultiplier              float64  `json:"rate_multiplier"`
	ImageRateMultiplier         *float64 `json:"image_rate_multiplier,omitempty"`
	ImageRateIndependent        bool     `json:"image_rate_independent,omitempty"`
	SubscriptionType            string   `json:"subscription_type,omitempty"`
	IsExclusive                 bool     `json:"is_exclusive"`
	Status                      string   `json:"status,omitempty"`
	RPMLimit                    int      `json:"rpm_limit,omitempty"`
	AllowImageGeneration        bool     `json:"allow_image_generation,omitempty"`
	ImageSuperResolutionEnabled bool     `json:"image_super_resolution_enabled,omitempty"`
	SortOrder                   int      `json:"sort_order,omitempty"`
}

type UpstreamRateSnapshot []UpstreamRateGroupSnapshot

type UpstreamRateMonitorListParams struct {
	Page     int
	PageSize int
	Enabled  *bool
	Search   string
}

type UpstreamRateMonitorCreateParams struct {
	Name      string
	BaseURL   string
	Username  string
	Password  string
	Enabled   bool
	CreatedBy int64
}

type UpstreamRateMonitorUpdateParams struct {
	Name     *string
	BaseURL  *string
	Username *string
	Password *string
	Enabled  *bool
}
