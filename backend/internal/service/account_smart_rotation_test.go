package service

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func smartRotationTestConfig(ids ...int64) *AccountTimeRotationConfig {
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Enabled = true
	cfg.Mode = "smart"
	cfg.Smart.AccountIDs = ids
	cfg.Smart.Periods = []AccountSmartRotationPeriod{{Start: "00:00", End: "24:00", PrimaryCount: 1}}
	return cfg
}

func smartRotationTestAccount(id int64, now time.Time, used float64) *Account {
	return &Account{ID: id, Name: "测试账号", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{
			"codex_usage_updated_at":  now.Format(time.RFC3339Nano),
			"codex_7d_window_minutes": 10080.0, "codex_7d_used_percent": used,
			"codex_7d_reset_at": now.Add(7 * 24 * time.Hour).Format(time.RFC3339Nano),
		},
	}
}

func smartRotationPrimaryIDs(plan *AccountSmartRotationPlan) []int64 {
	var ids []int64
	for _, a := range plan.Accounts {
		if a.Role == "primary" {
			ids = append(ids, a.AccountID)
		}
	}
	return ids
}

func TestBuildAccountSmartRotationPlanRotatesAllSafeAccounts(t *testing.T) {
	now := time.Date(2026, 9, 30, 11, 0, 0, 0, accountRotationLocation)
	cfg := smartRotationTestConfig(3, 1, 2, 4)
	accounts := []*Account{smartRotationTestAccount(1, now, 1), smartRotationTestAccount(2, now, 40), smartRotationTestAccount(3, now, 80), smartRotationTestAccount(4, now, 90)}
	// 不同套餐的百分比不能阻止尚未到保留线的账号轮到主力。
	accounts[0].Credentials = map[string]any{"plan_type": "plus"}
	accounts[1].Credentials = map[string]any{"plan_type": "pro"}
	seen := make(map[int64]bool)
	for i := 0; i < 3; i++ {
		at := now.Add(time.Duration(i) * time.Hour)
		plan := BuildAccountSmartRotationPlan(cfg, accounts, at)
		ids := smartRotationPrimaryIDs(plan)
		if len(ids) != 1 || ids[0] == 4 || seen[ids[0]] {
			t.Fatalf("安全账号没有依次轮换：轮次=%d，主力=%v，已轮换=%v", i, ids, seen)
		}
		seen[ids[0]] = true
		within := BuildAccountSmartRotationPlan(cfg, accounts, at.Add(59*time.Minute+59*time.Second))
		if !reflect.DeepEqual(ids, smartRotationPrimaryIDs(within)) {
			t.Fatalf("同一轮内主力不应变化：%v -> %v", ids, smartRotationPrimaryIDs(within))
		}
		for _, a := range plan.Accounts {
			if a.Role == "primary" && (!strings.Contains(a.Reason, "本轮主力") || strings.Contains(a.Reason, "不可用")) {
				t.Fatalf("主力原因错误：%+v", a)
			}
		}
		if plan.Tiers[4] != 2 {
			t.Fatal("额度恰好到保留线的账号应保持保护")
		}
	}
	reordered := []*Account{accounts[3], accounts[2], accounts[0], accounts[1]}
	if !reflect.DeepEqual(BuildAccountSmartRotationPlan(cfg, accounts, now), BuildAccountSmartRotationPlan(cfg, reordered, now)) {
		t.Fatal("同一轮的计划不应依赖查询结果顺序")
	}
	if accounts[0].Priority != 0 || !accounts[0].Schedulable || cfg.Smart.AccountIDs[0] != 3 {
		t.Fatal("规划过程不能改写输入账号或配置")
	}
}

func TestBuildAccountSmartRotationPlanGradualHandoff(t *testing.T) {
	now := time.Date(2026, 9, 30, 11, 0, 0, 0, accountRotationLocation)
	cfg := smartRotationTestConfig(1, 2, 3, 4)
	cfg.Smart.RotationMinutes = 15
	cfg.Smart.Periods[0].PrimaryCount = 3
	accounts := []*Account{}
	for id := int64(1); id <= 4; id++ {
		accounts = append(accounts, smartRotationTestAccount(id, now, 0))
	}
	first := smartRotationPrimaryIDs(BuildAccountSmartRotationPlan(cfg, accounts, now))
	second := smartRotationPrimaryIDs(BuildAccountSmartRotationPlan(cfg, accounts, now.Add(15*time.Minute)))
	overlap := 0
	for _, id := range first {
		for _, next := range second {
			if id == next {
				overlap++
			}
		}
	}
	if len(first) != 3 || len(second) != 3 || overlap != 2 {
		t.Fatalf("每轮只交接一个主力名额：前轮=%v，后轮=%v", first, second)
	}
	cfg.Smart.Periods[0].PrimaryCount = 20
	if got := len(smartRotationPrimaryIDs(BuildAccountSmartRotationPlan(cfg, accounts, now))); got != 4 {
		t.Fatalf("主力数必须收敛到安全账号数：%d", got)
	}
}

func TestSmartRotationQuotaSnapshotValidity(t *testing.T) {
	now := time.Date(2026, 9, 30, 11, 0, 0, 0, accountRotationLocation)
	tests := []struct {
		name  string
		edit  func(*Account)
		valid bool
	}{
		{"有效快照", func(a *Account) {}, true},
		{"账号旧更新时间不影响新额度", func(a *Account) { a.UpdatedAt = now.Add(-100 * time.Hour) }, true},
		{"字符串及 JSON 数值", func(a *Account) {
			a.Extra["codex_7d_used_percent"] = "90"
			a.Extra["codex_7d_window_minutes"] = json.Number("10080")
		}, true},
		{"八小时边界仍有效", func(a *Account) { a.Extra["codex_usage_updated_at"] = now.Add(-8 * time.Hour).Format(time.RFC3339Nano) }, true},
		{"超出八小时未知", func(a *Account) {
			a.Extra["codex_usage_updated_at"] = now.Add(-8*time.Hour - time.Nanosecond).Format(time.RFC3339Nano)
			a.UpdatedAt = now
		}, false},
		{"缺采样时间未知", func(a *Account) { delete(a.Extra, "codex_usage_updated_at"); a.UpdatedAt = now }, false},
		{"未来采样未知", func(a *Account) { a.Extra["codex_usage_updated_at"] = now.Add(time.Second).Format(time.RFC3339Nano) }, false},
		{"零窗口未知", func(a *Account) { a.Extra["codex_7d_window_minutes"] = 0.0 }, false},
		{"负窗口未知", func(a *Account) { a.Extra["codex_7d_window_minutes"] = -1.0 }, false},
		{"窗口非有限数未知", func(a *Account) { a.Extra["codex_7d_window_minutes"] = math.Inf(1) }, false},
		{"缺用量未知", func(a *Account) { delete(a.Extra, "codex_7d_used_percent") }, false},
		{"用量非数字未知", func(a *Account) { a.Extra["codex_7d_used_percent"] = math.NaN() }, false},
		{"字符串非数字未知", func(a *Account) { a.Extra["codex_7d_used_percent"] = "NaN" }, false},
		{"用量溢出未知", func(a *Account) { a.Extra["codex_7d_used_percent"] = 100.1 }, false},
		{"缺重置时间未知", func(a *Account) { delete(a.Extra, "codex_7d_reset_at") }, false},
		{"恰好重置即未知", func(a *Account) { a.Extra["codex_7d_reset_at"] = now.Format(time.RFC3339) }, false},
		{"已过重置时间未知", func(a *Account) { a.Extra["codex_7d_reset_at"] = now.Add(-time.Second).Format(time.RFC3339) }, false},
		{"相对重置锚定采样", func(a *Account) {
			delete(a.Extra, "codex_7d_reset_at")
			a.Extra["codex_7d_reset_after_seconds"] = 3600.0
		}, true},
		{"相对重置不可滑动", func(a *Account) {
			delete(a.Extra, "codex_7d_reset_at")
			a.Extra["codex_usage_updated_at"] = now.Add(-time.Hour).Format(time.RFC3339)
			a.Extra["codex_7d_reset_after_seconds"] = 3600.0
		}, false},
		{"相对重置溢出未知", func(a *Account) {
			delete(a.Extra, "codex_7d_reset_at")
			a.Extra["codex_7d_reset_after_seconds"] = math.MaxFloat64
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := smartRotationTestAccount(1, now, 90)
			tt.edit(a)
			remaining, valid := quotaRemaining(a, "codex_7d", now)
			if valid != tt.valid || (tt.valid && (remaining == nil || *remaining != 10)) || (!tt.valid && remaining != nil) {
				t.Fatalf("额度有效性不符：remaining=%v valid=%v", remaining, valid)
			}
			plan := BuildAccountSmartRotationPlan(smartRotationTestConfig(1), []*Account{a}, now)
			entry := plan.Accounts[0]
			if tt.valid && entry.Role != "protected" {
				t.Fatalf("有效低额度必须保护：%+v", entry)
			}
			if !tt.valid && (entry.Role != "primary" || !strings.Contains(entry.Reason, "额度未知")) {
				t.Fatalf("未知额度需明确展示且不能当作耗尽：%+v", entry)
			}
			if _, err := json.Marshal(plan); err != nil {
				t.Fatalf("无效数值不能污染状态接口：%v", err)
			}
		})
	}
}

func TestBuildAccountSmartRotationPlanShortWindowReserve(t *testing.T) {
	now := time.Date(2026, 9, 30, 11, 0, 0, 0, accountRotationLocation)
	a := smartRotationTestAccount(1, now, 0)
	a.Extra["codex_5h_window_minutes"] = 300.0
	a.Extra["codex_5h_used_percent"] = 95.0
	a.Extra["codex_5h_reset_at"] = now.Add(time.Hour).Format(time.RFC3339)
	cfg := smartRotationTestConfig(1)
	plan := BuildAccountSmartRotationPlan(cfg, []*Account{a}, now)
	if plan.Tiers[1] != 2 || len(smartRotationPrimaryIDs(plan)) != 0 {
		t.Fatal("短窗口到保留线时也不能强行补足主力名额")
	}
	a.Extra["codex_5h_window_minutes"] = 0.0
	plan = BuildAccountSmartRotationPlan(cfg, []*Account{a}, now)
	if plan.Tiers[1] != 0 || plan.Accounts[0].Quota5hRemaining != nil || !strings.Contains(plan.Accounts[0].Reason, "5 小时额度未知") {
		t.Fatalf("零窗口不能被解释为真实额度：%+v", plan.Accounts[0])
	}
}

func TestBuildAccountSmartRotationPlanDoesNotReviveUnavailableAccounts(t *testing.T) {
	now := time.Date(2026, 9, 30, 11, 0, 0, 0, accountRotationLocation)
	future, past, parent := now.Add(time.Hour), now.Add(-time.Hour), int64(100)
	tests := []struct {
		name string
		edit func(*Account)
	}{
		{"已禁用", func(a *Account) { a.Status = "disabled" }},
		{"停止调度", func(a *Account) { a.Schedulable = false }},
		{"到期", func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past }},
		{"过载", func(a *Account) { a.OverloadUntil = &future }},
		{"限流", func(a *Account) { a.RateLimitResetAt = &future }},
		{"临时冷却", func(a *Account) { a.TempUnschedulableUntil = &future }},
		{"影子账号", func(a *Account) { a.ParentAccountID = &parent }},
		{"其他平台", func(a *Account) { a.Platform = "anthropic" }},
		{"其他类型", func(a *Account) { a.Type = "apikey" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := smartRotationTestAccount(1, now, 99)
			tt.edit(a)
			plan := BuildAccountSmartRotationPlan(smartRotationTestConfig(1), []*Account{a}, now)
			if plan.Tiers[1] != 3 || plan.Accounts[0].Role != "unavailable" || len(smartRotationPrimaryIDs(plan)) != 0 {
				t.Fatalf("不可用状态优先于额度保护：%+v", plan)
			}
		})
	}
	cfg := smartRotationTestConfig(1, 2)
	plan := BuildAccountSmartRotationPlan(cfg, []*Account{smartRotationTestAccount(1, now, 0), smartRotationTestAccount(3, now, 0), nil}, now)
	if _, exists := plan.Tiers[2]; exists {
		t.Fatal("缺失或已过滤的账号不能补入调度等级")
	}
	if _, exists := plan.Tiers[3]; exists || len(plan.Accounts) != 2 || plan.Accounts[1].AccountID != 2 || plan.Accounts[1].Role != "unavailable" {
		t.Fatalf("未选账号不应进入计划，缺失账号应明确展示：%+v", plan)
	}
	a := smartRotationTestAccount(1, now, 0)
	a.RateLimitResetAt = &now
	a.OverloadUntil = &past
	a.TempUnschedulableUntil = &now
	if got := BuildAccountSmartRotationPlan(smartRotationTestConfig(1), []*Account{a}, now).Tiers[1]; got != 0 {
		t.Fatal("冷却已结束的账号应该恢复参与轮候")
	}
}

func TestSmartRotationPeriodBoundariesAndMidnight(t *testing.T) {
	cfg := smartRotationTestConfig(1, 2, 3)
	cfg.Smart.Periods = []AccountSmartRotationPeriod{{Start: "00:00", End: "08:10", PrimaryCount: 1}, {Start: "08:10", End: "24:00", PrimaryCount: 2}}
	tests := []struct {
		time, next string
		count      int
	}{
		{"2026-09-30T08:09:59+08:00", "2026-09-30T08:10:00+08:00", 1},
		{"2026-09-30T08:10:00+08:00", "2026-09-30T09:00:00+08:00", 2},
		{"2026-09-30T23:59:59+08:00", "2026-10-01T00:00:00+08:00", 2},
		{"2026-10-01T00:00:00+08:00", "2026-10-01T01:00:00+08:00", 1},
	}
	for _, tt := range tests {
		t.Run(tt.time, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, tt.time)
			next, _ := time.Parse(time.RFC3339, tt.next)
			// UTC 输入证明不依赖调用方所在时区。
			period, got := periodAt(cfg.Smart, now.UTC())
			if period == nil || period.PrimaryCount != tt.count || !got.Equal(next) {
				t.Fatalf("时段或下一边界错误：period=%+v next=%v", period, got)
			}
		})
	}
	cfg.Smart.Periods = []AccountSmartRotationPeriod{{Start: "00:00", End: "24:00", PrimaryCount: 1}}
	now, _ := time.Parse(time.RFC3339, "2026-09-30T23:00:00+08:00")
	accounts := []*Account{smartRotationTestAccount(1, now, 0), smartRotationTestAccount(2, now, 0), smartRotationTestAccount(3, now, 0)}
	before := smartRotationPrimaryIDs(BuildAccountSmartRotationPlan(cfg, accounts, now.Add(59*time.Minute)))
	after := smartRotationPrimaryIDs(BuildAccountSmartRotationPlan(cfg, accounts, now.Add(time.Hour)))
	if reflect.DeepEqual(before, after) {
		t.Fatalf("午夜也必须按周期接班：%v -> %v", before, after)
	}
	plan := BuildAccountSmartRotationPlan(cfg, accounts, now)
	plan.Period.PrimaryCount = 99
	if cfg.Smart.Periods[0].PrimaryCount != 1 {
		t.Fatal("返回的时段不能修改原配置")
	}
}

func TestBuildAccountSmartRotationPlanInvalidConfigFallsBack(t *testing.T) {
	now := time.Now()
	for _, edit := range []func(*AccountTimeRotationConfig){
		func(c *AccountTimeRotationConfig) { c.Enabled = false },
		func(c *AccountTimeRotationConfig) { c.Mode = "manual" },
		func(c *AccountTimeRotationConfig) { c.Smart = nil },
		func(c *AccountTimeRotationConfig) { c.Smart.RotationMinutes = 0 },
		func(c *AccountTimeRotationConfig) { c.Smart.Periods = nil },
	} {
		cfg := smartRotationTestConfig(1)
		edit(cfg)
		if plan := BuildAccountSmartRotationPlan(cfg, nil, now); plan != nil {
			t.Fatalf("不可执行的配置应回退原调度：%+v", plan)
		}
	}
}
