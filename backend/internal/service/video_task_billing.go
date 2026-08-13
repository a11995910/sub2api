package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrVideoTaskBillingNotFound                = errors.New("video task billing not found")
	ErrVideoTaskBillingInvalidState            = errors.New("video task billing state is invalid")
	ErrVideoTaskSettlementCostExceedsHold      = errors.New("video task settlement cost exceeds hold")
	ErrVideoTaskFrozenBalanceInvariantViolated = errors.New("video task frozen balance invariant violated")
)

type VideoTaskSubmissionUncertainError struct {
	cause error
}

func NewVideoTaskSubmissionUncertainError(cause error) error {
	if cause == nil {
		cause = errors.New("video task submission outcome is unknown")
	}
	return &VideoTaskSubmissionUncertainError{cause: cause}
}

func (e *VideoTaskSubmissionUncertainError) Error() string { return e.cause.Error() }
func (e *VideoTaskSubmissionUncertainError) Unwrap() error { return e.cause }

func IsVideoTaskSubmissionUncertain(err error) bool {
	if err == nil {
		return false
	}
	var uncertain *VideoTaskSubmissionUncertainError
	if errors.As(err, &uncertain) {
		return true
	}
	var upstream *UpstreamFailoverError
	return errors.As(err, &upstream) && (upstream.StatusCode == 0 || upstream.StatusCode == 429 || upstream.StatusCode >= 500)
}

func videoTaskErrorAfterSubmission(err error) error {
	if err == nil || IsVideoTaskSubmissionUncertain(err) {
		return err
	}
	var upstream *UpstreamFailoverError
	if errors.As(err, &upstream) && upstream.StatusCode >= 400 && upstream.StatusCode < 500 && upstream.StatusCode != 429 {
		return err
	}
	return NewVideoTaskSubmissionUncertainError(err)
}

const (
	VideoTaskStatusSubmitting        = "submitting"
	VideoTaskStatusPending           = "pending"
	VideoTaskStatusProcessing        = "processing"
	VideoTaskStatusUnknown           = "unknown"
	VideoTaskStatusCompleted         = "completed"
	VideoTaskStatusFailed            = "failed"
	VideoTaskStatusSubmissionUnknown = "submission_unknown"

	VideoTaskBillingReserved     = "reserved"
	VideoTaskBillingSettling     = "settling"
	VideoTaskBillingCaptured     = "captured"
	VideoTaskBillingReleased     = "released"
	VideoTaskBillingManualReview = "manual_review"
)

type VideoTaskBilling struct {
	ID                  int64
	RequestID           string
	UpstreamTaskID      string
	Platform            string
	UserID              int64
	APIKeyID            int64
	GroupID             *int64
	AccountID           int64
	Model               string
	UpstreamModel       string
	Resolution          string
	DurationSeconds     int
	ReferenceImageCount int
	UsageContextJSON    json.RawMessage
	EstimatedCost       float64
	ActualCost          *float64
	TaskStatus          string
	BillingStatus       string
	ResponseJSON        json.RawMessage
	PollCount           int
	LastPolledAt        *time.Time
	NextPollAt          time.Time
	LastPollError       string
	SubmissionDeadline  *time.Time
	TerminalAt          *time.Time
	ClaimedUntil        *time.Time
	UpdatedAt           time.Time
	CreatedAt           time.Time
}

type VideoTaskBillingRepository interface {
	ReserveAndCreate(ctx context.Context, task *VideoTaskBilling) error
	GetByID(ctx context.Context, id int64) (*VideoTaskBilling, error)
	GetByTask(ctx context.Context, platform, taskID string) (*VideoTaskBilling, error)
	AttachUpstreamTask(ctx context.Context, id int64, taskID, status string, response json.RawMessage) (*VideoTaskBilling, error)
	MarkSubmissionUnknown(ctx context.Context, id int64, reason string) error
	ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]*VideoTaskBilling, error)
	UpdatePoll(ctx context.Context, id int64, outcome VideoTaskOutcome, nextPollAt time.Time) (*VideoTaskBilling, error)
	BeginSettlement(ctx context.Context, id int64) (*VideoTaskBilling, error)
	Capture(ctx context.Context, id int64, actualCost float64) error
	Release(ctx context.Context, id int64, reason string) error
}

type VideoTaskDeletionGuard interface {
	WithUserDeletionGuard(ctx context.Context, userID int64, deleteFunc func() error) error
	WithAccountDeletionGuard(ctx context.Context, accountIDs []int64, deleteFunc func() error) error
}

type VideoTaskOutcome struct {
	Status       string
	ResponseJSON json.RawMessage
	ErrorMessage string
}

func ClassifyVideoTaskResult(result *OpenAIForwardResult) VideoTaskOutcome {
	if result == nil {
		return VideoTaskOutcome{Status: VideoTaskStatusUnknown, ErrorMessage: "video task result is empty"}
	}
	outcome := VideoTaskOutcome{
		ResponseJSON: append(json.RawMessage(nil), result.VideoResponseJSON...),
		ErrorMessage: result.VideoErrorMessage,
	}
	switch NormalizeOpenAIVideoStatus(result.VideoStatus) {
	case "queued":
		outcome.Status = VideoTaskStatusPending
	case "in_progress":
		outcome.Status = VideoTaskStatusProcessing
	case "completed":
		if result.VideoArtifactAvailable {
			outcome.Status = VideoTaskStatusCompleted
		} else {
			outcome.Status = VideoTaskStatusUnknown
			outcome.ErrorMessage = "video task completed without a verifiable artifact"
		}
	case "failed":
		if result.VideoArtifactAvailable {
			outcome.Status = VideoTaskStatusCompleted
		} else {
			outcome.Status = VideoTaskStatusFailed
		}
	default:
		outcome.Status = VideoTaskStatusUnknown
		if outcome.ErrorMessage == "" {
			outcome.ErrorMessage = "video task returned an unknown status"
		}
	}
	return outcome
}

type VideoTaskUsageRecorder interface {
	RecordDeferredVideoUsage(ctx context.Context, task *VideoTaskBilling) error
}

type VideoTaskBalanceCache interface {
	InvalidateUserBalance(ctx context.Context, userID int64) error
}

type VideoTaskCostEstimator interface {
	EstimateVideoCost(ctx context.Context, apiKey *APIKey, model, resolution string, durationSeconds int) (*CostBreakdown, error)
}

type VideoTaskReserveInput struct {
	RequestID           string
	Platform            string
	UserID              int64
	APIKeyID            int64
	GroupID             *int64
	AccountID           int64
	APIKey              *APIKey
	Model               string
	UpstreamModel       string
	Resolution          string
	DurationSeconds     int
	ReferenceImageCount int
	UsageContextJSON    json.RawMessage
}

type VideoTaskBillingService struct {
	repo      VideoTaskBillingRepository
	estimator VideoTaskCostEstimator
	usage     VideoTaskUsageRecorder
	cache     VideoTaskBalanceCache
	now       func() time.Time
}

func NewVideoTaskBillingService(repo VideoTaskBillingRepository, estimator VideoTaskCostEstimator, usage VideoTaskUsageRecorder, caches ...VideoTaskBalanceCache) *VideoTaskBillingService {
	var cache VideoTaskBalanceCache
	if len(caches) > 0 {
		cache = caches[0]
	}
	return &VideoTaskBillingService{
		repo:      repo,
		estimator: estimator,
		usage:     usage,
		cache:     cache,
		now:       func() time.Time { return time.Now().UTC() },
	}
}

func (s *VideoTaskBillingService) Reserve(ctx context.Context, input VideoTaskReserveInput) (*VideoTaskBilling, error) {
	if s == nil || s.repo == nil || s.estimator == nil {
		return nil, errors.New("video task billing service is unavailable")
	}
	if input.RequestID == "" || input.Platform == "" || input.UserID <= 0 || input.APIKeyID <= 0 || input.AccountID <= 0 || input.APIKey == nil || input.Model == "" {
		return nil, errors.New("video task reservation input is invalid")
	}
	cost, err := s.estimator.EstimateVideoCost(ctx, input.APIKey, input.Model, input.Resolution, input.DurationSeconds)
	if err != nil {
		return nil, err
	}
	if cost == nil || cost.ActualCost < 0 {
		return nil, errors.New("video task estimated cost is invalid")
	}
	estimatedCost := QuantizeUsageBillingAmount(cost.ActualCost)
	costSnapshot := *cost
	costSnapshot.ActualCost = estimatedCost
	var usageContext VideoTaskUsageContext
	if len(input.UsageContextJSON) > 0 {
		if err := json.Unmarshal(input.UsageContextJSON, &usageContext); err != nil {
			return nil, fmt.Errorf("decode video task usage context: %w", err)
		}
	}
	usageContext.CostSnapshot = &costSnapshot
	usageContextJSON, err := json.Marshal(usageContext)
	if err != nil {
		return nil, fmt.Errorf("encode video task usage context: %w", err)
	}
	now := s.now()
	task := &VideoTaskBilling{
		RequestID: input.RequestID, Platform: input.Platform, UserID: input.UserID, APIKeyID: input.APIKeyID,
		GroupID: input.GroupID, AccountID: input.AccountID, Model: input.Model, UpstreamModel: input.UpstreamModel,
		Resolution: input.Resolution, DurationSeconds: input.DurationSeconds, ReferenceImageCount: input.ReferenceImageCount,
		UsageContextJSON: usageContextJSON, EstimatedCost: estimatedCost,
		TaskStatus: VideoTaskStatusSubmitting, BillingStatus: VideoTaskBillingReserved,
		NextPollAt: now, SubmissionDeadline: timePointer(now.Add(2 * time.Minute)),
	}
	if err := s.repo.ReserveAndCreate(ctx, task); err != nil {
		return nil, err
	}
	s.invalidateBalance(ctx, task.UserID)
	return task, nil
}

func (s *VideoTaskBillingService) invalidateBalance(ctx context.Context, userID int64) {
	if s != nil && s.cache != nil && userID > 0 {
		_ = s.cache.InvalidateUserBalance(ctx, userID)
	}
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func (s *VideoTaskBillingService) ObserveCreated(ctx context.Context, task *VideoTaskBilling, taskID string, result *OpenAIForwardResult) (*VideoTaskBilling, error) {
	if s == nil || s.repo == nil || task == nil || task.ID <= 0 || taskID == "" {
		return nil, errors.New("video task created outcome is invalid")
	}
	outcome := ClassifyVideoTaskResult(result)
	if outcome.Status == VideoTaskStatusUnknown && (result == nil || result.VideoStatus == "") {
		outcome.Status = VideoTaskStatusPending
		outcome.ErrorMessage = ""
	}
	attached, err := s.repo.AttachUpstreamTask(ctx, task.ID, taskID, outcome.Status, outcome.ResponseJSON)
	if err != nil {
		return nil, err
	}
	if outcome.Status == VideoTaskStatusCompleted || outcome.Status == VideoTaskStatusFailed {
		if err := s.ApplyOutcome(ctx, attached, outcome); err != nil {
			return attached, err
		}
	}
	return attached, nil
}

func (s *VideoTaskBillingService) MarkSubmissionUnknown(ctx context.Context, task *VideoTaskBilling, cause error) error {
	if s == nil || s.repo == nil || task == nil || task.ID <= 0 {
		return errors.New("video task submission outcome is invalid")
	}
	reason := "video submission outcome is unknown"
	if cause != nil && cause.Error() != "" {
		reason = cause.Error()
	}
	if err := s.repo.MarkSubmissionUnknown(ctx, task.ID, reason); err != nil {
		return err
	}
	task.TaskStatus = VideoTaskStatusSubmissionUnknown
	task.BillingStatus = VideoTaskBillingManualReview
	task.LastPollError = reason
	return nil
}

func (s *VideoTaskBillingService) ResolveOwnedTask(ctx context.Context, platform, taskID string, userID, apiKeyID int64) (*VideoTaskBilling, error) {
	if s == nil || s.repo == nil || platform == "" || taskID == "" || userID <= 0 || apiKeyID <= 0 {
		return nil, ErrVideoTaskBillingNotFound
	}
	task, err := s.repo.GetByTask(ctx, platform, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.UserID != userID || task.APIKeyID != apiKeyID {
		return nil, ErrVideoTaskBillingNotFound
	}
	return task, nil
}

func (s *VideoTaskBillingService) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]*VideoTaskBilling, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("video task billing service is unavailable")
	}
	return s.repo.ClaimDue(ctx, limit, lease)
}

func (s *VideoTaskBillingService) ApplyOutcome(ctx context.Context, task *VideoTaskBilling, outcome VideoTaskOutcome) error {
	if s == nil || s.repo == nil || task == nil {
		return errors.New("video task billing service is unavailable")
	}
	if task.BillingStatus == VideoTaskBillingCaptured || task.BillingStatus == VideoTaskBillingReleased {
		return nil
	}
	if task.BillingStatus == VideoTaskBillingSettling {
		if task.TaskStatus != VideoTaskStatusCompleted {
			return ErrVideoTaskBillingInvalidState
		}
		if outcome.Status != VideoTaskStatusCompleted {
			return nil
		}
		if s.usage == nil {
			return errors.New("video task usage recorder is unavailable")
		}
		if err := s.usage.RecordDeferredVideoUsage(ctx, task); err != nil {
			return err
		}
		err := s.repo.Capture(ctx, task.ID, task.EstimatedCost)
		if err == nil {
			s.invalidateBalance(ctx, task.UserID)
		}
		return err
	}
	switch outcome.Status {
	case VideoTaskStatusPending, VideoTaskStatusProcessing, VideoTaskStatusUnknown, VideoTaskStatusCompleted, VideoTaskStatusFailed:
	default:
		return fmt.Errorf("unsupported video task outcome %q", outcome.Status)
	}
	nextPollAt := s.now().Add(30 * time.Second)
	updated, err := s.repo.UpdatePoll(ctx, task.ID, outcome, nextPollAt)
	if err != nil {
		return err
	}
	switch outcome.Status {
	case VideoTaskStatusCompleted:
		settling, err := s.repo.BeginSettlement(ctx, updated.ID)
		if err != nil {
			return err
		}
		if s.usage == nil {
			return errors.New("video task usage recorder is unavailable")
		}
		if err := s.usage.RecordDeferredVideoUsage(ctx, settling); err != nil {
			return err
		}
		err = s.repo.Capture(ctx, settling.ID, settling.EstimatedCost)
		if err == nil {
			s.invalidateBalance(ctx, settling.UserID)
		}
		return err
	case VideoTaskStatusFailed:
		err = s.repo.Release(ctx, updated.ID, outcome.ErrorMessage)
		if err == nil {
			s.invalidateBalance(ctx, updated.UserID)
		}
		return err
	default:
		return nil
	}
}
