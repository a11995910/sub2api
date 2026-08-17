package service

import (
	"context"
	"sync"
	"time"
)

const defaultImageTaskRecoveryInterval = 30 * time.Second

type ImageTaskWorkerRuntime struct {
	worker           *ImageTaskWorker
	recoveryInterval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewImageTaskWorkerRuntime(worker *ImageTaskWorker) *ImageTaskWorkerRuntime {
	return &ImageTaskWorkerRuntime{
		worker:           worker,
		recoveryInterval: defaultImageTaskRecoveryInterval,
	}
}

func (r *ImageTaskWorkerRuntime) Start() {
	if r == nil || r.worker == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel = cancel
	r.done = done
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.worker.Run(ctx)
		}()
		go func() {
			defer wg.Done()
			r.runRecovery(ctx)
		}()
		wg.Wait()
	}()
}

func (r *ImageTaskWorkerRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.done = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (r *ImageTaskWorkerRuntime) Running() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cancel != nil
}

func (r *ImageTaskWorkerRuntime) runRecovery(ctx context.Context) {
	_, _ = r.worker.RecoverStaleOnce(ctx)
	ticker := time.NewTicker(r.recoveryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = r.worker.RecoverStaleOnce(ctx)
		}
	}
}
