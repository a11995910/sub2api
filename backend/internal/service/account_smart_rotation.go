package service

import (
	"fmt"
	"time"
)

type AccountSmartRotationPlan struct {
	Accounts   []AccountSmartRotationAccount `json:"accounts"`
	Tiers      map[int64]int                 `json:"tiers"`
	Priorities map[int64]int                 `json:"priorities"`
}

type AccountSmartRotationAccount struct {
	AccountID         int64      `json:"account_id"`
	Name              string     `json:"name"`
	Role              string     `json:"role"`
	Reason            string     `json:"reason"`
	OriginalPriority  int        `json:"original_priority"`
	EffectivePriority int        `json:"effective_priority"`
	SlowStreak        int        `json:"slow_streak"`
	HealthyStreak     int        `json:"healthy_streak"`
	LastTTFTMs        *int       `json:"last_ttft_ms,omitempty"`
	LastDurationMs    *int64     `json:"last_duration_ms,omitempty"`
	LastSampleAt      *time.Time `json:"last_sample_at,omitempty"`
	CooldownUntil     *time.Time `json:"cooldown_until,omitempty"`
	NextProbeAt       *time.Time `json:"next_probe_at,omitempty"`
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

// HealthPlan 与实际调度共用状态；页面读取只展示，不消费观察机会。
func (s *AccountTimeRotationService) HealthPlan(cfg *AccountTimeRotationConfig, accounts []*Account, now time.Time) *AccountSmartRotationPlan {
	if cfg == nil || !cfg.Enabled || cfg.Mode != "smart" || cfg.Smart == nil {
		return nil
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if s.snapshot == nil || s.snapshot.Revision != cfg.Revision || now.Sub(s.refreshedAt) > 45*time.Second {
		return nil
	}
	plan := &AccountSmartRotationPlan{Accounts: []AccountSmartRotationAccount{}, Tiers: map[int64]int{}, Priorities: map[int64]int{}}
	byID := make(map[int64]*Account, len(accounts))
	for _, a := range accounts {
		if a != nil {
			byID[a.ID] = a
		}
	}
	for _, id := range cfg.Smart.AccountIDs {
		entry := AccountSmartRotationAccount{AccountID: id, Role: "unavailable", Reason: "账号不存在或不属于当前候选范围"}
		if a := byID[id]; a != nil {
			entry.Name, entry.OriginalPriority, entry.EffectivePriority = a.Name, a.Priority, a.Priority
			if reason := smartRotationUnavailableReason(a, now); reason != "" {
				if a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.ParentAccountID != nil {
					delete(s.health, id)
				}
				entry.Reason = reason
			} else {
				h := s.healthLocked(id, now)
				entry.Role, entry.SlowStreak, entry.HealthyStreak = h.State, h.SlowStreak, h.HealthyStreak
				if !h.StreakStartedAt.IsZero() && now.Sub(h.StreakStartedAt) >= time.Duration(cfg.Smart.SampleWindowMinutes)*time.Minute {
					entry.SlowStreak, entry.HealthyStreak = 0, 0
				}
				entry.LastTTFTMs, entry.LastDurationMs = h.LastTTFTMs, h.LastDurationMs
				if !h.LastSampleAt.IsZero() {
					sample := h.LastSampleAt
					entry.LastSampleAt = &sample
				}
				plan.Tiers[id] = 1
				entry.Reason = "使用账号设置的优先级；等待最近请求的健康样本"
				if entry.SlowStreak > 0 {
					entry.Reason = fmt.Sprintf("连续慢请求 %d/%d", entry.SlowStreak, cfg.Smart.SlowRequestCount)
				}
				if h.State != "normal" {
					entry.EffectivePriority = max(a.Priority, cfg.Smart.WaitingPriority)
					plan.Tiers[id] = 2
					if h.State == "cooling" {
						until := h.CooldownUntil
						entry.CooldownUntil = &until
						entry.Reason = "首字连续变慢，降权等待；其他账号无法承接时备用"
					} else {
						next := h.NextProbeAt
						entry.NextProbeAt = &next
						entry.Reason = fmt.Sprintf("观察恢复中，连续健康 %d/%d", entry.HealthyStreak, cfg.Smart.HealthyRequestCount)
						if !h.ProbeInFlight && !now.Before(h.NextProbeAt) {
							plan.Tiers[id] = 0
						}
					}
				}
				plan.Priorities[id] = entry.EffectivePriority
			}
		}
		plan.Accounts = append(plan.Accounts, entry)
	}
	return plan
}
