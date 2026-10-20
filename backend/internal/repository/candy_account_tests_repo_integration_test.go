//go:build integration || pelicanpostgres

package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type candyFixtures struct {
	accounts []int64
	groups   []int64
}

func createCandyFixtures(t *testing.T) candyFixtures {
	t.Helper()
	_, err := integrationDB.Exec(`DELETE FROM candy_test_settings`)
	require.NoError(t, err)
	f := candyFixtures{}
	for i, value := range [][3]string{{"openai", "oauth", "active"}, {"openai", "oauth", "disabled"}, {"openai", "apikey", "active"}, {"anthropic", "oauth", "active"}, {"openai", "oauth", "active"}, {"openai", "oauth", "error"}} {
		var id int64
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,status,schedulable)
 VALUES ($1,$2,$3,$4,$5) RETURNING id`, fmt.Sprintf("糖果测试账号%d", i), value[0], value[1], value[2], i != 1).Scan(&id))
		f.accounts = append(f.accounts, id)
	}
	_, err = integrationDB.Exec(`UPDATE accounts SET deleted_at=NOW() WHERE id=$1`, f.accounts[4])
	require.NoError(t, err)
	var shadow int64
	require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type,parent_account_id,quota_dimension)
 VALUES ('糖果测试影子','openai','oauth',$1,'spark') RETURNING id`, f.accounts[0]).Scan(&shadow))
	f.accounts = append(f.accounts, shadow)
	for i := range 4 {
		var id int64
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO groups(name,platform) VALUES ($1,'openai') RETURNING id`, fmt.Sprintf("糖果测试分组%d-%d", time.Now().UnixNano(), i)).Scan(&id))
		f.groups = append(f.groups, id)
	}
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DELETE FROM scheduler_outbox WHERE account_id=ANY($1)`, pq.Array(f.accounts))
		_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=$1`, shadow)
		_, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=ANY($1)`, pq.Array(f.accounts))
		_, _ = integrationDB.Exec(`DELETE FROM groups WHERE id=ANY($1)`, pq.Array(f.groups))
		_, _ = integrationDB.Exec(`DELETE FROM candy_test_settings`)
	})
	return f
}

func candyFixtureSettings(f candyFixtures) service.CandyTestSettingsInput {
	s := service.DefaultCandyTestSettings()
	s.AutoGroupEnabled = true
	s.NormalAddGroupIDs = []int64{f.groups[2]}
	s.NormalRemoveGroupIDs = []int64{f.groups[1]}
	s.DegradedAddGroupIDs = []int64{f.groups[1]}
	s.DegradedRemoveGroupIDs = []int64{f.groups[2]}
	return s
}

func candyFixtureClaim(t *testing.T, repo service.CandyTestRepository, accountID int64, runID string) *service.CandyTestResult {
	t.Helper()
	now := time.Now()
	batch, err := repo.Claim(context.Background(), service.CandyTestStart{AccountIDs: []int64{accountID}}, runID, now, now.Add(time.Minute), false)
	require.NoError(t, err)
	require.Len(t, batch.Results, 1)
	return batch.Results[0]
}

func candyAccountGroups(t *testing.T, accountID int64) []int64 {
	t.Helper()
	rows, err := integrationDB.Query(`SELECT group_id FROM account_groups WHERE account_id=$1 ORDER BY group_id`, accountID)
	require.NoError(t, err)
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	return ids
}

func TestCandyRepositoryConcurrentClaimAndEligibility(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.False(t, settings.ScheduleEnabled)
	require.False(t, settings.AutoGroupEnabled)
	require.Equal(t, "21", settings.ExpectedAnswer)
	now := time.Now()
	var wg sync.WaitGroup
	batches := make(chan *service.CandyTestBatch, 2)
	errs := make(chan error, 2)
	start := make(chan struct{})
	for _, run := range []string{"并发一", "并发二"} {
		wg.Add(1)
		go func(run string) {
			defer wg.Done()
			<-start
			batch, err := NewCandyTestRepository(integrationDB).Claim(ctx, service.CandyTestStart{All: true}, run, now, now.Add(time.Minute), false)
			batches <- batch
			errs <- err
		}(run)
	}
	close(start)
	wg.Wait()
	close(batches)
	close(errs)
	busy := 0
	for err := range errs {
		if err != nil {
			require.ErrorIs(t, err, service.ErrCandyTestRunning)
			busy++
		}
	}
	require.Equal(t, 1, busy)
	var winner *service.CandyTestBatch
	for batch := range batches {
		if batch != nil {
			winner = batch
		}
	}
	require.NotNil(t, winner)
	ids := []int64{}
	for _, result := range winner.Results {
		ids = append(ids, result.AccountID)
		require.WithinDuration(t, now.Add(service.CandyTestLeaseDuration(len(winner.Results))), result.RunningUntil, time.Microsecond)
	}
	require.ElementsMatch(t, []int64{f.accounts[0], f.accounts[1], f.accounts[5]}, ids)
	settings, err = repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, winner.RunID, settings.RunningRunID)
	require.NoError(t, repo.Stop(ctx, winner.RunID, now))
	_, err = repo.Claim(ctx, service.CandyTestStart{AccountIDs: []int64{f.accounts[2], f.accounts[3], f.accounts[4], f.accounts[6]}}, "非目标账号", now, now.Add(time.Minute), false)
	require.ErrorIs(t, err, service.ErrCandyTestNoAccounts)
}

func TestCandyRepositorySnapshotAndIncrementalGroups(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	input := candyFixtureSettings(f)
	_, err := repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id,priority) VALUES ($1,$2,7),($1,$3,13)`, f.accounts[0], f.groups[0], f.groups[1])
	require.NoError(t, err)
	job := candyFixtureClaim(t, repo, f.accounts[0], "保留快照")
	input.Prompt = "下一轮的问题"
	input.ExpectedAnswer = "22"
	input.NormalAddGroupIDs = []int64{f.groups[3]}
	updated, err := repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
	require.Equal(t, job.RunID, updated.RunningRunID)
	job.Status, job.Answer, job.ResponseText = "normal", "21", "最终答案：21"
	require.NoError(t, repo.Complete(ctx, job))
	require.Equal(t, []int64{f.groups[0], f.groups[2]}, candyAccountGroups(t, f.accounts[0]))
	var priority int
	require.NoError(t, integrationDB.QueryRow(`SELECT priority FROM account_groups WHERE account_id=$1 AND group_id=$2`, f.accounts[0], f.groups[0]).Scan(&priority))
	require.Equal(t, 7, priority)
	restored, err := NewCandyTestRepository(integrationDB).GetResult(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "21", restored.SettingsSnapshot.ExpectedAnswer)
	require.Equal(t, service.CandyTestDefaultPrompt, restored.SettingsSnapshot.Prompt)
	require.Equal(t, job.ResponseText, restored.ResponseText)
	require.Equal(t, []int64{f.groups[2]}, restored.GroupAction.AddedGroupIDs)
	require.Equal(t, []int64{f.groups[1]}, restored.GroupAction.RemovedGroupIDs)
	var event string
	var affected []int64
	require.NoError(t, integrationDB.QueryRow(`SELECT event_type, ARRAY(SELECT jsonb_array_elements_text(payload->'group_ids')::bigint)
 FROM scheduler_outbox WHERE account_id=$1 ORDER BY id DESC LIMIT 1`, f.accounts[0]).Scan(&event, pq.Array(&affected)))
	require.Equal(t, service.SchedulerOutboxEventAccountGroupsChanged, event)
	require.ElementsMatch(t, []int64{f.groups[1], f.groups[2]}, affected)
	list, total, err := repo.ListResults(ctx, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Empty(t, list[0].ResponseText)
	require.Nil(t, list[0].SettingsSnapshot)
	require.NoError(t, repo.Finish(ctx, job.RunID, time.Now()))
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Empty(t, settings.RunningRunID)
	require.NotNil(t, settings.LastRunAt)
	// 第二轮智力下降按新快照反向调组，无关分组依然保留。
	job = candyFixtureClaim(t, repo, f.accounts[0], "智力下降")
	job.Status, job.Answer = "degraded", "22"
	require.NoError(t, repo.Complete(ctx, job))
	require.Equal(t, []int64{f.groups[0], f.groups[1]}, candyAccountGroups(t, f.accounts[0]))
}

func TestCandyRepositoryErrorStoppedAndExpiredCannotChangeGroups(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	input := candyFixtureSettings(f)
	_, err := repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
	job := candyFixtureClaim(t, repo, f.accounts[0], "连接失败")
	job.Status, job.ErrorMessage = "error", "连接中断"
	require.NoError(t, repo.Complete(ctx, job))
	require.NoError(t, repo.Finish(ctx, job.RunID, time.Now()))
	require.Empty(t, candyAccountGroups(t, f.accounts[0]))
	stopped := candyFixtureClaim(t, repo, f.accounts[0], "已停止")
	require.NoError(t, repo.Stop(ctx, stopped.RunID, time.Now()))
	stopped.Status, stopped.Answer = "normal", "21"
	require.NoError(t, repo.Complete(ctx, stopped))
	saved, err := repo.GetResult(ctx, stopped.ID)
	require.NoError(t, err)
	require.Equal(t, "error", saved.Status)
	require.Equal(t, "测试已停止", saved.ErrorMessage)
	expired := candyFixtureClaim(t, repo, f.accounts[0], "已过期")
	_, err = integrationDB.Exec(`UPDATE candy_test_settings SET running_until=NOW()-INTERVAL '1 second'`)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE candy_test_results SET running_until=NOW()-INTERVAL '1 second' WHERE id=$1`, expired.ID)
	require.NoError(t, err)
	expired.Status = "normal"
	require.NoError(t, repo.Complete(ctx, expired))
	require.NoError(t, repo.Expire(ctx, time.Now()))
	next := candyFixtureClaim(t, repo, f.accounts[0], "恢复新批次")
	require.NoError(t, repo.Complete(ctx, expired))
	require.NoError(t, repo.Stop(ctx, expired.RunID, time.Now()))
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, next.RunID, settings.RunningRunID)
	require.Empty(t, candyAccountGroups(t, f.accounts[0]))
	var outboxCount int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, f.accounts[0]).Scan(&outboxCount))
	require.Zero(t, outboxCount)
}

func TestCandyRepositoryInvalidGroupsAndTransactionRollback(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	input := candyFixtureSettings(f)
	_, err := repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
	_, err = integrationDB.Exec(`INSERT INTO account_groups(account_id,group_id) VALUES ($1,$2)`, f.accounts[0], f.groups[1])
	require.NoError(t, err)
	job := candyFixtureClaim(t, repo, f.accounts[0], "无效目标分组")
	_, err = integrationDB.Exec(`UPDATE groups SET platform='anthropic' WHERE id=$1`, f.groups[2])
	require.NoError(t, err)
	job.Status = "normal"
	require.NoError(t, repo.Complete(ctx, job))
	require.NotEmpty(t, job.GroupError)
	require.Equal(t, []int64{f.groups[1]}, candyAccountGroups(t, f.accounts[0]))
	_, err = repo.UpdateSettings(ctx, &input, time.Now())
	require.ErrorIs(t, err, service.ErrGroupNotFound)
	require.NoError(t, repo.Finish(ctx, job.RunID, time.Now()))
	_, err = integrationDB.Exec(`UPDATE groups SET platform='openai' WHERE id=$1`, f.groups[2])
	require.NoError(t, err)
	job = candyFixtureClaim(t, repo, f.accounts[0], "通知失败回滚")
	// 在隔离临时库中故意拒绝该账号的 outbox，验证业务分组与结果一起回滚。
	_, err = integrationDB.Exec(fmt.Sprintf(`CREATE FUNCTION candy_test_reject_outbox() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.account_id=%d THEN RAISE EXCEPTION '糖果测试模拟通知失败'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER candy_test_reject_outbox BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION candy_test_reject_outbox()`, f.accounts[0]))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.Exec(`DROP TRIGGER IF EXISTS candy_test_reject_outbox ON scheduler_outbox; DROP FUNCTION IF EXISTS candy_test_reject_outbox()`)
	})
	job.Status = "normal"
	require.NoError(t, repo.Complete(ctx, job))
	require.Contains(t, job.GroupError, "所有分组变更已撤销")
	require.Equal(t, []int64{f.groups[1]}, candyAccountGroups(t, f.accounts[0]))
	saved, err := repo.GetResult(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "normal", saved.Status)
	require.NotEmpty(t, saved.GroupError)
	var outboxCount int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, f.accounts[0]).Scan(&outboxCount))
	require.Zero(t, outboxCount)
	_, err = integrationDB.Exec(`DROP TRIGGER candy_test_reject_outbox ON scheduler_outbox; DROP FUNCTION candy_test_reject_outbox()`)
	require.NoError(t, err)
	require.NoError(t, repo.Finish(ctx, job.RunID, time.Now()))
	job = candyFixtureClaim(t, repo, f.accounts[0], "通知恢复后重测")
	job.Status = "normal"
	require.NoError(t, repo.Complete(ctx, job))
	require.Equal(t, []int64{f.groups[2]}, candyAccountGroups(t, f.accounts[0]))
}

func TestCandyRepositoryScheduledClaimAndConfigUpdate(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	input := service.DefaultCandyTestSettings()
	input.ScheduleEnabled, input.IntervalHours = true, 6
	// 用两天前的时刻建立已到期计划，完成仍使用真实当前时间。
	now := time.Now().Truncate(time.Microsecond)
	before := now.AddDate(0, 0, -2)
	settings, err := repo.UpdateSettings(ctx, &input, before)
	require.NoError(t, err)
	require.NotNil(t, settings.NextRunAt)
	require.True(t, settings.NextRunAt.Equal(before.Add(6*time.Hour)))
	// 编辑题目不重置计时，修改间隔则从本次保存时间重新计算。
	input.Prompt = "修改后的题目"
	settings, err = repo.UpdateSettings(ctx, &input, before.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, settings.NextRunAt.Equal(before.Add(6*time.Hour)))
	input.IntervalHours = 2
	settings, err = repo.UpdateSettings(ctx, &input, before.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, settings.NextRunAt.Equal(before.Add(3*time.Hour)))
	batch, err := repo.Claim(ctx, service.CandyTestStart{}, "定时测试", now, now.Add(time.Minute), true)
	require.NoError(t, err)
	require.Len(t, batch.Results, 3)
	settings, err = repo.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.NextRunAt.Equal(now.Add(2*time.Hour)))
	duplicate, err := repo.Claim(ctx, service.CandyTestStart{}, "重复定时", now, now.Add(time.Minute), true)
	require.NoError(t, err)
	require.Nil(t, duplicate)
	input.ScheduleEnabled = false
	settings, err = repo.UpdateSettings(ctx, &input, now)
	require.NoError(t, err)
	require.Nil(t, settings.NextRunAt)
	require.Equal(t, batch.RunID, settings.RunningRunID)
	require.NoError(t, repo.Stop(ctx, batch.RunID, now))
	disabled, err := repo.Claim(ctx, service.CandyTestStart{}, "已禁用定时", now, now.Add(time.Minute), true)
	require.NoError(t, err)
	require.Nil(t, disabled)
	manual := candyFixtureClaim(t, repo, f.accounts[0], "禁用后手动测试")
	require.Equal(t, "running", manual.Status)
}

func TestCandyRepositoryLegacyScheduleKeepsNextRun(t *testing.T) {
	createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	_, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	next := time.Now().Add(time.Hour).Truncate(time.Microsecond)
	_, err = integrationDB.Exec(`UPDATE candy_test_settings
 SET config=(config-'interval_hours') || '{"daily_time":"08:00","model_id":"","schedule_enabled":true}'::jsonb,
 next_run_at=$1 WHERE id=1`, next)
	require.NoError(t, err)
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 24, settings.IntervalHours)
	require.Equal(t, "gpt-6-astra", settings.ModelID)
	require.True(t, settings.NextRunAt.Equal(next))
	notDue, err := repo.Claim(ctx, service.CandyTestStart{}, "尚未到期", next.Add(-time.Minute), next.Add(time.Minute), true)
	require.NoError(t, err)
	require.Nil(t, notDue)
	batch, err := repo.Claim(ctx, service.CandyTestStart{}, "旧计划衔接", next, next.Add(time.Minute), true)
	require.NoError(t, err)
	require.Len(t, batch.Results, 3)
	settings, err = repo.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.NextRunAt.Equal(next.Add(24*time.Hour)))
}

func TestCandyRepositoryNoGroupChangeWhenDisabled(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	input := candyFixtureSettings(f)
	input.AutoGroupEnabled = false
	_, err := repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
	job := candyFixtureClaim(t, repo, f.accounts[0], "关闭自动调组")
	job.Status = "normal"
	require.NoError(t, repo.Complete(ctx, job))
	require.Empty(t, candyAccountGroups(t, f.accounts[0]))
	require.Nil(t, job.GroupAction)
	// 保留旧分组选择时也可以关闭功能，不会被已失效分组阻挡。
	_, err = integrationDB.Exec(`UPDATE groups SET deleted_at=NOW() WHERE id=$1`, f.groups[2])
	require.NoError(t, err)
	_, err = repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
}

func TestCandyRepositoryLeaseExpiryWhileWaitingForGroupLock(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	input := candyFixtureSettings(f)
	_, err := repo.UpdateSettings(ctx, &input, time.Now())
	require.NoError(t, err)
	job := candyFixtureClaim(t, repo, f.accounts[0], "等待锁期间过期")
	leaseEnd := time.Now().Add(time.Second)
	_, err = integrationDB.Exec(`UPDATE candy_test_results SET running_until=$2 WHERE id=$1`, job.ID, leaseEnd)
	require.NoError(t, err)
	_, err = integrationDB.Exec(`UPDATE candy_test_settings SET running_until=$1`, leaseEnd)
	require.NoError(t, err)
	blocker, err := integrationDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback()
	_, err = blocker.Exec(`UPDATE groups SET name=name WHERE id=$1`, f.groups[1])
	require.NoError(t, err)
	completed := make(chan error, 1)
	job.Status = "normal"
	go func() { completed <- repo.Complete(ctx, job) }()
	require.Eventually(t, func() bool {
		var waiting bool
		err := integrationDB.QueryRow(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM groups WHERE id=ANY%')`).Scan(&waiting)
		return err == nil && waiting
	}, 2*time.Second, 10*time.Millisecond, "完成事务应实际等待分组锁")
	require.Eventually(t, func() bool { return time.Now().After(leaseEnd) }, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, blocker.Commit())
	require.NoError(t, <-completed)
	require.Empty(t, candyAccountGroups(t, f.accounts[0]))
	saved, err := repo.GetResult(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "running", saved.Status)
	var outboxCount int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM scheduler_outbox WHERE account_id=$1`, f.accounts[0]).Scan(&outboxCount))
	require.Zero(t, outboxCount)
	require.NoError(t, repo.Expire(ctx, time.Now()))
	saved, err = repo.GetResult(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "error", saved.Status)
}

func TestCandyRepositoryEmptyScheduledBatchAndHistoryRetention(t *testing.T) {
	f := createCandyFixtures(t)
	ctx := context.Background()
	repo := NewCandyTestRepository(integrationDB)
	old := candyFixtureClaim(t, repo, f.accounts[0], "超期历史")
	old.Status = "normal"
	require.NoError(t, repo.Complete(ctx, old))
	require.NoError(t, repo.Finish(ctx, old.RunID, time.Now()))
	running := candyFixtureClaim(t, repo, f.accounts[0], "运行历史保留")
	_, err := integrationDB.Exec(`UPDATE candy_test_results SET started_at=NOW()-INTERVAL '31 days' WHERE id=ANY($1)`, pq.Array([]int64{old.ID, running.ID}))
	require.NoError(t, err)
	require.NoError(t, repo.Expire(ctx, time.Now()))
	deleted, err := repo.GetResult(ctx, old.ID)
	require.NoError(t, err)
	require.Nil(t, deleted)
	preserved, err := repo.GetResult(ctx, running.ID)
	require.NoError(t, err)
	require.NotNil(t, preserved)
	require.NoError(t, repo.Stop(ctx, running.RunID, time.Now()))
	_, err = integrationDB.Exec(`UPDATE accounts SET type='apikey' WHERE id=ANY($1)`, pq.Array(f.accounts))
	require.NoError(t, err)
	input := service.DefaultCandyTestSettings()
	input.ScheduleEnabled = true
	now := time.Now()
	_, err = repo.UpdateSettings(ctx, &input, now.AddDate(0, 0, -2))
	require.NoError(t, err)
	_, err = repo.Claim(ctx, service.CandyTestStart{}, "空定时批次", now, now.Add(time.Minute), true)
	require.ErrorIs(t, err, service.ErrCandyTestNoAccounts)
	settings, err := repo.GetSettings(ctx)
	require.NoError(t, err)
	require.True(t, settings.NextRunAt.After(now))
	require.Empty(t, settings.RunningRunID)
	require.Nil(t, settings.RunningUntil)
}
