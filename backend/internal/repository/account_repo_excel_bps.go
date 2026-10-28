package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountExcelBPSRepository = (*accountRepository)(nil)

// 403 只修改协议开关；已更换凭据或独立授权的账号不受旧请求影响。
func (r *accountRepository) DisableExcelBPSOn403(ctx context.Context, account *service.Account, expectedGrant string) (bool, error) {
	if !account.IsExcelBPSAutoDisableOn403Enabled() {
		return false, nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.disableExcelBPSOn403InTx(ctx, account, expectedGrant)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.disableExcelBPSOn403InTx(ctx, account, expectedGrant)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := r.disableExcelBPSOn403InTx(dbent.NewTxContext(ctx, tx), account, expectedGrant)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if changed {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return changed, nil
}

func (r *accountRepository) disableExcelBPSOn403InTx(ctx context.Context, account *service.Account, expectedGrant string) (bool, error) {
	credentials, err := json.Marshal(account.Credentials)
	if err != nil {
		return false, err
	}
	client := clientFromContext(ctx, r.client)
	// 与授权 CAS 使用相同的账号行锁；等待并发授权提交后，以新语句快照核对密文。
	if _, err := client.ExecContext(ctx, "SELECT id FROM accounts WHERE id=$1 FOR UPDATE", account.ID); err != nil {
		return false, err
	}
	// 关闭开关时同时记录时间，便于管理员核查原因。
	result, err := client.ExecContext(ctx, `
UPDATE accounts
SET extra = jsonb_set(extra, '{openai_excel_bps}', 'false'::jsonb) || jsonb_build_object('openai_excel_bps_403_disabled_at', $3::text), updated_at = NOW()
WHERE id = $1 AND deleted_at IS NULL AND parent_account_id IS NULL
  AND platform = 'openai' AND type = 'oauth'
  AND credentials = $2::jsonb
  AND extra -> 'openai_excel_bps' = 'true'::jsonb
  AND extra -> 'openai_excel_bps_auto_disable_on_403' = 'true'::jsonb
 AND COALESCE((SELECT g.ciphertext FROM account_excel_oauth_grants g WHERE g.account_id=accounts.id), '') = $4`,
		account.ID, string(credentials), time.Now().UTC().Format(time.RFC3339), expectedGrant)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil || affected == 0 {
		return false, err
	}
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountChanged, &account.ID, nil, nil); err != nil {
		return false, err
	}
	return true, nil
}
