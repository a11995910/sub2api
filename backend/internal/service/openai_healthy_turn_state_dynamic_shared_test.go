//go:build unit

package service

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthyDynamicTestSettingRepo struct {
	*stubSettingRepo
	getAllCalls int
	bulkErr     error
}

func (r *healthyDynamicTestSettingRepo) SetMultiple(_ context.Context, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bulkErr != nil {
		return r.bulkErr
	}
	maps.Copy(r.values, values)
	return nil
}

func (r *healthyDynamicTestSettingRepo) GetAll(context.Context) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getAllCalls++
	return maps.Clone(r.values), nil
}

func TestHealthyDynamicSharedProxyKeepsAccountInventoryIndependent(t *testing.T) {
	svc, repo, account := healthyDynamicTestService(t, 2, 4)
	ctx := context.Background()
	otherID := account.ID + 1
	keepShared := false
	_, err := svc.SaveHealthyTurnStateDynamicConfig(ctx, otherID, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 7, MaxAttempts: 12, Models: []string{"gpt-5.6-sol"}, Transport: "websocket", UpdateSharedProxy: &keepShared,
	})
	require.NoError(t, err)
	first, err := svc.healthyTurnStateDynamic.loadConfig(ctx, account.ID)
	require.NoError(t, err)
	other, err := svc.healthyTurnStateDynamic.loadConfig(ctx, otherID)
	require.NoError(t, err)
	require.Equal(t, first.APIURL, other.APIURL)
	require.Equal(t, 2, first.TargetCount)
	require.Equal(t, []string{"gpt-6-astra"}, first.Models)
	require.Equal(t, 7, other.TargetCount)
	require.Equal(t, 12, other.MaxAttempts)
	require.Equal(t, []string{"gpt-5.6-sol"}, other.Models)
	require.Equal(t, "websocket", other.Transport)
	require.NotContains(t, repo.values[healthyDynamicSharedProxyKey], "private-token")
	require.NotContains(t, repo.values[healthyDynamicConfigKey(otherID)], "private-token")
	legacy, err := svc.healthyTurnStateDynamic.cipher.Decrypt(repo.values[healthyDynamicConfigKey(otherID)])
	require.NoError(t, err)
	require.Contains(t, legacy, "private-token", "账号加密副本保留 URL，支持旧版本回滚")
}

func TestHealthyDynamicSharedProxyUpdateCancelsOtherRunAndRejectsStaleProtocol(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 2, 4)
	d, ctx := svc.healthyTurnStateDynamic, context.Background()
	other := *account
	other.ID++
	run, err := svc.StartHealthyTurnStateDynamic(ctx, &other, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	d.maintenanceRetry = map[int64]healthyDynamicRetry{account.ID: {after: time.Now().Add(time.Hour)}, other.ID: {after: time.Now().Add(time.Hour)}}
	started, canceled, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	d.fetch = func(ctx context.Context, config HealthyTurnStateDynamicConfigInput) ([]string, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	}
	go func() {
		defer close(finished)
		_, _ = svc.StepHealthyTurnStateDynamic(ctx, other.ID, run.ID)
	}()
	<-started
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, HealthyTurnStateDynamicConfigInput{
		APIURL: "https://replacement.example/key?secret=new", Protocol: "socks5h", TargetCount: 2, MaxAttempts: 4, Models: []string{"gpt-6-astra"},
	})
	require.NoError(t, err)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("更新全局接口未取消其他账号正在进行的提取")
	}
	<-finished
	require.Empty(t, d.maintenanceRetry)
	keepShared := false
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, other.ID, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 3, MaxAttempts: 5, Models: []string{"gpt-5.6-sol"}, UpdateSharedProxy: &keepShared,
	})
	require.NoError(t, err)
	stored, err := d.loadConfig(ctx, other.ID)
	require.NoError(t, err)
	require.Equal(t, "socks5h", stored.Protocol, "旧弹窗只修改账号设置，不得覆盖共享协议")
	require.Equal(t, "https://replacement.example/key?secret=new", stored.APIURL)
}

func TestHealthyDynamicAccountOnlySavePreservesOtherRun(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 2, 4)
	d, ctx := svc.healthyTurnStateDynamic, context.Background()
	other := *account
	other.ID++
	run, err := svc.StartHealthyTurnStateDynamic(ctx, &other, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	d.maintenanceRetry = map[int64]healthyDynamicRetry{other.ID: {after: time.Now().Add(time.Hour)}}
	keepShared := false
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, HealthyTurnStateDynamicConfigInput{
		Protocol: "http", TargetCount: 3, MaxAttempts: 6, Models: []string{"gpt-6-astra"}, UpdateSharedProxy: &keepShared,
	})
	require.NoError(t, err)
	session, err := d.findRun(other.ID, run.ID)
	require.NoError(t, err)
	session.mu.Lock()
	require.Equal(t, "running", session.view.Status)
	session.mu.Unlock()
	require.Contains(t, d.maintenanceRetry, other.ID)
}

func TestHealthyDynamicSharedProxyAtomicSaveFailure(t *testing.T) {
	svc, repo, account := healthyDynamicTestService(t, 2, 4)
	before := maps.Clone(repo.values)
	run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	repo.bulkErr = errors.New("测试原子写入失败")
	_, err = svc.SaveHealthyTurnStateDynamicConfig(context.Background(), account.ID, HealthyTurnStateDynamicConfigInput{
		APIURL: "https://replacement.example/private", Protocol: "https", TargetCount: 3, MaxAttempts: 6, Models: []string{"gpt-5.6-sol"},
	})
	require.Error(t, err)
	require.Equal(t, before, repo.values, "共享配置和账号配置均不能部分保存")
	session, err := svc.healthyTurnStateDynamic.findRun(account.ID, run.ID)
	require.NoError(t, err)
	session.mu.Lock()
	require.Equal(t, "running", session.view.Status, "保存失败不能取消原有效配置运行")
	session.mu.Unlock()
}

func TestHealthyDynamicLegacySharedProxyMigration(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "唯一地址自动沿用", true: "冲突必须显式选择"}[conflict], func(t *testing.T) {
			svc, repo, account := healthyDynamicTestService(t, 2, 4)
			delete(repo.values, healthyDynamicSharedProxyKey)
			repo.values[healthyDynamicConfigKey(account.ID+1)] = repo.values[healthyDynamicConfigKey(account.ID)]
			if conflict {
				other, err := svc.healthyTurnStateDynamic.encryptHealthyDynamicConfig(HealthyTurnStateDynamicConfigInput{
					APIURL: "https://second.example/private", Protocol: "http", TargetCount: 2, MaxAttempts: 4,
				})
				require.NoError(t, err)
				repo.values[healthyDynamicConfigKey(account.ID+1)] = other
			}
			// 重启后的首次读取触发迁移；尚未配置的新账号也应得到同一结果。
			svc.SetHealthyTurnStateDynamicStorage(repo, healthyDynamicTestCipher{})
			view, err := svc.GetHealthyTurnStateDynamicConfig(context.Background(), account.ID+2)
			if !conflict {
				require.NoError(t, err)
				require.True(t, view.Configured)
				require.Equal(t, "https://supplier.example/••••", view.APIURLMasked)
				require.Empty(t, view.Models)
				require.NotEmpty(t, repo.values[healthyDynamicSharedProxyKey])
			} else {
				require.NoError(t, err)
				require.True(t, view.SharedProxyConflict)
				require.False(t, view.Configured)
				require.Empty(t, view.APIURLMasked)
				_, loadErr := svc.healthyTurnStateDynamic.loadConfig(context.Background(), account.ID)
				require.ErrorContains(t, loadErr, "历史账号使用了不同")
				require.NotContains(t, loadErr.Error(), "second.example")
				original, readErr := svc.GetHealthyTurnStateDynamicConfig(context.Background(), account.ID)
				require.NoError(t, readErr)
				require.Equal(t, 2, original.TargetCount)
				require.Equal(t, 4, original.MaxAttempts)
				require.Equal(t, []string{"gpt-6-astra"}, original.Models)
				require.Empty(t, repo.values[healthyDynamicSharedProxyKey])
			}
			_, _ = svc.GetHealthyTurnStateDynamicConfig(context.Background(), account.ID)
			_, _ = svc.GetHealthyTurnStateDynamicConfig(context.Background(), account.ID+3)
			require.Equal(t, 1, repo.getAllCalls, "不能为每账号或维护轮次反复扫描全部设置")
			if conflict {
				_, err = svc.SaveHealthyTurnStateDynamicConfig(context.Background(), account.ID, HealthyTurnStateDynamicConfigInput{
					APIURL: "https://chosen.example/credential", Protocol: "socks5h", TargetCount: 2, MaxAttempts: 4, Models: []string{"gpt-6-astra"},
				})
				require.NoError(t, err)
				view, err = svc.GetHealthyTurnStateDynamicConfig(context.Background(), account.ID+2)
				require.NoError(t, err)
				require.True(t, view.Configured)
				require.Equal(t, "https://chosen.example/••••", view.APIURLMasked)
			}
		})
	}
}

func TestHealthyDynamicMaintenanceLoadsUpdatedSharedProxy(t *testing.T) {
	svc, _, account, _, _ := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	d := svc.healthyTurnStateDynamic
	// 模拟其他账号保存共享设置；原账号密文仍是旧值，维护必须加载最新共享值。
	encrypted, err := d.encryptHealthyDynamicConfig(healthyDynamicSharedProxy{APIURL: "https://latest.example/secret", Protocol: "socks5h"})
	require.NoError(t, err)
	require.NoError(t, d.settings.Set(context.Background(), healthyDynamicSharedProxyKey, encrypted))
	var got HealthyTurnStateDynamicConfigInput
	d.fetch = func(_ context.Context, config HealthyTurnStateDynamicConfigInput) ([]string, error) {
		got = config
		return []string{"socks5h://8.8.8.8:80"}, nil
	}
	filled, _ := svc.maintainHealthyDynamicAccount(context.Background(), account.ID)
	require.True(t, filled)
	require.Equal(t, "https://latest.example/secret", got.APIURL)
	require.Equal(t, "socks5h", got.Protocol)
	require.Equal(t, []string{"gpt-6-astra"}, got.Models)
}

func TestHealthyDynamicSharedProxyExplicitSaveRepairsCorruptCiphertext(t *testing.T) {
	svc, repo, account := healthyDynamicTestService(t, 2, 4)
	ctx := context.Background()
	repo.values[healthyDynamicSharedProxyKey] = "损坏的密文"
	_, err := svc.GetHealthyTurnStateDynamicConfig(ctx, account.ID)
	require.Error(t, err)
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, HealthyTurnStateDynamicConfigInput{
		APIURL: "https://repaired.example/secret", Protocol: "https", TargetCount: 2, MaxAttempts: 4, Models: []string{"gpt-6-astra"},
	})
	require.NoError(t, err)
	view, err := svc.GetHealthyTurnStateDynamicConfig(ctx, account.ID+1)
	require.NoError(t, err)
	require.True(t, view.Configured)
	require.Equal(t, "https://repaired.example/••••", view.APIURLMasked)
	require.Equal(t, "https", view.Protocol)
}
