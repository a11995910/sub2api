package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoTestTaskServiceNormalizesStatusesAndKeepsTerminalState(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := newMemoryVideoTestTaskStore()
	svc := NewVideoTestTaskServiceWithClock(store, func() time.Time { return now })
	task := store.seed(VideoTestTask{ID: "local-1", UserID: 7, Status: VideoTestTaskStatusQueued})

	progress := 42.0
	updated, err := svc.ApplyPollResult(context.Background(), 7, task.ID, VideoTestTaskPollResult{
		Status: "processing", Progress: &progress, ResponseJSON: json.RawMessage(`{"status":"processing"}`),
	})
	require.NoError(t, err)
	require.Equal(t, VideoTestTaskStatusInProgress, updated.Status)
	require.Equal(t, &progress, updated.Progress)
	require.Equal(t, &now, updated.LastPolledAt)

	now = now.Add(time.Minute)
	updated, err = svc.ApplyPollResult(context.Background(), 7, task.ID, VideoTestTaskPollResult{Status: "success"})
	require.NoError(t, err)
	require.Equal(t, VideoTestTaskStatusCompleted, updated.Status)
	require.Equal(t, &now, updated.CompletedAt)

	now = now.Add(time.Minute)
	updated, err = svc.ApplyPollResult(context.Background(), 7, task.ID, VideoTestTaskPollResult{Status: "running"})
	require.NoError(t, err)
	require.Equal(t, VideoTestTaskStatusCompleted, updated.Status)
	require.Equal(t, &now, updated.LastPolledAt)
}

func TestVideoTestTaskServicePollErrorPreservesWaitingStatus(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := newMemoryVideoTestTaskStore()
	store.seed(VideoTestTask{ID: "local-1", UserID: 7, Status: VideoTestTaskStatusInProgress})
	svc := NewVideoTestTaskServiceWithClock(store, func() time.Time { return now })

	updated, err := svc.RecordPollError(context.Background(), 7, "local-1", "upstream timeout")
	require.NoError(t, err)
	require.Equal(t, VideoTestTaskStatusInProgress, updated.Status)
	require.Equal(t, "upstream timeout", updated.LastPollError)
	require.Empty(t, updated.ErrorMessage)
	require.Equal(t, &now, updated.LastPolledAt)
}

func TestVideoTestTaskServiceTreatsExplicitDoneAsCompleted(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := newMemoryVideoTestTaskStore()
	store.seed(VideoTestTask{ID: "local-1", UserID: 7, Status: VideoTestTaskStatusInProgress})
	svc := NewVideoTestTaskServiceWithClock(store, func() time.Time { return now })

	updated, err := svc.ApplyPollResult(context.Background(), 7, "local-1", VideoTestTaskPollResult{Status: "done"})
	require.NoError(t, err)
	require.Equal(t, VideoTestTaskStatusCompleted, updated.Status)
	require.Equal(t, &now, updated.CompletedAt)
}

func TestVideoTestTaskServiceShouldPollOnlyNonTerminalAfterThrottle(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	svc := NewVideoTestTaskServiceWithClock(newMemoryVideoTestTaskStore(), func() time.Time { return now })
	recent := now.Add(-4 * time.Second)
	old := now.Add(-6 * time.Second)

	require.False(t, svc.ShouldPoll(&VideoTestTask{Status: VideoTestTaskStatusQueued, LastPolledAt: &recent}))
	require.True(t, svc.ShouldPoll(&VideoTestTask{Status: VideoTestTaskStatusInProgress, LastPolledAt: &old}))
	require.False(t, svc.ShouldPoll(&VideoTestTask{Status: VideoTestTaskStatusCompleted, LastPolledAt: &old}))
}

func TestVideoTestTaskServiceRecordAcceptedAndResolveAccount(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := newMemoryVideoTestTaskStore()
	svc := NewVideoTestTaskServiceWithClock(store, func() time.Time { return now })

	task, err := svc.RecordAccepted(context.Background(), VideoTestTaskAcceptedInput{
		UserID: 7, APIKeyID: 11, GroupID: 13, AccountID: 17, UpstreamTaskID: "upstream-1",
		Platform: PlatformOpenAI, Model: "jing-video-2-pro", Prompt: "雨夜街道",
		Resolution: "720p", DurationSeconds: 8, ReferenceImageCount: 1, Status: "pending",
	})
	require.NoError(t, err)
	require.NotEmpty(t, task.ID)
	require.Equal(t, VideoTestTaskStatusQueued, task.Status)
	require.Equal(t, now, task.CreatedAt)

	accountID, err := svc.ResolveAccountID(context.Background(), 7, 11, "upstream-1")
	require.NoError(t, err)
	require.Equal(t, int64(17), accountID)
}

func TestVideoTestTaskCleanupDeletesOnlyExpiredTerminalCutoff(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	store := newMemoryVideoTestTaskStore()
	cleanup := NewVideoTestTaskCleanupServiceWithOptions(store, func() time.Time { return now }, time.Hour, 30*24*time.Hour)

	deleted, err := cleanup.runOnce(context.Background())
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.Equal(t, now.Add(-30*24*time.Hour), store.cleanupCutoff)
}

type memoryVideoTestTaskStore struct {
	tasks         map[string]VideoTestTask
	cleanupCutoff time.Time
}

func newMemoryVideoTestTaskStore() *memoryVideoTestTaskStore {
	return &memoryVideoTestTaskStore{tasks: make(map[string]VideoTestTask)}
}

func (s *memoryVideoTestTaskStore) seed(task VideoTestTask) *VideoTestTask {
	s.tasks[task.ID] = task
	copy := task
	return &copy
}

func (s *memoryVideoTestTaskStore) Create(_ context.Context, task *VideoTestTask) error {
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now().UTC()
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = task.CreatedAt
	}
	s.tasks[task.ID] = *task
	return nil
}

func (s *memoryVideoTestTaskStore) GetByOwner(_ context.Context, userID int64, id string) (*VideoTestTask, error) {
	task, ok := s.tasks[id]
	if !ok || task.UserID != userID {
		return nil, ErrVideoTestTaskNotFound
	}
	return &task, nil
}

func (s *memoryVideoTestTaskStore) GetByUpstreamOwner(_ context.Context, userID, apiKeyID int64, upstreamTaskID string) (*VideoTestTask, error) {
	for _, task := range s.tasks {
		if task.UserID == userID && task.APIKeyID == apiKeyID && task.UpstreamTaskID == upstreamTaskID {
			copy := task
			return &copy, nil
		}
	}
	return nil, ErrVideoTestTaskNotFound
}

func (s *memoryVideoTestTaskStore) ListByUser(_ context.Context, userID int64, _, _ int) ([]VideoTestTask, int64, error) {
	items := make([]VideoTestTask, 0)
	for _, task := range s.tasks {
		if task.UserID == userID {
			items = append(items, task)
		}
	}
	return items, int64(len(items)), nil
}

func (s *memoryVideoTestTaskStore) UpdatePollResult(_ context.Context, task *VideoTestTask) error {
	if _, ok := s.tasks[task.ID]; !ok {
		return ErrVideoTestTaskNotFound
	}
	s.tasks[task.ID] = *task
	return nil
}

func (s *memoryVideoTestTaskStore) DeleteByOwner(_ context.Context, userID int64, id string) error {
	task, ok := s.tasks[id]
	if !ok || task.UserID != userID {
		return ErrVideoTestTaskNotFound
	}
	delete(s.tasks, id)
	return nil
}

func (s *memoryVideoTestTaskStore) DeleteExpiredTerminal(_ context.Context, cutoff time.Time) (int64, error) {
	s.cleanupCutoff = cutoff
	return 0, nil
}
