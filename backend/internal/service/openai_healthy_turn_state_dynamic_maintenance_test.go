//go:build unit

package service

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthyDynamicPoolRepo struct {
	HealthyTurnStateRepository
	mu     sync.Mutex
	values map[string][]time.Time
	inUse  map[string]int64
	reads  atomic.Int64
}

func (r *healthyDynamicPoolRepo) Stats(context.Context, int64) (*HealthyTurnStateStats, error) {
	r.reads.Add(1)
	r.mu.Lock()
	defer r.mu.Unlock()
	stats := &HealthyTurnStateStats{}
	for model, expirations := range r.values {
		alive := expirations[:0]
		for _, expiration := range expirations {
			if time.Now().Before(expiration) {
				alive = append(alive, expiration)
			}
		}
		r.values[model] = alive
		stats.Models = append(stats.Models, HealthyTurnStateModelStats{Model: model, Available: int64(len(alive)) - r.inUse[model], InUse: r.inUse[model]})
	}
	return stats, nil
}

func (r *healthyDynamicPoolRepo) record(model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values[model] = append(r.values[model], time.Now().Add(time.Hour))
}

func (r *healthyDynamicPoolRepo) counts() map[string]int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	counts := make(map[string]int64)
	for model, values := range r.values {
		counts[model] = int64(len(values))
	}
	return counts
}

type healthyDynamicAccountRepo struct {
	AccountRepository
	mu      sync.Mutex
	account Account
}

func (r *healthyDynamicAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account := r.account
	account.Extra = maps.Clone(r.account.Extra)
	return &account, nil
}
func (r *healthyDynamicAccountRepo) ListByPlatform(ctx context.Context, _ string) ([]Account, error) {
	account, _ := r.GetByID(ctx, r.account.ID)
	return []Account{*account}, nil
}

func healthyDynamicPoolService(t *testing.T, models []string, target int) (*AccountTestService, *healthyDynamicPoolRepo, *Account, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	svc, _, account := healthyDynamicTestService(t, target, 100)
	_, err := svc.SaveHealthyTurnStateDynamicConfig(context.Background(), account.ID, HealthyTurnStateDynamicConfigInput{Protocol: "http", TargetCount: target, MaxAttempts: 100, Models: models})
	require.NoError(t, err)
	account.Status = StatusActive
	account.Extra[openAIHealthyTurnStateReplaceKey] = true
	svc.accountRepo = &healthyDynamicAccountRepo{account: *account}
	pool := &healthyDynamicPoolRepo{values: make(map[string][]time.Time), inUse: make(map[string]int64)}
	svc.openaiGatewayService.openaiHealthyTurnStates.repo = pool
	fetches, probes := &atomic.Int64{}, &atomic.Int64{}
	svc.healthyTurnStateDynamic.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		fetches.Add(1)
		proxies := make([]string, 20)
		for i := range proxies {
			proxies[i] = fmt.Sprintf("http://8.8.8.8:%d", 1000+i)
		}
		return proxies, nil
	}
	svc.healthyTurnStateDynamic.probe = func(_ context.Context, account *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes.Add(1)
		require.NotEmpty(t, proxy, "采集必须指定临时代理")
		model = account.GetMappedModel(model)
		if account.UsesOpenAICodexProtocol() {
			model = normalizeOpenAIModelForUpstream(account, model)
		}
		pool.record(model)
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded", Model: model, Transport: transport}, nil
	}
	t.Cleanup(svc.StopHealthyTurnStateMaintenance)
	return svc, pool, account, fetches, probes
}

func TestHealthyDynamicPoolFillsEachModelAndStopsAtExistingInventory(t *testing.T) {
	svc, pool, account, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, 2)
	pool.record("gpt-6-astra")
	run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	for run.Status == "running" {
		run, err = svc.StepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID)
		require.NoError(t, err)
	}
	require.Equal(t, map[string]int64{"gpt-6-astra": 2, "gpt-5.6-sol": 2}, pool.counts())
	require.EqualValues(t, 3, probes.Load(), "只填已有库存缺口")
	require.EqualValues(t, 1, fetches.Load())
	full, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	full, err = svc.StepHealthyTurnStateDynamic(context.Background(), account.ID, full.ID)
	require.NoError(t, err)
	require.Equal(t, "completed", full.Status)
	require.Zero(t, full.Attempts)
	require.EqualValues(t, 1, fetches.Load(), "满池重新打开手动入口也不能继续提取")
}

func TestHealthyDynamicPoolCountsLeasesAndReplenishesExpiredOrRejectedValues(t *testing.T) {
	svc, pool, account, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 3)
	for range 3 {
		pool.record("gpt-6-astra")
	}
	pool.inUse["gpt-6-astra"] = 2
	filled, attempted := svc.maintainHealthyDynamicAccount(context.Background(), account.ID)
	require.True(t, filled)
	require.False(t, attempted)
	require.Zero(t, fetches.Load(), "借出中的头仍计入目标库存")
	pool.mu.Lock()
	pool.values["gpt-6-astra"][0] = time.Now().Add(-time.Minute)
	pool.inUse["gpt-6-astra"] = 1
	pool.mu.Unlock()
	filled, attempted = svc.maintainHealthyDynamicAccount(context.Background(), account.ID)
	require.True(t, filled)
	require.True(t, attempted)
	require.EqualValues(t, 1, probes.Load(), "过期只补缺少的一条")
	pool.mu.Lock()
	pool.values["gpt-6-astra"] = pool.values["gpt-6-astra"][1:]
	pool.inUse["gpt-6-astra"] = 0
	pool.mu.Unlock()
	filled, _ = svc.maintainHealthyDynamicAccount(context.Background(), account.ID)
	require.True(t, filled)
	require.EqualValues(t, 2, probes.Load(), "失效删除后再次只补一条")
}

func TestHealthyDynamicMaintenanceRestoresConfigurationAfterRestart(t *testing.T) {
	svc, pool, _, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	svc.StartHealthyTurnStateMaintenance()
	require.Eventually(t, func() bool { return probes.Load() == 1 }, 3*time.Second, 10*time.Millisecond)
	svc.StopHealthyTurnStateMaintenance()
	require.EqualValues(t, 1, fetches.Load())
	reads := pool.reads.Load()
	restarted := &AccountTestService{accountRepo: svc.accountRepo, openaiGatewayService: svc.openaiGatewayService}
	restarted.SetHealthyTurnStateDynamicStorage(svc.healthyTurnStateDynamic.settings, healthyDynamicTestCipher{})
	restarted.healthyTurnStateDynamic.fetch = svc.healthyTurnStateDynamic.fetch
	restarted.healthyTurnStateDynamic.probe = svc.healthyTurnStateDynamic.probe
	restarted.StartHealthyTurnStateMaintenance()
	t.Cleanup(restarted.StopHealthyTurnStateMaintenance)
	require.Eventually(t, func() bool { return pool.reads.Load() > reads }, time.Second, 10*time.Millisecond)
	restarted.StopHealthyTurnStateMaintenance()
	require.EqualValues(t, 1, fetches.Load(), "重启按持久化库存检查，满池不额外采集")
}

func TestHealthyDynamicMaintenanceCancelsInFlightFetchOnShutdown(t *testing.T) {
	svc, _, _, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	started := make(chan struct{})
	canceled := make(chan struct{})
	svc.healthyTurnStateDynamic.fetch = func(ctx context.Context, _ HealthyTurnStateDynamicConfigInput) ([]string, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	svc.StartHealthyTurnStateMaintenance()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("后台没有开始补位")
	}
	stopped := make(chan struct{})
	go func() { svc.StopHealthyTurnStateMaintenance(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("关闭未及时取消提取")
	}
	select {
	case <-canceled:
	default:
		t.Fatal("提取上下文未取消")
	}
	require.Zero(t, probes.Load())
}

func TestHealthyDynamicModelAliasesShareInventoryAndRetryIsBounded(t *testing.T) {
	account := healthyTurnStateProbeAccount()
	account.Credentials["model_mapping"] = map[string]any{"first": "gpt-6-astra", "second": "gpt-6-astra"}
	models, err := healthyDynamicModels(account, []string{"first", "second"}, "")
	require.NoError(t, err)
	require.Len(t, models, 1)
	_, missing := healthyDynamicMissingModel(models, map[string]int64{"gpt-6-astra": 3}, 3)
	require.False(t, missing)
	mediaAccount := healthyTurnStateProbeAccount()
	mediaAccount.Credentials["model_mapping"] = map[string]any{"text-alias": "gpt-image-2"}
	_, err = healthyDynamicModels(mediaAccount, []string{"text-alias"}, "")
	require.Error(t, err, "编辑映射后不能用文本别名采集图片模型")
	_, err = healthyDynamicModels(mediaAccount, []string{"provider/gpt-image-2"}, "")
	require.Error(t, err, "带提供商前缀的媒体模型也不能采集")
	now := time.Now()
	retry := healthyDynamicRetry{}
	for i := 0; i < 10; i++ {
		retry = nextHealthyDynamicRetry(retry, now)
		require.Equal(t, 15*time.Second, retry.after.Sub(now), "连续轮次失败也保持固定重试间隔")
	}
}

func TestHealthyDynamicMaintenanceOldConfigWithoutDefaultsDoesNotStartAutomatically(t *testing.T) {
	svc, _, account, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	d := svc.healthyTurnStateDynamic
	// 模拟升级前的已加密配置，其余设置继续可读但没有已授权的模型选择。
	legacy, err := d.cipher.Encrypt(`{"api_url":"https://supplier.example/private","protocol":"http","target_count":3,"max_attempts":100}`)
	require.NoError(t, err)
	require.NoError(t, d.settings.Set(context.Background(), healthyDynamicConfigKey(account.ID), legacy))
	empty, err := d.encryptHealthyDynamicConfig([]string{})
	require.NoError(t, err)
	require.NoError(t, d.settings.Set(context.Background(), healthyDynamicLastModelsKey, empty))
	view, err := svc.GetHealthyTurnStateDynamicConfig(context.Background(), account.ID)
	require.NoError(t, err)
	require.Empty(t, view.Models)
	d.maintenanceActive = make(map[int64]bool)
	d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	require.Zero(t, fetches.Load())
	require.Zero(t, probes.Load())
}

func TestHealthyDynamicMaintenanceFailureBackoffAndModelFairness(t *testing.T) {
	t.Parallel()
	svc, pool, account, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, 3)
	d := svc.healthyTurnStateDynamic
	d.maintenanceActive = make(map[int64]bool)
	d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
	d.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes.Add(1)
		status := "failed"
		if model == "gpt-5.6-sol" {
			pool.record(model)
			status = "recorded"
		}
		return &OpenAIHealthyTurnStateProbeResult{Status: status, Model: model, Transport: transport}, nil
	}
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	require.EqualValues(t, 1, fetches.Load())
	firstAttempts := probes.Load()
	require.GreaterOrEqual(t, firstAttempts, int64(8), "补齐另一模型后仍须连续五次失败才暂停")
	require.LessOrEqual(t, firstAttempts, int64(15), "并行批次失败后不能无限占据采集槽")
	require.EqualValues(t, 3, pool.counts()["gpt-5.6-sol"], "失败模型不能阻止同一轮补满其他模型")
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	require.EqualValues(t, 1, fetches.Load(), "退避时间内不重试供应商")
	d.mu.Lock()
	retry := d.maintenanceRetry[account.ID]
	retry.after = time.Now().Add(-time.Second)
	d.maintenanceRetry[account.ID] = retry
	d.mu.Unlock()
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	require.EqualValues(t, 3, pool.counts()["gpt-5.6-sol"], "即使前一模型始终失败，其他模型仍须补满全部目标")
	require.EqualValues(t, firstAttempts+6, probes.Load(), "退避结束后仅剩一个模型三个缺口，第二批达到五次失败且保留已发出的第六个结果")
}

func TestHealthyDynamicMaintenanceDisablingAccountCancelsCurrentFetch(t *testing.T) {
	svc, _, _, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	d := svc.healthyTurnStateDynamic
	d.maintenanceActive = make(map[int64]bool)
	d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
	started := make(chan struct{})
	canceled := make(chan struct{})
	d.fetch = func(ctx context.Context, _ HealthyTurnStateDynamicConfigInput) ([]string, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	svc.scanHealthyDynamicMaintenance(context.Background())
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("后台未开始提取")
	}
	repo := svc.accountRepo.(*healthyDynamicAccountRepo)
	repo.mu.Lock()
	repo.account.Extra[openAIHealthyTurnStateReplaceKey] = false
	repo.mu.Unlock()
	svc.scanHealthyDynamicMaintenance(context.Background())
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("关闭开关后未取消在途采集")
	}
	d.maintenanceWorkers.Wait()
	require.Zero(t, probes.Load())
}

func TestHealthyDynamicMaintenanceStatusShowsSafeFailureAndRetryTime(t *testing.T) {
	t.Parallel()
	svc, _, account, _, _ := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	d := svc.healthyTurnStateDynamic
	d.maintenanceActive = make(map[int64]bool)
	d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return nil, newHealthyDynamicPublicError("代理提取接口返回 HTTP 403，请检查接口授权与服务器 IP 白名单")
	}
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "backoff", status.Status)
	require.Contains(t, status.Message, "HTTP 403")
	require.NotNil(t, status.NextRetryAt)
	require.NotContains(t, status.Message, "private-token")
	require.NotContains(t, status.Message, "supplier.example")
	repo := svc.accountRepo.(*healthyDynamicAccountRepo)
	repo.mu.Lock()
	repo.account.Extra[openAIHealthyTurnStateReplaceKey] = false
	repo.mu.Unlock()
	status, err = svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "disabled", status.Status)
	require.Nil(t, status.NextRetryAt)
}
