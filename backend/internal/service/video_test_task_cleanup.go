package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	defaultVideoTestTaskCleanupInterval = time.Hour
	defaultVideoTestTaskRetention       = 30 * 24 * time.Hour
)

type VideoTestTaskCleanupService struct {
	store     VideoTestTaskStore
	now       func() time.Time
	interval  time.Duration
	retention time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewVideoTestTaskCleanupService(store VideoTestTaskStore) *VideoTestTaskCleanupService {
	return NewVideoTestTaskCleanupServiceWithOptions(
		store,
		func() time.Time { return time.Now().UTC() },
		defaultVideoTestTaskCleanupInterval,
		defaultVideoTestTaskRetention,
	)
}

func NewVideoTestTaskCleanupServiceWithOptions(
	store VideoTestTaskStore,
	now func() time.Time,
	interval time.Duration,
	retention time.Duration,
) *VideoTestTaskCleanupService {
	return &VideoTestTaskCleanupService{store: store, now: now, interval: interval, retention: retention}
}

func (s *VideoTestTaskCleanupService) Start() {
	if s == nil || s.store == nil || s.now == nil || s.interval <= 0 || s.retention <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		s.logRun(ctx)
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.logRun(ctx)
			}
		}
	}(s.done)
}

func (s *VideoTestTaskCleanupService) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *VideoTestTaskCleanupService) runOnce(ctx context.Context) (int64, error) {
	return s.store.DeleteExpiredTerminal(ctx, s.now().UTC().Add(-s.retention))
}

func (s *VideoTestTaskCleanupService) logRun(ctx context.Context) {
	deleted, err := s.runOnce(ctx)
	if err != nil {
		logger.L().Warn("video_test_task.cleanup_failed", zap.Error(err))
		return
	}
	if deleted > 0 {
		logger.L().Info("video_test_task.cleanup_completed", zap.Int64("deleted", deleted))
	}
}
