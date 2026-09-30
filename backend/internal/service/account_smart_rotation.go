package service

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// AccountSmartRotationPlan 是一次智能轮候计算结果。Tiers 数值越小越优先。
type AccountSmartRotationPlan struct {
	Period         *AccountSmartRotationPeriod   `json:"period,omitempty"`
	NextRotationAt time.Time                     `json:"next_rotation_at"`
	Accounts       []AccountSmartRotationAccount `json:"accounts"`
	Tiers          map[int64]int                 `json:"tiers"`
}

type AccountSmartRotationAccount struct {
	AccountID        int64    `json:"account_id"`
	Name             string   `json:"name"`
	Role             string   `json:"role"`
	Reason           string   `json:"reason"`
	Quota7dRemaining *float64 `json:"quota_7d_remaining,omitempty"`
	Quota5hRemaining *float64 `json:"quota_5h_remaining,omitempty"`
}

const smartRotationQuotaFreshness = 8 * time.Hour

func extraFloat(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	var f float64
	var err error
	switch x := v.(type) {
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case json.Number:
		f, err = x.Float64()
	case string:
		f, err = strconv.ParseFloat(strings.TrimSpace(x), 64)
	default:
		return 0, false
	}
	return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
}

// 只采用尚未重置且采样未过期的规范额度字段；账号其他资料的更新时间不代表额度新鲜。
func quotaRemaining(a *Account, prefix string, now time.Time) (*float64, bool) {
	if a == nil || len(a.Extra) == 0 {
		return nil, false
	}
	updatedAt, err := parseTime(fmt.Sprint(a.Extra["codex_usage_updated_at"]))
	if err != nil || updatedAt.After(now) || now.Sub(updatedAt) > smartRotationQuotaFreshness {
		return nil, false
	}
	win, ok := extraFloat(a.Extra, prefix+"_window_minutes")
	if !ok || win <= 0 {
		return nil, false
	}
	used, ok := extraFloat(a.Extra, prefix+"_used_percent")
	if !ok || used < 0 || used > 100 {
		return nil, false
	}
	resetAt, err := parseTime(fmt.Sprint(a.Extra[prefix+"_reset_at"]))
	if err != nil {
		// 相对重置时间锚定采样时刻，不能随每次请求向后延长。
		seconds, valid := extraFloat(a.Extra, prefix+"_reset_after_seconds")
		if !valid || seconds <= 0 || seconds >= float64(math.MaxInt64/int64(time.Second)) {
			return nil, false
		}
		resetAt = updatedAt.Add(time.Duration(seconds * float64(time.Second)))
	}
	if !resetAt.After(now) {
		return nil, false
	}
	remaining := 100 - used
	return &remaining, true
}

func periodAt(cfg *AccountSmartRotationConfig, now time.Time) (*AccountSmartRotationPeriod, time.Time) {
	if cfg == nil || cfg.RotationMinutes < 15 || cfg.RotationMinutes > 240 || 1440%cfg.RotationMinutes != 0 {
		return nil, time.Time{}
	}
	local := now.In(accountRotationLocation)
	minute := local.Hour()*60 + local.Minute()
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, accountRotationLocation)
	for _, period := range cfg.Periods {
		start, startErr := rotationMinute(period.Start, false)
		end, endErr := rotationMinute(period.End, true)
		if startErr != nil || endErr != nil || period.PrimaryCount < 1 || minute < start || minute >= end {
			continue
		}
		nextMinute := (minute/cfg.RotationMinutes + 1) * cfg.RotationMinutes
		if end < nextMinute {
			nextMinute = end
		}
		// 返回副本，展示结果不持有可修改配置的指针。
		return &period, midnight.Add(time.Duration(nextMinute) * time.Minute)
	}
	return nil, time.Time{}
}

// 使用传入时间复用 OAuth 根账号的可调度条件，使状态预览和实际规划遵循同一时刻。
func smartRotationUnavailableReason(a *Account, now time.Time) string {
	if a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.ParentAccountID != nil {
		return "不属于 OpenAI OAuth 根账号"
	}
	if !a.IsActive() {
		return "账号状态不可用"
	}
	if !a.Schedulable {
		return "账号已停止调度"
	}
	if a.AutoPauseOnExpired && a.ExpiresAt != nil && !now.Before(*a.ExpiresAt) {
		return "账号已到期"
	}
	if a.OverloadUntil != nil && now.Before(*a.OverloadUntil) {
		return "账号处于过载冷却期"
	}
	if a.RateLimitResetAt != nil && now.Before(*a.RateLimitResetAt) {
		return "账号处于限流期"
	}
	if a.TempUnschedulableUntil != nil && now.Before(*a.TempUnschedulableUntil) {
		return "账号处于临时冷却期"
	}
	return ""
}

func smartRotationQuotaNote(q7, q5 *float64) string {
	switch {
	case q7 == nil && q5 == nil:
		return "；7 天及 5 小时额度未知"
	case q7 == nil:
		return "；7 天额度未知"
	case q5 == nil:
		return "；5 小时额度未知"
	default:
		return ""
	}
}

// BuildAccountSmartRotationPlan 根据实时账号状态计算确定性的轮候计划，不修改任何账号。
// accounts 必须是调用方已过滤的候选；缺失账号只展示不可用状态，不补入调度等级。
func BuildAccountSmartRotationPlan(cfg *AccountTimeRotationConfig, accounts []*Account, now time.Time) *AccountSmartRotationPlan {
	if cfg == nil || !cfg.Enabled || cfg.Mode != "smart" || cfg.Smart == nil {
		return nil
	}
	period, next := periodAt(cfg.Smart, now)
	if period == nil {
		return nil
	}
	plan := &AccountSmartRotationPlan{Period: period, NextRotationAt: next, Accounts: []AccountSmartRotationAccount{}, Tiers: map[int64]int{}}
	byID := make(map[int64]*Account, len(accounts))
	for _, a := range accounts {
		if a != nil {
			byID[a.ID] = a
		}
	}
	selected := make(map[int64]bool, len(cfg.Smart.AccountIDs))
	safe := make([]int, 0, len(cfg.Smart.AccountIDs))
	reserve := float64(cfg.Smart.QuotaReservePercent)
	for _, id := range cfg.Smart.AccountIDs {
		if id <= 0 || selected[id] {
			continue
		}
		selected[id] = true
		a := byID[id]
		entry := AccountSmartRotationAccount{AccountID: id, Role: "unavailable", Reason: "账号不存在或不属于当前候选范围"}
		if a != nil {
			entry.Name = a.Name
			entry.Quota7dRemaining, _ = quotaRemaining(a, "codex_7d", now)
			entry.Quota5hRemaining, _ = quotaRemaining(a, "codex_5h", now)
			plan.Tiers[id] = 3
			if reason := smartRotationUnavailableReason(a, now); reason != "" {
				entry.Reason = reason
			} else if (entry.Quota7dRemaining != nil && *entry.Quota7dRemaining <= reserve) || (entry.Quota5hRemaining != nil && *entry.Quota5hRemaining <= reserve) {
				entry.Role = "protected"
				entry.Reason = "有效额度已到保留线，仅在其他候选无法承接时兜底" + smartRotationQuotaNote(entry.Quota7dRemaining, entry.Quota5hRemaining)
				plan.Tiers[id] = 2
			} else {
				entry.Role = "standby"
				entry.Reason = "等待后续轮候，主力繁忙时可承接" + smartRotationQuotaNote(entry.Quota7dRemaining, entry.Quota5hRemaining)
				plan.Tiers[id] = 1
				safe = append(safe, len(plan.Accounts))
			}
		}
		plan.Accounts = append(plan.Accounts, entry)
	}
	// 百分比只判定账号自己的保留线，不跨套餐当作绝对容量比较。
	// 所有安全账号进入同一稳定环，每轮前移一个位置，避免高额度账号长期占据主力。
	sort.Slice(safe, func(i, j int) bool { return plan.Accounts[safe[i]].AccountID < plan.Accounts[safe[j]].AccountID })
	if len(safe) > 0 {
		_, offset := now.In(accountRotationLocation).Zone()
		round := (now.Unix() + int64(offset)) / int64(cfg.Smart.RotationMinutes*60)
		start := int(round % int64(len(safe)))
		if start < 0 {
			start += len(safe)
		}
		count := min(period.PrimaryCount, len(safe))
		for i := 0; i < count; i++ {
			entry := &plan.Accounts[safe[(start+i)%len(safe)]]
			entry.Role = "primary"
			entry.Reason = "本轮主力，按轮换周期接班" + smartRotationQuotaNote(entry.Quota7dRemaining, entry.Quota5hRemaining)
			plan.Tiers[entry.AccountID] = 0
		}
	}
	sort.Slice(plan.Accounts, func(i, j int) bool {
		left, right := plan.Accounts[i], plan.Accounts[j]
		leftTier, leftOK := plan.Tiers[left.AccountID]
		rightTier, rightOK := plan.Tiers[right.AccountID]
		if !leftOK {
			leftTier = 3
		}
		if !rightOK {
			rightTier = 3
		}
		if leftTier != rightTier {
			return leftTier < rightTier
		}
		return left.AccountID < right.AccountID
	})
	return plan
}
