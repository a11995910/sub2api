//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthyDynamicPreferredParallelBatchUsesEachCachedProxyOnce(t *testing.T) {
	svc, pool, account, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, 2)
	d := svc.healthyTurnStateDynamic
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	config, err := d.loadConfig(ctx, account.ID)
	require.NoError(t, err)
	d.preferred.complete(healthyDynamicPreferredScope(account.ID, "gpt-6-astra", "http", config), "http://8.8.8.8:80", true, d.clock())
	d.preferred.complete(healthyDynamicPreferredScope(account.ID, "gpt-5.6-sol", "http", config), "http://8.8.4.4:80", true, d.clock())
	run, err := svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	for range 2 {
		started := make(chan string, 4)
		resume := make(chan struct{})
		var active atomic.Int64
		d.probe = func(ctx context.Context, _ *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
			probes.Add(1)
			active.Add(1)
			defer active.Add(-1)
			started <- proxy
			select {
			case <-resume:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			pool.record(model)
			return &OpenAIHealthyTurnStateProbeResult{Status: "recorded", Model: model, Transport: transport}, nil
		}
		done := make(chan error, 1)
		go func() {
			_, err := svc.stepHealthyTurnStateDynamic(ctx, account.ID, run.ID, healthyDynamicProbeConcurrency)
			done <- err
		}()
		seen := make(map[string]bool)
		for range 2 {
			select {
			case proxy := <-started:
				require.False(t, seen[proxy], "同一优选入口不能并发复用")
				seen[proxy] = true
			case <-ctx.Done():
				t.Fatal("不同模型的优选入口未并行启动")
			}
		}
		require.EqualValues(t, 2, active.Load())
		close(resume)
		require.NoError(t, <-done)
	}
	require.Zero(t, fetches.Load(), "存在优选入口时不额外提取供应商代理")
	require.EqualValues(t, 4, probes.Load(), "每个模型按缺口顺序复用，不并行超采")
	require.EqualValues(t, 2, pool.counts()["gpt-6-astra"])
	require.EqualValues(t, 2, pool.counts()["gpt-5.6-sol"])
}

func TestHealthyDynamicPreferredReuseAndFailureFallback(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 3, 6)
	d := svc.healthyTurnStateDynamic
	ctx := context.Background()
	const first = "http://user:private-password@8.8.8.8:80"
	const second = "http://8.8.4.4:80"
	fetches := 0
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		fetches++
		return []string{first, second}, nil
	}
	var used []string
	d.probe = func(_ context.Context, _ *Account, _, _, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		used = append(used, proxy)
		status := "recorded"
		if len(used) == 2 {
			status = "no_header"
		}
		return &OpenAIHealthyTurnStateProbeResult{Status: status}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	for range 4 {
		run, err = svc.StepHealthyTurnStateDynamic(ctx, account.ID, run.ID)
		require.NoError(t, err)
	}
	require.Equal(t, []string{first, first, second, second}, used)
	require.Equal(t, 1, fetches)
	require.Equal(t, 3, run.Recorded)
	require.Equal(t, "completed", run.Status)
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-password")
	require.NotContains(t, string(encoded), "8.8.")
}

func TestHealthyDynamicPreferredAcrossRunsExpiryAndIsolation(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 1, 4)
	d, ctx := svc.healthyTurnStateDynamic, context.Background()
	now := time.Now()
	d.now = func() time.Time { return now }
	fetches := 0
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		fetches++
		return []string{"http://8.8.8.8:80"}, nil
	}
	d.probe = func(context.Context, *Account, string, string, string) (*OpenAIHealthyTurnStateProbeResult, error) {
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded"}, nil
	}
	step := func(a *Account, transport string) {
		t.Helper()
		run, err := svc.StartHealthyTurnStateDynamic(ctx, a, HealthyTurnStateDynamicStartInput{Transport: transport})
		require.NoError(t, err)
		_, err = svc.StepHealthyTurnStateDynamic(ctx, a.ID, run.ID)
		require.NoError(t, err)
		_, err = svc.StopHealthyTurnStateDynamic(ctx, a.ID, run.ID)
		require.NoError(t, err)
	}
	step(account, "http")
	step(account, "http")
	require.Equal(t, 1, fetches, "跨运行先复用成功入口")
	step(account, "websocket")
	require.Equal(t, 2, fetches, "传输方式独立验证")
	other := *account
	other.ID++
	step(&other, "http")
	require.Equal(t, 3, fetches, "账号独立验证")
	now = now.Add(healthyDynamicPreferredTTL)
	step(account, "http")
	require.Equal(t, 4, fetches, "到期重新提取")
}

func TestHealthyDynamicPreferredDuplicateFailureAndNewBatch(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 1, 3)
	d, ctx := svc.healthyTurnStateDynamic, context.Background()
	fetches := 0
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		fetches++
		if fetches == 1 {
			return []string{"http://8.8.8.8:80"}, nil
		}
		return []string{"http://8.8.8.8:80", "http://8.8.4.4:80"}, nil
	}
	var used []string
	d.probe = func(_ context.Context, _ *Account, _, _, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		used = append(used, proxy)
		status := []string{"already_recorded", "failed", "recorded"}[len(used)-1]
		return &OpenAIHealthyTurnStateProbeResult{Status: status}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	for range 3 {
		run, err = svc.StepHealthyTurnStateDynamic(ctx, account.ID, run.ID)
		require.NoError(t, err)
	}
	require.Equal(t, []string{"http://8.8.8.8:80", "http://8.8.8.8:80", "http://8.8.4.4:80"}, used)
	require.Equal(t, 2, fetches)
	require.Equal(t, 1, run.Recorded)
	require.Equal(t, "completed", run.Status)
}

func TestHealthyDynamicPreferredConfigChangeCancelsOldResult(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 1, 3)
	d, ctx := svc.healthyTurnStateDynamic, context.Background()
	config, err := d.loadConfig(ctx, account.ID)
	require.NoError(t, err)
	otherKey := healthyDynamicPreferredScope(account.ID+1, "gpt-6-astra", "http", config)
	d.preferred.complete(otherKey, "http://8.8.4.4:80", true, d.clock())
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return []string{"http://8.8.8.8:80"}, nil
	}
	started, resume := make(chan struct{}), make(chan struct{})
	d.probe = func(context.Context, *Account, string, string, string) (*OpenAIHealthyTurnStateProbeResult, error) {
		close(started)
		<-resume
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded"}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = svc.StepHealthyTurnStateDynamic(ctx, account.ID, run.ID)
	}()
	<-started
	_, err = svc.SaveHealthyTurnStateDynamicConfig(ctx, account.ID, HealthyTurnStateDynamicConfigInput{
		APIURL: "https://new.example/private", Protocol: "http", Models: []string{"gpt-6-astra"}, TargetCount: 1, MaxAttempts: 3,
	})
	close(resume)
	require.NoError(t, err)
	<-done
	d.preferred.mu.Lock()
	defer d.preferred.mu.Unlock()
	require.Empty(t, d.preferred.entries, "旧配置探测不能重新填入缓存")
}

func TestHealthyDynamicPreferredScopeAndBoundedMemory(t *testing.T) {
	var cache healthyDynamicPreferredCache
	config := HealthyTurnStateDynamicConfigInput{APIURL: "https://supplier.example/secret", Protocol: "http"}
	now := time.Now()
	key := healthyDynamicPreferredScope(1, "model-a", "http", config)
	cache.complete(key, "http://8.8.8.8:80", true, now)
	require.Empty(t, cache.get(healthyDynamicPreferredScope(1, "model-b", "http", config), now))
	config.APIURL = "https://supplier.example/changed"
	require.Empty(t, cache.get(healthyDynamicPreferredScope(1, "model-a", "http", config), now))
	for i := range healthyDynamicPreferredLimit {
		other := healthyDynamicPreferredScope(int64(i+2), "model-a", "http", config)
		cache.complete(other, "http://8.8.4.4:80", true, now.Add(time.Second))
	}
	require.Len(t, cache.entries, healthyDynamicPreferredLimit)
	require.Empty(t, cache.get(key, now), "容量满时淘汰最早成功的入口")
	cache.get(key, now.Add(healthyDynamicPreferredTTL+time.Second))
	require.Empty(t, cache.entries, "过期入口清理释放内存")
}
