package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskWorkerStore struct {
	task *ImageTaskRecord
}

func (s *imageTaskWorkerStore) Save(_ context.Context, task *ImageTaskRecord, _ time.Duration) error {
	copy := *task
	s.task = &copy
	return nil
}

func (s *imageTaskWorkerStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func (s *imageTaskWorkerStore) CreateOrGet(_ context.Context, task *ImageTaskRecord, _ time.Duration) (*ImageTaskRecord, bool, error) {
	copy := *task
	s.task = &copy
	return &copy, true, nil
}

func (s *imageTaskWorkerStore) Reserve(_ context.Context, token string, now time.Time) (*ImageTaskRecord, error) {
	if s.task == nil || s.task.Status != ImageTaskStatusQueued {
		return nil, ErrImageTaskQueueEmpty
	}
	s.task.Status = ImageTaskStatusRunning
	s.task.ExecutionToken = token
	s.task.UpdatedAt = now.Unix()
	copy := *s.task
	return &copy, nil
}

func (s *imageTaskWorkerStore) Heartbeat(context.Context, string, string, time.Time) error {
	return nil
}

func (s *imageTaskWorkerStore) SaveClaim(_ context.Context, task *ImageTaskRecord, token string, _ time.Duration) error {
	if s.task == nil || s.task.Status != ImageTaskStatusRunning || s.task.ExecutionToken != token {
		return ErrImageTaskClaimLost
	}
	copy := *task
	copy.ExecutionToken = ""
	s.task = &copy
	return nil
}

func (s *imageTaskWorkerStore) RecoverStale(context.Context, time.Time, time.Time, time.Duration) (int, error) {
	return 0, nil
}

type imageTaskExecutorFunc func(context.Context, *ImageTaskRecord) (int, json.RawMessage, error)

func (f imageTaskExecutorFunc) Execute(ctx context.Context, task *ImageTaskRecord) (int, json.RawMessage, error) {
	return f(ctx, task)
}

func TestImageTaskWorkerCompletesClaimedTask(t *testing.T) {
	store := &imageTaskWorkerStore{task: &ImageTaskRecord{
		ID:        "imgtask_123",
		Status:    ImageTaskStatusQueued,
		CreatedAt: 100,
		UpdatedAt: 100,
	}}
	tasks := NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	worker := NewImageTaskWorker(tasks, imageTaskExecutorFunc(func(_ context.Context, task *ImageTaskRecord) (int, json.RawMessage, error) {
		require.Equal(t, ImageTaskStatusRunning, task.Status)
		return http.StatusOK, json.RawMessage(`{"data":[{"url":"https://example.test/dog.png"}]}`), nil
	}))

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, ImageTaskStatusSucceeded, store.task.Status)
	require.Contains(t, string(store.task.Result), "dog.png")
	require.Empty(t, store.task.ExecutionToken)
}

func TestImageTaskWorkerStoresStableProviderFailure(t *testing.T) {
	store := &imageTaskWorkerStore{task: &ImageTaskRecord{
		ID:        "imgtask_123",
		Status:    ImageTaskStatusQueued,
		CreatedAt: 100,
		UpdatedAt: 100,
	}}
	tasks := NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	worker := NewImageTaskWorker(tasks, imageTaskExecutorFunc(func(context.Context, *ImageTaskRecord) (int, json.RawMessage, error) {
		return http.StatusBadGateway, nil, errors.New("upstream unavailable")
	}))

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, ImageTaskStatusFailed, store.task.Status)
	require.Contains(t, string(store.task.Error), "PROVIDER_REQUEST_FAILED")
	require.Contains(t, string(store.task.Error), `"retryable":true`)
}

func TestImageTaskWorkerRecoversExecutorPanic(t *testing.T) {
	store := &imageTaskWorkerStore{task: &ImageTaskRecord{
		ID:        "imgtask_123",
		Status:    ImageTaskStatusQueued,
		CreatedAt: 100,
		UpdatedAt: 100,
	}}
	tasks := NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	worker := NewImageTaskWorker(tasks, imageTaskExecutorFunc(func(context.Context, *ImageTaskRecord) (int, json.RawMessage, error) {
		panic("executor panic")
	}))

	require.NoError(t, worker.RunOnce(context.Background()))
	require.Equal(t, ImageTaskStatusFailed, store.task.Status)
	require.Contains(t, string(store.task.Error), "PROVIDER_REQUEST_FAILED")
}

func TestImageTaskWorkerRuntimeStartsAndStops(t *testing.T) {
	store := &imageTaskWorkerStore{task: &ImageTaskRecord{
		ID:        "imgtask_123",
		Status:    ImageTaskStatusQueued,
		CreatedAt: 100,
		UpdatedAt: 100,
	}}
	tasks := NewImageTaskServiceWithUploader(store, nil, time.Hour, time.Minute)
	var calls atomic.Int32
	worker := NewImageTaskWorker(tasks, imageTaskExecutorFunc(func(context.Context, *ImageTaskRecord) (int, json.RawMessage, error) {
		calls.Add(1)
		return http.StatusOK, json.RawMessage(`{"data":[{"url":"https://example.test/dog.png"}]}`), nil
	}))
	worker.pollInterval = time.Millisecond
	runtime := NewImageTaskWorkerRuntime(worker)

	runtime.Start()
	require.Eventually(t, func() bool { return calls.Load() == 1 }, time.Second, 10*time.Millisecond)
	runtime.Stop()

	require.False(t, runtime.Running())
	require.Equal(t, int32(1), calls.Load())
}
