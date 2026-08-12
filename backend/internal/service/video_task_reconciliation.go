package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type VideoTaskQueryGateway interface {
	QueryOpenAIVideoTask(ctx context.Context, account *Account, taskID string) (*OpenAIForwardResult, error)
	QueryGrokVideoTask(ctx context.Context, account *Account, taskID string) (*OpenAIForwardResult, error)
}

type VideoTaskReconciliationService struct {
	billing      *VideoTaskBillingService
	gateway      VideoTaskQueryGateway
	accounts     AccountRepository
	pollInterval time.Duration
	stop         chan struct{}
	done         chan struct{}
	startOnce    sync.Once
	stopOnce     sync.Once
}

func NewVideoTaskReconciliationService(billing *VideoTaskBillingService, gateway VideoTaskQueryGateway, accounts AccountRepository) *VideoTaskReconciliationService {
	return &VideoTaskReconciliationService{
		billing: billing, gateway: gateway, accounts: accounts, pollInterval: 15 * time.Second,
		stop: make(chan struct{}), done: make(chan struct{}),
	}
}

func (s *VideoTaskReconciliationService) Start() {
	if s == nil {
		return
	}
	s.startOnce.Do(func() { go s.loop() })
}

func (s *VideoTaskReconciliationService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
	}
}

func (s *VideoTaskReconciliationService) loop() {
	defer close(s.done)
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.ReconcileOnce(ctx); err != nil {
				logger.L().Warn("video_task_reconciliation.failed", zap.Error(err))
			}
			cancel()
		}
	}
}

func (s *VideoTaskReconciliationService) ReconcileOnce(ctx context.Context) error {
	if s == nil || s.billing == nil || s.gateway == nil || s.accounts == nil {
		return errors.New("video task reconciliation service is unavailable")
	}
	tasks, err := s.billing.ClaimDue(ctx, 20, 45*time.Second)
	if err != nil {
		return err
	}
	var reconcileErrors []error
	for _, task := range tasks {
		if err := s.reconcileTask(ctx, task); err != nil {
			reconcileErrors = append(reconcileErrors, fmt.Errorf("video task billing %d: %w", task.ID, err))
		}
	}
	return errors.Join(reconcileErrors...)
}

func (s *VideoTaskReconciliationService) reconcileTask(ctx context.Context, task *VideoTaskBilling) error {
	if task == nil {
		return nil
	}
	if task.TaskStatus == VideoTaskStatusSubmitting && task.UpstreamTaskID == "" {
		return s.billing.MarkSubmissionUnknown(ctx, task, errors.New("video submission deadline expired without task_id"))
	}
	if task.BillingStatus == VideoTaskBillingSettling && task.TaskStatus == VideoTaskStatusCompleted {
		return s.billing.ApplyOutcome(ctx, task, VideoTaskOutcome{Status: VideoTaskStatusCompleted, ResponseJSON: task.ResponseJSON})
	}
	account, err := s.accounts.GetByID(ctx, task.AccountID)
	if err != nil || account == nil {
		return s.billing.ApplyOutcome(ctx, task, VideoTaskOutcome{Status: VideoTaskStatusUnknown, ErrorMessage: "bound video account is unavailable"})
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
		return s.billing.ApplyOutcome(ctx, task, VideoTaskOutcome{Status: VideoTaskStatusUnknown, ErrorMessage: err.Error()})
	}
	return s.billing.ApplyOutcome(ctx, task, ClassifyVideoTaskResult(result))
}
