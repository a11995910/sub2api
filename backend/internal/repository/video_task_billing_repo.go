package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoTaskBillingRepository struct {
	db *sql.DB
}

func NewVideoTaskBillingRepository(db *sql.DB) service.VideoTaskBillingRepository {
	return &videoTaskBillingRepository{db: db}
}

func (r *videoTaskBillingRepository) ReserveAndCreate(ctx context.Context, task *service.VideoTaskBilling) (err error) {
	if r == nil || r.db == nil {
		return errors.New("video task billing repository db is nil")
	}
	if task == nil || task.UserID <= 0 || task.APIKeyID <= 0 || task.AccountID <= 0 || task.RequestID == "" {
		return errors.New("video task billing reservation is invalid")
	}
	task.EstimatedCost = service.QuantizeUsageBillingAmount(task.EstimatedCost)
	if task.EstimatedCost < 0 {
		return fmt.Errorf("video task estimated cost must be non-negative")
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	if task.EstimatedCost > 0 {
		var balance, frozen float64
		err = tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance - $1,
				frozen_balance = COALESCE(frozen_balance, 0) + $1,
				updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
			RETURNING balance, frozen_balance
		`, task.EstimatedCost, task.UserID).Scan(&balance, &frozen)
		if errors.Is(err, sql.ErrNoRows) {
			exists, existsErr := userExistsForBilling(ctx, tx, task.UserID)
			if existsErr != nil {
				return existsErr
			}
			if !exists {
				return service.ErrUserNotFound
			}
			return service.ErrBatchImageInsufficientBalance
		}
		if err != nil {
			return err
		}
	}

	response := task.ResponseJSON
	if len(response) == 0 {
		response = json.RawMessage(`{}`)
	}
	usageContext := task.UsageContextJSON
	if len(usageContext) == 0 {
		usageContext = json.RawMessage(`{}`)
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO video_task_billings (
			request_id, upstream_task_id, platform, user_id, api_key_id, group_id, account_id,
			model, upstream_model, resolution, duration_seconds, reference_image_count, usage_context_json,
			estimated_cost, task_status, billing_status, response_json, submission_deadline, next_poll_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id, created_at, updated_at
	`, task.RequestID, task.UpstreamTaskID, task.Platform, task.UserID, task.APIKeyID, task.GroupID, task.AccountID,
		task.Model, task.UpstreamModel, task.Resolution, task.DurationSeconds, task.ReferenceImageCount, usageContext,
		task.EstimatedCost, task.TaskStatus, task.BillingStatus, response, task.SubmissionDeadline, task.NextPollAt,
	).Scan(&task.ID, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (r *videoTaskBillingRepository) GetByTask(ctx context.Context, platform, taskID string) (*service.VideoTaskBilling, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, videoTaskBillingSelectSQL()+`
		WHERE platform = $1 AND upstream_task_id = $2
	`, platform, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskBillingNotFound
	}
	return task, err
}

func (r *videoTaskBillingRepository) GetByID(ctx context.Context, id int64) (*service.VideoTaskBilling, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, videoTaskBillingSelectSQL()+`
		WHERE id = $1
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskBillingNotFound
	}
	return task, err
}

func (r *videoTaskBillingRepository) AttachUpstreamTask(
	ctx context.Context,
	id int64,
	taskID, status string,
	response json.RawMessage,
) (*service.VideoTaskBilling, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	if len(response) == 0 {
		response = json.RawMessage(`{}`)
	}
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, `
		UPDATE video_task_billings
		SET upstream_task_id = $2,
			task_status = $3,
			response_json = $4,
			next_poll_at = NOW(),
			last_poll_error = '',
			updated_at = NOW()
		WHERE id = $1
			AND task_status = $5
			AND billing_status = $6
		RETURNING `+videoTaskBillingColumnsSQL(),
		id, taskID, status, response, service.VideoTaskStatusSubmitting, service.VideoTaskBillingReserved,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskBillingInvalidState
	}
	return task, err
}

func (r *videoTaskBillingRepository) MarkSubmissionUnknown(ctx context.Context, id int64, reason string) error {
	if r == nil || r.db == nil {
		return errors.New("video task billing repository db is nil")
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_task_billings
		SET task_status = $2,
			billing_status = $3,
			last_poll_error = $4,
			claimed_until = NULL,
			terminal_at = NOW(),
			updated_at = NOW()
		WHERE id = $1
			AND task_status = $5
			AND billing_status = $6
	`, id, service.VideoTaskStatusSubmissionUnknown, service.VideoTaskBillingManualReview, reason,
		service.VideoTaskStatusSubmitting, service.VideoTaskBillingReserved)
	if err != nil {
		return err
	}
	return ensureVideoTaskBillingAffected(result)
}

func (r *videoTaskBillingRepository) ClaimDue(ctx context.Context, limit int, lease time.Duration) ([]*service.VideoTaskBilling, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	if limit <= 0 {
		limit = 20
	}
	leaseSeconds := int64(lease / time.Second)
	if leaseSeconds < 1 {
		leaseSeconds = 30
	}
	rows, err := r.db.QueryContext(ctx, `
		WITH candidates AS (
			SELECT id
			FROM video_task_billings
			WHERE (
					billing_status = $1
					AND (
						(task_status IN ($2, $3, $4) AND next_poll_at <= NOW())
						OR (task_status = $7 AND submission_deadline <= NOW())
					)
					OR (billing_status = $8 AND task_status = $9)
				)
				AND (claimed_until IS NULL OR claimed_until < NOW())
			ORDER BY next_poll_at ASC, id ASC
			LIMIT $5
			FOR UPDATE SKIP LOCKED
		)
		UPDATE video_task_billings AS tasks
		SET claimed_until = NOW() + ($6 * INTERVAL '1 second'),
			updated_at = NOW()
		FROM candidates
		WHERE tasks.id = candidates.id
		RETURNING `+videoTaskBillingColumnsSQL(),
		service.VideoTaskBillingReserved, service.VideoTaskStatusPending, service.VideoTaskStatusProcessing,
		service.VideoTaskStatusUnknown, limit, leaseSeconds, service.VideoTaskStatusSubmitting,
		service.VideoTaskBillingSettling, service.VideoTaskStatusCompleted)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	tasks := make([]*service.VideoTaskBilling, 0, limit)
	for rows.Next() {
		task, scanErr := scanVideoTaskBilling(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (r *videoTaskBillingRepository) BeginSettlement(ctx context.Context, id int64) (*service.VideoTaskBilling, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, `
		UPDATE video_task_billings
		SET billing_status = $2,
			claimed_until = NULL,
			updated_at = NOW()
		WHERE id = $1
			AND task_status = $3
			AND billing_status = $4
		RETURNING `+videoTaskBillingColumnsSQL(),
		id, service.VideoTaskBillingSettling, service.VideoTaskStatusCompleted, service.VideoTaskBillingReserved,
	))
	if err == nil {
		return task, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	existing, getErr := r.GetByID(ctx, id)
	if getErr != nil {
		return nil, getErr
	}
	if existing.BillingStatus == service.VideoTaskBillingSettling || existing.BillingStatus == service.VideoTaskBillingCaptured {
		return existing, nil
	}
	return nil, service.ErrVideoTaskBillingInvalidState
}

func (r *videoTaskBillingRepository) UpdatePoll(
	ctx context.Context,
	id int64,
	outcome service.VideoTaskOutcome,
	nextPollAt time.Time,
) (*service.VideoTaskBilling, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	var response any
	if len(outcome.ResponseJSON) > 0 {
		response = outcome.ResponseJSON
	}
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, `
		UPDATE video_task_billings
		SET task_status = $2,
				response_json = COALESCE($3, response_json),
			last_poll_error = $4,
			poll_count = poll_count + 1,
			last_polled_at = NOW(),
			next_poll_at = $5,
			claimed_until = NULL,
			terminal_at = CASE WHEN $2 IN ($6, $7) THEN NOW() ELSE terminal_at END,
			updated_at = NOW()
		WHERE id = $1 AND billing_status = $8
		RETURNING `+videoTaskBillingColumnsSQL(),
		id, outcome.Status, response, outcome.ErrorMessage, nextPollAt,
		service.VideoTaskStatusCompleted, service.VideoTaskStatusFailed,
		service.VideoTaskBillingReserved,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskBillingInvalidState
	}
	return task, err
}

func (r *videoTaskBillingRepository) Capture(ctx context.Context, id int64, actualCost float64) (err error) {
	if r == nil || r.db == nil {
		return errors.New("video task billing repository db is nil")
	}
	actualCost = service.QuantizeUsageBillingAmount(actualCost)
	if actualCost < 0 {
		return service.ErrVideoTaskBillingInvalidState
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	task, err := getVideoTaskBillingByIDForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if task.BillingStatus == service.VideoTaskBillingCaptured {
		return nil
	}
	if task.BillingStatus != service.VideoTaskBillingSettling {
		return service.ErrVideoTaskBillingInvalidState
	}
	if actualCost-task.EstimatedCost > 0.00000001 {
		return service.ErrVideoTaskSettlementCostExceedsHold
	}
	if task.EstimatedCost > 0 {
		var balance, frozen float64
		err = tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance + CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END,
				frozen_balance = COALESCE(frozen_balance, 0) - $1,
				updated_at = NOW()
			WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
			RETURNING balance, frozen_balance
		`, task.EstimatedCost, actualCost, task.UserID).Scan(&balance, &frozen)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrVideoTaskFrozenBalanceInvariantViolated
		}
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE video_task_billings
		SET actual_cost = $2,
			billing_status = $3,
			terminal_at = NOW(),
			updated_at = NOW()
		WHERE id = $1 AND billing_status = $4
	`, id, actualCost, service.VideoTaskBillingCaptured, service.VideoTaskBillingSettling)
	if err != nil {
		return err
	}
	if err := ensureVideoTaskBillingAffected(result); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func (r *videoTaskBillingRepository) Release(ctx context.Context, id int64, reason string) (err error) {
	if r == nil || r.db == nil {
		return errors.New("video task billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	task, err := getVideoTaskBillingByIDForUpdate(ctx, tx, id)
	if err != nil {
		return err
	}
	if task.BillingStatus == service.VideoTaskBillingReleased {
		return nil
	}
	if task.BillingStatus != service.VideoTaskBillingReserved && task.BillingStatus != service.VideoTaskBillingManualReview {
		return service.ErrVideoTaskBillingInvalidState
	}
	if task.EstimatedCost > 0 {
		var balance, frozen float64
		err = tx.QueryRowContext(ctx, `
			UPDATE users
			SET balance = balance + $1,
				frozen_balance = COALESCE(frozen_balance, 0) - $1,
				updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
			RETURNING balance, frozen_balance
		`, task.EstimatedCost, task.UserID).Scan(&balance, &frozen)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrVideoTaskFrozenBalanceInvariantViolated
		}
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE video_task_billings
		SET billing_status = $2,
			terminal_at = NOW(),
			updated_at = NOW(),
			last_poll_error = $4
		WHERE id = $1 AND billing_status = $3
	`, id, service.VideoTaskBillingReleased, task.BillingStatus, reason)
	if err != nil {
		return err
	}
	if err := ensureVideoTaskBillingAffected(result); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	tx = nil
	return nil
}

func videoTaskBillingSelectSQL() string {
	return "SELECT " + videoTaskBillingColumnsSQL() + " FROM video_task_billings "
}

func videoTaskBillingColumnsSQL() string {
	return `id, request_id, upstream_task_id, platform, user_id, api_key_id, group_id, account_id,
		model, upstream_model, estimated_cost, actual_cost, task_status, billing_status, response_json,
		poll_count, last_polled_at, next_poll_at, last_poll_error, submission_deadline, terminal_at,
		claimed_until, updated_at, created_at, resolution, duration_seconds, reference_image_count, usage_context_json`
}

type videoTaskBillingScanner interface {
	Scan(dest ...any) error
}

func getVideoTaskBillingByIDForUpdate(ctx context.Context, tx *sql.Tx, id int64) (*service.VideoTaskBilling, error) {
	task, err := scanVideoTaskBilling(tx.QueryRowContext(ctx, videoTaskBillingSelectSQL()+`
		WHERE id = $1
		FOR UPDATE
	`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskBillingNotFound
	}
	return task, err
}

func ensureVideoTaskBillingAffected(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrVideoTaskBillingInvalidState
	}
	return nil
}

func scanVideoTaskBilling(row videoTaskBillingScanner) (*service.VideoTaskBilling, error) {
	var task service.VideoTaskBilling
	var response, usageContext []byte
	var actualCost sql.NullFloat64
	var lastPolledAt, submissionDeadline, terminalAt, claimedUntil sql.NullTime
	if err := row.Scan(
		&task.ID, &task.RequestID, &task.UpstreamTaskID, &task.Platform, &task.UserID, &task.APIKeyID,
		&task.GroupID, &task.AccountID, &task.Model, &task.UpstreamModel, &task.EstimatedCost, &actualCost,
		&task.TaskStatus, &task.BillingStatus, &response, &task.PollCount, &lastPolledAt, &task.NextPollAt,
		&task.LastPollError, &submissionDeadline, &terminalAt, &claimedUntil, &task.UpdatedAt, &task.CreatedAt,
		&task.Resolution, &task.DurationSeconds, &task.ReferenceImageCount, &usageContext,
	); err != nil {
		return nil, err
	}
	if actualCost.Valid {
		task.ActualCost = &actualCost.Float64
	}
	if len(response) > 0 {
		task.ResponseJSON = append([]byte(nil), response...)
	}
	if len(usageContext) > 0 {
		task.UsageContextJSON = append([]byte(nil), usageContext...)
	}
	if lastPolledAt.Valid {
		task.LastPolledAt = &lastPolledAt.Time
	}
	if submissionDeadline.Valid {
		task.SubmissionDeadline = &submissionDeadline.Time
	}
	if terminalAt.Valid {
		task.TerminalAt = &terminalAt.Time
	}
	if claimedUntil.Valid {
		task.ClaimedUntil = &claimedUntil.Time
	}
	return &task, nil
}
