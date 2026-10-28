package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.ExcelOAuthGrantRepository = (*accountRepository)(nil)

func (r *accountRepository) GetExcelOAuthGrant(ctx context.Context, id int64) (string, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT g.ciphertext FROM account_excel_oauth_grants g JOIN accounts a ON a.id=g.account_id WHERE a.id=$1 AND a.deleted_at IS NULL`, id)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", rows.Err()
	}
	var ciphertext string
	if err := rows.Scan(&ciphertext); err != nil {
		return "", err
	}
	return ciphertext, rows.Err()
}

// 账号行锁防止并发删除或身份替换；授权 CAS 防止旧刷新结果覆盖新授权。
func (r *accountRepository) CompareAndSwapExcelOAuthGrant(ctx context.Context, a *service.Account, old, replacement string) (bool, error) {
	snapshot, err := json.Marshal(a.Credentials)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, `
WITH eligible AS (
 SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL
 AND platform='openai' AND type='oauth' AND parent_account_id IS NULL
 AND credentials=$2::jsonb FOR UPDATE
)
INSERT INTO account_excel_oauth_grants (account_id, ciphertext)
 SELECT id, $4 FROM eligible
 WHERE $3='' OR EXISTS (SELECT 1 FROM account_excel_oauth_grants WHERE account_id=$1 AND ciphertext=$3)
ON CONFLICT (account_id) DO UPDATE SET ciphertext=EXCLUDED.ciphertext, updated_at=NOW()
 WHERE account_excel_oauth_grants.ciphertext=$3`, a.ID, string(snapshot), old, replacement)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
