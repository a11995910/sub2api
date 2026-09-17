package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.SuccessfulTestRecoveryRepository = (*accountRepository)(nil)

// RecoverAccountTestIfUnchanged 在同一语句内校验行版本、恢复本次观测并记录调度事件。
func (r *accountRepository) RecoverAccountTestIfUnchanged(ctx context.Context, observation *service.AccountTestObservation) (*service.SuccessfulTestRecoveryResult, error) {
	return r.completeAccountTestIfUnchanged(ctx, observation, true)
}

// SaveAccountTestExtraIfUnchanged 只保存本次仍有效的用量观测，不触发状态恢复。
func (r *accountRepository) SaveAccountTestExtraIfUnchanged(ctx context.Context, observation *service.AccountTestObservation) error {
	_, err := r.completeAccountTestIfUnchanged(ctx, observation, false)
	return err
}

func (r *accountRepository) completeAccountTestIfUnchanged(ctx context.Context, observation *service.AccountTestObservation, recoverState bool) (*service.SuccessfulTestRecoveryResult, error) {
	result := &service.SuccessfulTestRecoveryResult{}
	if observation == nil || observation.UpdatedAt.IsZero() || (recoverState && !observation.Succeeded) {
		return result, nil
	}
	clearError := recoverState && observation.ClearError
	clearRateLimit := recoverState && observation.ClearRateLimit
	clearOverload := recoverState && observation.ClearOverload
	clearTemp := recoverState && observation.ClearTempUnsched
	keys := []string{}
	if recoverState {
		keys = observation.ModelRateLimitKeys
	}
	if keys == nil {
		keys = []string{}
	}
	clearRuntime := clearRateLimit || clearOverload || clearTemp || len(keys) > 0
	if !clearError && !clearRuntime && len(observation.PendingExtra) == 0 {
		return result, nil
	}
	extra := observation.PendingExtra
	if extra == nil {
		extra = map[string]any{}
	}
	payload, err := json.Marshal(extra)
	if err != nil {
		return nil, err
	}
	updated, err := r.sql.ExecContext(ctx, `
		WITH recovered AS (
			UPDATE accounts SET
				status = CASE WHEN $3 THEN 'active' ELSE status END,
				error_message = CASE WHEN $3 THEN '' ELSE error_message END,
				rate_limited_at = CASE WHEN $4 THEN NULL ELSE rate_limited_at END,
				rate_limit_reset_at = CASE WHEN $4 THEN NULL ELSE rate_limit_reset_at END,
				overload_until = CASE WHEN $5 THEN NULL ELSE overload_until END,
				temp_unschedulable_until = CASE WHEN $6 THEN NULL ELSE temp_unschedulable_until END,
				temp_unschedulable_reason = CASE WHEN $6 THEN NULL ELSE temp_unschedulable_reason END,
				extra = CASE WHEN cardinality($7::text[]) > 0 AND jsonb_typeof(extra->'model_rate_limits') = 'object'
					THEN jsonb_set(COALESCE(extra, '{}'::jsonb) || $9::jsonb, '{model_rate_limits}', (extra->'model_rate_limits') - $7::text[])
					ELSE COALESCE(extra, '{}'::jsonb) || $9::jsonb END,
				updated_at = NOW()
			WHERE id = $1 AND updated_at = $2 AND deleted_at IS NULL
			RETURNING id
		)
		INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		SELECT $8, id, NULL, NULL FROM recovered
	`, observation.AccountID, observation.UpdatedAt, clearError, clearRateLimit,
		clearOverload, clearTemp, pq.Array(keys), service.SchedulerOutboxEventAccountChanged, string(payload))
	if err != nil {
		return nil, err
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected > 0 {
		result.ClearedError, result.ClearedRateLimit = clearError, clearRuntime
	}
	// 即使版本冲突也读取最新状态，不把测试前的旧快照重新放回缓存。
	r.syncSchedulerAccountSnapshotDetached(ctx, observation.AccountID)
	return result, nil
}
