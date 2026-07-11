package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type creativeDrawingRepository struct {
	db *sql.DB
}

func NewCreativeDrawingRepository(db *sql.DB) service.CreativeDrawingRepository {
	return &creativeDrawingRepository{db: db}
}

func (r *creativeDrawingRepository) Create(ctx context.Context, task *service.CreativeDrawingTask) error {
	refs, err := json.Marshal(task.ReferenceImages)
	if err != nil {
		return fmt.Errorf("marshal creative drawing references: %w", err)
	}
	requestJSON, err := json.Marshal(task.RequestJSON)
	if err != nil {
		return fmt.Errorf("marshal creative drawing request: %w", err)
	}
	images, err := json.Marshal(task.Images)
	if err != nil {
		return fmt.Errorf("marshal creative drawing images: %w", err)
	}
	now := task.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO creative_drawing_tasks (
			id, user_id, api_key_id, conversation_id, turn_id, mode, model, prompt,
			size, image_count, output_format, reference_images, request_json,
			status, error, result_images, created_at, updated_at
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$17)
		RETURNING created_at, updated_at
	`,
		task.ID, task.UserID, task.APIKeyID, task.ConversationID, task.TurnID,
		task.Mode, task.Model, task.Prompt, task.Size, task.Count, task.OutputFormat,
		refs, requestJSON, task.Status, task.Error, images, now,
	).Scan(&task.CreatedAt, &task.UpdatedAt)
	return err
}

func (r *creativeDrawingRepository) GetByID(ctx context.Context, id string) (*service.CreativeDrawingTask, error) {
	row := r.db.QueryRowContext(ctx, creativeDrawingTaskSelectSQL()+` WHERE id = $1`, id)
	task, err := scanCreativeDrawingTask(row)
	if err == sql.ErrNoRows {
		return nil, service.ErrCreativeDrawingTaskNotFound
	}
	return task, err
}

func (r *creativeDrawingRepository) ListByUserID(ctx context.Context, userID int64, limit int) ([]service.CreativeDrawingTask, error) {
	rows, err := r.db.QueryContext(ctx, creativeDrawingTaskSelectSQL()+`
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCreativeDrawingTasks(rows)
}

func (r *creativeDrawingRepository) ListPending(ctx context.Context, limit int, runningTimeout time.Duration) ([]service.CreativeDrawingTask, error) {
	if runningTimeout <= 0 {
		runningTimeout = time.Minute
	}
	rows, err := r.db.QueryContext(ctx, creativeDrawingTaskSelectSQL()+`
		WHERE status = $1
			OR (
				status = $2
				AND updated_at < NOW() - INTERVAL '2 minutes'
				AND COALESCE(started_at, updated_at, created_at) > NOW() - ($4::bigint * INTERVAL '1 second')
			)
		ORDER BY created_at ASC
		LIMIT $3
	`, service.CreativeDrawingTaskStatusQueued, service.CreativeDrawingTaskStatusRunning, limit, int64(runningTimeout.Seconds()))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanCreativeDrawingTasks(rows)
}

func (r *creativeDrawingRepository) MarkStaleRunning(ctx context.Context, timeout time.Duration, message string, completedAt time.Time) (int64, error) {
	result, err := r.db.ExecContext(ctx, `
		UPDATE creative_drawing_tasks
		SET status = $1,
			error = $2,
			completed_at = $3,
			updated_at = $3
		WHERE status = $4
			AND COALESCE(started_at, updated_at, created_at) < $3::timestamptz - ($5::bigint * INTERVAL '1 second')
	`, service.CreativeDrawingTaskStatusError, message, completedAt.UTC(), service.CreativeDrawingTaskStatusRunning, int64(timeout.Seconds()))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (r *creativeDrawingRepository) MarkRunning(ctx context.Context, id string, startedAt time.Time) (*service.CreativeDrawingTask, error) {
	row := r.db.QueryRowContext(ctx, `
		WITH updated AS (
			UPDATE creative_drawing_tasks
			SET status = $2,
				error = '',
				started_at = COALESCE(started_at, $3),
				updated_at = $3
			WHERE id = $1
				AND status IN ($4, $5)
			RETURNING id
		)
		SELECT t.id, t.user_id, t.api_key_id, t.conversation_id, t.turn_id, t.mode, t.model, t.prompt,
			t.size, t.image_count, t.output_format, t.reference_images, t.request_json,
			t.status, t.error, t.result_images, t.created_at, t.updated_at, t.started_at, t.completed_at
		FROM creative_drawing_tasks t
		INNER JOIN updated u ON u.id = t.id
	`, id, service.CreativeDrawingTaskStatusRunning, startedAt.UTC(), service.CreativeDrawingTaskStatusQueued, service.CreativeDrawingTaskStatusRunning)
	task, err := scanCreativeDrawingTask(row)
	if err == sql.ErrNoRows {
		return nil, service.ErrCreativeDrawingTaskNotFound
	}
	return task, err
}

func (r *creativeDrawingRepository) MarkSuccess(ctx context.Context, id string, images []service.CreativeDrawingImageResult, completedAt time.Time) error {
	raw, err := json.Marshal(images)
	if err != nil {
		return fmt.Errorf("marshal creative drawing results: %w", err)
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE creative_drawing_tasks
		SET status = $2,
			error = '',
			result_images = $3,
			completed_at = $4,
			updated_at = $4
		WHERE id = $1
	`, id, service.CreativeDrawingTaskStatusSuccess, raw, completedAt.UTC())
	if err != nil {
		return err
	}
	return ensureCreativeDrawingRowsAffected(result)
}

func (r *creativeDrawingRepository) MarkError(ctx context.Context, id string, message string, completedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE creative_drawing_tasks
		SET status = $2,
			error = $3,
			completed_at = $4,
			updated_at = $4
		WHERE id = $1
	`, id, service.CreativeDrawingTaskStatusError, message, completedAt.UTC())
	if err != nil {
		return err
	}
	return ensureCreativeDrawingRowsAffected(result)
}

func creativeDrawingTaskSelectSQL() string {
	return `
		SELECT id, user_id, api_key_id, conversation_id, turn_id, mode, model, prompt,
			size, image_count, output_format, reference_images, request_json,
			status, error, result_images, created_at, updated_at, started_at, completed_at
		FROM creative_drawing_tasks
	`
}

type creativeDrawingScanner interface {
	Scan(dest ...any) error
}

func scanCreativeDrawingTask(row creativeDrawingScanner) (*service.CreativeDrawingTask, error) {
	var task service.CreativeDrawingTask
	var refsRaw, requestRaw, imagesRaw []byte
	var startedAt, completedAt sql.NullTime
	err := row.Scan(
		&task.ID,
		&task.UserID,
		&task.APIKeyID,
		&task.ConversationID,
		&task.TurnID,
		&task.Mode,
		&task.Model,
		&task.Prompt,
		&task.Size,
		&task.Count,
		&task.OutputFormat,
		&refsRaw,
		&requestRaw,
		&task.Status,
		&task.Error,
		&imagesRaw,
		&task.CreatedAt,
		&task.UpdatedAt,
		&startedAt,
		&completedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(refsRaw) > 0 {
		_ = json.Unmarshal(refsRaw, &task.ReferenceImages)
	}
	if len(requestRaw) > 0 {
		_ = json.Unmarshal(requestRaw, &task.RequestJSON)
	}
	if len(imagesRaw) > 0 {
		_ = json.Unmarshal(imagesRaw, &task.Images)
	}
	task.Images = service.NormalizeCreativeDrawingImageResults(task.Images)
	if task.ReferenceImages == nil {
		task.ReferenceImages = []service.CreativeDrawingReference{}
	}
	if task.Images == nil {
		task.Images = []service.CreativeDrawingImageResult{}
	}
	if startedAt.Valid {
		task.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		task.CompletedAt = &completedAt.Time
	}
	return &task, nil
}

func scanCreativeDrawingTasks(rows *sql.Rows) ([]service.CreativeDrawingTask, error) {
	out := []service.CreativeDrawingTask{}
	for rows.Next() {
		task, err := scanCreativeDrawingTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func ensureCreativeDrawingRowsAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrCreativeDrawingTaskNotFound
	}
	return nil
}
