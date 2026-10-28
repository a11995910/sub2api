package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func protectionWaitFixture(t *testing.T, rpm int) (*AccountTimeRotationService, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	mr.SetTime(time.Now())
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return protectionTestService(t, rdb, rpm), mr, rdb
}

func waitProtectionQueue(t *testing.T, rdb *redis.Client, count int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		return rdb.ZCard(context.Background(), "account_request_limit:{1}:waiters").Val() == count
	}, time.Second, time.Millisecond)
}

func TestAccountProtectionWaitSharedQueueAndRelease(t *testing.T) {
	svc, mr, rdb := protectionWaitFixture(t, 1)
	other := protectionTestService(t, rdb, 1)
	root := protectionTestAccount(1)
	shadow := protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	require.NoError(t, svc.AdmitRequest(context.Background(), root))
	result := make(chan error, 1)
	go func() { result <- other.waitAdmitRequest(context.Background(), shadow, 4*time.Second) }()
	waitProtectionQueue(t, rdb, 1)
	err := svc.WaitAdmitRequest(context.Background(), root)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	code, _, _ := failure.AccountProtectionClientError()
	require.Equal(t, "account_rate_limit_queue_full", code)
	require.False(t, failure.ShouldRetryNextAccount())
	require.EqualValues(t, 1, rdb.ZCard(context.Background(), "account_request_limit:{1}").Val())
	mr.SetTime(time.Now().Add(61 * time.Second))
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("窗口释放后原账号请求未恢复")
	}
	waitProtectionQueue(t, rdb, 0)
	// 仍只有一个新准入，另一个实例不能利用旧本地状态再次发送。
	require.EqualValues(t, 1, rdb.ZCard(context.Background(), "account_request_limit:{1}").Val())
	require.Error(t, svc.AdmitRequest(context.Background(), root))
}

func TestAccountProtectionWaitCancellationAndTimeout(t *testing.T) {
	svc, _, rdb := protectionWaitFixture(t, 1)
	account := protectionTestAccount(1)
	require.NoError(t, svc.AdmitRequest(context.Background(), account))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- svc.WaitAdmitRequest(ctx, account) }()
	waitProtectionQueue(t, rdb, 1)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	waitProtectionQueue(t, rdb, 0)
	err := svc.waitAdmitRequest(context.Background(), account, 20*time.Millisecond)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	code, _, _ := failure.AccountProtectionClientError()
	require.Equal(t, "account_rate_limit_wait_timeout", code)
	require.False(t, failure.ShouldRetryNextAccount())
	require.False(t, failure.ShouldReportAccountScheduleFailure())
	waitProtectionQueue(t, rdb, 0)
	require.EqualValues(t, 1, rdb.ZCard(context.Background(), "account_request_limit:{1}").Val())
}

func TestAccountProtectionWaitRechecksConfiguration(t *testing.T) {
	for _, mode := range []string{"休息", "移除限速", "过期配置", "存储故障"} {
		t.Run(mode, func(t *testing.T) {
			svc, mr, rdb := protectionWaitFixture(t, 1)
			account := protectionTestAccount(1)
			require.NoError(t, svc.AdmitRequest(context.Background(), account))
			done := make(chan error, 1)
			go func() { done <- svc.waitAdmitRequest(context.Background(), account, 5*time.Second) }()
			waitProtectionQueue(t, rdb, 1)
			cfg := cloneAccountTimeRotationConfig(svc.snapshot)
			cfg.Revision++
			switch mode {
			case "休息":
				cfg.StrictRest = true
				now := time.Now().In(accountRotationLocation)
				cfg.Slots[0] = AccountRotationSlot{Start: now.Add(time.Hour).Format("15:04"), End: now.Add(2 * time.Hour).Format("15:04"), AccountIDs: []int64{1}, ActivePriority: 1, InactivePriority: 50}
				svc.publish(cfg, time.Now())
			case "移除限速":
				cfg.RequestLimits = nil
				svc.publish(cfg, time.Now())
			case "过期配置":
				svc.publish(cfg, time.Now().Add(-time.Minute))
			case "存储故障":
				mr.SetError("模拟存储不可用")
			}
			select {
			case err := <-done:
				if mode == "移除限速" {
					require.NoError(t, err)
				} else {
					require.True(t, isAccountProtectionError(err))
				}
			case <-time.After(4 * time.Second):
				t.Fatal("等待没有及时响应配置或存储变化")
			}
			mr.SetError("")
			// 存储故障时清理可能失败，租约到期仍会自动释放。
			if mode == "存储故障" {
				mr.FastForward(91 * time.Second)
			}
			waitProtectionQueue(t, rdb, 0)
		})
	}
}

func TestAccountProtectionHTTPWaitKeepsAccount(t *testing.T) {
	svc, mr, rdb := protectionWaitFixture(t, 1)
	account := protectionTestAccount(1)
	inner := &protectionHTTP{}
	client := NewProtectedHTTPUpstream(inner, svc, protectionAccountRepo{account: account})
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", nil)
	resp, err := client.Do(req, "", 1, 1)
	require.NoError(t, err)
	_ = resp.Body.Close()
	done := make(chan error, 1)
	go func() {
		resp, err := client.Do(req, "", 1, 1)
		if resp != nil {
			_ = resp.Body.Close()
		}
		done <- err
	}()
	waitProtectionQueue(t, rdb, 1)
	mr.SetTime(time.Now().Add(61 * time.Second))
	require.NoError(t, <-done)
	require.Equal(t, 2, inner.calls)
}

func TestAccountProtectionStickyAndNewSessionScheduling(t *testing.T) {
	for _, mode := range []string{"基础", "批量", "高级", "加权"} {
		t.Run(mode, func(t *testing.T) {
			rotation, _, rdb := protectionWaitFixture(t, 10)
			_, accounts, _, _ := smartSchedulingFixture()
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = mode == "批量"
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}), cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
			ctx := context.Background()
			for i := 0; i < 10; i++ {
				require.NoError(t, rotation.AdmitRequest(ctx, &accounts[0]))
			}
			require.Error(t, rotation.AdmitRequest(ctx, &accounts[0]))
			require.NoError(t, svc.setStickySessionAccountID(ctx, nil, "固定会话", 1, time.Hour))
			for _, session := range []string{"固定会话", "新会话"} {
				var result *AccountSelectionResult
				var err error
				if mode == "高级" || mode == "加权" {
					scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
					result, _, err = scheduler.Select(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: session, RequestedModel: "gpt-5.1", StickyWeighted: mode == "加权"})
				} else {
					result, err = svc.SelectAccountWithLoadAwareness(ctx, nil, session, "gpt-5.1", nil)
				}
				require.NoError(t, err)
				require.NotNil(t, result)
				if session == "固定会话" {
					require.EqualValues(t, 1, result.Account.ID)
				} else {
					require.NotEqualValues(t, 1, result.Account.ID)
				}
				if result.ReleaseFunc != nil {
					result.ReleaseFunc()
				}
			}
			// 选号没有领取任何新额度。
			require.EqualValues(t, 10, rdb.ZCard(ctx, "account_request_limit:{1}").Val())
		})
	}
}

func TestAccountProtectionNewSessionReserveIncludesShadow(t *testing.T) {
	svc, _, _ := protectionWaitFixture(t, 10)
	root := protectionTestAccount(1)
	shadow := protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	for i := 0; i < 8; i++ {
		require.True(t, svc.allowsNewSession(context.Background(), shadow))
		require.NoError(t, svc.AdmitRequest(context.Background(), root))
	}
	require.False(t, svc.allowsNewSession(context.Background(), shadow))
	// 预留容量仍可供老会话使用，但不能突破 10 次上限。
	for i := 0; i < 2; i++ {
		require.NoError(t, svc.WaitAdmitRequest(context.Background(), shadow))
	}
	require.Error(t, svc.AdmitRequest(context.Background(), root))
}

func TestAccountProtectionWaitDoesNotDegradeHealthyAccount(t *testing.T) {
	rotation := NewAccountTimeRotationService(nil)
	cfg := smartRotationTestConfig(1)
	rotation.publish(cfg, time.Now())
	svc := &OpenAIGatewayService{accountTimeRotation: rotation}
	account := smartRotationTestAccount(1)
	ttft := 61000
	result := &OpenAIForwardResult{FirstTokenMs: &ttft, Duration: 62 * time.Second, LocalAdmissionWait: time.Minute}
	for i := 0; i < 3; i++ {
		svc.ReportOpenAIAccountForwardResult(account, "gpt-5.1", true, result)
	}
	state := rotation.HealthPlan(cfg, []*Account{account}, time.Now()).Accounts[0]
	require.Equal(t, "normal", state.Role)
	require.EqualValues(t, 1000, *state.LastTTFTMs)
	// 健康采样修正不修改用户侧原始耗时。
	require.Equal(t, 61000, *result.FirstTokenMs)
	require.Equal(t, 62*time.Second, result.Duration)
}

func TestAccountProtectionPreviousResponseKeepsAccountAtLimit(t *testing.T) {
	rotation, _, _ := protectionWaitFixture(t, 1)
	_, accounts, _, _ := smartSchedulingFixture()
	accounts[0].Extra = map[string]any{"responses_websockets_v2_enabled": true}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	svc := &OpenAIGatewayService{
		cfg: newOpenAIWSV2TestConfig(), accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
		cache: cache, openaiWSStateStore: store, accountTimeRotation: rotation,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	ctx := context.Background()
	require.NoError(t, store.BindResponseAccount(ctx, 0, "resp_protected", 1, time.Hour))
	require.NoError(t, rotation.AdmitRequest(ctx, &accounts[0]))
	require.Error(t, rotation.AdmitRequest(ctx, &accounts[0]))
	for _, weighted := range []bool{false, true} {
		result, decision, err := newDefaultOpenAIAccountScheduler(svc, nil).Select(ctx, OpenAIAccountScheduleRequest{
			Platform: PlatformOpenAI, PreviousResponseID: "resp_protected", PreviousResponseCanMove: true,
			RequestedModel: "gpt-5.1", StickyWeighted: weighted,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.EqualValues(t, 1, result.Account.ID)
		require.True(t, decision.StickyPreviousHit)
		if result.ReleaseFunc != nil {
			result.ReleaseFunc()
		}
	}
}
