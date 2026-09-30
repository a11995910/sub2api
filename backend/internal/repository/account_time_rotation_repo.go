package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const accountTimeRotationKey = "account_time_rotation"

type accountTimeRotationState struct {
	Config             service.AccountTimeRotationConfig `json:"config"`
	OriginalPriorities map[int64]int                     `json:"original_priorities"`
}

type accountTimeRotationRepository struct{ db *sql.DB }

func NewAccountTimeRotationRepository(db *sql.DB) service.AccountTimeRotationRepository {
	return &accountTimeRotationRepository{db: db}
}

func decodeAccountTimeRotation(raw string) (*accountTimeRotationState, error) {
	state := &accountTimeRotationState{}
	if err := json.Unmarshal([]byte(raw), state); err != nil {
		return nil, fmt.Errorf("读取时段轮候配置失败：%w", err)
	}
	if err := state.Config.Validate(); err != nil {
		return nil, fmt.Errorf("已保存的时段轮候配置无效：%w", err)
	}
	if state.OriginalPriorities == nil {
		state.OriginalPriorities = make(map[int64]int)
	}
	return state, nil
}

func (r *accountTimeRotationRepository) Get(ctx context.Context) (*service.AccountTimeRotationConfig, error) {
	var raw string
	err := r.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1`, accountTimeRotationKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return service.DefaultAccountTimeRotationConfig(), nil
	}
	if err != nil {
		return nil, err
	}
	state, err := decodeAccountTimeRotation(raw)
	if err != nil {
		return nil, err
	}
	return &state.Config, nil
}

func (r *accountTimeRotationRepository) Apply(ctx context.Context, replacement *service.AccountTimeRotationConfig, now time.Time) (*service.AccountTimeRotationConfig, error) {
	if replacement != nil {
		if err := replacement.Validate(); err != nil {
			return nil, err
		}
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if replacement != nil {
		initial, err := json.Marshal(accountTimeRotationState{Config: *service.DefaultAccountTimeRotationConfig(), OriginalPriorities: map[int64]int{}})
		if err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, NOW()) ON CONFLICT (key) DO NOTHING`, accountTimeRotationKey, string(initial)); err != nil {
			return nil, err
		}
	}
	// 配置行锁同时阻止多实例轮候和管理端保存交叉覆盖。
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = $1 FOR UPDATE`, accountTimeRotationKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return service.DefaultAccountTimeRotationConfig(), nil
	}
	if err != nil {
		return nil, err
	}
	state, err := decodeAccountTimeRotation(raw)
	if err != nil {
		return nil, err
	}
	if replacement != nil {
		if replacement.Revision != state.Config.Revision {
			return nil, infraerrors.Conflict("TIME_ROTATION_CHANGED", "时段轮候已被其他管理员修改，请重新加载后再保存")
		}
		state.Config = *replacement
		state.Config.Revision++
	}
	// 智能模式只提供运行时偏好，不改写账号 priority。切入智能模式时，先在同一事务恢复手动模式留下的原值。
	desired := state.Config.Priorities(now)
	idsMap := make(map[int64]bool)
	for id := range desired {
		idsMap[id] = true
	}
	for id := range state.OriginalPriorities {
		idsMap[id] = true
	}
	// 仅在保存启用智能配置时校验账号；周期 worker 不锁定整个智能账号池。
	validateSmartIDs := replacement != nil && state.Config.Mode == "smart" && state.Config.Enabled
	if validateSmartIDs {
		for _, id := range state.Config.Smart.AccountIDs {
			idsMap[id] = true
		}
	}
	ids := make([]int64, 0, len(idsMap))
	for id := range idsMap {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	type account struct {
		priority int
		eligible bool
	}
	accounts := make(map[int64]account)
	if len(ids) > 0 {
		rows, err := tx.QueryContext(ctx, `SELECT id, priority, (platform = 'openai' AND type = 'oauth' AND parent_account_id IS NULL) FROM accounts WHERE id = ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, pq.Array(ids))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var a account
			if err := rows.Scan(&id, &a.priority, &a.eligible); err != nil {
				_ = rows.Close()
				return nil, err
			}
			accounts[id] = a
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return nil, err
		}
	}
	if replacement != nil && replacement.Enabled {
		idsToValidate := make([]int64, 0)
		if replacement.Mode == "smart" {
			idsToValidate = replacement.Smart.AccountIDs
		} else {
			for id := range desired {
				idsToValidate = append(idsToValidate, id)
			}
		}
		for _, id := range idsToValidate {
			a, exists := accounts[id]
			if !exists || !a.eligible {
				return nil, infraerrors.BadRequest("INVALID_ROTATION_ACCOUNT", fmt.Sprintf("账号 %d 不存在或不是独立 OpenAI OAuth 账号，请移除后重新选择", id))
			}
		}
	}
	for _, id := range ids {
		a, exists := accounts[id]
		if !exists {
			delete(state.OriginalPriorities, id)
			continue
		}
		priority, managed := desired[id]
		if managed && a.eligible {
			if _, saved := state.OriginalPriorities[id]; !saved {
				state.OriginalPriorities[id] = a.priority
			}
		} else {
			var saved bool
			priority, saved = state.OriginalPriorities[id]
			delete(state.OriginalPriorities, id)
			if !saved {
				continue
			}
		}
		if priority == a.priority {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET priority = $1, updated_at = NOW() WHERE id = $2 AND deleted_at IS NULL`, priority, id); err != nil {
			return nil, err
		}
		// 与优先级更新同事务提交，通知失败时整笔回滚，避免数据库和调度快照长期不一致。
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	if string(encoded) != raw {
		if _, err := tx.ExecContext(ctx, `UPDATE settings SET value = $1, updated_at = NOW() WHERE key = $2`, string(encoded), accountTimeRotationKey); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &state.Config, nil
}
