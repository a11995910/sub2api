package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.ExcelAutoRepository = (*accountRepository)(nil)

func (r *accountRepository) GetExcelAuto(ctx context.Context, id int64) (*service.ExcelAutoRecord, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT account_id,ciphertext,enabled,revision,state,run_id,next_attempt_at,last_error FROM account_excel_auto_authorizations WHERE account_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	v := new(service.ExcelAutoRecord)
	err = rows.Scan(&v.AccountID, &v.Ciphertext, &v.Enabled, &v.Revision, &v.State, &v.RunID, &v.NextAttemptAt, &v.LastError)
	return v, err
}
func (r *accountRepository) SaveExcelAuto(ctx context.Context, id, revision int64, ciphertext string, enabled bool) (bool, error) {
	result, err := r.sql.ExecContext(ctx, `INSERT INTO account_excel_auto_authorizations(account_id,ciphertext,enabled,state)
 SELECT id,$3,$4,CASE WHEN $4 THEN 'queued' ELSE 'disabled' END FROM accounts WHERE id=$1 AND deleted_at IS NULL AND ($2=0 OR EXISTS(SELECT 1 FROM account_excel_auto_authorizations WHERE account_id=$1 AND revision=$2))
 ON CONFLICT(account_id) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,enabled=EXCLUDED.enabled,state=EXCLUDED.state,revision=account_excel_auto_authorizations.revision+1,run_id='',last_error='',next_attempt_at=NOW(),updated_at=NOW()
 WHERE account_excel_auto_authorizations.revision=$2`, id, revision, ciphertext, enabled)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (r *accountRepository) ClaimExcelAuto(ctx context.Context, runID string) (*service.ExcelAutoRecord, error) {
	rows, err := r.sql.QueryContext(ctx, `WITH candidate AS (
 SELECT c.account_id FROM account_excel_auto_authorizations c JOIN accounts a ON a.id=c.account_id
 WHERE c.enabled AND c.next_attempt_at<=NOW() AND (c.state IN ('queued','failed') OR (c.state='running' AND c.started_at<NOW()-INTERVAL '15 minutes') OR (c.state='authorized' AND NOT EXISTS(SELECT 1 FROM account_excel_oauth_grants g WHERE g.account_id=a.id)))
 AND a.deleted_at IS NULL AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL
 AND a.status='active' AND a.schedulable AND a.extra->'openai_excel_bps'='true'::jsonb
 ORDER BY c.next_attempt_at,c.account_id FOR UPDATE OF c SKIP LOCKED LIMIT 1
 ) UPDATE account_excel_auto_authorizations c SET state='running',run_id=$1,started_at=NOW(),updated_at=NOW()
 FROM candidate WHERE c.account_id=candidate.account_id
 RETURNING c.account_id,c.ciphertext,c.enabled,c.revision,c.state,c.run_id,c.next_attempt_at,c.last_error`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	v := new(service.ExcelAutoRecord)
	err = rows.Scan(&v.AccountID, &v.Ciphertext, &v.Enabled, &v.Revision, &v.State, &v.RunID, &v.NextAttemptAt, &v.LastError)
	return v, err
}
func (r *accountRepository) FinishExcelAuto(ctx context.Context, job *service.ExcelAutoRecord, a *service.Account, old, replacement, reason string) (bool, error) {
	if reason != "" {
		result, err := r.sql.ExecContext(ctx, `UPDATE account_excel_auto_authorizations SET state='failed',run_id='',last_error=$4,next_attempt_at=NOW()+INTERVAL '30 minutes',updated_at=NOW() WHERE account_id=$1 AND revision=$2 AND run_id=$3 AND enabled AND state='running'`, job.AccountID, job.Revision, job.RunID, reason)
		if err != nil {
			return false, err
		}
		n, err := result.RowsAffected()
		return n == 1, err
	}
	if a == nil || replacement == "" {
		return false, errors.New("授权结果为空")
	}
	credentials, err := json.Marshal(a.Credentials)
	if err != nil {
		return false, err
	}
	// 配置行锁与账号行锁共同约束最终写入；手动授权变化由授权密文 CAS 拦截。
	rows, err := r.sql.QueryContext(ctx, `WITH config AS (
 SELECT account_id FROM account_excel_auto_authorizations WHERE account_id=$1 AND revision=$2 AND run_id=$3 AND enabled AND state='running' FOR UPDATE
 ), eligible AS (
 SELECT a.id FROM accounts a JOIN config c ON c.account_id=a.id WHERE a.deleted_at IS NULL
 AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL
 AND a.credentials=$4::jsonb AND a.extra->'openai_excel_bps'='true'::jsonb AND a.status='active' AND a.schedulable FOR UPDATE OF a
 ), saved AS (
 INSERT INTO account_excel_oauth_grants(account_id,ciphertext)
 SELECT id,$6 FROM eligible WHERE $5='' OR EXISTS(SELECT 1 FROM account_excel_oauth_grants WHERE account_id=$1 AND ciphertext=$5)
 ON CONFLICT(account_id) DO UPDATE SET ciphertext=EXCLUDED.ciphertext,updated_at=NOW() WHERE account_excel_oauth_grants.ciphertext=$5
 RETURNING account_id
 ) UPDATE account_excel_auto_authorizations SET state=CASE WHEN EXISTS(SELECT 1 FROM saved) THEN 'authorized' ELSE 'failed' END,
 last_error=CASE WHEN EXISTS(SELECT 1 FROM saved) THEN '' ELSE 'account_changed' END,run_id='',next_attempt_at=NOW()+INTERVAL '30 minutes',updated_at=NOW()
 WHERE account_id IN(SELECT account_id FROM config) RETURNING EXISTS(SELECT 1 FROM saved)`, job.AccountID, job.Revision, job.RunID, string(credentials), old, replacement)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var saved bool
	err = rows.Scan(&saved)
	return saved, err
}
func (r *accountRepository) RetryExcelAuto(ctx context.Context, id int64) error {
	_, err := r.sql.ExecContext(ctx, `UPDATE account_excel_auto_authorizations SET state='queued',last_error='refresh_failed',next_attempt_at=NOW()+INTERVAL '30 minutes',updated_at=NOW() WHERE account_id=$1 AND enabled AND state='authorized'`, id)
	return err
}
