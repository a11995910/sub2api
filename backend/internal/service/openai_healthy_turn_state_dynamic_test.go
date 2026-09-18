//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 测试替身只验证加解密接口和不泄露原文，真实加密由已有 SecretEncryptor 提供。
type healthyDynamicTestCipher struct{}

func (healthyDynamicTestCipher) Encrypt(plain string) (string, error) {
	return "opaque:" + base64.StdEncoding.EncodeToString([]byte(plain)), nil
}
func (healthyDynamicTestCipher) Decrypt(encrypted string) (string, error) {
	encoded, ok := strings.CutPrefix(encrypted, "opaque:")
	if !ok {
		return "", errors.New("测试密文无效")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	return string(data), err
}

func healthyDynamicTestService(t *testing.T, target, attempts int) (*AccountTestService, *stubSettingRepo, *Account) {
	t.Helper()
	repo := newStubSettingRepo()
	svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{}}
	svc.SetHealthyTurnStateDynamicStorage(repo, healthyDynamicTestCipher{})
	account := healthyTurnStateProbeAccount()
	_, err := svc.SaveHealthyTurnStateDynamicConfig(context.Background(), account.ID, HealthyTurnStateDynamicConfigInput{
		APIURL: "https://supplier.example/secret-path?token=private-token", Protocol: "http", TargetCount: target, MaxAttempts: attempts,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		svc.healthyTurnStateDynamic.mu.Lock()
		defer svc.healthyTurnStateDynamic.mu.Unlock()
		for _, run := range svc.healthyTurnStateDynamic.runs {
			run.mu.Lock()
			run.finishLocked("stopped", "测试结束")
			run.mu.Unlock()
		}
	})
	return svc, repo, account
}

func TestHealthyDynamicConfigEncryptedAccountScopedAndRedacted(t *testing.T) {
	svc, repo, account := healthyDynamicTestService(t, 2, 4)
	ctx := context.Background()
	view, err := svc.GetHealthyTurnStateDynamicConfig(ctx, account.ID)
	require.NoError(t, err)
	require.True(t, view.Configured)
	require.Equal(t, "https://supplier.example/••••", view.APIURLMasked)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	for _, secret := range []string{"secret-path", "private-token", "token="} {
		require.NotContains(t, string(encoded), secret)
		require.NotContains(t, repo.values[healthyDynamicConfigKey(account.ID)], secret)
	}
	other, err := svc.GetHealthyTurnStateDynamicConfig(ctx, account.ID+1)
	require.NoError(t, err)
	require.False(t, other.Configured)
	require.Equal(t, 5, other.TargetCount)
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, HealthyTurnStateDynamicConfigInput{Protocol: "socks5h", TargetCount: 3, MaxAttempts: 5})
	require.NoError(t, err)
	stored, err := svc.healthyTurnStateDynamic.loadConfig(ctx, account.ID)
	require.NoError(t, err)
	require.Equal(t, "https://supplier.example/secret-path?token=private-token", stored.APIURL)
	require.Equal(t, "socks5h", stored.Protocol)
	require.NotContains(t, account.Extra, "api_url")
	for _, input := range []HealthyTurnStateDynamicConfigInput{
		{APIURL: "http://supplier.example/private-token", Protocol: "http", TargetCount: 1, MaxAttempts: 1},
		{APIURL: "https://127.0.0.1/private-token", Protocol: "http", TargetCount: 1, MaxAttempts: 1},
		{APIURL: "https://supplier.example/private-token", Protocol: "ftp", TargetCount: 1, MaxAttempts: 1},
		{APIURL: "https://supplier.example/private-token", Protocol: "http", TargetCount: 0, MaxAttempts: 1},
		{APIURL: "https://supplier.example/private-token", Protocol: "http", TargetCount: 101, MaxAttempts: 101},
		{APIURL: "https://supplier.example/private-token", Protocol: "http", TargetCount: 2, MaxAttempts: 1},
		{APIURL: "https://supplier.example/private-token", Protocol: "http", TargetCount: 1, MaxAttempts: 1001},
	} {
		_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, input)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-token")
	}
}

func TestHealthyDynamicRunSerialStepsCountOnlyNewRecords(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 2, 4)
	d := svc.healthyTurnStateDynamic
	fetches, probes := 0, 0
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		fetches++
		return []string{"http://8.8.8.8:80", "http://8.8.8.8:80", "http://8.8.4.4:80", "http://1.1.1.1:80"}, nil
	}
	d.probe = func(_ context.Context, got *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes++
		require.Equal(t, account.ID, got.ID)
		require.Equal(t, "gpt-6-astra", model)
		require.Equal(t, "http", transport)
		require.NotEmpty(t, proxy)
		status := "recorded"
		if probes == 1 {
			status = "already_recorded"
		}
		return &OpenAIHealthyTurnStateProbeResult{Status: status, Model: model, Transport: transport}, nil
	}
	ctx := context.Background()
	run, err := svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	require.Zero(t, fetches)
	require.Zero(t, probes, "Start 只建立运行，不进行网络采集")
	_, err = svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.Error(t, err, "同一账号仅允许一个活跃运行")
	_, err = svc.StepHealthyTurnStateDynamic(ctx, account.ID+1, run.ID)
	require.Error(t, err)
	_, err = svc.StopHealthyTurnStateDynamic(ctx, account.ID+1, run.ID)
	require.Error(t, err)
	for i := range 3 {
		run, err = svc.StepHealthyTurnStateDynamic(ctx, account.ID, run.ID)
		require.NoError(t, err)
		require.Equal(t, i+1, run.Attempts)
		require.Equal(t, i, run.Recorded)
		require.Equal(t, 1, fetches, "同一批剩余入口按步骤消费")
	}
	require.Equal(t, "completed", run.Status)
	run, err = svc.StepHealthyTurnStateDynamic(ctx, account.ID, run.ID)
	require.NoError(t, err)
	require.Equal(t, 3, probes, "目标完成后不再探测")
	encoded, _ := json.Marshal(run)
	require.NotContains(t, string(encoded), "8.8.8.8")
	require.NotContains(t, string(encoded), "private-token")
}

func TestHealthyDynamicRunStopsAtAttemptAndEmptyBatchLimits(t *testing.T) {
	t.Run("尝试上限", func(t *testing.T) {
		svc, _, account := healthyDynamicTestService(t, 1, 2)
		d := svc.healthyTurnStateDynamic
		d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
			return []string{"http://8.8.8.8:80", "http://8.8.4.4:80", "http://1.1.1.1:80"}, nil
		}
		d.probe = func(context.Context, *Account, string, string, string) (*OpenAIHealthyTurnStateProbeResult, error) {
			return &OpenAIHealthyTurnStateProbeResult{Status: "already_recorded"}, nil
		}
		run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
		require.NoError(t, err)
		for range 2 {
			run, err = svc.StepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID)
			require.NoError(t, err)
		}
		require.Equal(t, "completed", run.Status)
		require.Equal(t, 2, run.Attempts)
		require.Zero(t, run.Recorded)
	})
	t.Run("连续无新入口", func(t *testing.T) {
		svc, _, account := healthyDynamicTestService(t, 1, 10)
		d := svc.healthyTurnStateDynamic
		d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
			return []string{"http://8.8.8.8:80"}, nil
		}
		d.probe = func(context.Context, *Account, string, string, string) (*OpenAIHealthyTurnStateProbeResult, error) {
			return &OpenAIHealthyTurnStateProbeResult{Status: "no_header"}, nil
		}
		run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
		require.NoError(t, err)
		for i := range 4 {
			run, err = svc.StepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID)
			require.NoError(t, err)
			require.Equal(t, i+1, run.FetchedBatches)
		}
		require.Equal(t, "failed", run.Status)
		require.Equal(t, 1, run.Attempts)
	})
}

func TestHealthyDynamicRunStopCancellationAndConcurrentStep(t *testing.T) {
	for _, during := range []string{"提取", "探测"} {
		for _, action := range []string{"停止", "请求取消"} {
			t.Run(during+action, func(t *testing.T) {
				svc, _, account := healthyDynamicTestService(t, 1, 2)
				d := svc.healthyTurnStateDynamic
				started := make(chan struct{})
				var probes atomic.Int32
				d.fetch = func(ctx context.Context, _ HealthyTurnStateDynamicConfigInput) ([]string, error) {
					if during == "提取" {
						close(started)
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return []string{"http://8.8.8.8:80"}, nil
				}
				d.probe = func(ctx context.Context, _ *Account, _, _, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
					probes.Add(1)
					close(started)
					<-ctx.Done()
					return nil, ctx.Err()
				}
				run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
				require.NoError(t, err)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan *HealthyTurnStateDynamicRun, 1)
				go func() { view, _ := svc.StepHealthyTurnStateDynamic(ctx, account.ID, run.ID); done <- view }()
				<-started
				_, err = svc.StepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID)
				require.Error(t, err, "并发 step 必须立即拒绝，不能同时消费出口")
				if action == "停止" {
					stopped, err := svc.StopHealthyTurnStateDynamic(context.Background(), account.ID, run.ID)
					require.NoError(t, err)
					require.Equal(t, "stopped", stopped.Status)
				} else {
					cancel()
				}
				select {
				case view := <-done:
					require.Equal(t, "stopped", view.Status)
					require.Zero(t, view.Recorded)
				case <-time.After(time.Second):
					t.Fatal("取消后网络步骤没有及时退出")
				}
				if during == "提取" {
					require.Zero(t, probes.Load())
				}
			})
		}
	}
}

func TestHealthyDynamicRunExpiryCapacityAndSafeErrors(t *testing.T) {
	svc, repo, account := healthyDynamicTestService(t, 1, 1)
	d := svc.healthyTurnStateDynamic
	current := time.Now()
	d.now = func() time.Time { return current }
	run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	current = current.Add(healthyDynamicIdleLimit)
	run, err = svc.StepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID)
	require.NoError(t, err)
	require.Equal(t, "stopped", run.Status)
	for i := range healthyDynamicMaxRuns {
		a := *account
		a.ID = int64(i + 1000)
		repo.values[healthyDynamicConfigKey(a.ID)] = repo.values[healthyDynamicConfigKey(account.ID)]
		_, err = svc.StartHealthyTurnStateDynamic(context.Background(), &a, HealthyTurnStateDynamicStartInput{})
		require.NoError(t, err)
	}
	a := *account
	a.ID = 9999
	repo.values[healthyDynamicConfigKey(a.ID)] = repo.values[healthyDynamicConfigKey(account.ID)]
	_, err = svc.StartHealthyTurnStateDynamic(context.Background(), &a, HealthyTurnStateDynamicStartInput{})
	require.Error(t, err)
	require.Len(t, d.runs, healthyDynamicMaxRuns)
	current = current.Add(healthyDynamicRunLimit)
	for _, cached := range d.runs {
		cached.lastUsed = current
	}
	run, err = svc.StartHealthyTurnStateDynamic(context.Background(), &a, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err, "超过最长运行时间的记录应释放容量")
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return nil, fmt.Errorf("secret provider body token=private-token")
	}
	run, err = svc.StepHealthyTurnStateDynamic(context.Background(), a.ID, run.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", run.Status)
	require.NotContains(t, run.Message, "private-token")
	run, err = svc.StartHealthyTurnStateDynamic(context.Background(), &a, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return nil, newHealthyDynamicPublicError("代理提取接口返回 HTTP 403，请检查接口授权与服务器 IP 白名单")
	}
	run, err = svc.StepHealthyTurnStateDynamic(context.Background(), a.ID, run.ID)
	require.NoError(t, err)
	require.Contains(t, run.Message, "HTTP 403")
}
