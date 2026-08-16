package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrVideoTaskReviewCannotRecheck = errors.New("video task cannot be rechecked without upstream task id")

type VideoTaskReviewFilter struct {
	Page, PageSize            int
	UserID, APIKeyID          int64
	AccountID                 int64
	Search, Platform, Model   string
	TaskStatus, BillingStatus string
	StartTime, EndTime        *time.Time
}

type VideoTaskReviewItem struct {
	Task       *VideoTaskBilling `json:"task"`
	UserEmail  string            `json:"user_email"`
	Username   string            `json:"username"`
	APIKeyName string            `json:"api_key_name"`
}

type VideoTaskReviewRepository interface {
	ListReviewTasks(ctx context.Context, filter VideoTaskReviewFilter) ([]VideoTaskReviewItem, int64, error)
	BeginManualSettlement(ctx context.Context, id int64, reason string) (*VideoTaskBilling, error)
	ReleaseReviewedFailure(ctx context.Context, id int64, reason string) (*VideoTaskBilling, error)
	UpdateReviewObservation(ctx context.Context, id int64, outcome VideoTaskOutcome) error
}

type VideoTaskReviewService struct {
	repo     VideoTaskReviewRepository
	billing  *VideoTaskBillingService
	gateway  VideoTaskQueryGateway
	accounts AccountRepository
}

func NewVideoTaskReviewService(repo VideoTaskReviewRepository, billing *VideoTaskBillingService, gateway VideoTaskQueryGateway, accounts AccountRepository) *VideoTaskReviewService {
	return &VideoTaskReviewService{repo: repo, billing: billing, gateway: gateway, accounts: accounts}
}

func (s *VideoTaskReviewService) List(ctx context.Context, filter VideoTaskReviewFilter) ([]VideoTaskReviewItem, int64, error) {
	if s == nil || s.repo == nil {
		return nil, 0, errors.New("video task review service is unavailable")
	}
	return s.repo.ListReviewTasks(ctx, filter)
}

func (s *VideoTaskReviewService) ConfirmFailed(ctx context.Context, id int64, reason string) error {
	if s == nil || s.billing == nil || s.billing.repo == nil || id <= 0 || strings.TrimSpace(reason) == "" {
		return errors.New("video task review failure reason is required")
	}
	task, err := s.repo.ReleaseReviewedFailure(ctx, id, "人工确认失败: "+strings.TrimSpace(reason))
	if err != nil {
		return err
	}
	s.billing.invalidateBalance(ctx, task.UserID)
	return nil
}

func (s *VideoTaskReviewService) ConfirmSucceeded(ctx context.Context, id int64, reason string) error {
	if s == nil || s.repo == nil || s.billing == nil || s.billing.usage == nil || id <= 0 || strings.TrimSpace(reason) == "" {
		return errors.New("video task review success reason is required")
	}
	task, err := s.billing.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !isVideoTaskReviewCandidate(task) {
		return ErrVideoTaskBillingInvalidState
	}
	if task.BillingStatus != VideoTaskBillingSettling {
		task, err = s.repo.BeginManualSettlement(ctx, id, "人工确认成功: "+strings.TrimSpace(reason))
		if err != nil {
			return err
		}
	}
	if err := s.billing.usage.RecordDeferredVideoUsage(ctx, task); err != nil {
		return err
	}
	if err := s.billing.repo.Capture(ctx, task.ID, task.EstimatedCost); err != nil {
		return err
	}
	s.billing.invalidateBalance(ctx, task.UserID)
	return nil
}

func (s *VideoTaskReviewService) Recheck(ctx context.Context, id int64) error {
	if s == nil || s.repo == nil || s.billing == nil || s.billing.repo == nil {
		return errors.New("video task review service is unavailable")
	}
	task, err := s.billing.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if !isVideoTaskReviewCandidate(task) || task.BillingStatus == VideoTaskBillingSettling {
		return ErrVideoTaskBillingInvalidState
	}
	if strings.TrimSpace(task.UpstreamTaskID) == "" {
		return ErrVideoTaskReviewCannotRecheck
	}
	if s.gateway == nil || s.accounts == nil {
		return errors.New("video task review service is unavailable")
	}
	account, err := s.accounts.GetByID(ctx, task.AccountID)
	if err != nil {
		return err
	}
	var result *OpenAIForwardResult
	switch task.Platform {
	case PlatformOpenAI:
		result, err = s.gateway.QueryOpenAIVideoTask(ctx, account, task.UpstreamTaskID)
	case PlatformGrok:
		result, err = s.gateway.QueryGrokVideoTask(ctx, account, task.UpstreamTaskID)
	default:
		return fmt.Errorf("unsupported video task platform %q", task.Platform)
	}
	if err != nil {
		return s.repo.UpdateReviewObservation(ctx, id, VideoTaskOutcome{Status: VideoTaskStatusUnknown, ErrorMessage: err.Error()})
	}
	outcome := ClassifyVideoTaskResult(result)
	switch outcome.Status {
	case VideoTaskStatusCompleted:
		return s.ConfirmSucceeded(ctx, id, "重新核对确认上游已生成可用产物")
	case VideoTaskStatusFailed:
		return s.ConfirmFailed(ctx, id, "重新核对确认上游明确失败")
	default:
		return s.repo.UpdateReviewObservation(ctx, id, outcome)
	}
}

func isVideoTaskReviewCandidate(task *VideoTaskBilling) bool {
	if task == nil {
		return false
	}
	return task.BillingStatus == VideoTaskBillingManualReview ||
		(task.BillingStatus == VideoTaskBillingReserved && task.TaskStatus == VideoTaskStatusUnknown) ||
		(task.BillingStatus == VideoTaskBillingSettling && task.TaskStatus == VideoTaskStatusCompleted)
}
