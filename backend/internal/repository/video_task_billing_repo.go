package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoTaskBillingRepository struct {
	db *sql.DB
}

const (
	videoTaskUserLockNamespace    int64 = 1
	videoTaskAccountLockNamespace int64 = -1
)

func NewVideoTaskBillingRepository(db *sql.DB) service.VideoTaskBillingRepository {
	return &videoTaskBillingRepository{db: db}
}

func NewVideoTaskReviewRepository(db *sql.DB) service.VideoTaskReviewRepository {
	return &videoTaskBillingRepository{db: db}
}

func NewVideoTaskDeletionGuardRepository(db *sql.DB) service.VideoTaskDeletionGuard {
	return &videoTaskBillingRepository{db: db}
}

func (r *videoTaskBillingRepository) WithUserDeletionGuard(ctx context.Context, userID int64, deleteFunc func() error) error {
	if r == nil || r.db == nil {
		return errors.New("video task billing repository db is nil")
	}
	if userID <= 0 || deleteFunc == nil {
		return errors.New("video task user deletion guard input is invalid")
	}
	return r.withDeletionGuard(ctx, []videoTaskSubjectLock{{namespace: videoTaskUserLockNamespace, id: userID}}, func(tx *sql.Tx) (bool, error) {
		var exists bool
		err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM video_task_billings
			WHERE billing_status IN ($1, $2, $3) AND user_id = $4
		)
	`, service.VideoTaskBillingReserved, service.VideoTaskBillingSettling, service.VideoTaskBillingManualReview, userID).Scan(&exists)
		return exists, err
	}, deleteFunc)
}

func (r *videoTaskBillingRepository) WithAccountDeletionGuard(ctx context.Context, accountIDs []int64, deleteFunc func() error) error {
	if r == nil || r.db == nil {
		return errors.New("video task billing repository db is nil")
	}
	if deleteFunc == nil {
		return errors.New("video task account deletion guard input is invalid")
	}
	accountIDs = normalizeVideoTaskSubjectIDs(accountIDs)
	if len(accountIDs) == 0 {
		return deleteFunc()
	}
	locks := make([]videoTaskSubjectLock, 0, len(accountIDs))
	placeholders := make([]string, 0, len(accountIDs))
	args := []any{service.VideoTaskBillingReserved, service.VideoTaskBillingSettling, service.VideoTaskBillingManualReview}
	for _, accountID := range accountIDs {
		locks = append(locks, videoTaskSubjectLock{namespace: videoTaskAccountLockNamespace, id: accountID})
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)+1))
		args = append(args, accountID)
	}
	return r.withDeletionGuard(ctx, locks, func(tx *sql.Tx) (bool, error) {
		var exists bool
		err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM video_task_billings
			WHERE billing_status IN ($1, $2, $3) AND account_id IN (`+strings.Join(placeholders, ", ")+`)
		)
	`, args...).Scan(&exists)
		return exists, err
	}, deleteFunc)
}

type videoTaskSubjectLock struct {
	namespace int64
	id        int64
}

func videoTaskSubjectLockKey(lock videoTaskSubjectLock) int64 {
	return lock.namespace * lock.id
}

func (r *videoTaskBillingRepository) withDeletionGuard(ctx context.Context, locks []videoTaskSubjectLock, hasUnresolved func(*sql.Tx) (bool, error), deleteFunc func() error) (err error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, lock := range locks {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, videoTaskSubjectLockKey(lock)); err != nil {
			return err
		}
	}
	unresolved, err := hasUnresolved(tx)
	if err != nil {
		return err
	}
	if unresolved {
		return service.ErrVideoTaskBillingPending
	}
	if err := deleteFunc(); err != nil {
		return err
	}
	_ = tx.Rollback()
	return nil
}

func normalizeVideoTaskSubjectIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
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
	for _, lock := range []videoTaskSubjectLock{{namespace: videoTaskUserLockNamespace, id: task.UserID}, {namespace: videoTaskAccountLockNamespace, id: task.AccountID}} {
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, videoTaskSubjectLockKey(lock)); err != nil {
			return err
		}
	}
	if err = requireActiveVideoTaskSubject(ctx, tx, "users", task.UserID, service.ErrUserNotFound); err != nil {
		return err
	}
	if err = requireActiveVideoTaskSubject(ctx, tx, "accounts", task.AccountID, service.ErrAccountNotFound); err != nil {
		return err
	}

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
	var upstreamTaskID any
	if value := strings.TrimSpace(task.UpstreamTaskID); value != "" {
		upstreamTaskID = value
	}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO video_task_billings (
			request_id, upstream_task_id, platform, user_id, api_key_id, group_id, account_id,
			model, upstream_model, resolution, duration_seconds, reference_image_count, usage_context_json,
			estimated_cost, task_status, billing_status, response_json, submission_deadline, next_poll_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		RETURNING id, created_at, updated_at
	`, task.RequestID, upstreamTaskID, task.Platform, task.UserID, task.APIKeyID, task.GroupID, task.AccountID,
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

func requireActiveVideoTaskSubject(ctx context.Context, tx *sql.Tx, table string, id int64, notFound error) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = $1 AND deleted_at IS NULL`, id).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound
	}
	return err
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

func (r *videoTaskBillingRepository) ListReviewTasks(ctx context.Context, filter service.VideoTaskReviewFilter) ([]service.VideoTaskReviewItem, int64, error) {
	if r == nil || r.db == nil {
		return nil, 0, errors.New("video task billing repository db is nil")
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 || filter.PageSize > 100 {
		filter.PageSize = 20
	}
	clauses := []string{"(v.billing_status = 'manual_review' OR v.billing_status = 'settling' OR (v.billing_status = 'reserved' AND v.task_status = 'unknown'))"}
	args := make([]any, 0, 10)
	add := func(expr string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf(expr, len(args)))
	}
	if filter.UserID > 0 {
		add("v.user_id = $%d", filter.UserID)
	}
	if filter.APIKeyID > 0 {
		add("v.api_key_id = $%d", filter.APIKeyID)
	}
	if filter.AccountID > 0 {
		add("v.account_id = $%d", filter.AccountID)
	}
	if value := strings.TrimSpace(filter.Platform); value != "" {
		add("v.platform = $%d", value)
	}
	if value := strings.TrimSpace(filter.Model); value != "" {
		add("v.model ILIKE '%%' || $%d || '%%'", value)
	}
	if value := strings.TrimSpace(filter.TaskStatus); value != "" {
		add("v.task_status = $%d", value)
	}
	if value := strings.TrimSpace(filter.BillingStatus); value != "" {
		add("v.billing_status = $%d", value)
	}
	if filter.StartTime != nil {
		add("v.created_at >= $%d", *filter.StartTime)
	}
	if filter.EndTime != nil {
		add("v.created_at < $%d", *filter.EndTime)
	}
	if value := strings.TrimSpace(filter.Search); value != "" {
		add("(u.email ILIKE '%%' || $%[1]d || '%%' OR COALESCE(u.username, '') ILIKE '%%' || $%[1]d || '%%' OR COALESCE(k.name, '') ILIKE '%%' || $%[1]d || '%%' OR v.request_id ILIKE '%%' || $%[1]d || '%%' OR COALESCE(v.upstream_task_id, '') ILIKE '%%' || $%[1]d || '%%')", value)
	}
	where := strings.Join(clauses, " AND ")
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_task_billings v JOIN users u ON u.id=v.user_id LEFT JOIN api_keys k ON k.id=v.api_key_id WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+videoTaskBillingColumnsSQLWithPrefix("v.")+`, u.email, COALESCE(u.username, ''), COALESCE(k.name, '')
		FROM video_task_billings v
		JOIN users u ON u.id=v.user_id
		LEFT JOIN api_keys k ON k.id=v.api_key_id
		WHERE `+where+fmt.Sprintf(" ORDER BY v.created_at DESC, v.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.VideoTaskReviewItem, 0, filter.PageSize)
	for rows.Next() {
		item, scanErr := scanVideoTaskReviewItem(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *videoTaskBillingRepository) BeginManualSettlement(ctx context.Context, id int64, reason string) (*service.VideoTaskBilling, error) {
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, `
		UPDATE video_task_billings
		SET task_status=$2, billing_status=$3, last_poll_error=$4, terminal_at=NOW(), claimed_until=NULL, updated_at=NOW()
		WHERE id=$1 AND (billing_status=$5 OR (billing_status=$6 AND task_status=$7))
		RETURNING `+videoTaskBillingColumnsSQL(), id, service.VideoTaskStatusCompleted, service.VideoTaskBillingSettling, reason,
		service.VideoTaskBillingManualReview, service.VideoTaskBillingReserved, service.VideoTaskStatusUnknown))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskBillingInvalidState
	}
	return task, err
}

func (r *videoTaskBillingRepository) ReleaseReviewedFailure(ctx context.Context, id int64, reason string) (task *service.VideoTaskBilling, err error) {
	if r == nil || r.db == nil {
		return nil, errors.New("video task billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	task, err = getVideoTaskBillingByIDForUpdate(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if task.BillingStatus != service.VideoTaskBillingManualReview &&
		(task.BillingStatus != service.VideoTaskBillingReserved || task.TaskStatus != service.VideoTaskStatusUnknown) {
		return nil, service.ErrVideoTaskBillingInvalidState
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
			return nil, service.ErrVideoTaskFrozenBalanceInvariantViolated
		}
		if err != nil {
			return nil, err
		}
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE video_task_billings
		SET billing_status=$2, terminal_at=NOW(), updated_at=NOW(), last_poll_error=$3
		WHERE id=$1 AND billing_status=$4 AND task_status=$5
	`, id, service.VideoTaskBillingReleased, reason, task.BillingStatus, task.TaskStatus)
	if err != nil {
		return nil, err
	}
	if err := ensureVideoTaskBillingAffected(result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	task.BillingStatus = service.VideoTaskBillingReleased
	task.LastPollError = reason
	return task, nil
}

func (r *videoTaskBillingRepository) UpdateReviewObservation(ctx context.Context, id int64, outcome service.VideoTaskOutcome) error {
	var response any
	if len(outcome.ResponseJSON) > 0 {
		response = outcome.ResponseJSON
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE video_task_billings
		SET response_json=COALESCE($2,response_json), last_poll_error=$3, poll_count=poll_count+1,
			last_polled_at=NOW(), updated_at=NOW()
		WHERE id=$1 AND billing_status IN ($4,$5,$6)
	`, id, response, outcome.ErrorMessage, service.VideoTaskBillingManualReview, service.VideoTaskBillingSettling, service.VideoTaskBillingReserved)
	if err != nil {
		return err
	}
	return ensureVideoTaskBillingAffected(result)
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
		RETURNING `+videoTaskBillingColumnsSQLWithPrefix("tasks."),
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
	terminal := outcome.Status == service.VideoTaskStatusCompleted || outcome.Status == service.VideoTaskStatusFailed
	task, err := scanVideoTaskBilling(r.db.QueryRowContext(ctx, `
		UPDATE video_task_billings
		SET task_status = $2,
				response_json = COALESCE($3, response_json),
			last_poll_error = $4,
			poll_count = poll_count + 1,
			last_polled_at = NOW(),
			next_poll_at = $5,
			claimed_until = NULL,
			terminal_at = CASE WHEN $6 THEN NOW() ELSE terminal_at END,
			updated_at = NOW()
		WHERE id = $1 AND billing_status = $7
		RETURNING `+videoTaskBillingColumnsSQL(),
		id, outcome.Status, response, outcome.ErrorMessage, nextPollAt,
		terminal, service.VideoTaskBillingReserved,
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

func videoTaskBillingColumnsSQLWithPrefix(prefix string) string {
	columns := []string{
		"id", "request_id", "upstream_task_id", "platform", "user_id", "api_key_id", "group_id", "account_id",
		"model", "upstream_model", "estimated_cost", "actual_cost", "task_status", "billing_status", "response_json",
		"poll_count", "last_polled_at", "next_poll_at", "last_poll_error", "submission_deadline", "terminal_at",
		"claimed_until", "updated_at", "created_at", "resolution", "duration_seconds", "reference_image_count", "usage_context_json",
	}
	for i := range columns {
		columns[i] = prefix + columns[i]
	}
	return strings.Join(columns, ", ")
}

type videoTaskBillingScanner interface {
	Scan(dest ...any) error
}

func scanVideoTaskReviewItem(row videoTaskBillingScanner) (service.VideoTaskReviewItem, error) {
	var item service.VideoTaskReviewItem
	var task service.VideoTaskBilling
	var response, usageContext []byte
	var actualCost sql.NullFloat64
	var lastPolledAt, submissionDeadline, terminalAt, claimedUntil sql.NullTime
	var upstreamTaskID, lastPollError sql.NullString
	err := row.Scan(
		&task.ID, &task.RequestID, &upstreamTaskID, &task.Platform, &task.UserID, &task.APIKeyID,
		&task.GroupID, &task.AccountID, &task.Model, &task.UpstreamModel, &task.EstimatedCost, &actualCost,
		&task.TaskStatus, &task.BillingStatus, &response, &task.PollCount, &lastPolledAt, &task.NextPollAt,
		&lastPollError, &submissionDeadline, &terminalAt, &claimedUntil, &task.UpdatedAt, &task.CreatedAt,
		&task.Resolution, &task.DurationSeconds, &task.ReferenceImageCount, &usageContext, &item.UserEmail, &item.Username, &item.APIKeyName,
	)
	if err != nil {
		return item, err
	}
	if actualCost.Valid {
		task.ActualCost = &actualCost.Float64
	}
	if upstreamTaskID.Valid {
		task.UpstreamTaskID = upstreamTaskID.String
	}
	if lastPollError.Valid {
		task.LastPollError = lastPollError.String
	}
	task.ResponseJSON = append([]byte(nil), response...)
	task.UsageContextJSON = append([]byte(nil), usageContext...)
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
	item.Task = &task
	return item, nil
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
	var upstreamTaskID, lastPollError sql.NullString
	if err := row.Scan(
		&task.ID, &task.RequestID, &upstreamTaskID, &task.Platform, &task.UserID, &task.APIKeyID,
		&task.GroupID, &task.AccountID, &task.Model, &task.UpstreamModel, &task.EstimatedCost, &actualCost,
		&task.TaskStatus, &task.BillingStatus, &response, &task.PollCount, &lastPolledAt, &task.NextPollAt,
		&lastPollError, &submissionDeadline, &terminalAt, &claimedUntil, &task.UpdatedAt, &task.CreatedAt,
		&task.Resolution, &task.DurationSeconds, &task.ReferenceImageCount, &usageContext,
	); err != nil {
		return nil, err
	}
	if actualCost.Valid {
		task.ActualCost = &actualCost.Float64
	}
	if upstreamTaskID.Valid {
		task.UpstreamTaskID = upstreamTaskID.String
	}
	if lastPollError.Valid {
		task.LastPollError = lastPollError.String
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
