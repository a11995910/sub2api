package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type imageTaskMemoryStore struct {
	task    *ImageTaskRecord
	ttl     time.Duration
	saveErr error
	getErr  error
}

type imageTaskQueueMemoryStore struct {
	imageTaskMemoryStore
	tasksByKey map[string]*ImageTaskRecord
}

func (s *imageTaskQueueMemoryStore) CreateOrGet(_ context.Context, task *ImageTaskRecord, _ time.Duration) (*ImageTaskRecord, bool, error) {
	if s.tasksByKey == nil {
		s.tasksByKey = make(map[string]*ImageTaskRecord)
	}
	key := task.ClientRequestID
	if existing := s.tasksByKey[key]; existing != nil {
		if existing.RequestFingerprint != task.RequestFingerprint {
			return nil, false, ErrImageTaskIdempotencyConflict
		}
		copy := *existing
		return &copy, false, nil
	}
	copy := *task
	s.tasksByKey[key] = &copy
	return &copy, true, nil
}

func (s *imageTaskQueueMemoryStore) Reserve(context.Context, string, time.Time) (*ImageTaskRecord, error) {
	return nil, ErrImageTaskQueueEmpty
}

func (s *imageTaskQueueMemoryStore) Heartbeat(context.Context, string, string, time.Time) error {
	return nil
}

func (s *imageTaskQueueMemoryStore) SaveClaim(_ context.Context, task *ImageTaskRecord, _ string, _ time.Duration) error {
	copy := *task
	s.task = &copy
	return nil
}

func (s *imageTaskQueueMemoryStore) RecoverStale(context.Context, time.Time, time.Time, time.Duration) (int, error) {
	return 0, nil
}

func (s *imageTaskMemoryStore) Save(_ context.Context, task *ImageTaskRecord, ttl time.Duration) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	copy := *task
	s.task = &copy
	s.ttl = ttl
	return nil
}

func (s *imageTaskMemoryStore) Get(_ context.Context, _ string) (*ImageTaskRecord, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.task == nil {
		return nil, ErrImageTaskNotFound
	}
	copy := *s.task
	return &copy, nil
}

func TestImageTaskServiceEnabledWithoutObjectStorage(t *testing.T) {
	svc := NewImageTaskServiceWithResolver(&imageTaskQueueMemoryStore{}, func() (*ImageResultUploader, bool) {
		return nil, false
	}, time.Hour, time.Minute)

	require.True(t, svc.Enabled())
}

func TestImageTaskServiceCompletesWithLocalizedURLWithoutObjectStorage(t *testing.T) {
	store := &imageTaskQueueMemoryStore{}
	svc := NewImageTaskServiceWithResolver(store, func() (*ImageResultUploader, bool) {
		return nil, false
	}, time.Hour, time.Minute)
	task := &ImageTaskRecord{
		ID:             "imgtask_local",
		UserID:         7,
		APIKeyID:       9,
		Status:         ImageTaskStatusRunning,
		ExecutionToken: "claim_local",
	}
	result := json.RawMessage(`{"data":[{"url":"/generated-images/local.png"}]}`)

	require.NoError(t, svc.CompleteGeneration(context.Background(), task, http.StatusOK, result))
	require.Equal(t, ImageTaskStatusSucceeded, store.task.Status)
	require.JSONEq(t, string(result), string(store.task.Result))
	require.Nil(t, store.task.Error)
}

func TestImageTaskServiceLifecycleAndOwnership(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, 10*time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}

	created, err := svc.Create(context.Background(), owner)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusProcessing, created.Status)
	require.Equal(t, created.ID, created.TaskID)
	require.Equal(t, "image.generation.task", created.Object)
	require.Equal(t, time.Hour, store.ttl)
	require.Equal(t, owner.UserID, store.task.UserID)
	require.Equal(t, owner.APIKeyID, store.task.APIKeyID)

	_, err = svc.Get(context.Background(), ImageTaskOwner{UserID: 7, APIKeyID: 10}, created.ID)
	require.ErrorIs(t, err, ErrImageTaskNotFound)

	result := json.RawMessage(`{"created":123,"data":[{"url":"https://example.test/image.png"}]}`)
	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, result))

	completed, err := svc.Get(context.Background(), owner, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusCompleted, completed.Status)
	require.Equal(t, http.StatusOK, completed.HTTPStatus)
	require.Equal(t, "https://example.test/image.png", completed.ImageURL)
	require.JSONEq(t, string(result), string(completed.Result))
	require.NotNil(t, completed.CompletedAt)
}

func TestImageTaskServiceInvalidResultBecomesFailed(t *testing.T) {
	store := &imageTaskMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	created, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.NoError(t, err)

	require.NoError(t, svc.Complete(context.Background(), created.ID, http.StatusOK, json.RawMessage(`not-json`)))
	got, err := svc.Get(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2}, created.ID)
	require.NoError(t, err)
	require.Equal(t, ImageTaskStatusFailed, got.Status)
	require.Equal(t, http.StatusBadGateway, got.HTTPStatus)
	require.Contains(t, string(got.Error), "non-JSON")
}

func TestImageTaskServiceMapsStoreFailures(t *testing.T) {
	store := &imageTaskMemoryStore{saveErr: errors.New("redis down")}
	svc := NewImageTaskService(store)

	_, err := svc.Create(context.Background(), ImageTaskOwner{UserID: 1, APIKeyID: 2})
	require.ErrorIs(t, err, ErrImageTaskUnavailable)
}

func TestImageTaskServiceCreateGenerationIsIdempotent(t *testing.T) {
	store := &imageTaskQueueMemoryStore{}
	svc := NewImageTaskServiceWithOptions(store, time.Hour, time.Minute)
	owner := ImageTaskOwner{UserID: 7, APIKeyID: 9}
	body := json.RawMessage(`{"model":"gpt-image-2","prompt":"dog"}`)

	first, created, err := svc.CreateGeneration(context.Background(), owner, "request_123", "fingerprint", body, "application/json", "openai")
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, ImageTaskStatusQueued, first.Status)
	require.Equal(t, "request_123", first.ClientRequestID)

	replayed, created, err := svc.CreateGeneration(context.Background(), owner, "request_123", "fingerprint", body, "application/json", "openai")
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, first.ID, replayed.ID)

	_, _, err = svc.CreateGeneration(context.Background(), owner, "request_123", "different", body, "application/json", "openai")
	require.ErrorIs(t, err, ErrImageTaskIdempotencyConflict)
}

func TestImageTaskGenerationViewPromotesResultFields(t *testing.T) {
	task := &ImageTaskRecord{
		ID:              "imgtask_123",
		ClientRequestID: "request_123",
		Status:          ImageTaskStatusSucceeded,
		CreatedAt:       100,
		UpdatedAt:       110,
		Result:          json.RawMessage(`{"data":[{"url":"https://example.test/dog.png"}],"usage":{"total_tokens":3}}`),
	}

	view := ImageTaskGenerationView(task)

	require.Equal(t, "imgtask_123", view["id"])
	require.Equal(t, ImageTaskStatusSucceeded, view["status"])
	require.Equal(t, "request_123", view["client_request_id"])
	require.Contains(t, string(view["data"].(json.RawMessage)), "dog.png")
	require.Contains(t, string(view["usage"].(json.RawMessage)), "total_tokens")
}

func TestImageTaskGenerationViewMapsLegacyStatuses(t *testing.T) {
	processing := ImageTaskGenerationView(&ImageTaskRecord{ID: "imgtask_processing", Status: ImageTaskStatusProcessing})
	require.Equal(t, ImageTaskStatusRunning, processing["status"])

	completed := ImageTaskGenerationView(&ImageTaskRecord{ID: "imgtask_completed", Status: ImageTaskStatusCompleted})
	require.Equal(t, ImageTaskStatusSucceeded, completed["status"])
}
