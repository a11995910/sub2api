package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestImageTaskStoreRoundTripAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:        "imgtask_123",
		UserID:    7,
		APIKeyID:  9,
		Status:    service.ImageTaskStatusProcessing,
		CreatedAt: 100,
		ExpiresAt: 200,
	}

	require.NoError(t, store.Save(context.Background(), task, 24*time.Hour))
	got, err := store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, got)
	require.Equal(t, 24*time.Hour, mr.TTL(imageTaskKey(task.ID)))
}

func TestImageTaskStoreMissing(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)

	_, err := store.Get(context.Background(), "imgtask_missing")
	require.ErrorIs(t, err, service.ErrImageTaskNotFound)
}

func TestImageTaskStoreCreateOrGetIsIdempotent(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	base := &service.ImageTaskRecord{
		ID:                 "imgtask_first",
		UserID:             7,
		APIKeyID:           9,
		ClientRequestID:    "request_123",
		RequestFingerprint: "fingerprint",
		RequestBody:        json.RawMessage(`{"prompt":"dog"}`),
		Status:             service.ImageTaskStatusQueued,
		CreatedAt:          100,
		UpdatedAt:          100,
		ExpiresAt:          200,
	}

	const callers = 20
	var wg sync.WaitGroup
	type createResult struct {
		id      string
		created bool
		err     error
	}
	results := make(chan createResult, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task := *base
			task.ID = "imgtask_" + string(rune('a'+i))
			got, created, err := store.CreateOrGet(context.Background(), &task, 24*time.Hour)
			result := createResult{created: created, err: err}
			if got != nil {
				result.id = got.ID
			}
			results <- result
		}(i)
	}
	wg.Wait()
	close(results)

	var taskID string
	createdCount := 0
	for result := range results {
		require.NoError(t, result.err)
		if taskID == "" {
			taskID = result.id
		}
		require.Equal(t, taskID, result.id)
		if result.created {
			createdCount++
		}
	}
	require.Equal(t, 1, createdCount)
	require.Equal(t, int64(1), rdb.LLen(context.Background(), imageTaskReadyQueueKey).Val())
}

func TestImageTaskStoreCreateOrGetRejectsFingerprintConflict(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	first := &service.ImageTaskRecord{
		ID:                 "imgtask_first",
		UserID:             7,
		APIKeyID:           9,
		ClientRequestID:    "request_123",
		RequestFingerprint: "first",
		Status:             service.ImageTaskStatusQueued,
	}
	_, _, err := store.CreateOrGet(context.Background(), first, 24*time.Hour)
	require.NoError(t, err)

	second := *first
	second.ID = "imgtask_second"
	second.RequestFingerprint = "second"
	_, _, err = store.CreateOrGet(context.Background(), &second, 24*time.Hour)

	require.ErrorIs(t, err, service.ErrImageTaskIdempotencyConflict)
	require.Equal(t, int64(1), rdb.LLen(context.Background(), imageTaskReadyQueueKey).Val())
}

func TestImageTaskStoreReserveClaimsQueuedTaskOnce(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:                 "imgtask_first",
		UserID:             7,
		APIKeyID:           9,
		ClientRequestID:    "request_123",
		RequestFingerprint: "fingerprint",
		Status:             service.ImageTaskStatusQueued,
	}
	_, _, err := store.CreateOrGet(context.Background(), task, 24*time.Hour)
	require.NoError(t, err)

	reserved, err := store.Reserve(context.Background(), "worker_token", time.Unix(123, 0))
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusRunning, reserved.Status)
	require.Equal(t, "worker_token", reserved.ExecutionToken)
	require.Equal(t, int64(123), reserved.UpdatedAt)

	_, err = store.Reserve(context.Background(), "other_token", time.Unix(124, 0))
	require.ErrorIs(t, err, service.ErrImageTaskQueueEmpty)
}

func TestImageTaskStoreSaveClaimUsesExecutionToken(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:              "imgtask_first",
		UserID:          7,
		APIKeyID:        9,
		ClientRequestID: "request_123",
		Status:          service.ImageTaskStatusQueued,
	}
	_, _, err := store.CreateOrGet(context.Background(), task, 24*time.Hour)
	require.NoError(t, err)
	reserved, err := store.Reserve(context.Background(), "worker_token", time.Unix(123, 0))
	require.NoError(t, err)

	reserved.Status = service.ImageTaskStatusSucceeded
	reserved.Result = json.RawMessage(`{"data":[{"url":"https://example.test/dog.png"}]}`)
	reserved.UpdatedAt = 124
	require.ErrorIs(t, store.SaveClaim(context.Background(), reserved, "other_token", time.Hour), service.ErrImageTaskClaimLost)
	got, err := store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusRunning, got.Status)

	require.NoError(t, store.SaveClaim(context.Background(), reserved, "worker_token", time.Hour))
	got, err = store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusSucceeded, got.Status)
	require.Empty(t, got.ExecutionToken)
	require.Equal(t, int64(124), got.UpdatedAt)
	require.Equal(t, time.Hour, mr.TTL(imageTaskIdempotencyKey(task.UserID, task.APIKeyID, task.ClientRequestID)))
}

func TestImageTaskStoreRecoversStaleRunningTaskAsFailed(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewImageTaskStore(rdb)
	task := &service.ImageTaskRecord{
		ID:              "imgtask_first",
		UserID:          7,
		APIKeyID:        9,
		ClientRequestID: "request_123",
		Status:          service.ImageTaskStatusQueued,
	}
	_, _, err := store.CreateOrGet(context.Background(), task, 24*time.Hour)
	require.NoError(t, err)
	_, err = store.Reserve(context.Background(), "worker_token", time.Unix(100, 0))
	require.NoError(t, err)

	recovered, err := store.RecoverStale(context.Background(), time.Unix(200, 0), time.Unix(201, 0), time.Hour)
	require.NoError(t, err)
	require.Equal(t, 1, recovered)
	got, err := store.Get(context.Background(), task.ID)
	require.NoError(t, err)
	require.Equal(t, service.ImageTaskStatusFailed, got.Status)
	require.Contains(t, string(got.Error), "EXECUTION_INTERRUPTED")
	require.Empty(t, got.ExecutionToken)
}
