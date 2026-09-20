//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func prepareHealthyDynamicRetryTest(svc *AccountTestService) time.Time {
	d := svc.healthyTurnStateDynamic
	d.maintenanceActive = make(map[int64]bool)
	d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
	now := time.Now()
	d.now = func() time.Time { return now }
	return now
}

func scanHealthyDynamicRetryTest(t *testing.T, svc *AccountTestService) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	svc.scanHealthyDynamicMaintenance(ctx)
	svc.healthyTurnStateDynamic.maintenanceWorkers.Wait()
	require.NoError(t, ctx.Err(), "维护必须在本轮达到库存或失败上限后结束")
}

func TestHealthyDynamicMaintenanceNineFailuresThenHealthyContinuesWithoutBackoff(t *testing.T) {
	t.Parallel()
	svc, pool, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	prepareHealthyDynamicRetryTest(svc)
	svc.healthyTurnStateDynamic.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		status := "failed"
		if probes.Add(1) == 10 {
			pool.record(model)
			status = "recorded"
		}
		return &OpenAIHealthyTurnStateProbeResult{Status: status, Model: model, Transport: transport}, nil
	}
	scanHealthyDynamicRetryTest(t, svc)
	require.EqualValues(t, 10, probes.Load())
	require.EqualValues(t, 1, pool.counts()["gpt-6-astra"])
	require.NotContains(t, svc.healthyTurnStateDynamic.maintenanceRetry, account.ID)
}

func TestHealthyDynamicMaintenanceTenFailuresUseDistinctProxiesThenPause(t *testing.T) {
	t.Parallel()
	svc, _, account, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	now := prepareHealthyDynamicRetryTest(svc)
	proxies := make(map[string]bool)
	probeTimes := make([]time.Time, 0, 10)
	svc.healthyTurnStateDynamic.probe = func(_ context.Context, _ *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes.Add(1)
		proxies[proxy] = true
		probeTimes = append(probeTimes, time.Now())
		return &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: model, Transport: transport, Message: "测试代理未通过健康检查"}, nil
	}
	scanHealthyDynamicRetryTest(t, svc)
	require.EqualValues(t, 10, probes.Load())
	require.Len(t, proxies, 10, "失败后必须使用尚未尝试过的代理入口")
	for i := 1; i < len(probeTimes); i++ {
		require.GreaterOrEqual(t, probeTimes[i].Sub(probeTimes[i-1]), 900*time.Millisecond, "切换代理仍须保留一秒节流")
	}
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "backoff", status.Status)
	require.NotNil(t, status.NextRetryAt)
	require.Equal(t, now.Add(15*time.Second), *status.NextRetryAt)
	scanHealthyDynamicRetryTest(t, svc)
	require.EqualValues(t, 10, probes.Load(), "固定退避尚未结束时不提前请求")
	require.EqualValues(t, 1, fetches.Load())
}

func TestHealthyDynamicMaintenanceHealthyResultsResetConsecutiveFailures(t *testing.T) {
	for _, healthyStatus := range []string{"recorded", "already_recorded"} {
		t.Run(healthyStatus, func(t *testing.T) {
			t.Parallel()
			target := 1
			if healthyStatus == "recorded" {
				target = 2
			}
			svc, pool, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, target)
			prepareHealthyDynamicRetryTest(svc)
			svc.healthyTurnStateDynamic.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
				attempt := probes.Add(1)
				status := "failed"
				if attempt == 9 {
					status = healthyStatus
				} else if healthyStatus == "recorded" && attempt == 10 {
					// 两个并行缺口在八次失败后同时恢复。
					status = "recorded"
				} else if attempt == 18 {
					status = "recorded"
				}
				if status == "recorded" {
					pool.record(model)
				}
				return &OpenAIHealthyTurnStateProbeResult{Status: status, Model: model, Transport: transport}, nil
			}
			scanHealthyDynamicRetryTest(t, svc)
			expectedProbes := 18
			if healthyStatus == "recorded" {
				expectedProbes = 10
			}
			require.EqualValues(t, expectedProbes, probes.Load(), "健康响应后重新累计连续失败，重复健康头也应清零")
			require.EqualValues(t, target, pool.counts()["gpt-6-astra"], "重复头不能虚增有效库存")
			require.NotContains(t, svc.healthyTurnStateDynamic.maintenanceRetry, account.ID)
		})
	}
}

func TestHealthyDynamicMaintenanceAttemptLimitTakesPriorityBeforeTenFailures(t *testing.T) {
	t.Parallel()
	svc, _, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	d := svc.healthyTurnStateDynamic
	config, err := d.loadConfig(context.Background(), account.ID)
	require.NoError(t, err)
	config.MaxAttempts = 3
	encrypted, err := d.encryptHealthyDynamicConfig(config)
	require.NoError(t, err)
	require.NoError(t, d.settings.Set(context.Background(), healthyDynamicConfigKey(account.ID), encrypted))
	prepareHealthyDynamicRetryTest(svc)
	svc.healthyTurnStateDynamic.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes.Add(1)
		return &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: model, Transport: transport}, nil
	}
	scanHealthyDynamicRetryTest(t, svc)
	require.EqualValues(t, 3, probes.Load())
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "backoff", status.Status)
	require.Contains(t, status.Message, "尝试上限")
}

func TestHealthyDynamicMaintenanceStopsDuringRetryIntervalWithoutAnotherRequest(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprintf("服务停止=%t", shutdown), func(t *testing.T) {
			t.Parallel()
			svc, _, _, fetches, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
			prepareHealthyDynamicRetryTest(svc)
			firstProbe := make(chan struct{})
			svc.healthyTurnStateDynamic.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
				if probes.Add(1) == 1 {
					close(firstProbe)
				}
				return &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: model, Transport: transport}, nil
			}
			svc.scanHealthyDynamicMaintenance(context.Background())
			select {
			case <-firstProbe:
			case <-time.After(time.Second):
				t.Fatal("后台未开始第一次采集")
			}
			finished := make(chan struct{})
			if shutdown {
				go func() { svc.StopHealthyTurnStateMaintenance(); close(finished) }()
			} else {
				repo := svc.accountRepo.(*healthyDynamicAccountRepo)
				repo.mu.Lock()
				repo.account.Extra[openAIHealthyTurnStateReplaceKey] = false
				repo.mu.Unlock()
				svc.scanHealthyDynamicMaintenance(context.Background())
				go func() { svc.healthyTurnStateDynamic.maintenanceWorkers.Wait(); close(finished) }()
			}
			select {
			case <-finished:
			case <-time.After(750 * time.Millisecond):
				t.Fatal("关闭后应立即取消一秒重试等待")
			}
			require.EqualValues(t, 1, probes.Load())
			require.EqualValues(t, 1, fetches.Load())
		})
	}
}

func TestHealthyDynamicMaintenanceFetchFailuresAreCountedWithoutReusingLastProbe(t *testing.T) {
	cases := []struct {
		name             string
		firstProbe       string
		fetchFailures    int
		emptyBatch       bool
		recover          bool
		expectedFetches  int64
		expectedProbes   int64
		expectedRecorded int64
	}{
		{name: "连续十次提取错误", fetchFailures: 10, expectedFetches: 10},
		{name: "连续十次空批次", fetchFailures: 10, emptyBatch: true, expectedFetches: 10},
		{name: "九次提取错误后恢复", fetchFailures: 9, recover: true, expectedFetches: 10, expectedProbes: 1, expectedRecorded: 1},
		{name: "旧成功结果不能重置后续提取失败", firstProbe: "recorded", fetchFailures: 10, expectedFetches: 11, expectedProbes: 1, expectedRecorded: 1},
		{name: "旧失败结果不能重复累计", firstProbe: "failed", fetchFailures: 3, recover: true, expectedFetches: 5, expectedProbes: 2, expectedRecorded: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			target := 1
			if tc.firstProbe == "recorded" {
				target = 2
			}
			svc, pool, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, target)
			prepareHealthyDynamicRetryTest(svc)
			fetches := &atomic.Int64{}
			svc.healthyTurnStateDynamic.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
				fetch := fetches.Add(1)
				failureStart := int64(1)
				if tc.firstProbe != "" {
					failureStart = 2
				}
				if tc.firstProbe == "recorded" && fetch == 1 {
					// 首批提供两个入口；第二个重复健康头不填充缺口。
					return []string{"http://8.8.8.8:1001", "http://8.8.8.8:1002"}, nil
				}
				if fetch >= failureStart && fetch < failureStart+int64(tc.fetchFailures) {
					if tc.emptyBatch {
						return nil, nil
					}
					return nil, errors.New("测试供应商暂时不可用")
				}
				return []string{fmt.Sprintf("http://8.8.8.8:%d", 1000+fetch)}, nil
			}
			svc.healthyTurnStateDynamic.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
				status := "recorded"
				probeNumber := probes.Add(1)
				if probeNumber == 1 && tc.firstProbe != "" {
					status = tc.firstProbe
				} else if tc.firstProbe == "recorded" && probeNumber == 2 {
					status = "already_recorded"
				}
				if status == "recorded" {
					pool.record(model)
				}
				return &OpenAIHealthyTurnStateProbeResult{Status: status, Model: model, Transport: transport}, nil
			}
			scanHealthyDynamicRetryTest(t, svc)
			require.Equal(t, tc.expectedFetches, fetches.Load())
			expectedProbes := tc.expectedProbes
			if tc.firstProbe == "recorded" {
				expectedProbes++
			}
			require.Equal(t, expectedProbes, probes.Load())
			require.Equal(t, tc.expectedRecorded, pool.counts()["gpt-6-astra"])
			if tc.recover {
				require.NotContains(t, svc.healthyTurnStateDynamic.maintenanceRetry, account.ID)
			} else {
				require.Contains(t, svc.healthyTurnStateDynamic.maintenanceRetry, account.ID)
			}
		})
	}
}
