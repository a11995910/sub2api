package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamRateMonitorRepository struct {
	db *sql.DB
}

func NewUpstreamRateMonitorRepository(db *sql.DB) service.UpstreamRateMonitorRepository {
	return &upstreamRateMonitorRepository{db: db}
}

func (r *upstreamRateMonitorRepository) Create(ctx context.Context, m *service.UpstreamRateMonitor) error {
	snapshotJSON, err := marshalUpstreamRateSnapshot(m.LastSnapshot)
	if err != nil {
		return err
	}
	err = r.db.QueryRowContext(ctx, `
		INSERT INTO upstream_rate_monitors
			(name, base_url, username, password_encrypted, enabled, last_checked_at, last_status, last_error, last_group_count, last_snapshot, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at, updated_at
	`, m.Name, m.BaseURL, m.Username, m.PasswordEncrypted, m.Enabled, nullableTimeArg(m.LastCheckedAt), normalizeUpstreamRateStatus(m.LastStatus), m.LastError, m.LastGroupCount, snapshotJSON, m.CreatedBy).
		Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create upstream rate monitor: %w", err)
	}
	return nil
}

func (r *upstreamRateMonitorRepository) GetByID(ctx context.Context, id int64) (*service.UpstreamRateMonitor, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, base_url, username, password_encrypted, enabled, last_checked_at,
		       last_status, last_error, last_group_count, last_snapshot, created_by, created_at, updated_at
		FROM upstream_rate_monitors
		WHERE id = $1
	`, id)
	return scanUpstreamRateMonitor(row)
}

func (r *upstreamRateMonitorRepository) Update(ctx context.Context, m *service.UpstreamRateMonitor) error {
	snapshotJSON, err := marshalUpstreamRateSnapshot(m.LastSnapshot)
	if err != nil {
		return err
	}
	err = r.db.QueryRowContext(ctx, `
		UPDATE upstream_rate_monitors
		SET name = $2,
		    base_url = $3,
		    username = $4,
		    password_encrypted = $5,
		    enabled = $6,
		    last_checked_at = $7,
		    last_status = $8,
		    last_error = $9,
		    last_group_count = $10,
		    last_snapshot = $11,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING updated_at
	`, m.ID, m.Name, m.BaseURL, m.Username, m.PasswordEncrypted, m.Enabled, nullableTimeArg(m.LastCheckedAt), normalizeUpstreamRateStatus(m.LastStatus), m.LastError, m.LastGroupCount, snapshotJSON).
		Scan(&m.UpdatedAt)
	if err == sql.ErrNoRows {
		return service.ErrUpstreamRateMonitorNotFound
	}
	if err != nil {
		return fmt.Errorf("update upstream rate monitor: %w", err)
	}
	return nil
}

func (r *upstreamRateMonitorRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM upstream_rate_monitors WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete upstream rate monitor: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrUpstreamRateMonitorNotFound
	}
	return nil
}

func (r *upstreamRateMonitorRepository) List(ctx context.Context, params service.UpstreamRateMonitorListParams) ([]*service.UpstreamRateMonitor, int64, error) {
	where, args := buildUpstreamRateMonitorWhere(params)
	countQuery := `SELECT COUNT(*) FROM upstream_rate_monitors` + where

	var total int64
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count upstream rate monitors: %w", err)
	}

	page, pageSize := normalizeUpstreamRatePagination(params.Page, params.PageSize)
	args = append(args, pageSize, (page-1)*pageSize)
	query := `
		SELECT id, name, base_url, username, password_encrypted, enabled, last_checked_at,
		       last_status, last_error, last_group_count, last_snapshot, created_by, created_at, updated_at
		FROM upstream_rate_monitors` + where + fmt.Sprintf(`
		ORDER BY updated_at DESC, id DESC
		LIMIT $%d OFFSET $%d
	`, len(args)-1, len(args))

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list upstream rate monitors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	items := make([]*service.UpstreamRateMonitor, 0)
	for rows.Next() {
		m, err := scanUpstreamRateMonitorRows(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate upstream rate monitors: %w", err)
	}
	return items, total, nil
}

func (r *upstreamRateMonitorRepository) UpdateSnapshot(ctx context.Context, id int64, snapshot service.UpstreamRateSnapshot, checkedAt time.Time) error {
	snapshotJSON, err := marshalUpstreamRateSnapshot(snapshot)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `
		UPDATE upstream_rate_monitors
		SET last_checked_at = $2,
		    last_status = $3,
		    last_error = '',
		    last_group_count = $4,
		    last_snapshot = $5,
		    updated_at = NOW()
		WHERE id = $1
	`, id, checkedAt, service.UpstreamRateMonitorStatusSuccess, len(snapshot), snapshotJSON)
	if err != nil {
		return fmt.Errorf("update upstream rate snapshot: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrUpstreamRateMonitorNotFound
	}
	return nil
}

func (r *upstreamRateMonitorRepository) MarkRefreshFailed(ctx context.Context, id int64, message string, checkedAt time.Time) error {
	result, err := r.db.ExecContext(ctx, `
		UPDATE upstream_rate_monitors
		SET last_checked_at = $2,
		    last_status = $3,
		    last_error = $4,
		    updated_at = NOW()
		WHERE id = $1
	`, id, checkedAt, service.UpstreamRateMonitorStatusFailed, truncateString(strings.TrimSpace(message), 1000))
	if err != nil {
		return fmt.Errorf("mark upstream rate refresh failed: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return service.ErrUpstreamRateMonitorNotFound
	}
	return nil
}

type upstreamRateRowScanner interface {
	Scan(dest ...any) error
}

func scanUpstreamRateMonitor(row upstreamRateRowScanner) (*service.UpstreamRateMonitor, error) {
	m, err := scanUpstreamRateMonitorFrom(row)
	if err == sql.ErrNoRows {
		return nil, service.ErrUpstreamRateMonitorNotFound
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

func scanUpstreamRateMonitorRows(row upstreamRateRowScanner) (*service.UpstreamRateMonitor, error) {
	m, err := scanUpstreamRateMonitorFrom(row)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func scanUpstreamRateMonitorFrom(row upstreamRateRowScanner) (*service.UpstreamRateMonitor, error) {
	m := &service.UpstreamRateMonitor{}
	var checked sql.NullTime
	var snapshotJSON []byte
	if err := row.Scan(
		&m.ID,
		&m.Name,
		&m.BaseURL,
		&m.Username,
		&m.PasswordEncrypted,
		&m.Enabled,
		&checked,
		&m.LastStatus,
		&m.LastError,
		&m.LastGroupCount,
		&snapshotJSON,
		&m.CreatedBy,
		&m.CreatedAt,
		&m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if checked.Valid {
		m.LastCheckedAt = &checked.Time
	}
	snapshot, err := unmarshalUpstreamRateSnapshot(snapshotJSON)
	if err != nil {
		return nil, err
	}
	m.LastSnapshot = snapshot
	return m, nil
}

func buildUpstreamRateMonitorWhere(params service.UpstreamRateMonitorListParams) (string, []any) {
	clauses := make([]string, 0, 2)
	args := make([]any, 0, 2)
	if params.Enabled != nil {
		args = append(args, *params.Enabled)
		clauses = append(clauses, fmt.Sprintf("enabled = $%d", len(args)))
	}
	if search := strings.TrimSpace(params.Search); search != "" {
		args = append(args, "%"+search+"%")
		clauses = append(clauses, fmt.Sprintf("(name ILIKE $%d OR base_url ILIKE $%d OR username ILIKE $%d)", len(args), len(args), len(args)))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

func normalizeUpstreamRatePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func normalizeUpstreamRateStatus(status string) string {
	switch status {
	case service.UpstreamRateMonitorStatusSuccess, service.UpstreamRateMonitorStatusFailed:
		return status
	default:
		return service.UpstreamRateMonitorStatusUnknown
	}
}

func marshalUpstreamRateSnapshot(snapshot service.UpstreamRateSnapshot) ([]byte, error) {
	if snapshot == nil {
		snapshot = service.UpstreamRateSnapshot{}
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal upstream rate snapshot: %w", err)
	}
	return b, nil
}

func unmarshalUpstreamRateSnapshot(raw []byte) (service.UpstreamRateSnapshot, error) {
	if len(raw) == 0 {
		return service.UpstreamRateSnapshot{}, nil
	}
	var snapshot service.UpstreamRateSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("unmarshal upstream rate snapshot: %w", err)
	}
	if snapshot == nil {
		return service.UpstreamRateSnapshot{}, nil
	}
	return snapshot, nil
}

func nullableTimeArg(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
