package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

type healthyTurnStateRepository struct {
	db     *sql.DB
	cipher service.SecretEncryptor
}

func NewHealthyTurnStateRepository(db *sql.DB, cipher service.SecretEncryptor) service.HealthyTurnStateRepository {
	return &healthyTurnStateRepository{db, cipher}
}

func healthyStateHash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func (r *healthyTurnStateRepository) lock(ctx context.Context, scope service.HealthyTurnStateScope) (*sql.Tx, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_states(account_id,scope_key,model,transport,proxy_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, scope.AccountID, scope.Key, scope.Model, scope.Transport, scope.ProxyID)
	if err == nil {
		var id int64
		err = tx.QueryRowContext(ctx, `SELECT account_id FROM openai_healthy_turn_states WHERE account_id=$1 AND scope_key=$2 FOR UPDATE`, scope.AccountID, scope.Key).Scan(&id)
	}
	if err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	return tx, nil
}

func (r *healthyTurnStateRepository) Save(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue) (bool, error) {
	if value.Value == "" || len(value.Value) > 16<<10 || strings.ContainsAny(value.Value, "\r\n") || !value.ExpiresAt.After(time.Now()) {
		return false, nil
	}
	encrypted, err := r.cipher.Encrypt(value.Value)
	if err != nil {
		return false, err
	}
	tx, err := r.lock(ctx, scope)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_rejections WHERE account_id=$1 AND expires_at<=NOW()`, scope.AccountID)
	if err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE openai_healthy_turn_states SET value_encrypted=$3,value_hash=$4,expires_at=$5,lease_token='',lease_until=NULL,captures=captures+1,last_captured_at=NOW(),proxy_id=$6
 WHERE account_id=$1 AND scope_key=$2 AND (lease_until IS NULL OR lease_until<=NOW())
	AND (value_hash<>$4 OR expires_at<=NOW() OR expires_at IS NULL)
	AND (expires_at IS NULL OR expires_at<=$5)
 AND NOT EXISTS(SELECT 1 FROM openai_healthy_turn_state_rejections WHERE account_id=$1 AND scope_key=$2 AND value_hash=$4 AND expires_at>NOW())`, scope.AccountID, scope.Key, encrypted, healthyStateHash(value.Value), value.ExpiresAt, scope.ProxyID)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, tx.Commit()
}

func (r *healthyTurnStateRepository) Get(ctx context.Context, scope service.HealthyTurnStateScope) (*service.HealthyTurnStateValue, error) {
	var encrypted string
	value := &service.HealthyTurnStateValue{}
	err := r.db.QueryRowContext(ctx, `SELECT value_encrypted,expires_at FROM openai_healthy_turn_states WHERE account_id=$1 AND scope_key=$2 AND value_encrypted IS NOT NULL AND expires_at>NOW() AND (lease_until IS NULL OR lease_until<=NOW())`, scope.AccountID, scope.Key).Scan(&encrypted, &value.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value.Value, err = r.cipher.Decrypt(encrypted)
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (r *healthyTurnStateRepository) Claim(ctx context.Context, scope service.HealthyTurnStateScope, current string) (*service.HealthyTurnStateValue, error) {
	value := &service.HealthyTurnStateValue{LeaseToken: uuid.NewString()}
	var encrypted string
	err := r.db.QueryRowContext(ctx, `UPDATE openai_healthy_turn_states SET lease_token=$3,lease_until=expires_at
 WHERE account_id=$1 AND scope_key=$2 AND value_encrypted IS NOT NULL AND expires_at>NOW()
 AND value_hash<>$4 AND (lease_until IS NULL OR lease_until<=NOW()) RETURNING value_encrypted,expires_at`, scope.AccountID, scope.Key, value.LeaseToken, healthyStateHash(strings.TrimSpace(current))).Scan(&encrypted, &value.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	value.Value, err = r.cipher.Decrypt(encrypted)
	if err != nil {
		// 不能解密的记录不可领取，保留历史统计，不输出密文或错误参数。
		_, _ = r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_states SET value_encrypted=NULL,lease_token='',lease_until=NULL,last_outcome='decrypt_failed' WHERE account_id=$1 AND scope_key=$2 AND lease_token=$3`, scope.AccountID, scope.Key, value.LeaseToken)
		return nil, err
	}
	return value, nil
}

func (r *healthyTurnStateRepository) Release(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue) error {
	_, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_states SET lease_token='',lease_until=NULL WHERE account_id=$1 AND scope_key=$2 AND lease_token=$3`, scope.AccountID, scope.Key, value.LeaseToken)
	return err
}

func (r *healthyTurnStateRepository) Start(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue, status int) error {
	result, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_states SET attempts=attempts+1,last_used_at=NOW(),last_outcome='pending',last_http_status=$4 WHERE account_id=$1 AND scope_key=$2 AND lease_token=$3 AND lease_token<>'' AND value_encrypted IS NOT NULL AND expires_at>NOW()`, scope.AccountID, scope.Key, value.LeaseToken, status)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("健康状态头已失效或不再属于当前请求")
	}
	return nil
}

func (r *healthyTurnStateRepository) Complete(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue, success bool, status int) error {
	tx, err := r.lock(ctx, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var token string
	if err = tx.QueryRowContext(ctx, `SELECT lease_token FROM openai_healthy_turn_states WHERE account_id=$1 AND scope_key=$2`, scope.AccountID, scope.Key).Scan(&token); err != nil {
		return err
	}
	if token == "" || token != value.LeaseToken {
		return tx.Commit()
	}
	outcome := "success"
	if !success {
		outcome = "failed"
		if err = r.rejectInTx(ctx, tx, scope, value.Value); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_states SET lease_token='',lease_until=NULL,
 successes=successes+CASE WHEN $4 THEN 1 ELSE 0 END,failures=failures+CASE WHEN $4 THEN 0 ELSE 1 END,
 last_success_at=CASE WHEN $4 THEN NOW() ELSE last_success_at END,
 last_failure_at=CASE WHEN $4 THEN last_failure_at ELSE NOW() END,last_outcome=$5,last_http_status=$6
 WHERE account_id=$1 AND scope_key=$2 AND lease_token=$3`, scope.AccountID, scope.Key, value.LeaseToken, success, outcome, status)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) rejectInTx(ctx context.Context, tx *sql.Tx, scope service.HealthyTurnStateScope, value string) error {
	hash := healthyStateHash(value)
	_, err := tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_rejections(account_id,scope_key,value_hash,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '40 minutes') ON CONFLICT(account_id,scope_key,value_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at`, scope.AccountID, scope.Key, hash)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_states SET value_encrypted=NULL WHERE account_id=$1 AND scope_key=$2 AND value_hash=$3`, scope.AccountID, scope.Key, hash)
	return err
}

func (r *healthyTurnStateRepository) Reject(ctx context.Context, scope service.HealthyTurnStateScope, value string) error {
	if value == "" {
		return nil
	}
	tx, err := r.lock(ctx, scope)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = r.rejectInTx(ctx, tx, scope, value); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) RecordProbe(ctx context.Context, accountID int64, probe service.HealthyTurnStateProbeLog) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_probes(account_id,model,transport,proxy_id,status,http_status) VALUES($1,$2,$3,$4,$5,$6)`, accountID, probe.Model, probe.Transport, probe.ProxyID, probe.Status, probe.HTTPStatus)
	if err != nil {
		return err
	}
	// 每账号保留最近五十次手动测试，失败和无状态头同样可以查到。
	_, err = tx.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_probes WHERE account_id=$1 AND id IN (SELECT id FROM openai_healthy_turn_state_probes WHERE account_id=$1 ORDER BY id DESC OFFSET 50)`, accountID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) Stats(ctx context.Context, accountID int64) (*service.HealthyTurnStateStats, error) {
	result := &service.HealthyTurnStateStats{Records: []service.HealthyTurnStateRecord{}, Probes: []service.HealthyTurnStateProbeLog{}}
	rows, err := r.db.QueryContext(ctx, `SELECT model,transport,proxy_id,
 CASE WHEN expires_at<=NOW() THEN 'expired' WHEN value_encrypted IS NULL THEN 'invalid' WHEN lease_until>NOW() THEN 'in_use' ELSE 'available' END,
 captures,attempts,successes,failures,expires_at,last_captured_at,last_used_at,last_success_at,last_failure_at,last_outcome,last_http_status
 FROM openai_healthy_turn_states WHERE account_id=$1 ORDER BY last_captured_at DESC NULLS LAST`, accountID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item service.HealthyTurnStateRecord
		if err = rows.Scan(&item.Model, &item.Transport, &item.ProxyID, &item.Status, &item.Captures, &item.Attempts, &item.Successes, &item.Failures, &item.ExpiresAt, &item.LastCapturedAt, &item.LastUsedAt, &item.LastSuccessAt, &item.LastFailureAt, &item.LastOutcome, &item.LastHTTPStatus); err != nil {
			rows.Close()
			return nil, err
		}
		result.Captures += item.Captures
		result.Attempts += item.Attempts
		result.Successes += item.Successes
		result.Failures += item.Failures
		if item.Status == "available" {
			result.Available++
		}
		if item.Status == "in_use" {
			result.InUse++
		}
		if len(result.Records) < 50 {
			result.Records = append(result.Records, item)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, `SELECT model,transport,proxy_id,status,http_status,created_at FROM openai_healthy_turn_state_probes WHERE account_id=$1 ORDER BY id DESC LIMIT 50`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item service.HealthyTurnStateProbeLog
		if err = rows.Scan(&item.Model, &item.Transport, &item.ProxyID, &item.Status, &item.HTTPStatus, &item.CreatedAt); err != nil {
			return nil, err
		}
		result.Probes = append(result.Probes, item)
	}
	return result, rows.Err()
}
