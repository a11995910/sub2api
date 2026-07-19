package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoTestTaskRepository struct {
	db *sql.DB
}

func NewVideoTestTaskRepository(db *sql.DB) service.VideoTestTaskStore {
	return &videoTestTaskRepository{db: db}
}

func (r *videoTestTaskRepository) Create(ctx context.Context, task *service.VideoTestTask) error {
	response := task.ResponseJSON
	if len(response) == 0 {
		response = json.RawMessage(`{}`)
	}
	return r.db.QueryRowContext(ctx, `
		INSERT INTO model_test_video_tasks (
			id, user_id, api_key_id, group_id, account_id, upstream_task_id,
			platform, model, prompt, resolution, duration_seconds, reference_image_count,
			status, progress, response_json, error_message, last_poll_error, last_polled_at,
			completed_at, failed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20
		)
		ON CONFLICT (user_id, api_key_id, upstream_task_id) DO UPDATE SET
			account_id = EXCLUDED.account_id,
			status = EXCLUDED.status,
			progress = EXCLUDED.progress,
			response_json = EXCLUDED.response_json,
			updated_at = NOW()
		RETURNING created_at, updated_at
	`, task.ID, task.UserID, task.APIKeyID, task.GroupID, task.AccountID, task.UpstreamTaskID,
		task.Platform, task.Model, task.Prompt, task.Resolution, task.DurationSeconds, task.ReferenceImageCount,
		task.Status, task.Progress, response, task.ErrorMessage, task.LastPollError, task.LastPolledAt,
		task.CompletedAt, task.FailedAt,
	).Scan(&task.CreatedAt, &task.UpdatedAt)
}

func (r *videoTestTaskRepository) GetByOwner(ctx context.Context, userID int64, id string) (*service.VideoTestTask, error) {
	task, err := scanVideoTestTask(r.db.QueryRowContext(ctx, videoTestTaskSelectSQL()+` WHERE user_id = $1 AND id = $2`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTestTaskNotFound
	}
	return task, err
}

func (r *videoTestTaskRepository) GetByUpstreamOwner(ctx context.Context, userID, apiKeyID int64, upstreamTaskID string) (*service.VideoTestTask, error) {
	task, err := scanVideoTestTask(r.db.QueryRowContext(ctx, videoTestTaskSelectSQL()+`
		WHERE user_id = $1 AND api_key_id = $2 AND upstream_task_id = $3
	`, userID, apiKeyID, upstreamTaskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTestTaskNotFound
	}
	return task, err
}

func (r *videoTestTaskRepository) ListByUser(ctx context.Context, userID int64, offset, limit int) ([]service.VideoTestTask, int64, error) {
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM model_test_video_tasks WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, videoTestTaskSelectSQL()+`
		WHERE user_id = $1
		ORDER BY created_at DESC, id DESC
		OFFSET $2 LIMIT $3
	`, userID, offset, limit)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]service.VideoTestTask, 0)
	for rows.Next() {
		task, scanErr := scanVideoTestTask(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		tasks = append(tasks, *task)
	}
	return tasks, total, rows.Err()
}

func (r *videoTestTaskRepository) UpdatePollResult(ctx context.Context, task *service.VideoTestTask) error {
	response := task.ResponseJSON
	if len(response) == 0 {
		response = json.RawMessage(`{}`)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE model_test_video_tasks SET
			status = $3,
			progress = $4,
			response_json = $5,
			error_message = $6,
			last_poll_error = $7,
			last_polled_at = $8,
			completed_at = $9,
			failed_at = $10,
			updated_at = NOW()
		WHERE user_id = $1 AND id = $2
	`, task.UserID, task.ID, task.Status, task.Progress, response, task.ErrorMessage,
		task.LastPollError, task.LastPolledAt, task.CompletedAt, task.FailedAt)
	if err != nil {
		return err
	}
	return ensureVideoTestTaskAffected(result)
}

func (r *videoTestTaskRepository) DeleteByOwner(ctx context.Context, userID int64, id string) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM model_test_video_tasks WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	return ensureVideoTestTaskAffected(result)
}

func (r *videoTestTaskRepository) DeleteExpiredTerminal(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		DELETE FROM model_test_video_tasks
		WHERE status IN ($1, $2)
			AND COALESCE(completed_at, failed_at, updated_at) < $3
	`, service.VideoTestTaskStatusCompleted, service.VideoTestTaskStatusFailed, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func videoTestTaskSelectSQL() string {
	return `
		SELECT id, user_id, api_key_id, group_id, account_id, upstream_task_id,
			platform, model, prompt, resolution, duration_seconds, reference_image_count,
			status, progress, response_json, error_message, last_poll_error, last_polled_at,
			completed_at, failed_at, created_at, updated_at
		FROM model_test_video_tasks
	`
}

type videoTestTaskScanner interface {
	Scan(dest ...any) error
}

func scanVideoTestTask(row videoTestTaskScanner) (*service.VideoTestTask, error) {
	var task service.VideoTestTask
	var progress sql.NullFloat64
	var response []byte
	var lastPolledAt, completedAt, failedAt sql.NullTime
	err := row.Scan(
		&task.ID, &task.UserID, &task.APIKeyID, &task.GroupID, &task.AccountID, &task.UpstreamTaskID,
		&task.Platform, &task.Model, &task.Prompt, &task.Resolution, &task.DurationSeconds, &task.ReferenceImageCount,
		&task.Status, &progress, &response, &task.ErrorMessage, &task.LastPollError, &lastPolledAt,
		&completedAt, &failedAt, &task.CreatedAt, &task.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if progress.Valid {
		task.Progress = &progress.Float64
	}
	if len(response) > 0 {
		task.ResponseJSON = append(json.RawMessage(nil), response...)
	}
	if lastPolledAt.Valid {
		task.LastPolledAt = &lastPolledAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	if failedAt.Valid {
		task.FailedAt = &failedAt.Time
	}
	return &task, nil
}

func ensureVideoTestTaskAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrVideoTestTaskNotFound
	}
	return nil
}
