//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthyDynamicParallelBatchFillsSingleProxyBatchesWithoutOvercapture(t *testing.T) {
	svc, pool, account, _, _ := healthyDynamicPoolService(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, 2)
	pool.record("gpt-6-astra")
	d := svc.healthyTurnStateDynamic
	var fetches atomic.Int64
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return []string{fmt.Sprintf("http://8.8.8.8:%d", 1000+fetches.Add(1))}, nil
	}
	started := make(chan string, 3)
	release := make(chan struct{})
	d.probe = func(_ context.Context, _ *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		started <- proxy
		<-release
		pool.record(model)
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded", Model: model}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	finished := make(chan *HealthyTurnStateDynamicRun, 1)
	go func() {
		result, stepErr := svc.stepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID, 3)
		if stepErr == nil {
			finished <- result
		}
	}()
	proxies := map[string]bool{}
	for range 3 {
		select {
		case proxy := <-started:
			proxies[proxy] = true
		case <-time.After(time.Second):
			t.Fatal("同账号必须实际同时发起三个探测")
		}
	}
	close(release)
	result := <-finished
	require.Len(t, proxies, 3)
	require.EqualValues(t, 3, fetches.Load())
	require.Equal(t, "completed", result.Status)
	require.Equal(t, 3, result.Attempts)
	require.Equal(t, map[string]int64{"gpt-6-astra": 2, "gpt-5.6-sol": 2}, pool.counts())
}

func TestHealthyDynamicParallelBatchBudgetAndDuplicateResults(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 3, 4)
	d := svc.healthyTurnStateDynamic
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return []string{"http://8.8.8.8:1001", "http://8.8.8.8:1002", "http://8.8.8.8:1003", "http://8.8.8.8:1004"}, nil
	}
	d.probe = func(_ context.Context, _ *Account, model, _, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		return &OpenAIHealthyTurnStateProbeResult{Status: "already_recorded", Model: model}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	run, err = svc.stepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID, 3)
	require.NoError(t, err)
	require.Equal(t, 3, run.Attempts)
	require.Zero(t, run.Recorded)
	run, err = svc.stepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID, 3)
	require.NoError(t, err)
	require.Equal(t, 4, run.Attempts, "最后一批只预留剩余预算")
	require.Zero(t, run.Recorded, "重复健康头不虚增库存")
	require.Equal(t, "completed", run.Status)
}

func TestHealthyDynamicParallelBatchPreservesRecordedResultAfterCancellation(t *testing.T) {
	svc, pool, account, _, _ := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 3)
	d := svc.healthyTurnStateDynamic
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var started atomic.Int64
	d.probe = func(ctx context.Context, _ *Account, model, _, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		if started.Add(1) == 3 {
			cancel()
		}
		<-ctx.Done()
		pool.record(model)
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded", Model: model}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	run, err = svc.stepHealthyTurnStateDynamic(ctx, account.ID, run.ID, 3)
	require.NoError(t, err)
	require.Equal(t, "stopped", run.Status)
	require.Equal(t, 3, run.Recorded, "取消不能抹掉已成功写库的并行结果")
	require.EqualValues(t, 3, pool.counts()["gpt-6-astra"])
}

func TestHealthyDynamicAccountSchedulerRotatesEightSlotsAndReportsQueue(t *testing.T) {
	svc, pool, original, _, _ := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	d := svc.healthyTurnStateDynamic
	config, err := d.loadConfig(context.Background(), original.ID)
	require.NoError(t, err)
	encrypted, err := d.encryptHealthyDynamicConfig(config)
	require.NoError(t, err)
	repo := &healthyDynamicDefaultsAccounts{accounts: make(map[int64]Account)}
	for id := int64(1); id <= 11; id++ {
		account := *original
		account.ID, account.Priority = id, 1
		account.Extra = maps.Clone(account.Extra)
		repo.accounts[id] = account
		require.NoError(t, d.settings.Set(context.Background(), healthyDynamicConfigKey(id), encrypted))
	}
	svc.accountRepo = repo
	d.inventory = func(context.Context, int64) (map[string]int64, error) { return map[string]int64{}, nil }
	d.fetch = func(ctx context.Context, _ HealthyTurnStateDynamicConfigInput) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	d.maintenanceActive, d.maintenanceRetry = make(map[int64]bool), make(map[int64]healthyDynamicRetry)
	ctx, cancel := context.WithCancel(context.Background())
	svc.scanHealthyDynamicMaintenance(ctx)
	d.mu.Lock()
	active := maps.Clone(d.maintenanceActive)
	d.mu.Unlock()
	require.Len(t, active, 8)
	require.NotContains(t, active, int64(9))
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), 9)
	require.NoError(t, err)
	require.Equal(t, "queued", status.Status)
	cancel()
	d.maintenanceWorkers.Wait()
	ctx, cancel = context.WithCancel(context.Background())
	svc.scanHealthyDynamicMaintenance(ctx)
	d.mu.Lock()
	active = maps.Clone(d.maintenanceActive)
	d.mu.Unlock()
	for _, id := range []int64{9, 10, 11} {
		require.Contains(t, active, id, "未获调度的账号下一轮必须优先")
	}
	cancel()
	d.maintenanceWorkers.Wait()
	require.Empty(t, pool.counts())
}

func TestHealthyDynamicMaintenanceYieldsAfterFifteenProbes(t *testing.T) {
	svc, pool, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 30)
	filled, attempted := svc.maintainHealthyDynamicAccount(context.Background(), account.ID)
	require.False(t, filled)
	require.True(t, attempted)
	require.EqualValues(t, 15, probes.Load(), "未补满也必须释放采集槽给其他账号")
	require.EqualValues(t, 15, pool.counts()["gpt-6-astra"])
}

func TestHealthyDynamicParallelAliasesReserveOneActualModelDeficit(t *testing.T) {
	_, _, account := healthyDynamicTestService(t, 2, 10)
	account.Credentials["model_mapping"] = map[string]any{"alias-a": "gpt-6-astra", "alias-b": "gpt-6-astra"}
	models, err := healthyDynamicModels(account, []string{"alias-a", "alias-b"}, "")
	require.NoError(t, err)
	reserved := reserveHealthyDynamicModels(models, map[string]int64{"gpt-6-astra": 1}, 2, 3)
	require.Len(t, reserved, 1, "两个别名不能为同一真实模型多预留库存位置")
}

func TestHealthyDynamicMaintenanceStatusReportsInvalidModelAndInventoryError(t *testing.T) {
	svc, _, account, _, _ := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 3)
	d := svc.healthyTurnStateDynamic
	d.inventory = func(context.Context, int64) (map[string]int64, error) {
		return nil, errors.New("测试库存读取错误")
	}
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "error", status.Status)
	d.inventory = func(context.Context, int64) (map[string]int64, error) { return map[string]int64{}, nil }
	repo := svc.accountRepo.(*healthyDynamicAccountRepo)
	repo.account.Credentials = maps.Clone(repo.account.Credentials)
	repo.account.Credentials["model_mapping"] = map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"}
	status, err = svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "error", status.Status)
	require.Contains(t, status.Message, "模型")
}
