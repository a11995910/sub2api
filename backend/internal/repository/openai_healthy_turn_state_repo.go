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

// healthyTurnStateRepository 使用全局共享池保存 OpenAI 状态头。
// HealthyTurnStateScope 中的账号、模型、传输方式和代理仅保留在调用层用于日志与统计来源，
// 不参与状态头的读写范围。
type healthyTurnStateRepository struct {
	db     *sql.DB
	cipher service.SecretEncryptor
}

func NewHealthyTurnStateRepository(db *sql.DB, cipher service.SecretEncryptor) service.HealthyTurnStateRepository {
	return &healthyTurnStateRepository{db, cipher}
}

func healthyStateHash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func (r *healthyTurnStateRepository) lock(ctx context.Context, _ service.HealthyTurnStateScope) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, nil)
}

func (r *healthyTurnStateRepository) Save(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue) (bool, error) {
	if value.Value == "" || len(value.Value) > 16<<10 || strings.ContainsAny(value.Value, "\r\n") || !value.ExpiresAt.After(time.Now()) {
		return false, nil
	}
	hash := healthyStateHash(value.Value)
	encrypted, err := r.cipher.Encrypt(value.Value)
	if err != nil {
		return false, err
	}
	tx, err := r.lock(ctx, scope)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_pool_rejections WHERE expires_at<=NOW()`); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_pool(value_hash,model,transport,proxy_id) VALUES($1,$2,$3,$4) ON CONFLICT(value_hash) DO NOTHING`, hash, scope.Model, scope.Transport, scope.ProxyID); err != nil {
		return false, err
	}
	var existingEncrypted sql.NullString
	var existingExpires *time.Time
	var leaseToken string
	err = tx.QueryRowContext(ctx, `SELECT value_encrypted,expires_at,lease_token FROM openai_healthy_turn_state_pool WHERE value_hash=$1 FOR UPDATE`, hash).Scan(&existingEncrypted, &existingExpires, &leaseToken)
	if err != nil {
		return false, err
	}
	var rejected int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM openai_healthy_turn_state_pool_rejections WHERE value_hash=$1 AND expires_at>NOW()`, hash).Scan(&rejected)
	if err == nil {
		return false, tx.Commit()
	} else if err != sql.ErrNoRows {
		return false, err
	}
	if leaseToken != "" {
		var leaseUntil *time.Time
		if err = tx.QueryRowContext(ctx, `SELECT lease_until FROM openai_healthy_turn_state_pool WHERE value_hash=$1`, hash).Scan(&leaseUntil); err != nil {
			return false, err
		}
		if leaseUntil != nil && leaseUntil.After(time.Now()) {
			return false, tx.Commit()
		}
	}
	if existingEncrypted.Valid && existingExpires != nil && existingExpires.After(time.Now()) {
		return false, tx.Commit()
	}
	if existingExpires != nil && existingExpires.After(value.ExpiresAt) {
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET value_encrypted=$2,model=$3,transport=$4,proxy_id=$5,expires_at=$6,lease_token='',lease_until=NULL,captures=captures+1,last_captured_at=NOW() WHERE value_hash=$1`, hash, encrypted, scope.Model, scope.Transport, scope.ProxyID, value.ExpiresAt)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *healthyTurnStateRepository) Get(ctx context.Context, _ service.HealthyTurnStateScope) (*service.HealthyTurnStateValue, error) {
	var encrypted string
	value := &service.HealthyTurnStateValue{}
	err := r.db.QueryRowContext(ctx, `SELECT value_encrypted,expires_at FROM openai_healthy_turn_state_pool WHERE value_encrypted IS NOT NULL AND expires_at>NOW() AND (lease_until IS NULL OR lease_until<=NOW()) ORDER BY last_captured_at DESC NULLS LAST,id DESC LIMIT 1`).Scan(&encrypted, &value.ExpiresAt)
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

func (r *healthyTurnStateRepository) Claim(ctx context.Context, _ service.HealthyTurnStateScope, current string) (*service.HealthyTurnStateValue, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var encrypted string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT id,value_encrypted,expires_at FROM openai_healthy_turn_state_pool p
		WHERE p.value_encrypted IS NOT NULL AND p.expires_at>NOW()
		  AND p.value_hash<>$1 AND (p.lease_until IS NULL OR p.lease_until<=NOW())
		  AND NOT EXISTS (SELECT 1 FROM openai_healthy_turn_state_pool_rejections r WHERE r.value_hash=p.value_hash AND r.expires_at>NOW())
		ORDER BY p.last_captured_at DESC NULLS LAST,p.id DESC LIMIT 1 FOR UPDATE SKIP LOCKED`, healthyStateHash(strings.TrimSpace(current))).Scan(&id, &encrypted, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	leaseToken := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET lease_token=$2,lease_until=$3 WHERE id=$1`, id, leaseToken, expiresAt); err != nil {
		return nil, err
	}
	value := &service.HealthyTurnStateValue{LeaseToken: leaseToken, ExpiresAt: expiresAt}
	value.Value, err = r.cipher.Decrypt(encrypted)
	if err != nil {
		_, _ = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET value_encrypted=NULL,lease_token='',lease_until=NULL,last_outcome='decrypt_failed' WHERE id=$1`, id)
		if commitErr := tx.Commit(); commitErr != nil {
			return nil, commitErr
		}
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return value, nil
}

func (r *healthyTurnStateRepository) Release(ctx context.Context, _ service.HealthyTurnStateScope, value service.HealthyTurnStateValue) error {
	_, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET lease_token='',lease_until=NULL WHERE lease_token=$1`, value.LeaseToken)
	return err
}

func (r *healthyTurnStateRepository) Start(ctx context.Context, _ service.HealthyTurnStateScope, value service.HealthyTurnStateValue, status int) error {
	result, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET attempts=attempts+1,last_used_at=NOW(),last_outcome='pending',last_http_status=$2 WHERE lease_token=$1 AND lease_token<>'' AND value_encrypted IS NOT NULL AND expires_at>NOW()`, value.LeaseToken, status)
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

func (r *healthyTurnStateRepository) Complete(ctx context.Context, _ service.HealthyTurnStateScope, value service.HealthyTurnStateValue, success bool, status int) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	if err = tx.QueryRowContext(ctx, `SELECT value_hash FROM openai_healthy_turn_state_pool WHERE lease_token=$1 FOR UPDATE`, value.LeaseToken).Scan(&hash); err == sql.ErrNoRows {
		return tx.Commit()
	} else if err != nil {
		return err
	}
	outcome := "success"
	if !success {
		outcome = "failed"
		if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_pool_rejections(value_hash,expires_at) VALUES($1,NOW()+INTERVAL '40 minutes') ON CONFLICT(value_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at`, hash); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET value_encrypted=CASE WHEN $2 THEN value_encrypted ELSE NULL END,lease_token='',lease_until=NULL,
		successes=successes+CASE WHEN $2 THEN 1 ELSE 0 END,failures=failures+CASE WHEN $2 THEN 0 ELSE 1 END,
		last_success_at=CASE WHEN $2 THEN NOW() ELSE last_success_at END,
		last_failure_at=CASE WHEN $2 THEN last_failure_at ELSE NOW() END,last_outcome=$3,last_http_status=$4
		WHERE lease_token=$1`, value.LeaseToken, success, outcome, status)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) Reject(ctx context.Context, _ service.HealthyTurnStateScope, value string) error {
	if value == "" {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hash := healthyStateHash(value)
	if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_pool_rejections(value_hash,expires_at) VALUES($1,NOW()+INTERVAL '40 minutes') ON CONFLICT(value_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at`, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_pool SET value_encrypted=NULL,lease_token='',lease_until=NULL WHERE value_hash=$1`, hash); err != nil {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_probes(account_id,model,transport,proxy_id,status,http_status) VALUES($1,$2,$3,$4,$5,$6)`, accountID, probe.Model, probe.Transport, probe.ProxyID, probe.Status, probe.HTTPStatus); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_probes WHERE account_id=$1 AND id IN (SELECT id FROM openai_healthy_turn_state_probes WHERE account_id=$1 ORDER BY id DESC OFFSET 50)`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) Stats(ctx context.Context, accountID int64) (*service.HealthyTurnStateStats, error) {
	// 记录与账号无关，任何 OpenAI 账号编辑页都查看同一个共享池；手动测试历史仍按账号过滤。
	result := &service.HealthyTurnStateStats{Records: []service.HealthyTurnStateRecord{}, Probes: []service.HealthyTurnStateProbeLog{}}
	rows, err := r.db.QueryContext(ctx, `SELECT model,transport,proxy_id,
		CASE WHEN expires_at<=NOW() THEN 'expired' WHEN value_encrypted IS NULL THEN 'invalid' WHEN lease_until>NOW() THEN 'in_use' ELSE 'available' END,
		captures,attempts,successes,failures,expires_at,last_captured_at,last_used_at,last_success_at,last_failure_at,last_outcome,last_http_status
		FROM openai_healthy_turn_state_pool ORDER BY last_captured_at DESC NULLS LAST,id DESC`)
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
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
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
