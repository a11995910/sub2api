package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type candyTestRepository struct{ db *sql.DB }

func NewCandyTestRepository(db *sql.DB) service.CandyTestRepository {
	return &candyTestRepository{db: db}
}

const candySettingsColumns = `config, next_run_at, last_run_at, run_id, running_until`

func ensureCandySettings(ctx context.Context, exec sqlExecutor) error {
	data, err := json.Marshal(service.DefaultCandyTestSettings())
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO candy_test_settings (id, config) VALUES (1,$1) ON CONFLICT (id) DO NOTHING`, string(data))
	return err
}

func scanCandySettings(row scannable) (*service.CandyTestSettings, error) {
	var data []byte
	s := &service.CandyTestSettings{}
	if err := row.Scan(&data, &s.NextRunAt, &s.LastRunAt, &s.RunningRunID, &s.RunningUntil); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.CandyTestSettingsInput); err != nil {
		return nil, err
	}
	return s, nil
}

func (r *candyTestRepository) GetSettings(ctx context.Context) (*service.CandyTestSettings, error) {
	if err := ensureCandySettings(ctx, r.db); err != nil {
		return nil, err
	}
	return scanCandySettings(r.db.QueryRowContext(ctx, `SELECT `+candySettingsColumns+` FROM candy_test_settings WHERE id=1`))
}

func candyGroupIDs(settings *service.CandyTestSettingsInput) []int64 {
	return mergeGroupIDs(mergeGroupIDs(settings.NormalAddGroupIDs, settings.NormalRemoveGroupIDs),
		mergeGroupIDs(settings.DegradedAddGroupIDs, settings.DegradedRemoveGroupIDs))
}

// 分组锁与现有分组删除逻辑兼容，避免校验后到写入前被删除、停用或改平台。
func lockCandyGroups(ctx context.Context, tx *sql.Tx, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM groups WHERE id=ANY($1)
 AND deleted_at IS NULL AND status='active' AND platform='openai' ORDER BY id FOR SHARE`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(ids) {
		return service.ErrGroupNotFound
	}
	return nil
}

func (r *candyTestRepository) UpdateSettings(ctx context.Context, input *service.CandyTestSettingsInput, now time.Time) (*service.CandyTestSettings, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var next *time.Time
	if input.ScheduleEnabled {
		value, err := service.NextCandyTestRun(input.DailyTime, now)
		if err != nil {
			return nil, err
		}
		next = &value
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := ensureCandySettings(ctx, tx); err != nil {
		return nil, err
	}
	// 统一先锁配置，避免与完成批次时的配置、分组锁顺序相反。
	if _, err := scanCandySettings(tx.QueryRowContext(ctx, `SELECT `+candySettingsColumns+` FROM candy_test_settings WHERE id=1 FOR UPDATE`)); err != nil {
		return nil, err
	}
	if input.AutoGroupEnabled {
		if err := lockCandyGroups(ctx, tx, candyGroupIDs(input)); err != nil {
			return nil, err
		}
	}
	saved, err := scanCandySettings(tx.QueryRowContext(ctx, `UPDATE candy_test_settings
 SET config=$1, next_run_at=$2, updated_at=$3 WHERE id=1 RETURNING `+candySettingsColumns, string(data), next, now))
	if err != nil {
		return nil, err
	}
	return saved, tx.Commit()
}

const candyResultColumns = `id, account_id, account_name, run_id, status, answer, error_message,
 model_id, reasoning_effort, settings_snapshot, group_action, group_error, started_at, finished_at, running_until`

func scanCandyResult(row scannable, detail bool) (*service.CandyTestResult, error) {
	r := &service.CandyTestResult{}
	var settings, action []byte
	dest := []any{&r.ID, &r.AccountID, &r.AccountName, &r.RunID, &r.Status, &r.Answer, &r.ErrorMessage,
		&r.ModelID, &r.ReasoningEffort, &settings, &action, &r.GroupError, &r.StartedAt, &r.FinishedAt, &r.RunningUntil}
	if detail {
		dest = append(dest, &r.ResponseText)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(settings, &r.SettingsSnapshot); err != nil {
		return nil, err
	}
	if len(action) > 0 {
		if err := json.Unmarshal(action, &r.GroupAction); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *candyTestRepository) Claim(ctx context.Context, input service.CandyTestStart, runID string, now, until time.Time, scheduled bool) (*service.CandyTestBatch, error) {
	if runID == "" || !until.After(now) {
		return nil, fmt.Errorf("糖果测试的运行标识或租约无效")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := ensureCandySettings(ctx, tx); err != nil {
		return nil, err
	}
	s, err := scanCandySettings(tx.QueryRowContext(ctx, `SELECT `+candySettingsColumns+` FROM candy_test_settings WHERE id=1 FOR UPDATE`))
	if err != nil {
		return nil, err
	}
	if scheduled && (!s.ScheduleEnabled || s.NextRunAt == nil || s.NextRunAt.After(now)) {
		return nil, nil
	}
	if s.RunningUntil != nil && s.RunningUntil.After(now) {
		if scheduled {
			return nil, nil
		}
		return nil, service.ErrCandyTestRunning
	}
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_results SET status='error', error_message='任务中断或超时，请重新测试', finished_at=$1
 WHERE status='running' AND running_until<=$1`, now); err != nil {
		return nil, err
	}
	var next = s.NextRunAt
	if scheduled {
		value, err := service.NextCandyTestRun(s.DailyTime, now)
		if err != nil {
			return nil, err
		}
		next = &value
	}
	data, err := json.Marshal(s.CandyTestSettingsInput)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `INSERT INTO candy_test_results
 (account_id,account_name,run_id,status,model_id,reasoning_effort,settings_snapshot,started_at,running_until)
 SELECT id,name,$1,'running',$2,$3,$4,$5,$6 FROM accounts
 WHERE deleted_at IS NULL AND platform='openai' AND type='oauth' AND parent_account_id IS NULL
 AND ($7 OR id=ANY($8)) ORDER BY id RETURNING `+candyResultColumns,
		runID, s.ModelID, s.ReasoningEffort, string(data), now, until, scheduled || input.All, pq.Array(input.AccountIDs))
	if err != nil {
		return nil, err
	}
	batch := &service.CandyTestBatch{RunID: runID, Settings: s.CandyTestSettingsInput, Results: []*service.CandyTestResult{}}
	for rows.Next() {
		result, err := scanCandyResult(rows, false)
		if err != nil {
			rows.Close()
			return nil, err
		}
		batch.Results = append(batch.Results, result)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(batch.Results) == 0 {
		// 无账号时仍推进当日定时，避免每次轮询重复启动同一个空批次。
		if scheduled {
			if _, err := tx.ExecContext(ctx, `UPDATE candy_test_settings SET next_run_at=$1,run_id='',running_until=NULL WHERE id=1`, next); err != nil {
				return nil, err
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
		}
		return nil, service.ErrCandyTestNoAccounts
	}
	// 批次时间按实际账号数和有界并发计算，排队账号仍各自获得完整测试时间。
	until = now.Add(service.CandyTestLeaseDuration(len(batch.Results)))
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_results SET running_until=$2 WHERE run_id=$1`, runID, until); err != nil {
		return nil, err
	}
	for _, result := range batch.Results {
		result.RunningUntil = until
	}
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_settings SET run_id=$1,running_until=$2,next_run_at=$3 WHERE id=1`, runID, until, next); err != nil {
		return nil, err
	}
	return batch, tx.Commit()
}

func (r *candyTestRepository) Complete(ctx context.Context, result *service.CandyTestResult) error {
	if result.Status != "normal" && result.Status != "degraded" && result.Status != "error" {
		return fmt.Errorf("糖果测试的完成状态无效")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var activeRun string
	err = tx.QueryRowContext(ctx, `SELECT run_id FROM candy_test_settings
 WHERE id=1 AND run_id=$1 AND running_until>clock_timestamp() FOR UPDATE`, result.RunID).Scan(&activeRun)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	saved, err := scanCandyResult(tx.QueryRowContext(ctx, `SELECT `+candyResultColumns+` FROM candy_test_results
 WHERE id=$1 AND account_id=$2 AND run_id=$3 AND status='running' AND running_until>clock_timestamp() FOR UPDATE`, result.ID, result.AccountID, result.RunID), false)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var action *service.CandyTestGroupAction
	groupError := ""
	if saved.SettingsSnapshot != nil && saved.SettingsSnapshot.AutoGroupEnabled && result.Status != "error" {
		if _, err := tx.ExecContext(ctx, `SAVEPOINT candy_group_change`); err != nil {
			return err
		}
		action, groupError, err = applyCandyGroups(ctx, tx, saved, result.Status)
		if err != nil {
			if _, rollbackErr := tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT candy_group_change`); rollbackErr != nil {
				return rollbackErr
			}
			logger.LegacyPrintf("repository.candy_tests", "糖果测试自动调组失败：account_id=%d error=%v", saved.AccountID, err)
			action = nil
			groupError = "自动调组失败，所有分组变更已撤销，请检查服务日志后重试"
		}
		if _, err := tx.ExecContext(ctx, `RELEASE SAVEPOINT candy_group_change`); err != nil {
			return err
		}
	}
	var actionJSON any
	if action != nil {
		data, err := json.Marshal(action)
		if err != nil {
			return err
		}
		actionJSON = string(data)
	}
	finished := result.FinishedAt
	if finished == nil {
		now := time.Now()
		finished = &now
	}
	updated, err := tx.ExecContext(ctx, `UPDATE candy_test_results SET status=$2,answer=$3,response_text=$4,error_message=$5,
 finished_at=$6,group_action=$7,group_error=$8,model_id=$9 WHERE id=$1 AND running_until>clock_timestamp()`,
		saved.ID, result.Status, result.Answer, result.ResponseText, result.ErrorMessage, finished, actionJSON, groupError, result.ModelID)
	if err != nil {
		return err
	}
	affected, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// 等待分组锁可能跨过租约到期点，此时连同分组和 outbox 一并回滚。
		return nil
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	result.GroupAction = action
	result.GroupError = groupError
	return nil
}

func applyCandyGroups(ctx context.Context, tx *sql.Tx, result *service.CandyTestResult, status string) (*service.CandyTestGroupAction, string, error) {
	settings := result.SettingsSnapshot
	add, remove := settings.NormalAddGroupIDs, settings.NormalRemoveGroupIDs
	if status == "degraded" {
		add, remove = settings.DegradedAddGroupIDs, settings.DegradedRemoveGroupIDs
	}
	ids := mergeGroupIDs(add, remove)
	if err := lockCandyGroups(ctx, tx, ids); err != nil {
		if errors.Is(err, service.ErrGroupNotFound) {
			return nil, "自动调组未执行：目标分组已删除、停用或不是 OpenAI 分组", nil
		}
		return nil, "", err
	}
	var accountID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL
 AND platform='openai' AND type='oauth' AND parent_account_id IS NULL FOR UPDATE`, result.AccountID).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "自动调组未执行：账号已删除或不再是 OpenAI OAuth 根账号", nil
	}
	if err != nil {
		return nil, "", err
	}
	action := &service.CandyTestGroupAction{AddedGroupIDs: []int64{}, RemovedGroupIDs: []int64{}}
	rows, err := tx.QueryContext(ctx, `DELETE FROM account_groups WHERE account_id=$1 AND group_id=ANY($2) RETURNING group_id`, accountID, pq.Array(remove))
	if err != nil {
		return nil, "", err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, "", err
		}
		action.RemovedGroupIDs = append(action.RemovedGroupIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	rows, err = tx.QueryContext(ctx, `INSERT INTO account_groups (account_id,group_id,priority)
 SELECT $1, id, 50 FROM unnest($2::bigint[]) AS id ON CONFLICT (account_id,group_id) DO NOTHING RETURNING group_id`, accountID, pq.Array(add))
	if err != nil {
		return nil, "", err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, "", err
		}
		action.AddedGroupIDs = append(action.AddedGroupIDs, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	sort.Slice(action.AddedGroupIDs, func(i, j int) bool { return action.AddedGroupIDs[i] < action.AddedGroupIDs[j] })
	sort.Slice(action.RemovedGroupIDs, func(i, j int) bool { return action.RemovedGroupIDs[i] < action.RemovedGroupIDs[j] })
	if affected := mergeGroupIDs(action.AddedGroupIDs, action.RemovedGroupIDs); len(affected) > 0 {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountGroupsChanged, &accountID, nil, buildSchedulerGroupPayload(affected)); err != nil {
			return nil, "", err
		}
	}
	return action, "", nil
}

func (r *candyTestRepository) Finish(ctx context.Context, runID string, finished time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE candy_test_settings SET run_id='',running_until=NULL,last_run_at=$2
 WHERE id=1 AND run_id=$1 AND NOT EXISTS (SELECT 1 FROM candy_test_results WHERE run_id=$1 AND status='running')`, runID, finished)
	return err
}

func (r *candyTestRepository) Stop(ctx context.Context, runID string, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 与 Complete 保持同一锁顺序，停止确认后任何迟到响应都不能再调组。
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_settings SET run_id='',running_until=NULL,last_run_at=$2 WHERE id=1 AND run_id=$1`, runID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_results SET status='error',error_message='测试已停止',finished_at=$2
 WHERE run_id=$1 AND status='running'`, runID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *candyTestRepository) Expire(ctx context.Context, now time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_settings SET run_id='',running_until=NULL
 WHERE id=1 AND running_until<=$1`, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE candy_test_results SET status='error',error_message='任务中断或超时，请重新测试',finished_at=$1
 WHERE status='running' AND running_until<=$1`, now); err != nil {
		return err
	}
	// 每轮最多清理一千条，避免账号规模较大时长时间占用数据库连接。
	if _, err := tx.ExecContext(ctx, `DELETE FROM candy_test_results WHERE id IN (
 SELECT id FROM candy_test_results WHERE status<>'running' AND
 (started_at<$1 OR id<(SELECT id FROM candy_test_results ORDER BY id DESC OFFSET 9999 LIMIT 1))
 ORDER BY id LIMIT 1000)`, now.AddDate(0, 0, -30)); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *candyTestRepository) ListResults(ctx context.Context, offset, limit int) ([]*service.CandyTestResult, int64, error) {
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM candy_test_results`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT `+candyResultColumns+` FROM candy_test_results ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	results := []*service.CandyTestResult{}
	for rows.Next() {
		result, err := scanCandyResult(rows, false)
		if err != nil {
			return nil, 0, err
		}
		// 列表只带汇总，题目和完整响应由详情端点按需读取。
		result.SettingsSnapshot = nil
		results = append(results, result)
	}
	return results, total, rows.Err()
}

func (r *candyTestRepository) GetResult(ctx context.Context, id int64) (*service.CandyTestResult, error) {
	result, err := scanCandyResult(r.db.QueryRowContext(ctx, `SELECT `+candyResultColumns+`,response_text FROM candy_test_results WHERE id=$1`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return result, err
}
