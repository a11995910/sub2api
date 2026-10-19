package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type pelicanAccountTestRepository struct{ db *sql.DB }

func NewPelicanAccountTestRepository(db *sql.DB) service.PelicanAccountTestRepository {
	return &pelicanAccountTestRepository{db: db}
}

const pelicanAccountColumns = `account_id, run_id, status, model_id, reasoning_effort, error_message, started_at, finished_at, running_until`

type pelicanAccountScanner interface{ Scan(...any) error }

func scanPelicanAccount(row pelicanAccountScanner, detail bool) (*service.PelicanAccountResult, error) {
	r := &service.PelicanAccountResult{}
	dest := []any{&r.AccountID, &r.RunID, &r.Status, &r.ModelID, &r.ReasoningEffort, &r.ErrorMessage, &r.StartedAt, &r.FinishedAt, &r.RunningUntil}
	if detail {
		dest = append(dest, &r.ResponseText)
	}
	err := row.Scan(dest...)
	return r, err
}
func (r *pelicanAccountTestRepository) Claim(ctx context.Context, input service.PelicanAccountStart, runID string, now, until time.Time) ([]*service.PelicanAccountResult, error) {
	models, err := json.Marshal(input.Models)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `INSERT INTO pelican_account_test_results
 (account_id,run_id,status,model_id,reasoning_effort,started_at,running_until)
 SELECT id,$1,'running',COALESCE($2::jsonb->>platform,''),$3,$4,$5 FROM accounts
 WHERE deleted_at IS NULL AND type='oauth' AND ($6 OR id=ANY($7)) ORDER BY id
 ON CONFLICT (account_id) DO UPDATE SET run_id=EXCLUDED.run_id,status='running',
 model_id=EXCLUDED.model_id,reasoning_effort=EXCLUDED.reasoning_effort,
 response_text='',error_message='',started_at=EXCLUDED.started_at,finished_at=NULL,running_until=EXCLUDED.running_until
 WHERE pelican_account_test_results.status<>'running' OR pelican_account_test_results.running_until<=$4
 RETURNING `+pelicanAccountColumns, runID, string(models), input.ReasoningEffort, now, until, input.All, pq.Array(input.AccountIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*service.PelicanAccountResult, 0)
	for rows.Next() {
		item, err := scanPelicanAccount(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *pelicanAccountTestRepository) List(ctx context.Context) ([]*service.PelicanAccountResult, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+pelicanAccountColumns+` FROM pelican_account_test_results WHERE account_id IN (SELECT id FROM accounts WHERE deleted_at IS NULL AND type='oauth') ORDER BY account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*service.PelicanAccountResult, 0)
	for rows.Next() {
		item, err := scanPelicanAccount(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (r *pelicanAccountTestRepository) Get(ctx context.Context, id int64) (*service.PelicanAccountResult, error) {
	item, err := scanPelicanAccount(r.db.QueryRowContext(ctx, `SELECT `+pelicanAccountColumns+`,response_text FROM pelican_account_test_results WHERE account_id=$1 AND account_id IN (SELECT id FROM accounts WHERE deleted_at IS NULL AND type='oauth')`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}
func (r *pelicanAccountTestRepository) Complete(ctx context.Context, result *service.PelicanAccountResult) error {
	_, err := r.db.ExecContext(ctx, `UPDATE pelican_account_test_results SET status=$3,model_id=$4,response_text=$5,error_message=$6,finished_at=$7
 WHERE account_id=$1 AND run_id=$2 AND status='running'`, result.AccountID, result.RunID, result.Status, result.ModelID, result.ResponseText, result.ErrorMessage, result.FinishedAt)
	return err
}
func (r *pelicanAccountTestRepository) Stop(ctx context.Context, runs []service.PelicanAccountRun) ([]service.PelicanAccountRun, error) {
	if len(runs) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(runs)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `UPDATE pelican_account_test_results AS r SET status='stopped',response_text='',error_message='',finished_at=NOW()
 FROM jsonb_to_recordset($1::jsonb) AS requested(account_id bigint,run_id text)
 WHERE r.account_id=requested.account_id AND r.run_id=requested.run_id AND r.status='running' RETURNING r.account_id,r.run_id`, string(data))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []service.PelicanAccountRun{}
	for rows.Next() {
		var run service.PelicanAccountRun
		if err := rows.Scan(&run.AccountID, &run.RunID); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
func (r *pelicanAccountTestRepository) Expire(ctx context.Context, now time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE pelican_account_test_results SET status='failed',response_text='',error_message='任务中断或超时，请重新测试',finished_at=$1 WHERE status='running' AND running_until<=$1`, now)
	return err
}
