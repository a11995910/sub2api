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

// healthyTurnStateRepository 按账号和模型保存 OpenAI 状态头。
// 传输方式和代理仅保留采集来源，同一账号的同一模型可以共用。
type healthyTurnStateRepository struct {
	db     *sql.DB
	cipher service.SecretEncryptor
}

func NewHealthyTurnStateRepository(db *sql.DB, cipher service.SecretEncryptor) service.HealthyTurnStateRepository {
	return &healthyTurnStateRepository{db, cipher}
}

func healthyStateHash(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func normalizeHealthyStateScope(scope service.HealthyTurnStateScope) service.HealthyTurnStateScope {
	scope.Model = strings.TrimSpace(scope.Model)
	return scope
}

func validHealthyStateScope(scope service.HealthyTurnStateScope) bool {
	return scope.AccountID > 0 && scope.Model != ""
}

func (r *healthyTurnStateRepository) Save(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue) (bool, error) {
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) || value.Value == "" || len(value.Value) > 16<<10 || strings.ContainsAny(value.Value, "\r\n") || !value.ExpiresAt.After(time.Now()) {
		return false, nil
	}
	hash := healthyStateHash(value.Value)
	encrypted, err := r.cipher.Encrypt(value.Value)
	if err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_account_model_rejections WHERE account_id=$1 AND model=$2 AND expires_at<=NOW()`, scope.AccountID, scope.Model); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_account_model_pool(account_id,model,value_hash,transport,proxy_id) VALUES($1,$2,$3,$4,$5) ON CONFLICT(account_id,model,value_hash) DO NOTHING`, scope.AccountID, scope.Model, hash, scope.Transport, scope.ProxyID); err != nil {
		return false, err
	}
	var existingEncrypted sql.NullString
	var existingExpires *time.Time
	var leaseToken string
	err = tx.QueryRowContext(ctx, `SELECT value_encrypted,expires_at,lease_token FROM openai_healthy_turn_state_account_model_pool WHERE account_id=$1 AND model=$2 AND value_hash=$3 FOR UPDATE`, scope.AccountID, scope.Model, hash).Scan(&existingEncrypted, &existingExpires, &leaseToken)
	if err != nil {
		return false, err
	}
	var rejected int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM openai_healthy_turn_state_account_model_rejections WHERE account_id=$1 AND model=$2 AND value_hash=$3 AND expires_at>NOW()`, scope.AccountID, scope.Model, hash).Scan(&rejected)
	if err == nil {
		return false, tx.Commit()
	} else if err != sql.ErrNoRows {
		return false, err
	}
	if leaseToken != "" {
		var leaseUntil *time.Time
		if err = tx.QueryRowContext(ctx, `SELECT lease_until FROM openai_healthy_turn_state_account_model_pool WHERE account_id=$1 AND model=$2 AND value_hash=$3`, scope.AccountID, scope.Model, hash).Scan(&leaseUntil); err != nil {
			return false, err
		}
		if leaseUntil != nil && leaseUntil.After(time.Now()) {
			return false, tx.Commit()
		}
	}
	// 提前退休的相同值也不得重新入库延寿；原有效期届满前保持原截止时间。
	if existingExpires != nil && existingExpires.After(time.Now()) {
		return false, tx.Commit()
	}
	if existingExpires != nil && existingExpires.After(value.ExpiresAt) {
		return false, tx.Commit()
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET value_encrypted=$4,transport=$5,proxy_id=$6,expires_at=$7,lease_token='',lease_until=NULL,captures=captures+1,last_captured_at=NOW() WHERE account_id=$1 AND model=$2 AND value_hash=$3`, scope.AccountID, scope.Model, hash, encrypted, scope.Transport, scope.ProxyID, value.ExpiresAt)
	if err != nil {
		return false, err
	}
	// 新头成功保存后才退休一张十分钟内到期的空闲旧头，采集失败不损失库存。
	// 跳过持有租约的头；并发领取造成的短暂备用最多随其原期限保留，不延长寿命。
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET value_encrypted=NULL,lease_token='',lease_until=NULL,last_outcome='refreshed'
		WHERE id IN (SELECT id FROM openai_healthy_turn_state_account_model_pool
		WHERE account_id=$1 AND model=$2 AND value_hash<>$3 AND value_encrypted IS NOT NULL
		AND expires_at>NOW() AND expires_at<=NOW()+INTERVAL '10 minutes'
		AND (lease_until IS NULL OR lease_until<=NOW()) AND $4::timestamptz>NOW()+INTERVAL '10 minutes'
		ORDER BY expires_at,id LIMIT 1 FOR UPDATE SKIP LOCKED)`, scope.AccountID, scope.Model, hash, value.ExpiresAt)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (r *healthyTurnStateRepository) Get(ctx context.Context, scope service.HealthyTurnStateScope) (*service.HealthyTurnStateValue, error) {
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) {
		return nil, nil
	}
	var encrypted string
	value := &service.HealthyTurnStateValue{}
	err := r.db.QueryRowContext(ctx, `SELECT value_encrypted,expires_at FROM openai_healthy_turn_state_account_model_pool p
		WHERE p.account_id=$1 AND p.model=$2 AND p.value_encrypted IS NOT NULL AND p.expires_at>NOW()
		  AND (p.lease_until IS NULL OR p.lease_until<=NOW())
		  AND NOT EXISTS (SELECT 1 FROM openai_healthy_turn_state_account_model_rejections r WHERE r.account_id=p.account_id AND r.model=p.model AND r.value_hash=p.value_hash AND r.expires_at>NOW())
		ORDER BY p.last_captured_at DESC NULLS LAST,p.id DESC LIMIT 1`, scope.AccountID, scope.Model).Scan(&encrypted, &value.ExpiresAt)
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
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) {
		return nil, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	var encrypted string
	var expiresAt time.Time
	err = tx.QueryRowContext(ctx, `SELECT id,value_encrypted,expires_at FROM openai_healthy_turn_state_account_model_pool p
		WHERE p.account_id=$1 AND p.model=$2 AND p.value_encrypted IS NOT NULL AND p.expires_at>NOW()
		  AND p.value_hash<>$3 AND (p.lease_until IS NULL OR p.lease_until<=NOW())
		  AND NOT EXISTS (SELECT 1 FROM openai_healthy_turn_state_account_model_rejections r WHERE r.account_id=p.account_id AND r.model=p.model AND r.value_hash=p.value_hash AND r.expires_at>NOW())
		ORDER BY p.last_captured_at DESC NULLS LAST,p.id DESC LIMIT 1 FOR UPDATE SKIP LOCKED`, scope.AccountID, scope.Model, healthyStateHash(strings.TrimSpace(current))).Scan(&id, &encrypted, &expiresAt)
	if err == sql.ErrNoRows {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	leaseToken := uuid.NewString()
	if _, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET lease_token=$4,lease_until=$5 WHERE account_id=$1 AND model=$2 AND id=$3`, scope.AccountID, scope.Model, id, leaseToken, expiresAt); err != nil {
		return nil, err
	}
	value := &service.HealthyTurnStateValue{LeaseToken: leaseToken, ExpiresAt: expiresAt}
	value.Value, err = r.cipher.Decrypt(encrypted)
	if err != nil {
		_, _ = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET value_encrypted=NULL,lease_token='',lease_until=NULL,last_outcome='decrypt_failed' WHERE account_id=$1 AND model=$2 AND id=$3`, scope.AccountID, scope.Model, id)
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

func (r *healthyTurnStateRepository) Release(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue) error {
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) || value.LeaseToken == "" {
		return nil
	}
	_, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET lease_token='',lease_until=NULL WHERE account_id=$1 AND model=$2 AND lease_token=$3 AND value_hash=$4`, scope.AccountID, scope.Model, value.LeaseToken, healthyStateHash(value.Value))
	return err
}

func (r *healthyTurnStateRepository) Start(ctx context.Context, scope service.HealthyTurnStateScope, value service.HealthyTurnStateValue, status int) error {
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) || value.LeaseToken == "" {
		return fmt.Errorf("健康状态头已失效或不再属于当前请求")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET attempts=attempts+1,last_used_at=NOW(),last_outcome='pending',last_http_status=$5 WHERE account_id=$1 AND model=$2 AND lease_token=$3 AND value_hash=$4 AND value_encrypted IS NOT NULL AND expires_at>NOW()`, scope.AccountID, scope.Model, value.LeaseToken, healthyStateHash(value.Value), status)
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
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) || value.LeaseToken == "" {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var hash string
	if err = tx.QueryRowContext(ctx, `SELECT value_hash FROM openai_healthy_turn_state_account_model_pool WHERE account_id=$1 AND model=$2 AND lease_token=$3 AND value_hash=$4 FOR UPDATE`, scope.AccountID, scope.Model, value.LeaseToken, healthyStateHash(value.Value)).Scan(&hash); err == sql.ErrNoRows {
		return tx.Commit()
	} else if err != nil {
		return err
	}
	outcome := "success"
	if !success {
		outcome = "failed"
		if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_account_model_rejections(account_id,model,value_hash,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '40 minutes') ON CONFLICT(account_id,model,value_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at`, scope.AccountID, scope.Model, hash); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET value_encrypted=CASE WHEN $5 THEN value_encrypted ELSE NULL END,lease_token='',lease_until=NULL,
		successes=successes+CASE WHEN $5 THEN 1 ELSE 0 END,failures=failures+CASE WHEN $5 THEN 0 ELSE 1 END,
		last_success_at=CASE WHEN $5 THEN NOW() ELSE last_success_at END,
		last_failure_at=CASE WHEN $5 THEN last_failure_at ELSE NOW() END,last_outcome=$6,last_http_status=$7
		WHERE account_id=$1 AND model=$2 AND lease_token=$3 AND value_hash=$4`, scope.AccountID, scope.Model, value.LeaseToken, hash, success, outcome, status)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) Reject(ctx context.Context, scope service.HealthyTurnStateScope, value string) error {
	scope = normalizeHealthyStateScope(scope)
	if !validHealthyStateScope(scope) || value == "" {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	hash := healthyStateHash(value)
	if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_account_model_rejections(account_id,model,value_hash,expires_at) VALUES($1,$2,$3,NOW()+INTERVAL '40 minutes') ON CONFLICT(account_id,model,value_hash) DO UPDATE SET expires_at=EXCLUDED.expires_at`, scope.AccountID, scope.Model, hash); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool SET value_encrypted=NULL,lease_token='',lease_until=NULL WHERE account_id=$1 AND model=$2 AND value_hash=$3`, scope.AccountID, scope.Model, hash); err != nil {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO openai_healthy_turn_state_probes(account_id,model,transport,proxy_id,status,http_status,temporary_proxy) VALUES($1,$2,$3,$4,$5,$6,$7)`, accountID, probe.Model, probe.Transport, probe.ProxyID, probe.Status, probe.HTTPStatus, probe.TemporaryProxy); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_probes WHERE account_id=$1 AND id IN (SELECT id FROM openai_healthy_turn_state_probes WHERE account_id=$1 ORDER BY id DESC OFFSET 50)`, accountID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *healthyTurnStateRepository) Stats(ctx context.Context, accountID int64) (*service.HealthyTurnStateStats, error) {
	result := &service.HealthyTurnStateStats{
		Models: []service.HealthyTurnStateModelStats{}, Records: []service.HealthyTurnStateRecord{}, Probes: []service.HealthyTurnStateProbeLog{},
	}
	// 过期即删除可复用的密文与租约，保留历史计数供排障；维护器据此补齐缺口。
	if _, err := r.db.ExecContext(ctx, `UPDATE openai_healthy_turn_state_account_model_pool
		SET value_encrypted=NULL,lease_token='',lease_until=NULL
		WHERE account_id=$1 AND expires_at<=NOW()
		AND (value_encrypted IS NOT NULL OR lease_token<>'' OR lease_until IS NOT NULL)`, accountID); err != nil {
		return nil, err
	}
	if _, err := r.db.ExecContext(ctx, `DELETE FROM openai_healthy_turn_state_account_model_rejections WHERE account_id=$1 AND expires_at<=NOW()`, accountID); err != nil {
		return nil, err
	}
	// 累计统计覆盖本账号的全部记录，详情列表的数量限制不影响每个模型的累计数。
	rows, err := r.db.QueryContext(ctx, `SELECT model,
		COUNT(*) FILTER (WHERE value_encrypted IS NOT NULL AND expires_at>NOW() AND (lease_until IS NULL OR lease_until<=NOW())),
		COUNT(*) FILTER (WHERE value_encrypted IS NOT NULL AND expires_at>NOW() AND lease_until>NOW()),
		COUNT(*) FILTER (WHERE value_encrypted IS NOT NULL AND expires_at>NOW() AND expires_at<=NOW()+INTERVAL '10 minutes' AND (lease_until IS NULL OR lease_until<=NOW())),
		SUM(captures),SUM(attempts),SUM(successes),SUM(failures)
		FROM openai_healthy_turn_state_account_model_pool WHERE account_id=$1 GROUP BY model ORDER BY model`, accountID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var model service.HealthyTurnStateModelStats
		if err = rows.Scan(&model.Model, &model.Available, &model.InUse, &model.RefreshDue, &model.Captures, &model.Attempts, &model.Successes, &model.Failures); err != nil {
			rows.Close()
			return nil, err
		}
		result.Available += model.Available
		result.InUse += model.InUse
		result.RefreshDue += model.RefreshDue
		result.Captures += model.Captures
		result.Attempts += model.Attempts
		result.Successes += model.Successes
		result.Failures += model.Failures
		result.Models = append(result.Models, model)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = r.db.QueryContext(ctx, `SELECT model,transport,proxy_id,
		CASE WHEN expires_at<=NOW() THEN 'expired' WHEN value_encrypted IS NULL THEN 'invalid' WHEN lease_until>NOW() THEN 'in_use' ELSE 'available' END,
		captures,attempts,successes,failures,expires_at,last_captured_at,last_used_at,last_success_at,last_failure_at,last_outcome,last_http_status
		FROM openai_healthy_turn_state_account_model_pool WHERE account_id=$1 ORDER BY last_captured_at DESC NULLS LAST,id DESC LIMIT 50`, accountID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item service.HealthyTurnStateRecord
		if err = rows.Scan(&item.Model, &item.Transport, &item.ProxyID, &item.Status, &item.Captures, &item.Attempts, &item.Successes, &item.Failures, &item.ExpiresAt, &item.LastCapturedAt, &item.LastUsedAt, &item.LastSuccessAt, &item.LastFailureAt, &item.LastOutcome, &item.LastHTTPStatus); err != nil {
			rows.Close()
			return nil, err
		}
		result.Records = append(result.Records, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = r.db.QueryContext(ctx, `SELECT model,transport,proxy_id,status,http_status,created_at,temporary_proxy FROM openai_healthy_turn_state_probes WHERE account_id=$1 ORDER BY id DESC LIMIT 50`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item service.HealthyTurnStateProbeLog
		if err = rows.Scan(&item.Model, &item.Transport, &item.ProxyID, &item.Status, &item.HTTPStatus, &item.CreatedAt, &item.TemporaryProxy); err != nil {
			return nil, err
		}
		result.Probes = append(result.Probes, item)
	}
	return result, rows.Err()
}
