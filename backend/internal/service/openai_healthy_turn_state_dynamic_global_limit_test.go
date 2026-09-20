//go:build unit

package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthyDynamicGlobalProbeLimitAcrossAccounts(t *testing.T) {
	svc, _, _ := healthyDynamicTestService(t, 1, 100)
	d := svc.healthyTurnStateDynamic
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan int64, 30)
	var active, peak atomic.Int64
	d.probe = func(ctx context.Context, account *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
		}
		started <- account.ID
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var workers sync.WaitGroup
	for i := range 30 {
		account := &Account{ID: int64(i/3 + 1)}
		proxy := fmt.Sprintf("http://8.8.8.8:%d", 12000+i)
		require.True(t, d.reserveHealthyDynamicProxy(proxy))
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, _ = svc.probeHealthyDynamicLimited(ctx, account, "gpt-6-astra", "http", proxy)
		}()
	}
	for range 24 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("跨账号探测未能并行启动至全局上限")
		}
	}
	select {
	case <-started:
		t.Fatal("全局已占满 24 路时仍启动了额外探测")
	case <-time.After(100 * time.Millisecond):
	}
	require.EqualValues(t, 24, peak.Load())
	cancel()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("取消后仍有探测或全局槽等待未退出")
	}
	require.Zero(t, active.Load())
	d.probeMu.Lock()
	remainingSlots, remainingProxies := len(d.probeSlots), len(d.activeProxies)
	d.probeMu.Unlock()
	require.Zero(t, remainingSlots, "取消不能泄漏全局并发槽")
	require.Zero(t, remainingProxies, "取消不能泄漏临时代理占用")
}

func TestHealthyDynamicGlobalProbeSlotWaitCancellation(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 1, 100)
	d := svc.healthyTurnStateDynamic
	d.probeSlots = make(chan struct{}, 24)
	for range 24 {
		d.probeSlots <- struct{}{}
	}
	var probes atomic.Int64
	d.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes.Add(1)
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded", Model: model, Transport: transport}, nil
	}
	const proxy = "http://8.8.4.4:12345"
	require.True(t, d.reserveHealthyDynamicProxy(proxy))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		close(entered)
		_, err := svc.probeHealthyDynamicLimited(ctx, account, "gpt-6-astra", "http", proxy)
		finished <- err
	}()
	<-entered
	select {
	case <-finished:
		t.Fatal("全局槽占满时应等待空位")
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-finished:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("等待全局槽的任务未及时响应取消")
	}
	require.Zero(t, probes.Load(), "等待时取消不能发起上游探测")
	require.Len(t, d.probeSlots, 24, "未取得槽位的任务不能释放其他任务的槽位")
	d.probeMu.Lock()
	remainingProxies := len(d.activeProxies)
	d.probeMu.Unlock()
	require.Zero(t, remainingProxies)
	<-d.probeSlots
	require.True(t, d.reserveHealthyDynamicProxy(proxy), "取消后同一代理应可重新预留")
	result, err := svc.probeHealthyDynamicLimited(context.Background(), account, "gpt-6-astra", "http", proxy)
	require.NoError(t, err)
	require.Equal(t, "recorded", result.Status)
	require.EqualValues(t, 1, probes.Load())
	require.Len(t, d.probeSlots, 23, "完成后应归还刚取得的槽位")
}

func TestHealthyDynamicConcurrentProbeAccountCopiesIsolateModelCaches(t *testing.T) {
	svc, _, account := healthyDynamicTestService(t, 3, 100)
	d := svc.healthyTurnStateDynamic
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return []string{"http://8.8.8.8:12001", "http://8.8.8.8:12002", "http://8.8.8.8:12003"}, nil
	}
	started := make(chan *Account, 3)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	resolved := make(chan string, 3)
	d.probe = func(ctx context.Context, got *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		started <- got
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		// 模型映射与请求头缓存会写入 Account；每路都修改自己的副本并触发缓存重建。
		got.Credentials["model_mapping"] = map[string]any{"probe-alias": model}
		got.Credentials["probe_marker"] = proxy
		got.Extra["probe_marker"] = proxy
		resolved <- got.GetMappedModel("probe-alias")
		return &OpenAIHealthyTurnStateProbeResult{Status: "recorded", Model: model, Transport: transport}, nil
	}
	run, err := svc.StartHealthyTurnStateDynamic(context.Background(), account, HealthyTurnStateDynamicStartInput{})
	require.NoError(t, err)
	session, err := d.findRun(account.ID, run.ID)
	require.NoError(t, err)
	session.mu.Lock()
	sourceAccount := session.account
	session.mu.Unlock()
	type stepResult struct {
		view *HealthyTurnStateDynamicRun
		err  error
	}
	finished := make(chan stepResult, 1)
	go func() {
		view, stepErr := svc.stepHealthyTurnStateDynamic(context.Background(), account.ID, run.ID, 3)
		finished <- stepResult{view, stepErr}
	}()
	copies := make(map[*Account]bool)
	for range 3 {
		select {
		case got := <-started:
			require.NotSame(t, sourceAccount, got)
			require.False(t, copies[got], "并发探测不能复用同一个 Account 指针")
			copies[got] = true
		case <-time.After(2 * time.Second):
			t.Fatal("账号内三路探测未同时启动")
		}
	}
	close(release)
	select {
	case result := <-finished:
		require.NoError(t, result.err)
		require.Equal(t, "completed", result.view.Status)
		require.Equal(t, 3, result.view.Recorded)
	case <-time.After(2 * time.Second):
		t.Fatal("并发模型映射探测未完成")
	}
	for range 3 {
		require.Equal(t, "gpt-6-astra", <-resolved)
	}
	require.NotContains(t, sourceAccount.Credentials, "probe_marker")
	require.NotContains(t, sourceAccount.Extra, "probe_marker")
	require.NotContains(t, account.Credentials, "probe_marker")
	require.NotContains(t, account.Extra, "probe_marker")
}
