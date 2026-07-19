package repository

import (
	"context"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestVideoTestTaskMigrationDefinesDurableOwnershipAndRetentionIndexes(t *testing.T) {
	content, err := migrations.FS.ReadFile("185_model_test_video_tasks.sql")
	require.NoError(t, err)
	sqlText := string(content)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS model_test_video_tasks",
		"CHECK (status IN ('queued', 'in_progress', 'completed', 'failed'))",
		"UNIQUE (user_id, api_key_id, upstream_task_id)",
		"idx_model_test_video_tasks_user_created",
		"idx_model_test_video_tasks_pending",
		"idx_model_test_video_tasks_terminal_cleanup",
	} {
		require.Contains(t, sqlText, required)
	}
	require.NotContains(t, sqlText, "api_key TEXT")
	require.NotContains(t, sqlText, "reference_image_data")
}

func TestVideoTestTaskRepositoryGetByOwnerScopesUser(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)FROM model_test_video_tasks.*WHERE user_id = \$1 AND id = \$2`).
		WithArgs(int64(7), "task-local-1").
		WillReturnRows(videoTestTaskRows().AddRow(
			"task-local-1", int64(7), int64(11), int64(13), int64(17), "upstream-1",
			"openai", "jing-video-2-pro", "雨夜街道", "720p", 8, 0,
			service.VideoTestTaskStatusQueued, 0.0, []byte(`{"status":"queued"}`), "", "", nil,
			nil, nil, now, now,
		))

	repo := NewVideoTestTaskRepository(db)
	task, err := repo.GetByOwner(context.Background(), 7, "task-local-1")
	require.NoError(t, err)
	require.Equal(t, "upstream-1", task.UpstreamTaskID)
	require.Equal(t, int64(17), task.AccountID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTestTaskRepositoryCreateIsIdempotentByOwnerAndUpstreamTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	progress := 12.5
	mock.ExpectQuery(`(?s)INSERT INTO model_test_video_tasks.*ON CONFLICT \(user_id, api_key_id, upstream_task_id\) DO UPDATE.*RETURNING created_at, updated_at`).
		WithArgs(
			"task-local-1", int64(7), int64(11), int64(13), int64(17), "upstream-1",
			"openai", "jing-video-2-pro", "雨夜街道", "720p", 8, 1,
			service.VideoTestTaskStatusInProgress, &progress, sqlmock.AnyArg(), "", "", nil, nil, nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now))

	task := &service.VideoTestTask{
		ID: "task-local-1", UserID: 7, APIKeyID: 11, GroupID: 13, AccountID: 17,
		UpstreamTaskID: "upstream-1", Platform: "openai", Model: "jing-video-2-pro",
		Prompt: "雨夜街道", Resolution: "720p", DurationSeconds: 8, ReferenceImageCount: 1,
		Status: service.VideoTestTaskStatusInProgress, Progress: &progress,
	}
	repo := NewVideoTestTaskRepository(db)
	require.NoError(t, repo.Create(context.Background(), task))
	require.Equal(t, now, task.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTestTaskRepositoryGetByUpstreamOwnerScopesAPIKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)FROM model_test_video_tasks.*WHERE user_id = \$1 AND api_key_id = \$2 AND upstream_task_id = \$3`).
		WithArgs(int64(7), int64(11), "upstream-1").
		WillReturnRows(videoTestTaskRows().AddRow(
			"task-local-1", int64(7), int64(11), int64(13), int64(17), "upstream-1",
			"openai", "jing-video-2-pro", "雨夜街道", "720p", 8, 0,
			service.VideoTestTaskStatusQueued, nil, []byte(`{}`), "", "", nil,
			nil, nil, now, now,
		))

	repo := NewVideoTestTaskRepository(db)
	task, err := repo.GetByUpstreamOwner(context.Background(), 7, 11, "upstream-1")
	require.NoError(t, err)
	require.Equal(t, "task-local-1", task.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTestTaskRepositoryListByUserIsPagedNewestFirst(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM model_test_video_tasks WHERE user_id = \$1`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`(?s)FROM model_test_video_tasks.*WHERE user_id = \$1.*ORDER BY created_at DESC, id DESC.*OFFSET \$2 LIMIT \$3`).
		WithArgs(int64(7), 20, 20).
		WillReturnRows(videoTestTaskRows().AddRow(
			"task-local-1", int64(7), int64(11), int64(13), int64(17), "upstream-1",
			"openai", "jing-video-2-pro", "雨夜街道", "720p", 8, 0,
			service.VideoTestTaskStatusQueued, nil, []byte(`{}`), "", "", nil,
			nil, nil, now, now,
		))

	repo := NewVideoTestTaskRepository(db)
	tasks, total, err := repo.ListByUser(context.Background(), 7, 20, 20)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, tasks, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTestTaskRepositoryUpdateAndDeleteRemainOwnerScoped(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	progress := 100.0
	mock.ExpectExec(`(?s)UPDATE model_test_video_tasks SET.*WHERE user_id = \$1 AND id = \$2`).
		WithArgs(int64(7), "task-local-1", service.VideoTestTaskStatusCompleted, &progress, sqlmock.AnyArg(), "", "", &now, &now, nil).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM model_test_video_tasks WHERE user_id = \$1 AND id = \$2`).
		WithArgs(int64(7), "task-local-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	task := &service.VideoTestTask{
		ID: "task-local-1", UserID: 7, Status: service.VideoTestTaskStatusCompleted,
		Progress: &progress, LastPolledAt: &now, CompletedAt: &now,
	}
	repo := NewVideoTestTaskRepository(db)
	require.NoError(t, repo.UpdatePollResult(context.Background(), task))
	require.NoError(t, repo.DeleteByOwner(context.Background(), 7, "task-local-1"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTestTaskRepositoryDeleteExpiredTerminalUsesCutoff(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	mock.ExpectExec(`(?s)DELETE FROM model_test_video_tasks.*status IN \(\$1, \$2\).*COALESCE\(completed_at, failed_at, updated_at\) < \$3`).
		WithArgs(service.VideoTestTaskStatusCompleted, service.VideoTestTaskStatusFailed, cutoff).
		WillReturnResult(sqlmock.NewResult(0, 4))

	repo := NewVideoTestTaskRepository(db)
	deleted, err := repo.DeleteExpiredTerminal(context.Background(), cutoff)
	require.NoError(t, err)
	require.Equal(t, int64(4), deleted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func videoTestTaskRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "user_id", "api_key_id", "group_id", "account_id", "upstream_task_id",
		"platform", "model", "prompt", "resolution", "duration_seconds", "reference_image_count",
		"status", "progress", "response_json", "error_message", "last_poll_error", "last_polled_at",
		"completed_at", "failed_at", "created_at", "updated_at",
	})
}
