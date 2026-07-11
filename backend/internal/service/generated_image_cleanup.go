package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const defaultGeneratedImageCleanupInterval = 30 * time.Minute

type GeneratedImageCleanupService struct {
	store    *GeneratedImageStore
	interval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewGeneratedImageCleanupService(store *GeneratedImageStore) *GeneratedImageCleanupService {
	return &GeneratedImageCleanupService{store: store, interval: defaultGeneratedImageCleanupInterval}
}

func (s *GeneratedImageCleanupService) Start() {
	if s == nil || s.store == nil || s.interval <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	done := make(chan struct{})
	interval := s.interval
	s.done = done
	go func() {
		defer close(done)
		s.runOnce()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runOnce()
			}
		}
	}()
}

func (s *GeneratedImageCleanupService) Stop() {
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

func (s *GeneratedImageCleanupService) runOnce() {
	deleted, err := s.store.Cleanup(time.Now().UTC())
	if err != nil {
		logger.L().Warn("generated_image.cleanup_failed", zap.Error(err))
		return
	}
	if deleted > 0 {
		logger.L().Info("generated_image.cleanup_completed", zap.Int("deleted", deleted))
	}
}
