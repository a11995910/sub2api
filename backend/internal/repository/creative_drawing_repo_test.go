//go:build unit

package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newCreativeDrawingSQLMock(t *testing.T) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, mock
}

func TestCreativeDrawingRepositoryListPendingUsesRunningTimeout(t *testing.T) {
	db, mock := newCreativeDrawingSQLMock(t)
	repo := &creativeDrawingRepository{db: db}

	mock.ExpectQuery("FROM creative_drawing_tasks").
		WithArgs(service.CreativeDrawingTaskStatusQueued, service.CreativeDrawingTaskStatusRunning, 20, int64(1800)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "api_key_id", "conversation_id", "turn_id", "mode", "model", "prompt",
			"size", "image_count", "output_format", "reference_images", "request_json",
			"status", "error", "result_images", "created_at", "updated_at", "started_at", "completed_at",
		}))

	tasks, err := repo.ListPending(context.Background(), 20, 30*time.Minute)

	require.NoError(t, err)
	require.Empty(t, tasks)
	require.NoError(t, mock.ExpectationsWereMet())
}
