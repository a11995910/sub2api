package service

import (
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func randomRestTestConfig(s *AccountTimeRotationService, probability, maxwork int) *AccountTimeRotationConfig {
	cfg := cloneAccountTimeRotationConfig(s.snapshot)
	cfg.RandomRest = &AccountRandomRestConfig{Enabled: true, AccountIDs: []int64{1}, CheckIntervalMinutes: 1, ProbabilityPercent: probability, MinRestMinutes: 3, MaxRestMinutes: 3, MaxWorkMinutes: maxwork}
	s.publish(cfg, time.Now())
	return cfg
}

func TestAccountRandomRestDrainSharedAndResume(t *testing.T) {
	s, mr, rdb := protectionWaitFixture(t, 0)
	cfg := randomRestTestConfig(s, 100, 0)
	other := protectionTestService(t, rdb, 0)
	other.publish(cfg, time.Now())
	root := protectionTestAccount(1)
	shadow := protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	ctx := context.Background()
	now := time.Now()
	mr.SetTime(now)
	done1, err := s.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done2, err := other.BeginProtectedRequest(ctx, shadow)
	require.NoError(t, err)
	require.EqualValues(t, 2, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
	mr.SetTime(now.Add(time.Minute))
	_, err = other.BeginProtectedRequest(ctx, shadow)
	var fail *UpstreamFailoverError
	require.ErrorAs(t, err, &fail)
	code, _, _ := fail.AccountProtectionClientError()
	require.Equal(t, "account_random_draining", code)
	require.True(t, fail.ShouldRetryNextAccount())
	require.False(t, fail.ShouldReportAccountScheduleFailure())
	require.False(t, s.allowsNewSession(ctx, root))
	// 休息同时阻止新老会话选择，不能继续占用绑定。
	require.True(t, IsAccountRestError(s.checkSessionRest(ctx, root)))
	done1()
	done1()
	require.EqualValues(t, 1, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
	mr.SetTime(now.Add(8 * time.Minute))
	done2()
	state := s.ProtectionStatus(ctx, cfg, root, time.Now())
	require.Equal(t, "account_random_resting", state.State)
	require.Equal(t, 0, state.InFlight)
	require.WithinDuration(t, now.Add(11*time.Minute), *state.RandomRestUntil, time.Millisecond)
	mr.SetTime(now.Add(10 * time.Minute))
	require.Error(t, s.checkRandomRest(ctx, root))
	mr.SetTime(now.Add(11 * time.Minute))
	done3, err := s.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done3()
	require.EqualValues(t, 0, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
}

func TestAccountRandomRestIntervalAndMaximum(t *testing.T) {
	s, mr, rdb := protectionWaitFixture(t, 0)
	cfg := randomRestTestConfig(s, 0, 2)
	ctx := context.Background()
	now := time.Now()
	mr.SetTime(now)
	// 状态轮询不启动从未工作的账号。
	require.NoError(t, s.checkRandomRest(ctx, protectionTestAccount(1)))
	mr.SetTime(now.Add(10 * time.Hour))
	done, err := s.BeginProtectedRequest(ctx, protectionTestAccount(1))
	require.NoError(t, err)
	done()
	start := now.Add(10 * time.Hour)
	mr.SetTime(start.Add(time.Minute))
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = s.runRandomRest(ctx, 1, cfg.RandomRest, "check", "") }()
	}
	wg.Wait()
	next := rdb.HGet(ctx, "account_random_rest:{1}", "next").Val()
	require.Equal(t, "working", rdb.HGet(ctx, "account_random_rest:{1}", "phase").Val())
	// 同一到期时刻只抽一次，即使后来调用者提供必中值也不能重新抽。
	values, err := accountRandomRestScript.Run(ctx, rdb, []string{"account_random_rest:{1}", "account_random_rest:{1}:active"}, "check", "", 60000, 100, 0, 180000, 120000).StringSlice()
	require.NoError(t, err)
	require.Equal(t, "working", values[0])
	require.Equal(t, next, rdb.HGet(ctx, "account_random_rest:{1}", "next").Val())
	mr.SetTime(start.Add(2 * time.Minute))
	require.Error(t, s.checkRandomRest(ctx, protectionTestAccount(1)))
	require.Equal(t, "resting", rdb.HGet(ctx, "account_random_rest:{1}", "phase").Val())
}

func TestAccountRandomRestHTTPBodyAndErrors(t *testing.T) {
	s, mr, rdb := protectionWaitFixture(t, 0)
	randomRestTestConfig(s, 100, 0)
	ctx := context.Background()
	now := time.Now()
	mr.SetTime(now)
	inner := &protectionHTTP{}
	upstream := NewProtectedHTTPUpstream(inner, s, protectionAccountRepo{account: protectionTestAccount(1)})
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", nil)
	resp, err := upstream.DoWithTLS(req, "", 1, 1, nil)
	require.NoError(t, err)
	require.EqualValues(t, 1, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
	mr.SetTime(now.Add(time.Minute))
	_, err = upstream.Do(req, "", 1, 1)
	require.Error(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(body), "完成")
	require.NoError(t, resp.Body.Close())
	require.EqualValues(t, 0, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
	require.Equal(t, 1, inner.calls)
	require.Equal(t, "resting", rdb.HGet(ctx, "account_random_rest:{1}", "phase").Val())
}

func TestAccountRandomRestConfigurationAndFailures(t *testing.T) {
	s, mr, rdb := protectionWaitFixture(t, 0)
	cfg := randomRestTestConfig(s, 100, 0)
	require.NoError(t, cfg.Validate())
	clone := cloneAccountTimeRotationConfig(cfg)
	clone.RandomRest.AccountIDs[0] = 2
	require.EqualValues(t, 1, cfg.RandomRest.AccountIDs[0])
	for _, modify := range []func(*AccountRandomRestConfig){
		func(r *AccountRandomRestConfig) { r.AccountIDs = nil }, func(r *AccountRandomRestConfig) { r.AccountIDs = []int64{1, 1} },
		func(r *AccountRandomRestConfig) { r.CheckIntervalMinutes = 0 }, func(r *AccountRandomRestConfig) { r.ProbabilityPercent = 101 },
		func(r *AccountRandomRestConfig) { r.MaxRestMinutes = 2 }, func(r *AccountRandomRestConfig) { r.MaxWorkMinutes = -1 },
	} {
		bad := cloneAccountTimeRotationConfig(cfg)
		modify(bad.RandomRest)
		require.Error(t, bad.Validate())
	}
	require.Contains(t, cfg.ProtectionAccountIDs(), int64(1))
	require.Error(t, s.checkPluginRequestProtection(protectionTestAccount(1)))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.BeginProtectedRequest(ctx, protectionTestAccount(1))
	require.ErrorIs(t, err, context.Canceled)
	require.EqualValues(t, 0, rdb.SCard(context.Background(), "account_random_rest:{1}:active").Val())
	cfg.RandomRest.Enabled = false
	s.publish(cfg, time.Now())
	mr.Close()
	done, err := s.BeginProtectedRequest(context.Background(), protectionTestAccount(1))
	require.NoError(t, err)
	done()
	cfg.RandomRest.Enabled = true
	s.publish(cfg, time.Now())
	_, err = s.BeginProtectedRequest(context.Background(), protectionTestAccount(1))
	require.Error(t, err)
}

func TestAccountRandomRestOrphanNeverExpiresIntoAvailable(t *testing.T) {
	s, mr, rdb := protectionWaitFixture(t, 0)
	cfg := randomRestTestConfig(s, 100, 0)
	now := time.Now()
	mr.SetTime(now)
	ctx := context.Background()
	done, err := s.BeginProtectedRequest(ctx, protectionTestAccount(1))
	require.NoError(t, err)
	// 模拟另一个实例重启，不能因为记录变旧就当作上游已结束。
	other := protectionTestService(t, rdb, 0)
	other.publish(cfg, time.Now())
	mr.SetTime(now.Add(24 * time.Hour))
	mr.FastForward(24 * time.Hour)
	state := other.ProtectionStatus(ctx, cfg, protectionTestAccount(1), time.Now())
	require.Equal(t, "account_random_draining", state.State)
	require.Equal(t, 1, state.InFlight)
	done()
	require.Error(t, other.checkRandomRest(ctx, protectionTestAccount(1)))
}

// 防止随机休息影响严格排班，以及管理员保存其他配置意外重置休息。
func TestAccountRandomRestStrictScheduleAndConfigChanges(t *testing.T) {
	s, mr, _ := protectionWaitFixture(t, 0)
	cfg := randomRestTestConfig(s, 100, 0)
	now := time.Now()
	mr.SetTime(now)
	ctx := context.Background()
	account := protectionTestAccount(1)
	done, err := s.BeginProtectedRequest(ctx, account)
	require.NoError(t, err)
	done()
	mr.SetTime(now.Add(time.Minute))
	require.Error(t, s.checkRandomRest(ctx, account))
	cfg.Revision++
	cfg.RandomRest.ProbabilityPercent = 0
	s.publish(cfg, time.Now())
	require.Error(t, s.checkRandomRest(ctx, account))
	cfg.StrictRest = true
	local := time.Now().In(accountRotationLocation)
	cfg.Slots[0].Start = local.Add(time.Hour).Format("15:04")
	cfg.Slots[0].End = local.Add(2 * time.Hour).Format("15:04")
	cfg.Slots[0].AccountIDs = []int64{1}
	s.publish(cfg, time.Now())
	mr.SetTime(now.Add(5 * time.Minute))
	_, err = s.BeginProtectedRequest(ctx, account)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	code, _, _ := failure.AccountProtectionClientError()
	require.Equal(t, "account_resting", code)
}

func TestAccountRandomRestStickyAndNewSessionScheduling(t *testing.T) {
	for _, mode := range []string{"基础", "批量", "高级", "加权"} {
		t.Run(mode, func(t *testing.T) {
			rotation, mr, rdb := protectionWaitFixture(t, 0)
			randomRestTestConfig(rotation, 100, 0)
			now := time.Now()
			mr.SetTime(now)
			_, accounts, _, _ := smartSchedulingFixture()
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = mode == "批量"
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}), cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
			ctx := context.Background()
			done, err := rotation.BeginProtectedRequest(ctx, &accounts[0])
			require.NoError(t, err)
			done()
			mr.SetTime(now.Add(time.Minute))
			require.Error(t, rotation.checkRandomRest(ctx, &accounts[0]))
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
				require.NotEqualValues(t, 1, result.Account.ID)
				bound, bindErr := svc.getStickySessionAccountID(ctx, nil, session)
				require.NoError(t, bindErr)
				require.NotEqualValues(t, 1, bound)
				if result.ReleaseFunc != nil {
					result.ReleaseFunc()
				}
			}
			// 选号没有领取任何新额度。
			require.EqualValues(t, 0, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
		})
	}
}

func TestAccountRandomRestInterruptsRPMQueue(t *testing.T) {
	rotation, mr, rdb := protectionWaitFixture(t, 1)
	randomRestTestConfig(rotation, 100, 0)
	now := time.Now()
	mr.SetTime(now)
	ctx := context.Background()
	account := protectionTestAccount(1)
	done, err := rotation.BeginProtectedRequest(ctx, account)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- rotation.WaitAdmitRequest(ctx, account) }()
	waitProtectionQueue(t, rdb, 1)
	mr.SetTime(now.Add(time.Minute))
	select {
	case err := <-result:
		require.True(t, IsAccountRestError(err))
		var failure *UpstreamFailoverError
		require.ErrorAs(t, err, &failure)
		require.True(t, failure.ShouldRetryNextAccount())
	case <-time.After(3 * time.Second):
		t.Fatal("休息应立即退出限速队列")
	}
	waitProtectionQueue(t, rdb, 0)
	require.EqualValues(t, 1, rdb.SCard(ctx, "account_random_rest:{1}:active").Val())
	done()
}

func TestAccountRandomRestClearsPreviousResponseWithoutSnapshot(t *testing.T) {
	rotation, mr, _ := protectionWaitFixture(t, 0)
	randomRestTestConfig(rotation, 100, 0)
	_, accounts, _, _ := smartSchedulingFixture()
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
	ctx := context.Background()
	store := svc.getOpenAIWSStateStore()
	require.NoError(t, store.BindResponseAccount(ctx, 0, "resp_rest", 1, time.Hour))
	now := time.Now()
	mr.SetTime(now)
	done, err := rotation.BeginProtectedRequest(ctx, &accounts[0])
	require.NoError(t, err)
	done()
	mr.SetTime(now.Add(time.Minute))
	id, _, _, _ := svc.resolveAccountByPreviousResponseIDForCapability(ctx, nil, "resp_rest", "gpt-5.1", nil, "", false)
	require.Zero(t, id)
	id, err = store.GetResponseAccount(ctx, 0, "resp_rest")
	require.NoError(t, err)
	require.Zero(t, id)
}

func TestAccountRandomRestOverridesPreservedStickyBinding(t *testing.T) {
	rotation, mr, _ := protectionWaitFixture(t, 0)
	randomRestTestConfig(rotation, 100, 0)
	_, accounts, _, _ := smartSchedulingFixture()
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
	ctx := context.Background()
	require.NoError(t, svc.setStickySessionAccountID(ctx, nil, "保护绑定", 1, time.Hour))
	now := time.Now()
	mr.SetTime(now)
	done, err := rotation.BeginProtectedRequest(ctx, &accounts[0])
	require.NoError(t, err)
	done()
	mr.SetTime(now.Add(time.Minute))
	scheduler := newDefaultOpenAIAccountScheduler(svc, nil)
	result, _, err := scheduler.(*defaultOpenAIAccountScheduler).selectBySessionHash(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, SessionHash: "保护绑定", RequestedModel: "gpt-5.1", PreserveStickyBinding: true, DisableStickyEscape: true})
	require.NoError(t, err)
	require.Nil(t, result)
	bound, err := svc.getStickySessionAccountID(ctx, nil, "保护绑定")
	require.EqualError(t, err, "sticky session not found")
	require.Zero(t, bound)
}
