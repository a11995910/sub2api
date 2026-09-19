//go:build unit

package service

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthyDynamicDefaultsAccounts struct {
	AccountRepository
	mu       sync.Mutex
	accounts map[int64]Account
}

func (r *healthyDynamicDefaultsAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	account, ok := r.accounts[id]
	if !ok {
		return nil, ErrAccountNotFound
	}
	account.Extra = maps.Clone(account.Extra)
	return &account, nil
}

func (r *healthyDynamicDefaultsAccounts) ListByPlatform(context.Context, string) ([]Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	accounts := make([]Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if !account.IsActive() {
			continue
		}
		account.Extra = maps.Clone(account.Extra)
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func healthyDynamicDefaultsAccount(id int64, enabled bool) Account {
	account := *healthyTurnStateProbeAccount()
	account.ID, account.Status = id, StatusActive
	account.Extra = map[string]any{openAIHealthyTurnStateReplaceKey: enabled}
	return account
}

func TestHealthyDynamicDefaultModelsPreviewIntersectionAndFreezePerAccount(t *testing.T) {
	_, calls := newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"gpt-5.6-sol"},{"slug":"gpt-5.5"}]}`)
	svc, settings, original := healthyDynamicTestService(t, 7, 50)
	ctx := context.Background()
	_, err := svc.SaveHealthyTurnStateDynamicConfig(ctx, original.ID, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 7, MaxAttempts: 50, Models: []string{"gpt-6-astra", "gpt-5.6-sol"}, Transport: "websocket",
	})
	require.NoError(t, err)
	repo := &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{original.ID: *original}}
	for _, id := range []int64{100, 101, 102} {
		repo.accounts[id] = healthyDynamicDefaultsAccount(id, true)
	}
	svc.accountRepo = repo
	preview, err := svc.GetHealthyTurnStateDynamicConfig(ctx, 100)
	require.NoError(t, err)
	require.True(t, preview.ModelsInherited)
	require.Empty(t, preview.ModelsInheritanceMessage)
	require.Equal(t, []string{"gpt-5.6-sol"}, preview.Models)
	require.Equal(t, 3, preview.TargetCount)
	require.Equal(t, 100, preview.MaxAttempts)
	require.Equal(t, "http", preview.Transport)
	require.Empty(t, settings.values[healthyDynamicConfigKey(100)], "预览不会固定账号选择")
	account, _ := repo.GetByID(ctx, 100)
	inherited, err := svc.inheritHealthyDynamicConfig(ctx, account)
	require.NoError(t, err)
	require.Equal(t, preview.Models, inherited.Models)
	require.EqualValues(t, 1, calls.Load(), "继承复用同账号的上游目录缓存")
	keepDefault := false
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, 100, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 4, MaxAttempts: 100, Models: inherited.Models, UpdateDefaultModels: &keepDefault,
	})
	require.NoError(t, err)
	defaults, err := svc.loadHealthyDynamicLastModels(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, defaults, "继承交集及仅改数量均不能缩窄全局默认")
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, 101, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 3, MaxAttempts: 100, Models: []string{"gpt-5.5"},
	})
	require.NoError(t, err)
	fixed, err := svc.inheritHealthyDynamicConfig(ctx, account)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.6-sol"}, fixed.Models)
	newAccount, _ := repo.GetByID(ctx, 102)
	latest, err := svc.inheritHealthyDynamicConfig(ctx, newAccount)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, latest.Models)
	existing, err := svc.healthyTurnStateDynamic.loadConfig(ctx, original.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, existing.Models)
	require.Equal(t, 7, existing.TargetCount)
	require.Equal(t, "websocket", existing.Transport)
}

func TestHealthyDynamicEnabledImportedAccountStartsWithoutDynamicConfigAPI(t *testing.T) {
	_, calls := newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"gpt-6-astra"}]}`)
	svc, pool, original, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	original.Extra[openAIHealthyTurnStateReplaceKey] = false
	repo := &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{
		original.ID: *original, 100: healthyDynamicDefaultsAccount(100, true), 101: healthyDynamicDefaultsAccount(101, false),
	}}
	svc.accountRepo = repo
	d := svc.healthyTurnStateDynamic
	d.maintenanceActive, d.maintenanceRetry = make(map[int64]bool), make(map[int64]healthyDynamicRetry)
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	require.EqualValues(t, 1, calls.Load(), "关闭开关的账号不请求上游目录")
	require.EqualValues(t, 1, fetches.Load())
	require.EqualValues(t, 3, probes.Load(), "新账号使用默认每模型 3 个，不继承来源账号的数量")
	require.EqualValues(t, 3, pool.counts()["gpt-6-astra"])
	config, err := d.loadConfig(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-astra"}, config.Models)
	require.Equal(t, 3, config.TargetCount)
	closed, err := d.loadConfig(context.Background(), 101)
	require.NoError(t, err)
	require.Empty(t, closed.Models)
	svc.scanHealthyDynamicMaintenance(context.Background())
	d.maintenanceWorkers.Wait()
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 1, fetches.Load(), "满池不继续采集")
}

func TestHealthyDynamicDefaultModelsUnavailableBackoffAndRecovery(t *testing.T) {
	for _, mode := range []string{"上游失败", "无模型交集"} {
		t.Run(mode, func(t *testing.T) {
			var recovered atomic.Bool
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !recovered.Load() && mode == "上游失败" {
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"error":"供应商敏感正文"}`))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				model := "gpt-6-astra"
				if !recovered.Load() {
					model = "gpt-5.5"
				}
				_, _ = fmt.Fprintf(w, `{"models":[{"slug":%q}]}`, model)
			}))
			t.Cleanup(server.Close)
			before := chatgptCodexModelsURL
			chatgptCodexModelsURL = server.URL
			t.Cleanup(func() { chatgptCodexModelsURL = before })
			svc, _, original, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
			original.Extra[openAIHealthyTurnStateReplaceKey] = false
			svc.accountRepo = &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{original.ID: *original, 100: healthyDynamicDefaultsAccount(100, true)}}
			d, ctx := svc.healthyTurnStateDynamic, context.Background()
			d.maintenanceActive, d.maintenanceRetry = make(map[int64]bool), make(map[int64]healthyDynamicRetry)
			svc.scanHealthyDynamicMaintenance(ctx)
			d.maintenanceWorkers.Wait()
			require.Zero(t, fetches.Load())
			require.Zero(t, probes.Load())
			value, err := d.settings.GetValue(ctx, healthyDynamicConfigKey(100))
			require.NoError(t, err)
			require.Empty(t, value, "失败和空交集不能落库空选择阻止后续继承")
			status, err := svc.HealthyTurnStateMaintenanceStatus(ctx, 100)
			require.NoError(t, err)
			require.Equal(t, "backoff", status.Status)
			require.NotNil(t, status.NextRetryAt)
			require.NotContains(t, status.Message, "供应商敏感正文")
			if mode == "上游失败" {
				require.Contains(t, status.Message, "无法核实")
			} else {
				require.Contains(t, status.Message, "不支持上次选择")
			}
			preview, err := svc.GetHealthyTurnStateDynamicConfig(ctx, 100)
			require.NoError(t, err)
			require.True(t, preview.ModelsInherited)
			require.Empty(t, preview.Models)
			require.Equal(t, status.Message, preview.ModelsInheritanceMessage)
			require.NotContains(t, preview.ModelsInheritanceMessage, "error:")
			beforeCalls := calls.Load()
			svc.scanHealthyDynamicMaintenance(ctx)
			d.maintenanceWorkers.Wait()
			require.Equal(t, beforeCalls, calls.Load(), "退避期间不重复请求上游")
			recovered.Store(true)
			// 模拟目录缓存过期，验证下一轮会实际重新核实。
			svc.openaiGatewayService.openAIModelsCache.mu.Lock()
			clear(svc.openaiGatewayService.openAIModelsCache.entries)
			svc.openaiGatewayService.openAIModelsCache.mu.Unlock()
			d.mu.Lock()
			retry := d.maintenanceRetry[100]
			retry.after = time.Now().Add(-time.Second)
			d.maintenanceRetry[100] = retry
			d.mu.Unlock()
			svc.scanHealthyDynamicMaintenance(ctx)
			d.maintenanceWorkers.Wait()
			require.EqualValues(t, 3, probes.Load())
			require.EqualValues(t, 1, fetches.Load())
			config, err := d.loadConfig(ctx, 100)
			require.NoError(t, err)
			require.Equal(t, []string{"gpt-6-astra"}, config.Models)
		})
	}
}

type healthyDynamicTimedSettings struct {
	*healthyDynamicTestSettingRepo
	updated map[string]time.Time
}

func (r *healthyDynamicTimedSettings) Get(_ context.Context, key string) (*Setting, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.values[key]
	if !ok {
		return nil, ErrSettingNotFound
	}
	return &Setting{Key: key, Value: value, UpdatedAt: r.updated[key]}, nil
}

func TestHealthyDynamicDefaultModelsUpgradeUsesLatestExistingOAuthSettings(t *testing.T) {
	svc, base, _ := healthyDynamicTestService(t, 3, 100)
	delete(base.values, healthyDynamicLastModelsKey)
	repo := &healthyDynamicTimedSettings{healthyDynamicTestSettingRepo: base, updated: map[string]time.Time{}}
	svc.healthyTurnStateDynamic.settings = repo
	accounts := &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{}}
	svc.accountRepo = accounts
	for i := int64(1); i <= 6; i++ {
		account := healthyDynamicDefaultsAccount(i, true)
		if i != 6 {
			accounts.accounts[i] = account
		}
		config := HealthyTurnStateDynamicConfigInput{APIURL: "https://supplier.example/private", Protocol: "http", TargetCount: 3, MaxAttempts: 100, Models: []string{fmt.Sprintf("test-model-%d", i)}, Transport: "http"}
		value, err := svc.healthyTurnStateDynamic.encryptHealthyDynamicConfig(config)
		require.NoError(t, err)
		base.values[healthyDynamicConfigKey(i)] = value
		repo.updated[healthyDynamicConfigKey(i)] = time.Unix(i*100, 0)
	}
	// 时间最近的已删、非 OAuth 和损坏设置均不能成为默认；暂停账号仍可提供上次选择。
	nonOAuth := accounts.accounts[5]
	nonOAuth.Type = AccountTypeAPIKey
	accounts.accounts[5] = nonOAuth
	base.values[healthyDynamicConfigKey(4)] = "坏密文"
	paused := accounts.accounts[3]
	paused.Status = StatusDisabled
	accounts.accounts[3] = paused
	// 相同保存时间以 ID 确定稳定顺序，不能取决于 map 遍历。
	repo.updated[healthyDynamicConfigKey(2)] = repo.updated[healthyDynamicConfigKey(3)]
	models, err := svc.loadHealthyDynamicLastModels(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"test-model-3"}, models)
	base.values[healthyDynamicConfigKey(3)] = "后续变动不应触发重新迁移"
	models, err = svc.loadHealthyDynamicLastModels(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"test-model-3"}, models)
}

func TestHealthyDynamicDefaultModelsConcurrentExplicitSaveWinsInheritance(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-6-astra"},{"slug":"gpt-5.5"}]}`))
	}))
	t.Cleanup(server.Close)
	before := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	t.Cleanup(func() { chatgptCodexModelsURL = before })
	svc, _, original := healthyDynamicTestService(t, 3, 100)
	account := healthyDynamicDefaultsAccount(100, true)
	svc.accountRepo = &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{original.ID: *original, 100: account}}
	ctx := context.Background()
	finished := make(chan error, 1)
	go func() {
		_, err := svc.inheritHealthyDynamicConfig(ctx, &account)
		finished <- err
	}()
	<-started
	// 另一账号先显式保存新默认，证明初始化的上游请求没有占住配置锁。
	_, err := svc.SaveHealthyTurnStateDynamicConfig(ctx, original.ID, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 3, MaxAttempts: 100, Models: []string{"gpt-5.5"},
	})
	require.NoError(t, err)
	close(release)
	require.ErrorContains(t, <-finished, "默认模型选择已更新")
	config, err := svc.healthyTurnStateDynamic.loadConfig(ctx, account.ID)
	require.NoError(t, err)
	require.Empty(t, config.Models, "旧初始化不得覆盖新默认")
	config, err = svc.inheritHealthyDynamicConfig(ctx, &account)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, config.Models)
	defaults, err := svc.loadHealthyDynamicLastModels(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, defaults)
}

func TestHealthyDynamicDefaultModelsConcurrentAccountSaveWinsInheritance(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-6-astra"},{"slug":"gpt-5.5"}]}`))
	}))
	t.Cleanup(server.Close)
	before := chatgptCodexModelsURL
	chatgptCodexModelsURL = server.URL
	t.Cleanup(func() { chatgptCodexModelsURL = before })
	svc, _, original := healthyDynamicTestService(t, 3, 100)
	account := healthyDynamicDefaultsAccount(100, true)
	svc.accountRepo = &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{original.ID: *original, 100: account}}
	ctx := context.Background()
	inherited, saved := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := svc.inheritHealthyDynamicConfig(ctx, &account)
		inherited <- err
	}()
	<-started
	go func() {
		_, err := svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, HealthyTurnStateDynamicConfigInput{
			Protocol: "http", TargetCount: 9, MaxAttempts: 100, Models: []string{"gpt-5.5"}, Transport: "websocket",
		})
		saved <- err
	}()
	unblock()
	require.NoError(t, <-saved)
	require.NoError(t, <-inherited)
	config, err := svc.healthyTurnStateDynamic.loadConfig(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, config.Models)
	require.Equal(t, 9, config.TargetCount)
	require.Equal(t, "websocket", config.Transport)
	defaults, err := svc.loadHealthyDynamicLastModels(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, defaults)
}

func TestHealthyDynamicDefaultModelsUpgradeBeforeAccountOnlySave(t *testing.T) {
	newCodexModelsOAuthCacheServer(t, `{"models":[{"slug":"gpt-6-astra"},{"slug":"gpt-5.5"}]}`)
	svc, settings, original := healthyDynamicTestService(t, 3, 100)
	delete(settings.values, healthyDynamicLastModelsKey)
	repo := &healthyDynamicTimedSettings{healthyDynamicTestSettingRepo: settings, updated: map[string]time.Time{
		healthyDynamicConfigKey(original.ID): time.Now(),
	}}
	svc.healthyTurnStateDynamic.settings = repo
	svc.accountRepo = &healthyDynamicDefaultsAccounts{accounts: map[int64]Account{
		original.ID: *original, 100: healthyDynamicDefaultsAccount(100, true),
	}}
	keepDefault := false
	_, err := svc.SaveHealthyTurnStateDynamicConfig(context.Background(), 100, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 8, MaxAttempts: 100, Models: []string{"gpt-5.5"}, UpdateDefaultModels: &keepDefault,
	})
	require.NoError(t, err)
	defaults, err := svc.loadHealthyDynamicLastModels(context.Background())
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-astra"}, defaults, "不更新默认的保存不能改变升级迁移来源")
	config, err := svc.healthyTurnStateDynamic.loadConfig(context.Background(), 100)
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-5.5"}, config.Models)
	require.Equal(t, 8, config.TargetCount)
}
