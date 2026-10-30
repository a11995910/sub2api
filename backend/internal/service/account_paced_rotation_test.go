package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func pacedFixture(t *testing.T) (*AccountTimeRotationService, *miniredis.Miniredis, *redis.Client, *AccountTimeRotationConfig) {
	t.Helper()
	svc, mr, rdb := protectionWaitFixture(t, 0)
	now := time.Now().In(accountRotationLocation)
	cfg := &AccountTimeRotationConfig{Enabled: true, Mode: "paced", Paced: &AccountPacedConfig{
		AccountID: 1, Start: now.Add(-4 * time.Hour).Format("15:04"), End: now.Add(8 * time.Hour).Format("15:04"),
		RPM: 40, SustainedRPM: 8, BurstCapacity: 40, MaxConcurrency: 12, MaxWaitSeconds: 0,
	}}
	require.NoError(t, cfg.Validate())
	svc.publish(cfg, time.Now())
	return svc, mr, rdb, cfg
}

func pacedErrorCode(t *testing.T, err error, code string) *UpstreamFailoverError {
	t.Helper()
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	actual, _, ok := failure.AccountProtectionClientError()
	require.True(t, ok)
	require.Equal(t, code, actual)
	return failure
}

func TestAccountPacedConfigurationAndSchedule(t *testing.T) {
	p := &AccountPacedConfig{AccountID: 1, Start: "22:30", End: "06:15", RPM: 40, SustainedRPM: 8, BurstCapacity: 40, MaxConcurrency: 12, MaxWaitSeconds: 15}
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Enabled, cfg.Mode, cfg.Paced = true, "paced", p
	cfg.Slots[0].AccountIDs = []int64{1}
	cfg.RandomRest = &AccountRandomRestConfig{Enabled: true}
	require.NoError(t, cfg.Validate())
	require.Empty(t, cfg.Slots)
	require.Nil(t, cfg.Smart)
	require.Nil(t, cfg.RandomRest)
	require.Empty(t, cfg.Priorities(time.Now()))
	require.Equal(t, []int64{1}, cfg.ProtectionAccountIDs())
	cloned := cloneAccountTimeRotationConfig(cfg)
	cloned.Paced.RPM = 1
	require.Equal(t, 40, cfg.Paced.RPM)
	for _, tc := range []struct {
		hour, minute int
		active       bool
		next         string
	}{{22, 29, false, "22:30"}, {22, 30, true, "06:15"}, {0, 0, true, "06:15"}, {6, 14, true, "06:15"}, {6, 15, false, "22:30"}} {
		now := time.Date(2026, 10, 26, tc.hour, tc.minute, 0, 0, accountRotationLocation)
		active, next := p.window(now)
		require.Equal(t, tc.active, active)
		require.Equal(t, tc.next, next.Format("15:04"))
		require.True(t, next.After(now))
		svc := NewAccountTimeRotationService(nil)
		svc.publish(cfg, now)
		root, shadow := protectionTestAccount(1), protectionTestAccount(2)
		shadow.ParentAccountID = &root.ID
		for _, a := range []*Account{root, shadow} {
			require.Equal(t, tc.active, svc.CheckRequestProtection(a, now) == nil)
		}
		require.NoError(t, svc.CheckRequestProtection(protectionTestAccount(9), now))
	}
	for _, change := range []func(*AccountPacedConfig){func(p *AccountPacedConfig) { p.AccountID = 0 }, func(p *AccountPacedConfig) { p.Start = "00:00"; p.End = "24:00" }, func(p *AccountPacedConfig) { p.End = p.Start }, func(p *AccountPacedConfig) { p.SustainedRPM = 41 }, func(p *AccountPacedConfig) { p.BurstCapacity = 41 }, func(p *AccountPacedConfig) { p.MaxConcurrency = 0 }, func(p *AccountPacedConfig) { p.MaxWaitSeconds = 66 }} {
		copy := *p
		change(&copy)
		require.Error(t, copy.validate(true))
	}
}

func TestAccountPacedAtomicSharedConcurrencyAndIdempotentRelease(t *testing.T) {
	svc, _, rdb, cfg := pacedFixture(t)
	cfg.Paced.MaxConcurrency = 3
	svc.publish(cfg, time.Now())
	other := NewAccountTimeRotationService(nil)
	other.protectionRedis = rdb
	other.publish(cfg, time.Now())
	root, shadow := protectionTestAccount(1), protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	ctx := context.Background()
	var count atomic.Int64
	var wg sync.WaitGroup
	callbacks := make(chan func(), 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, a := svc, root
			if i%2 == 0 {
				s, a = other, shadow
			}
			done, err := s.BeginProtectedRequest(ctx, a)
			if err == nil {
				count.Add(1)
				callbacks <- done
			}
		}(i)
	}
	wg.Wait()
	close(callbacks)
	require.EqualValues(t, 3, count.Load())
	require.EqualValues(t, 3, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	require.EqualValues(t, 3, rdb.ZCard(ctx, pacedKeys(1)[0]).Val())
	for done := range callbacks {
		done()
		done()
	}
	require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	require.EqualValues(t, 3, rdb.ZCard(ctx, pacedKeys(1)[0]).Val(), "结束及失败均不退还用量")
	require.True(t, svc.allowsNewSession(ctx, root), "单账号满额不提前排除新会话")
}

func TestAccountPacedWaitCancellationReleaseAndScheduleChange(t *testing.T) {
	for _, action := range []string{"释放", "取消", "下班", "关闭"} {
		t.Run(action, func(t *testing.T) {
			svc, _, rdb, cfg := pacedFixture(t)
			cfg.Paced.MaxConcurrency = 1
			cfg.Paced.MaxWaitSeconds = 3
			svc.publish(cfg, time.Now())
			root := protectionTestAccount(1)
			ctx := context.Background()
			first, err := svc.BeginProtectedRequest(ctx, root)
			require.NoError(t, err)
			defer first()
			waitCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			result := make(chan error, 1)
			go func() {
				done, err := svc.BeginProtectedRequest(waitCtx, root)
				if err == nil {
					done()
				}
				result <- err
			}()
			require.Eventually(t, func() bool { return rdb.ZCard(ctx, pacedKeys(1)[2]).Val() == 1 }, time.Second, time.Millisecond)
			switch action {
			case "释放":
				first()
			case "取消":
				cancel()
			case "下班":
				cfg.Paced.Start = time.Now().In(accountRotationLocation).Add(time.Hour).Format("15:04")
				cfg.Paced.End = time.Now().In(accountRotationLocation).Add(2 * time.Hour).Format("15:04")
				cfg.Revision++
				svc.publish(cfg, time.Now())
			case "关闭":
				cfg.Enabled = false
				cfg.Revision++
				svc.publish(cfg, time.Now())
			}
			select {
			case err := <-result:
				switch action {
				case "释放", "关闭":
					require.NoError(t, err)
				case "取消":
					require.ErrorIs(t, err, context.Canceled)
				case "下班":
					require.True(t, IsAccountRestError(err))
				}
			case <-time.After(4 * time.Second):
				t.Fatal("等待没有按期退出")
			}
			require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[2]).Val())
			first()
			require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
		})
	}
}

func TestAccountPacedQueueFullAndTimeout(t *testing.T) {
	svc, _, rdb, cfg := pacedFixture(t)
	cfg.Paced.RPM, cfg.Paced.SustainedRPM, cfg.Paced.BurstCapacity = 1, 1, 1
	cfg.Paced.MaxWaitSeconds = 1
	svc.publish(cfg, time.Now())
	ctx := context.Background()
	root := protectionTestAccount(1)
	done, err := svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done()
	stats := &atomic.Int64{}
	waitCtx := context.WithValue(ctx, accountProtectionWaitStatsKey{}, stats)
	result := make(chan error, 1)
	go func() { _, err := svc.BeginProtectedRequest(waitCtx, root); result <- err }()
	require.Eventually(t, func() bool { return rdb.ZCard(ctx, pacedKeys(1)[2]).Val() == 1 }, time.Second, time.Millisecond)
	_, err = svc.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_rate_limit_queue_full")
	select {
	case err := <-result:
		pacedErrorCode(t, err, "account_rate_limit_wait_timeout")
	case <-time.After(3 * time.Second):
		t.Fatal("未按等待上限退出")
	}
	require.Greater(t, stats.Load(), int64(0))
	require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[2]).Val())
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(1)[0]).Val())
}

func TestAccountPacedHTTPAndWebSocketLifecycle(t *testing.T) {
	svc, _, rdb, cfg := pacedFixture(t)
	ctx := context.Background()
	root := protectionTestAccount(1)
	inner := &protectionHTTP{}
	client := NewProtectedHTTPUpstream(inner, svc, protectionAccountRepo{account: root})
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	resp, err := client.Do(req, "", 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	inner.failure = errors.New("上游失败")
	_, err = client.Do(req, "", 1, 1)
	require.ErrorIs(t, err, inner.failure)
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	turn := &accountProtectionTurn{}
	require.NoError(t, turn.begin(ctx, svc, root))
	require.EqualValues(t, 2, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	turn.finish()
	turn.finishAll()
	cfg.Paced.Start = time.Now().In(accountRotationLocation).Add(time.Hour).Format("15:04")
	cfg.Paced.End = time.Now().In(accountRotationLocation).Add(2 * time.Hour).Format("15:04")
	cfg.Revision++
	svc.publish(cfg, time.Now())
	_, err = client.Do(req, "", 1, 1)
	require.True(t, IsAccountRestError(err))
	require.Error(t, (&accountProtectionTurn{}).begin(ctx, svc, root))
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(data), "完成")
	require.NoError(t, resp.Body.Close())
	require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	require.EqualValues(t, 3, rdb.ZCard(ctx, pacedKeys(1)[0]).Val())
}

func TestAccountPacedUnavailableReleaseCompensationAndReadOnlyStatus(t *testing.T) {
	svc, mr, rdb, cfg := pacedFixture(t)
	ctx := context.Background()
	root := protectionTestAccount(1)
	before := mr.Keys()
	state := svc.ProtectionStatus(ctx, cfg, root, time.Now())
	require.Equal(t, "available", state.State)
	require.Equal(t, before, mr.Keys(), "查看状态不能写入Redis")
	done, err := svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	mr.SetError("存储暂不可用")
	done()
	_, err = svc.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_protection_unavailable")
	state = svc.ProtectionStatus(ctx, cfg, root, time.Now())
	require.Equal(t, "account_protection_unavailable", state.State)
	require.Nil(t, state.Used)
	mr.SetError("")
	cfg.Enabled = false
	cfg.Revision++
	svc.publish(cfg, time.Now())
	svc.retryPacedReleases(ctx)
	require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
	cfg.Enabled = true
	cfg.Revision++
	svc.publish(cfg, time.Now().Add(-46*time.Second))
	_, err = svc.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_protection_unavailable")
}

func TestAccountPacedLuaReplayAndRedisScheduleBoundary(t *testing.T) {
	svc, mr, rdb, cfg := pacedFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 26, 18, 29, 59, 0, accountRotationLocation)
	cfg.Paced.Start, cfg.Paced.End = "08:30", "18:30"
	mr.SetTime(now)
	first, err := svc.runPaced(ctx, cfg.Paced, 1, "admit", "同一请求")
	require.NoError(t, err)
	require.Equal(t, "admitted", first.reason)
	second, err := svc.runPaced(ctx, cfg.Paced, 1, "admit", "同一请求")
	require.NoError(t, err)
	require.Equal(t, "admitted", second.reason)
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(1)[0]).Val())
	mr.SetTime(now.Add(time.Second))
	next, err := svc.runPaced(ctx, cfg.Paced, 1, "admit", "下班后请求")
	require.NoError(t, err)
	require.Equal(t, "resting", next.reason)
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
}

func TestAccountPacedLegacyWaitSwitchUsesNewAdmission(t *testing.T) {
	svc, _, rdb, cfg := pacedFixture(t)
	legacy := DefaultAccountTimeRotationConfig()
	legacy.Enabled = true
	legacy.RequestLimits = []AccountRequestLimit{{AccountID: 1, RPM: 1}}
	svc.publish(legacy, time.Now())
	root := protectionTestAccount(1)
	ctx := context.Background()
	first, err := svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	first()
	result := make(chan error, 1)
	go func() {
		done, err := svc.BeginProtectedRequest(ctx, root)
		if err == nil {
			done()
		}
		result <- err
	}()
	waitProtectionQueue(t, rdb, 1)
	cfg.Revision = 1
	svc.publish(cfg, time.Now())
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("旧队列切换后未退出")
	}
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(1)[0]).Val(), "新模式必须领取用量后才发送")
	require.Zero(t, rdb.ZCard(ctx, pacedKeys(1)[1]).Val())
}

func TestAccountPacedFullBudgetKeepsSingleAccountSelectable(t *testing.T) {
	for _, batch := range []bool{false, true} {
		t.Run(map[bool]string{false: "普通调度", true: "批量调度"}[batch], func(t *testing.T) {
			rotation, _, _, cfg := pacedFixture(t)
			cfg.Paced.RPM, cfg.Paced.SustainedRPM, cfg.Paced.BurstCapacity = 1, 1, 1
			rotation.publish(cfg, time.Now())
			_, accounts, _, _ := smartSchedulingFixture()
			accounts = accounts[:1]
			ctx := context.Background()
			done, err := rotation.BeginProtectedRequest(ctx, &accounts[0])
			require.NoError(t, err)
			done()
			gatewayCfg := &config.Config{}
			gatewayCfg.Gateway.Scheduling.LoadBatchEnabled = batch
			gateway := &OpenAIGatewayService{cfg: gatewayCfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}), cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
			result, err := gateway.SelectAccountWithLoadAwareness(ctx, nil, "新测试会话", "gpt-5.1", nil)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.EqualValues(t, 1, result.Account.ID)
			if result.ReleaseFunc != nil {
				defer result.ReleaseFunc()
			}
			_, err = rotation.BeginProtectedRequest(ctx, result.Account)
			pacedErrorCode(t, err, "account_rate_limited")
		})
	}
}

func TestAccountPacedBurstThenSustainedRate(t *testing.T) {
	for _, rate := range []int{6, 8} {
		t.Run(fmt.Sprintf("持续%dRPM", rate), func(t *testing.T) {
			svc, mr, _, cfg := pacedFixture(t)
			cfg.Paced.SustainedRPM = rate
			svc.publish(cfg, time.Now())
			ctx := context.Background()
			root := protectionTestAccount(1)
			base := time.Now()
			mr.SetTime(base)
			admit := func() { done, err := svc.BeginProtectedRequest(ctx, root); require.NoError(t, err); done() }
			// 满桶支持短时40次，但不能在同一个滚动60秒窗口继续发送。
			for i := 0; i < 40; i++ {
				admit()
			}
			_, err := svc.BeginProtectedRequest(ctx, root)
			failure := pacedErrorCode(t, err, "account_rate_limited")
			require.Equal(t, "60", failure.ResponseHeaders.Get("Retry-After"))
			mr.SetTime(base.Add(59 * time.Second))
			_, err = svc.BeginProtectedRequest(ctx, root)
			pacedErrorCode(t, err, "account_rate_limited")
			mr.SetTime(base.Add(time.Minute))
			for i := 0; i < rate; i++ {
				admit()
			}
			usage, err := svc.runPaced(ctx, cfg.Paced, 1, "read", "")
			require.NoError(t, err)
			require.Equal(t, "sustained", usage.reason)
			require.Zero(t, usage.units)
			interval := time.Minute / time.Duration(rate)
			mr.SetTime(base.Add(time.Minute + interval - time.Millisecond))
			_, err = svc.BeginProtectedRequest(ctx, root)
			pacedErrorCode(t, err, "account_rate_limited")
			// 持续30分钟每个补充间隔都能发出，跨过旧15分钟限制也没有长时间停顿。
			for i := 1; i <= 30*rate; i++ {
				mr.SetTime(base.Add(time.Minute + time.Duration(i)*interval))
				admit()
			}
			usage, err = svc.runPaced(ctx, cfg.Paced, 1, "read", "")
			require.NoError(t, err)
			require.Equal(t, rate, usage.minute)
			require.Zero(t, usage.units)
			// 无请求时只补充到桶容量，不制造流量。
			mr.SetTime(base.Add(40 * time.Minute))
			usage, err = svc.runPaced(ctx, cfg.Paced, 1, "read", "")
			require.NoError(t, err)
			require.Equal(t, 40*60000, usage.units)
			require.Zero(t, usage.minute)
		})
	}
}

func TestAccountPacedSharedTokensAcrossInstancesAndShadows(t *testing.T) {
	svc, _, rdb, cfg := pacedFixture(t)
	cfg.Paced.BurstCapacity = 5
	svc.publish(cfg, time.Now())
	other := NewAccountTimeRotationService(nil)
	other.protectionRedis = rdb
	other.publish(cfg, time.Now())
	root, shadow := protectionTestAccount(1), protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	var count atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, a := svc, root
			if i%2 == 0 {
				s, a = other, shadow
			}
			done, err := s.BeginProtectedRequest(context.Background(), a)
			if err == nil {
				count.Add(1)
				done()
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 5, count.Load())
	usage, err := svc.runPaced(context.Background(), cfg.Paced, 1, "read", "")
	require.NoError(t, err)
	require.Zero(t, usage.units)
	require.Zero(t, usage.active)
}

func TestAccountPacedRefillPrecisionAndReadOnlyStatus(t *testing.T) {
	svc, mr, rdb, cfg := pacedFixture(t)
	cfg.Paced.BurstCapacity = 1
	svc.publish(cfg, time.Now())
	ctx := context.Background()
	root := protectionTestAccount(1)
	base := time.Now()
	mr.SetTime(base)
	done, err := svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done()
	stored := rdb.HGetAll(ctx, pacedKeys(1)[3]).Val()
	mr.SetTime(base.Add(3750 * time.Millisecond))
	for i := 0; i < 3; i++ {
		state := svc.ProtectionStatus(ctx, cfg, root, time.Now())
		require.InDelta(t, 0.5, state.Paced.AvailableTokens, 0.00001)
	}
	require.Equal(t, stored, rdb.HGetAll(ctx, pacedKeys(1)[3]).Val(), "读取状态不得推进桶时钟或领取令牌")
	for ms := 3800; ms < 7500; ms += 100 {
		mr.SetTime(base.Add(time.Duration(ms) * time.Millisecond))
		_, err := svc.BeginProtectedRequest(ctx, root)
		pacedErrorCode(t, err, "account_rate_limited")
	}
	mr.SetTime(base.Add(7499 * time.Millisecond))
	_, err = svc.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_rate_limited")
	mr.SetTime(base.Add(7500 * time.Millisecond))
	done, err = svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done()
}

func TestAccountPacedConfigChangeAndRestartDoNotRefillBucket(t *testing.T) {
	svc, mr, rdb, cfg := pacedFixture(t)
	cfg.Paced.SustainedRPM = 6
	cfg.Paced.BurstCapacity = 1
	svc.publish(cfg, time.Now())
	ctx := context.Background()
	root := protectionTestAccount(1)
	base := time.Now()
	mr.SetTime(base)
	done, err := svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done()
	cfg.Paced.SustainedRPM = 8
	cfg.Paced.BurstCapacity = 2
	cfg.Revision++
	svc.publish(cfg, time.Now())
	mr.SetTime(base.Add(5 * time.Second))
	_, err = svc.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_rate_limited")
	require.Equal(t, "30000", rdb.HGet(ctx, pacedKeys(1)[3], "units").Val(), "提速扩容前的5秒按旧6RPM计算，不凭空补满")
	mr.SetTime(base.Add(8750 * time.Millisecond))
	done, err = svc.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done()
	cfg.Enabled = false
	cfg.Revision++
	svc.publish(cfg, time.Now())
	cfg.Enabled = true
	cfg.Paced.SustainedRPM = 1
	cfg.Paced.BurstCapacity = 1
	cfg.Revision++
	other := NewAccountTimeRotationService(nil)
	other.protectionRedis = rdb
	other.publish(cfg, time.Now())
	_, err = other.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_rate_limited")
	require.Equal(t, "0", rdb.HGet(ctx, pacedKeys(1)[3], "units").Val())
	// Redis时钟回退不重复累计已经结算的时间。
	mr.SetTime(base.Add(5 * time.Second))
	_, err = other.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_rate_limited")
	mr.SetTime(base.Add(68749 * time.Millisecond))
	_, err = other.BeginProtectedRequest(ctx, root)
	pacedErrorCode(t, err, "account_rate_limited")
	mr.SetTime(base.Add(68750 * time.Millisecond))
	done, err = other.BeginProtectedRequest(ctx, root)
	require.NoError(t, err)
	done()
}

func TestAccountPacedLegacyConfigAndBucketInitialization(t *testing.T) {
	svc, mr, rdb, cfg := pacedFixture(t)
	cfg.Paced.SustainedRPM, cfg.Paced.BurstCapacity = 0, 0
	cfg.Paced.RequestsPer15Min, cfg.Paced.RequestsPerHour = 120, 480
	require.NoError(t, cfg.Validate())
	require.Equal(t, 8, cfg.Paced.SustainedRPM)
	require.Equal(t, 40, cfg.Paced.BurstCapacity)
	require.Zero(t, cfg.Paced.RequestsPerHour)
	svc.publish(cfg, time.Now())
	ctx := context.Background()
	base := time.Now()
	mr.SetTime(base)
	require.NoError(t, rdb.ZAdd(ctx, pacedKeys(1)[0], redis.Z{Score: float64(base.UnixMilli()), Member: "旧准入"}).Err())
	_, err := svc.BeginProtectedRequest(ctx, protectionTestAccount(1))
	pacedErrorCode(t, err, "account_rate_limited")
	require.Equal(t, "0", rdb.HGet(ctx, pacedKeys(1)[3], "units").Val(), "存在旧记录不能重新发放40次")
	mr.SetTime(base.Add(7500 * time.Millisecond))
	done, err := svc.BeginProtectedRequest(ctx, protectionTestAccount(1))
	require.NoError(t, err)
	done()
	require.NoError(t, rdb.HSet(ctx, pacedKeys(1)[3], "units", "损坏").Err())
	_, err = svc.BeginProtectedRequest(ctx, protectionTestAccount(1))
	pacedErrorCode(t, err, "account_protection_unavailable")
}

func TestAccountPacedMultipleAccountsValidationAndCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ids      []int64
		legacy   int64
		enabled  bool
		valid    bool
		expected []int64
	}{
		{"兼容单账号", nil, 1, true, true, []int64{1}},
		{"多选覆盖旧字段", []int64{2, 3}, 1, true, true, []int64{2, 3}},
		{"停用可清空且不恢复旧字段", []int64{}, 1, false, true, nil},
		{"启用必须选账号", []int64{}, 1, true, false, nil},
		{"重复账号", []int64{1, 1}, 0, true, false, nil},
		{"零账号", []int64{0}, 0, false, false, nil},
		{"负账号", []int64{-1}, 0, false, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &AccountTimeRotationConfig{Enabled: tc.enabled, Mode: "paced", Paced: &AccountPacedConfig{AccountID: tc.legacy, AccountIDs: tc.ids, Start: "08:30", End: "18:30", RPM: 40, SustainedRPM: 8, BurstCapacity: 40, MaxConcurrency: 12}}
			if !tc.valid {
				require.Error(t, cfg.Validate())
				return
			}
			require.NoError(t, cfg.Validate())
			require.Equal(t, tc.expected, cfg.ProtectionAccountIDs())
			cloned := cloneAccountTimeRotationConfig(cfg)
			raw, err := json.Marshal(cloned)
			require.NoError(t, err)
			var restored AccountTimeRotationConfig
			require.NoError(t, json.Unmarshal(raw, &restored))
			require.NoError(t, restored.Validate())
			require.Equal(t, tc.expected, restored.ProtectionAccountIDs())
			if len(cloned.Paced.AccountIDs) > 0 {
				cloned.Paced.AccountIDs[0] = 99
				require.Equal(t, tc.expected, cfg.ProtectionAccountIDs(), "快照必须深复制账号列表")
			}
		})
	}
}

func TestAccountPacedMultipleAccountsKeepIndependentCapacity(t *testing.T) {
	svc, _, rdb, cfg := pacedFixture(t)
	cfg.Paced.AccountIDs = []int64{1, 3}
	cfg.Paced.BurstCapacity, cfg.Paced.MaxConcurrency = 1, 1
	require.NoError(t, cfg.Validate())
	svc.publish(cfg, time.Now())
	ctx := context.Background()
	first, second := protectionTestAccount(1), protectionTestAccount(3)
	shadow := protectionTestAccount(4)
	shadow.ParentAccountID = &second.ID
	releaseFirst, err := svc.BeginProtectedRequest(ctx, first)
	require.NoError(t, err)
	defer releaseFirst()
	_, err = svc.BeginProtectedRequest(ctx, first)
	pacedErrorCode(t, err, "account_rate_limited")
	// 首个根账号耗尽额度后，另一个根账号仍可通过自己的影子账号发送。
	releaseSecond, err := svc.BeginProtectedRequest(ctx, shadow)
	require.NoError(t, err)
	defer releaseSecond()
	_, err = svc.BeginProtectedRequest(ctx, second)
	pacedErrorCode(t, err, "account_rate_limited")
	for _, account := range []*Account{first, second, shadow} {
		state := svc.ProtectionStatus(ctx, cfg, account, time.Now())
		require.NotNil(t, state.Used)
		require.Equal(t, 1, *state.Used)
		require.Equal(t, 1, state.InFlight)
	}
	releaseFirst()
	require.EqualValues(t, 0, rdb.ZCard(ctx, pacedKeys(first.ID)[1]).Val())
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(second.ID)[1]).Val())
	// 移除账号后不再准入计数，但已开始请求仍能清理自己的在途记录。
	cfg.Paced.AccountIDs = []int64{1}
	svc.publish(cfg, time.Now())
	done, err := svc.BeginProtectedRequest(ctx, second)
	require.NoError(t, err)
	done()
	releaseSecond()
	require.EqualValues(t, 0, rdb.ZCard(ctx, pacedKeys(second.ID)[1]).Val())
	require.EqualValues(t, 1, rdb.ZCard(ctx, pacedKeys(second.ID)[0]).Val())
}

func TestAccountPacedMultipleAccountsShareSchedule(t *testing.T) {
	svc, _, _, cfg := pacedFixture(t)
	now := time.Now().In(accountRotationLocation)
	cfg.Paced.AccountIDs = []int64{1, 3}
	cfg.Paced.Start, cfg.Paced.End = now.Add(time.Hour).Format("15:04"), now.Add(2*time.Hour).Format("15:04")
	svc.publish(cfg, now)
	shadow := protectionTestAccount(4)
	rootID := int64(3)
	shadow.ParentAccountID = &rootID
	for _, account := range []*Account{protectionTestAccount(1), protectionTestAccount(3), shadow} {
		pacedErrorCode(t, svc.CheckRequestProtection(account, now), "account_resting")
	}
	require.NoError(t, svc.CheckRequestProtection(protectionTestAccount(9), now))
}
