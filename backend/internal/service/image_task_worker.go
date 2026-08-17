package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
)

const (
	defaultImageTaskWorkerPollInterval = time.Second
	defaultImageTaskHeartbeatInterval  = 15 * time.Second
	defaultImageTaskStaleAfter         = 90 * time.Second
)

type ImageTaskExecutor interface {
	Execute(ctx context.Context, task *ImageTaskRecord) (int, json.RawMessage, error)
}

type ImageTaskWorker struct {
	tasks             *ImageTaskService
	executor          ImageTaskExecutor
	pollInterval      time.Duration
	heartbeatInterval time.Duration
	staleAfter        time.Duration
}

func NewImageTaskWorker(tasks *ImageTaskService, executor ImageTaskExecutor) *ImageTaskWorker {
	return &ImageTaskWorker{
		tasks:             tasks,
		executor:          executor,
		pollInterval:      defaultImageTaskWorkerPollInterval,
		heartbeatInterval: defaultImageTaskHeartbeatInterval,
		staleAfter:        defaultImageTaskStaleAfter,
	}
}

func (w *ImageTaskWorker) RunOnce(ctx context.Context) error {
	if w == nil || w.tasks == nil || w.executor == nil {
		return nil
	}
	task, err := w.tasks.ReserveGeneration(ctx, uuid.NewString(), time.Now().UTC())
	if errors.Is(err, ErrImageTaskQueueEmpty) {
		return nil
	}
	if err != nil {
		return err
	}

	executionCtx, cancel := context.WithTimeout(ctx, w.tasks.ExecutionTimeout())
	defer cancel()
	stopHeartbeat := make(chan struct{})
	heartbeatDone := make(chan struct{})
	go w.runHeartbeat(executionCtx, task, stopHeartbeat, heartbeatDone)

	statusCode, body, executeErr := executeImageTaskSafely(executionCtx, w.executor, task)
	close(stopHeartbeat)
	<-heartbeatDone
	if executeErr != nil {
		if statusCode <= 0 {
			statusCode = http.StatusBadGateway
		}
		return w.tasks.FailGeneration(ctx, task, statusCode, "PROVIDER_REQUEST_FAILED", "Image generation failed", true)
	}
	if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
		retryable := statusCode >= http.StatusInternalServerError
		return w.tasks.FailGeneration(ctx, task, statusCode, "PROVIDER_REQUEST_FAILED", "Image generation failed", retryable)
	}
	return w.tasks.CompleteGeneration(ctx, task, statusCode, body)
}

func executeImageTaskSafely(
	ctx context.Context,
	executor ImageTaskExecutor,
	task *ImageTaskRecord,
) (statusCode int, body json.RawMessage, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			statusCode = http.StatusInternalServerError
			body = nil
			err = fmt.Errorf("image task executor panicked: %v", recovered)
		}
	}()
	return executor.Execute(ctx, task)
}

func (w *ImageTaskWorker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if err := w.RunOnce(ctx); err != nil && ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(w.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *ImageTaskWorker) RecoverStaleOnce(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	return w.tasks.RecoverStaleGenerations(ctx, now.Add(-w.staleAfter), now)
}

func (w *ImageTaskWorker) runHeartbeat(ctx context.Context, task *ImageTaskRecord, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(w.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case now := <-ticker.C:
			if err := w.tasks.HeartbeatGeneration(ctx, task, now.UTC()); err != nil {
				return
			}
		}
	}
}
