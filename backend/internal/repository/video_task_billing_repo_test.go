package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestVideoTaskBillingMigrationDefinesDurableTaskAndDueIndexes(t *testing.T) {
	content, err := migrations.FS.ReadFile("222_video_task_billings.sql")
	require.NoError(t, err)
	sqlText := string(content)
	for _, required := range []string{
		"CREATE TABLE IF NOT EXISTS video_task_billings",
		"idx_video_task_billings_platform_task",
		"idx_video_task_billings_due",
		"idx_video_task_billings_user_created",
		"estimated_cost DECIMAL(20,8)",
		"usage_context_json JSONB",
		"frozen_balance",
	} {
		require.Contains(t, sqlText, required)
	}
	require.NotContains(t, sqlText, "api_key TEXT")
	require.NotContains(t, sqlText, "api_key_value")
}

func TestVideoTaskBillingRepositoryGetByTaskScopesPlatformAndTaskID(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)FROM video_task_billings.*WHERE platform = \$1 AND upstream_task_id = \$2`).
		WithArgs("openai", "upstream-video-1").
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusPending, service.VideoTaskBillingReserved,
			[]byte(`{"status":"queued"}`), 1, nil, now, "", nil, nil, nil, now, now, "720p", 8, 0, []byte(`{}`),
		))

	repo := NewVideoTaskBillingRepository(db)
	task, err := repo.GetByTask(context.Background(), "openai", "upstream-video-1")
	require.NoError(t, err)
	require.Equal(t, int64(9), task.ID)
	require.Equal(t, int64(17), task.AccountID)
	require.Equal(t, service.VideoTaskBillingReserved, task.BillingStatus)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryReserveAndCreateMovesBalanceBeforeInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance - \$1,\s+frozen_balance = COALESCE\(frozen_balance, 0\) \+ \$1.*WHERE id = \$2.*balance >= \$1\s+RETURNING balance, frozen_balance`).
		WithArgs(1.25, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(8.75, 1.25))
	mock.ExpectQuery(`(?s)INSERT INTO video_task_billings.*RETURNING id, created_at, updated_at`).
		WithArgs(
			"request-1", "", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", "", 0, 0, sqlmock.AnyArg(),
			1.25, service.VideoTaskStatusSubmitting, service.VideoTaskBillingReserved, sqlmock.AnyArg(), nil, time.Time{},
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(9), now, now))
	mock.ExpectCommit()

	task := &service.VideoTaskBilling{
		RequestID: "request-1", Platform: "openai", UserID: 7, APIKeyID: 11, GroupID: ptrInt64(13), AccountID: 17,
		Model: "video-model", UpstreamModel: "video-model", EstimatedCost: 1.25,
		TaskStatus: service.VideoTaskStatusSubmitting, BillingStatus: service.VideoTaskBillingReserved,
	}
	repo := NewVideoTaskBillingRepository(db)
	require.NoError(t, repo.ReserveAndCreate(context.Background(), task))
	require.Equal(t, int64(9), task.ID)
	require.Equal(t, now, task.CreatedAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryReserveAndCreateRejectsInsufficientBalance(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance - \$1,\s+frozen_balance = COALESCE\(frozen_balance, 0\) \+ \$1.*WHERE id = \$2.*balance >= \$1\s+RETURNING balance, frozen_balance`).
		WithArgs(2.5, int64(7)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`(?s)SELECT 1\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectRollback()

	task := &service.VideoTaskBilling{RequestID: "request-1", UserID: 7, APIKeyID: 11, AccountID: 17, EstimatedCost: 2.5}
	repo := NewVideoTaskBillingRepository(db)
	require.ErrorIs(t, repo.ReserveAndCreate(context.Background(), task), service.ErrBatchImageInsufficientBalance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryReleaseReturnsFrozenBalanceOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM video_task_billings.*WHERE id = \$1.*FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusFailed, service.VideoTaskBillingReserved,
			[]byte(`{"status":"failed"}`), 2, now, now, "", nil, nil, nil, now, now, "720p", 8, 0, []byte(`{}`),
		))
	mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance \+ \$1,\s+frozen_balance = COALESCE\(frozen_balance, 0\) - \$1.*WHERE id = \$2.*frozen_balance.*>= \$1\s+RETURNING balance, frozen_balance`).
		WithArgs(1.25, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(10.0, 0.0))
	mock.ExpectExec(`(?s)UPDATE video_task_billings\s+SET billing_status = \$2,.*terminal_at = NOW\(\).*WHERE id = \$1 AND billing_status = \$3`).
		WithArgs(int64(9), service.VideoTaskBillingReleased, service.VideoTaskBillingReserved, "generation failed").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := NewVideoTaskBillingRepository(db)
	require.NoError(t, repo.Release(context.Background(), 9, "generation failed"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryCaptureReleasesUnusedHold(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM video_task_billings.*WHERE id = \$1.*FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusCompleted, service.VideoTaskBillingSettling,
			[]byte(`{"status":"completed"}`), 2, now, now, "", nil, nil, nil, now, now, "720p", 8, 0, []byte(`{}`),
		))
	mock.ExpectQuery(`(?s)UPDATE users\s+SET balance = balance \+ CASE WHEN \$1 > \$2 THEN \$1 - \$2 ELSE 0 END,\s+frozen_balance = COALESCE\(frozen_balance, 0\) - \$1.*WHERE id = \$3.*frozen_balance.*>= \$1\s+RETURNING balance, frozen_balance`).
		WithArgs(1.25, 1.0, int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(9.0, 0.0))
	mock.ExpectExec(`(?s)UPDATE video_task_billings\s+SET actual_cost = \$2,\s+billing_status = \$3,.*terminal_at = NOW\(\).*WHERE id = \$1 AND billing_status = \$4`).
		WithArgs(int64(9), 1.0, service.VideoTaskBillingCaptured, service.VideoTaskBillingSettling).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	repo := NewVideoTaskBillingRepository(db)
	require.NoError(t, repo.Capture(context.Background(), 9, 1.0))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryCaptureRejectsCostAboveHold(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)FROM video_task_billings.*WHERE id = \$1.*FOR UPDATE`).
		WithArgs(int64(9)).
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusCompleted, service.VideoTaskBillingSettling,
			[]byte(`{}`), 2, now, now, "", nil, nil, nil, now, now, "720p", 8, 0, []byte(`{}`),
		))
	mock.ExpectRollback()

	repo := NewVideoTaskBillingRepository(db)
	require.ErrorIs(t, repo.Capture(context.Background(), 9, 1.5), service.ErrVideoTaskSettlementCostExceedsHold)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryAttachUpstreamTaskRequiresSubmittingReservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)UPDATE video_task_billings\s+SET upstream_task_id = \$2,\s+task_status = \$3,.*WHERE id = \$1\s+AND task_status = \$5\s+AND billing_status = \$6.*RETURNING`).
		WithArgs(int64(9), "upstream-video-1", service.VideoTaskStatusPending, sqlmock.AnyArg(), service.VideoTaskStatusSubmitting, service.VideoTaskBillingReserved).
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusPending, service.VideoTaskBillingReserved,
			[]byte(`{"status":"queued"}`), 0, nil, now, "", nil, nil, nil, now, now, "720p", 8, 0, []byte(`{}`),
		))

	repo := NewVideoTaskBillingRepository(db)
	task, err := repo.AttachUpstreamTask(context.Background(), 9, "upstream-video-1", service.VideoTaskStatusPending, []byte(`{"status":"queued"}`))
	require.NoError(t, err)
	require.Equal(t, "upstream-video-1", task.UpstreamTaskID)
	require.Equal(t, service.VideoTaskStatusPending, task.TaskStatus)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryBeginSettlementRequiresCompletedReservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)UPDATE video_task_billings\s+SET billing_status = \$2,.*WHERE id = \$1\s+AND task_status = \$3\s+AND billing_status = \$4.*RETURNING`).
		WithArgs(int64(9), service.VideoTaskBillingSettling, service.VideoTaskStatusCompleted, service.VideoTaskBillingReserved).
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusCompleted, service.VideoTaskBillingSettling,
			[]byte(`{"status":"completed"}`), 2, now, now, "", nil, nil, nil, now, now, "720p", 8, 0, []byte(`{}`),
		))

	repo := NewVideoTaskBillingRepository(db)
	task, err := repo.BeginSettlement(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, service.VideoTaskBillingSettling, task.BillingStatus)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryMarkSubmissionUnknownMovesToManualReview(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectExec(`(?s)UPDATE video_task_billings\s+SET task_status = \$2,\s+billing_status = \$3,.*WHERE id = \$1\s+AND task_status = \$5\s+AND billing_status = \$6`).
		WithArgs(int64(9), service.VideoTaskStatusSubmissionUnknown, service.VideoTaskBillingManualReview, "upstream timeout", service.VideoTaskStatusSubmitting, service.VideoTaskBillingReserved).
		WillReturnResult(sqlmock.NewResult(0, 1))

	repo := NewVideoTaskBillingRepository(db)
	require.NoError(t, repo.MarkSubmissionUnknown(context.Background(), 9, "upstream timeout"))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryClaimDueUsesLeaseAndSkipLocked(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	now := time.Now().UTC()
	mock.ExpectQuery(`(?s)WITH candidates AS \(.*billing_status = \$1.*task_status IN \(\$2, \$3, \$4\).*next_poll_at <= NOW\(\).*claimed_until IS NULL OR claimed_until < NOW\(\).*FOR UPDATE SKIP LOCKED.*UPDATE video_task_billings AS tasks.*SET claimed_until = NOW\(\) \+ \(\$6 \* INTERVAL '1 second'\).*RETURNING`).
		WithArgs(service.VideoTaskBillingReserved, service.VideoTaskStatusPending, service.VideoTaskStatusProcessing, service.VideoTaskStatusUnknown, 10, int64(45), service.VideoTaskStatusSubmitting, service.VideoTaskBillingSettling, service.VideoTaskStatusCompleted).
		WillReturnRows(videoTaskBillingRows().AddRow(
			int64(9), "request-1", "upstream-video-1", "openai", int64(7), int64(11), int64(13), int64(17),
			"video-model", "video-model", 1.25, nil, service.VideoTaskStatusPending, service.VideoTaskBillingReserved,
			[]byte(`{"status":"queued"}`), 1, nil, now, "", nil, nil, now.Add(45*time.Second), now, now, "720p", 8, 0, []byte(`{}`),
		))

	repo := NewVideoTaskBillingRepository(db)
	tasks, err := repo.ClaimDue(context.Background(), 10, 45*time.Second)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, int64(9), tasks[0].ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestVideoTaskBillingRepositoryUpdatePollOnlyUpdatesReservedTasks(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := NewVideoTaskBillingRepository(db)

	mock.ExpectQuery(`(?s)UPDATE video_task_billings.*WHERE id = \$1 AND billing_status = \$8`).
		WithArgs(
			int64(9), service.VideoTaskStatusUnknown, nil, "upstream timeout", sqlmock.AnyArg(),
			service.VideoTaskStatusCompleted, service.VideoTaskStatusFailed, service.VideoTaskBillingReserved,
		).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.UpdatePoll(context.Background(), 9, service.VideoTaskOutcome{
		Status: service.VideoTaskStatusUnknown, ErrorMessage: "upstream timeout",
	}, time.Now())

	require.ErrorIs(t, err, service.ErrVideoTaskBillingInvalidState)
	require.NoError(t, mock.ExpectationsWereMet())
}

func ptrInt64(value int64) *int64 {
	return &value
}

func videoTaskBillingRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "request_id", "upstream_task_id", "platform", "user_id", "api_key_id", "group_id", "account_id",
		"model", "upstream_model", "estimated_cost", "actual_cost", "task_status", "billing_status", "response_json",
		"poll_count", "last_polled_at", "next_poll_at", "last_poll_error", "submission_deadline", "terminal_at",
		"claimed_until", "updated_at", "created_at", "resolution", "duration_seconds", "reference_image_count", "usage_context_json",
	})
}
