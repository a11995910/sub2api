package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func smartSchedulingFixture() (*AccountTimeRotationService, []Account, int64, int64) {
	now := time.Now()
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Mode, cfg.Enabled = "smart", true
	cfg.Smart.AccountIDs = []int64{1, 2, 3}
	cfg.Smart.Periods = []AccountSmartRotationPeriod{{Start: "00:00", End: "24:00", PrimaryCount: 1}}
	accounts := []Account{
		{ID: 1, Name: "甲", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 3, Concurrency: 1},
		{ID: 2, Name: "乙", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 3, Concurrency: 1},
		{ID: 3, Name: "保护", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 1, Concurrency: 1,
			Extra: map[string]any{"codex_usage_updated_at": now.Format(time.RFC3339), "codex_7d_window_minutes": 10080, "codex_7d_used_percent": 95, "codex_7d_reset_at": now.Add(time.Hour).Format(time.RFC3339)}},
	}
	rotation := NewAccountTimeRotationService(nil)
	rotation.publish(cfg, now)
	plan := BuildAccountSmartRotationPlan(cfg, []*Account{&accounts[0], &accounts[1], &accounts[2]}, now)
	var primary, standby int64
	for id, tier := range plan.Tiers {
		if tier == 0 {
			primary = id
		}
		if tier == 1 {
			standby = id
		}
	}
	return rotation, accounts, primary, standby
}

func TestSmartRotationLegacyCapacityAndSticky(t *testing.T) {
	for _, tc := range []struct {
		name                                               string
		batch, fullPrimary, fullStandby, loadError, sticky bool
	}{
		{name: "默认批量开关关闭仍选择主力"},
		{name: "开启批量负载选择主力", batch: true},
		{name: "主力满载切备用", fullPrimary: true},
		{name: "安全池满载切保护", fullPrimary: true, fullStandby: true},
		{name: "负载读取故障仍尝试备用", loadError: true, fullPrimary: true},
		{name: "健康粘性保持备用", sticky: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rotation, accounts, primary, standby := smartSchedulingFixture()
			if tc.sticky {
				// 排除原有手动优先级抢占规则，只验证轮换角色不会驱逐会话。
				accounts[2].Priority = 3
			}
			require.NotZero(t, primary)
			require.NotZero(t, standby)
			loads := map[int64]*AccountLoadInfo{}
			acquires := map[int64]bool{}
			if tc.fullPrimary {
				loads[primary] = &AccountLoadInfo{AccountID: primary, LoadRate: 100}
				acquires[primary] = false
			}
			if tc.fullStandby {
				loads[standby] = &AccountLoadInfo{AccountID: standby, LoadRate: 100}
				acquires[standby] = false
			}
			cache := schedulerTestConcurrencyCache{loadMap: loads, acquireResults: acquires}
			if tc.loadError {
				cache.loadBatchErr = errors.New("负载暂不可读")
			}
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = tc.batch
			svc := &OpenAIGatewayService{cfg: cfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(cache), cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
			session := ""
			if tc.sticky {
				session = "existing"
				require.NoError(t, svc.setStickySessionAccountID(context.Background(), nil, session, standby, time.Hour))
			}
			result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, session, "gpt-5.1", nil)
			require.NoError(t, err)
			require.NotNil(t, result)
			expected := primary
			if tc.fullPrimary || tc.sticky {
				expected = standby
			}
			if tc.fullStandby {
				expected = 3
			}
			require.Equal(t, expected, result.Account.ID)
			require.True(t, result.Acquired)
			if result.ReleaseFunc != nil {
				result.ReleaseFunc()
			}
		})
	}
}

func TestSmartRotationAdvancedRetainsFallbackBeyondTopK(t *testing.T) {
	rotation, accounts, primary, standby := smartSchedulingFixture()
	scheduler := openAIResetTestScheduler(0)
	scheduler.service.accountTimeRotation = rotation
	scheduler.service.cfg.Gateway.OpenAIWS.LBTopK = 1
	filtered := []*Account{&accounts[0], &accounts[1], &accounts[2]}
	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{}, filtered, nil)
	require.Len(t, plan.selectionOrder, 3, "topK 不应切掉备用和保护兜底")
	require.Equal(t, primary, plan.selectionOrder[0].account.ID)
	require.Equal(t, standby, plan.selectionOrder[1].account.ID)
	require.Equal(t, int64(3), plan.selectionOrder[2].account.ID)
	acquired := []int64{}
	scheduler.service.accountRepo = schedulerTestOpenAIAccountRepo{accounts: accounts}
	scheduler.service.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{primary: false}, acquiredIDs: &acquired})
	result, _, err := scheduler.tryAcquireOpenAISelectionOrder(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-5.1"}, plan.selectionOrder)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, standby, result.Account.ID)
	require.Equal(t, []int64{primary, standby}, acquired)
	if result.ReleaseFunc != nil {
		result.ReleaseFunc()
	}
	// 粘性只有仍在原评分 topK 中时才保留优先，不让轮换复活已降权会话。
	scheduler.service.cfg.Gateway.OpenAIWS.LBTopK = 3
	sticky := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{StickyWeighted: true, StickyAccountID: standby}, filtered, nil)
	require.Equal(t, standby, sticky.selectionOrder[0].account.ID)
}

func TestSmartRotationStaleAndUnmanagedFallback(t *testing.T) {
	rotation, accounts, _, _ := smartSchedulingFixture()
	svc := &OpenAIGatewayService{accountTimeRotation: rotation}
	now := time.Now()
	plan := svc.smartRotationPlanForAccounts([]*Account{&accounts[0], &accounts[1], &accounts[2]}, now)
	require.NotNil(t, plan)
	require.Equal(t, 1, smartRotationTier(plan, &Account{ID: 99}), "池外账号处于普通备用层")
	require.Nil(t, svc.smartRotationPlanForAccounts([]*Account{{ID: 99}}, now), "无入池候选时保留原调度")
	require.Nil(t, svc.smartRotationPlanForAccounts([]*Account{&accounts[0]}, now.Add(46*time.Second)))
	require.False(t, svc.smartRotationEnabled(now.Add(46*time.Second)))
}

func TestSmartRotationUnmanagedGroupKeepsLegacyWaitPath(t *testing.T) {
	rotation, _, _, _ := smartSchedulingFixture()
	accounts := []Account{
		{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 1, Concurrency: 1},
		{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 2, Concurrency: 1},
	}
	acquired := []int64{}
	svc := &OpenAIGatewayService{cfg: &config.Config{}, accountTimeRotation: rotation, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{10: false}, acquiredIDs: &acquired})}
	result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "gpt-5.1", nil)
	require.NoError(t, err)
	require.False(t, result.Acquired)
	require.Equal(t, int64(10), result.Account.ID, "池外保持原来的优先级等待，不强制切换备用路径")
	require.Equal(t, []int64{10}, acquired)
}

func TestSmartRotationWeightedStickyCannotBypassOriginalTopK(t *testing.T) {
	scheduler := openAIResetTestScheduler(0)
	primary := openAIAccountCandidateScore{account: &Account{ID: 1}, score: 100, loadInfo: &AccountLoadInfo{}}
	sticky := openAIAccountCandidateScore{account: &Account{ID: 2}, score: 1, loadInfo: &AccountLoadInfo{}}
	plan := openAIAccountLoadPlan{candidates: []openAIAccountCandidateScore{primary, sticky}, topK: 1, includeOverflowFallback: true,
		smartRotation: &AccountSmartRotationPlan{Tiers: map[int64]int{1: 0, 2: 1}}}
	order := scheduler.buildOpenAISelectionOrder(OpenAIAccountScheduleRequest{StickyWeighted: true, StickyAccountID: 2}, plan)
	require.Len(t, order, 2)
	require.Equal(t, int64(1), order[0].account.ID, "降权粘性不能越过健康主力")
}

func TestSmartRotationCompactUnsupportedDoesNotOccupyPrimary(t *testing.T) {
	rotation, accounts, primary, standby := smartSchedulingFixture()
	for i := range accounts {
		if accounts[i].ID == primary {
			accounts[i].Extra = map[string]any{"openai_compact_supported": false}
		}
		if accounts[i].ID == standby {
			accounts[i].Extra = map[string]any{"openai_compact_supported": true}
		}
	}
	scheduler := openAIResetTestScheduler(0)
	scheduler.service.accountTimeRotation = rotation
	plan := scheduler.buildOpenAIAccountLoadPlan(context.Background(), OpenAIAccountScheduleRequest{RequireCompact: true}, []*Account{&accounts[0], &accounts[1], &accounts[2]}, nil)
	require.NotNil(t, plan.smartRotation)
	require.Equal(t, 0, plan.smartRotation.Tiers[standby], "具备请求能力的备用必须补上主力名额")
	require.NotContains(t, plan.smartRotation.Tiers, primary)
	require.Equal(t, standby, plan.selectionOrder[0].account.ID)
	// 保存后账号改为其他凭据类型时，不应因旧配置被智能策略惩罚。
	changed := accounts[0]
	changed.Type = AccountTypeAPIKey
	require.Nil(t, scheduler.service.smartRotationPlanForAccounts([]*Account{&changed}, time.Now()))
}
