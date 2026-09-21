//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHealthyDynamicMaintenanceMixedFetchAndProbeFailuresPauseAtTen(t *testing.T) {
	for _, emptyBatch := range []bool{false, true} {
		name := "提取错误后已有入口探测失败"
		if emptyBatch {
			name = "空批次后已有入口探测失败"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			svc, _, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 3)
			now := prepareHealthyDynamicRetryTest(svc)
			d := svc.healthyTurnStateDynamic
			var fetches atomic.Int64
			d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
				switch fetches.Add(1) {
				case 1:
					return []string{"http://8.8.8.8:1001", "http://8.8.8.8:1002", "http://8.8.8.8:1003"}, nil
				case 2:
					return []string{"http://8.8.8.8:1004"}, nil
				default:
					if emptyBatch {
						return nil, nil
					}
					return nil, errors.New("测试供应商拒绝提取")
				}
			}
			d.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
				probes.Add(1)
				return &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: model, Transport: transport}, nil
			}
			scanHealthyDynamicRetryTest(t, svc)
			wantProbes, wantFetches := int64(4), int64(8)
			if emptyBatch {
				wantProbes, wantFetches = 3, 9
			}
			require.Equal(t, wantProbes, probes.Load(), "提取失败与已有入口探测失败累计达到阈值后，不能再发新批次")
			require.Equal(t, wantFetches, fetches.Load())
			status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, "backoff", status.Status)
			require.Contains(t, status.Message, "连续 10 次采集失败")
			require.Equal(t, now.Add(15*time.Second), *status.NextRetryAt)
		})
	}
}

func TestHealthyDynamicMaintenanceHealthyProbeResetsEarlierMixedFetchFailure(t *testing.T) {
	svc, pool, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 3)
	prepareHealthyDynamicRetryTest(svc)
	d := svc.healthyTurnStateDynamic
	var fetches atomic.Int64
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		switch fetches.Add(1) {
		case 1:
			return []string{"http://8.8.8.8:1001", "http://8.8.8.8:1002", "http://8.8.8.8:1003"}, nil
		case 2:
			return []string{"http://8.8.8.8:1004"}, nil
		case 3:
			return nil, errors.New("测试供应商暂时不可用")
		default:
			return []string{"http://8.8.8.8:1005", "http://8.8.8.8:1006"}, nil
		}
	}
	d.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		status := "failed"
		if probes.Add(1) > 3 {
			pool.record(model)
			status = "recorded"
		}
		return &OpenAIHealthyTurnStateProbeResult{Status: status, Model: model, Transport: transport}, nil
	}
	scanHealthyDynamicRetryTest(t, svc)
	require.EqualValues(t, 6, probes.Load())
	require.EqualValues(t, 3, fetches.Load(), "健康入口成功后优先复用，不再额外提取")
	require.EqualValues(t, 3, pool.counts()["gpt-6-astra"])
	require.NotContains(t, d.maintenanceRetry, account.ID, "尚未达到十次失败时，后到的健康结果应清零此前提取失败")
}

func TestHealthyDynamicMaintenanceProxyWaitingDoesNotHideFetchFailures(t *testing.T) {
	svc, _, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 3)
	prepareHealthyDynamicRetryTest(svc)
	d := svc.healthyTurnStateDynamic
	const occupiedProxy = "http://8.8.8.8:1004"
	require.True(t, d.reserveHealthyDynamicProxy(occupiedProxy))
	var fetches atomic.Int64
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		switch fetches.Add(1) {
		case 1:
			return []string{"http://8.8.8.8:1001", "http://8.8.8.8:1002", "http://8.8.8.8:1003"}, nil
		case 2:
			return []string{occupiedProxy}, nil
		default:
			return nil, nil
		}
	}
	d.probe = func(_ context.Context, _ *Account, model, transport, _ string) (*OpenAIHealthyTurnStateProbeResult, error) {
		probes.Add(1)
		return &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: model, Transport: transport}, nil
	}
	scanHealthyDynamicRetryTest(t, svc)
	require.EqualValues(t, 3, probes.Load(), "被占用的代理不能重复探测")
	require.EqualValues(t, 9, fetches.Load())
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "backoff", status.Status)
	require.Contains(t, status.Message, "连续 10 次采集失败", "代理争用不能覆盖已发生的七次空批次失败")
}

func TestHealthyDynamicMaintenancePureProxyWaitingDoesNotBackoff(t *testing.T) {
	svc, _, account, _, probes := healthyDynamicPoolService(t, []string{"gpt-6-astra"}, 1)
	prepareHealthyDynamicRetryTest(svc)
	d := svc.healthyTurnStateDynamic
	const occupiedProxy = "http://8.8.8.8:1001"
	require.True(t, d.reserveHealthyDynamicProxy(occupiedProxy))
	d.fetch = func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error) {
		return []string{occupiedProxy}, nil
	}
	scanHealthyDynamicRetryTest(t, svc)
	require.Zero(t, probes.Load())
	require.True(t, d.maintenanceRetry[account.ID].after.IsZero(), "仅有代理争用时不能产生失败退避")
	status, err := svc.HealthyTurnStateMaintenanceStatus(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, "queued", status.Status)
}
