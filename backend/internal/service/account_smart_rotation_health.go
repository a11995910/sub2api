package service

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"time"
)

func (c *AccountSmartRotationConfig) fillLegacyHealthPolicy() {
	// 完全没有健康规则的历史配置整体补齐默认值，部分填写的非法值仍需校验。
	if c.TTFTThresholdSeconds == 0 && c.SlowRequestCount == 0 && c.HealthyRequestCount == 0 && c.SampleWindowMinutes == 0 && c.CooldownMinutes == 0 && c.WaitingPriority == 0 && c.ProbeIntervalSeconds == 0 {
		ids := c.AccountIDs
		*c = *defaultSmartRotationConfig()
		c.AccountIDs = ids
	}
}

func (c *AccountSmartRotationConfig) validateHealthPolicy() error {
	c.fillLegacyHealthPolicy()
	for _, v := range []struct {
		name             string
		value, low, high int
	}{
		{"首字阈值（秒）", c.TTFTThresholdSeconds, 1, 300},
		{"连续慢请求数", c.SlowRequestCount, 2, 20},
		{"连续健康请求数", c.HealthyRequestCount, 2, 20},
		{"采样窗口（分钟）", c.SampleWindowMinutes, 1, 120},
		{"等待时间（分钟）", c.CooldownMinutes, 1, 1440},
		{"等待优先级", c.WaitingPriority, 1, 2147483647},
		{"观察请求间隔（秒）", c.ProbeIntervalSeconds, 1, 600},
	} {
		if v.value < v.low || v.value > v.high {
			return fmt.Errorf("%s必须为 %d 至 %d 的整数", v.name, v.low, v.high)
		}
	}
	if (max(c.HealthyRequestCount, c.SlowRequestCount)-1)*c.ProbeIntervalSeconds >= c.SampleWindowMinutes*60 {
		return fmt.Errorf("采样窗口必须大于连续健康或慢请求所需的最短观察时间")
	}
	return nil
}

// AccountSmartRotationHealth 只记录运行表现，不覆盖账号自行设置的优先级。
type AccountSmartRotationHealth struct {
	State           string    `json:"state"`
	SlowStreak      int       `json:"slow_streak"`
	HealthyStreak   int       `json:"healthy_streak"`
	StreakStartedAt time.Time `json:"streak_started_at"`
	LastSampleAt    time.Time `json:"last_sample_at"`
	LastTTFTMs      *int      `json:"last_ttft_ms,omitempty"`
	LastDurationMs  *int64    `json:"last_duration_ms,omitempty"`
	CooldownUntil   time.Time `json:"cooldown_until"`
	NextProbeAt     time.Time `json:"next_probe_at"`
	ProbeInFlight   bool      `json:"-"`
}

type AccountSmartRotationHealthSnapshot struct {
	Revision int64                                `json:"revision"`
	Accounts map[int64]AccountSmartRotationHealth `json:"accounts"`
}

type AccountSmartRotationHealthRepository interface {
	LoadHealth(context.Context) (*AccountSmartRotationHealthSnapshot, error)
	SaveHealth(context.Context, *AccountSmartRotationHealthSnapshot) error
}

func (s *AccountTimeRotationService) reconcileHealthLocked(config *AccountTimeRotationConfig) {
	if s.health == nil {
		s.health = make(map[int64]*AccountSmartRotationHealth)
	}
	if !config.Enabled || config.Mode != "smart" || config.Smart == nil {
		clear(s.health)
		return
	}
	policyChanged := false
	if s.snapshot != nil && s.snapshot.Smart != nil {
		oldPolicy, newPolicy := *s.snapshot.Smart, *config.Smart
		oldPolicy.AccountIDs, newPolicy.AccountIDs = nil, nil
		policyChanged = !reflect.DeepEqual(oldPolicy, newPolicy)
	}
	for id, h := range s.health {
		if !slices.Contains(config.Smart.AccountIDs, id) {
			delete(s.health, id)
			continue
		}
		if policyChanged {
			h.SlowStreak, h.HealthyStreak = 0, 0
			h.StreakStartedAt = time.Time{}
		}
	}
}

func (s *AccountTimeRotationService) healthPolicyLocked(id int64, now time.Time) *AccountSmartRotationConfig {
	if s.snapshot == nil || now.Sub(s.refreshedAt) > 45*time.Second || !s.snapshot.Enabled || s.snapshot.Mode != "smart" || s.snapshot.Smart == nil || !slices.Contains(s.snapshot.Smart.AccountIDs, id) {
		return nil
	}
	return s.snapshot.Smart
}

func (s *AccountTimeRotationService) healthLocked(id int64, now time.Time) *AccountSmartRotationHealth {
	if s.health == nil {
		s.health = make(map[int64]*AccountSmartRotationHealth)
	}
	h := s.health[id]
	if h == nil {
		h = &AccountSmartRotationHealth{State: "normal"}
		s.health[id] = h
	}
	if h.State == "cooling" && !now.Before(h.CooldownUntil) {
		h.State, h.SlowStreak, h.HealthyStreak = "recovering", 0, 0
		h.StreakStartedAt = time.Time{}
	}
	return h
}

// ObserveHealth 以请求完成顺序统计最近窗口内的连续表现。缺少首字或失败不能算绿色。
func (s *AccountTimeRotationService) ObserveHealth(account *Account, success bool, ttft *int, duration time.Duration, now time.Time) {
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.ParentAccountID != nil {
		return
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	cfg := s.healthPolicyLocked(account.ID, now)
	if cfg == nil {
		return
	}
	h := s.healthLocked(account.ID, now)
	h.LastSampleAt = now
	h.LastTTFTMs = nil
	if ttft != nil && *ttft >= 0 {
		value := *ttft
		h.LastTTFTMs = &value
	}
	h.LastDurationMs = nil
	if duration > 0 {
		value := duration.Milliseconds()
		h.LastDurationMs = &value
	}
	if h.State == "cooling" {
		return
	}
	// 冷却前启动的长请求不能用来证明等待后的恢复。
	if h.State == "recovering" && duration > 0 && now.Add(-duration).Before(h.CooldownUntil) {
		return
	}
	if !h.StreakStartedAt.IsZero() && now.Sub(h.StreakStartedAt) >= time.Duration(cfg.SampleWindowMinutes)*time.Minute {
		h.SlowStreak, h.HealthyStreak = 0, 0
		h.StreakStartedAt = time.Time{}
	}
	if !success || ttft == nil || *ttft < 0 {
		h.SlowStreak, h.HealthyStreak = 0, 0
		h.StreakStartedAt = time.Time{}
		return
	}
	if *ttft >= cfg.TTFTThresholdSeconds*1000 {
		if h.SlowStreak == 0 {
			h.StreakStartedAt = now
		}
		h.SlowStreak++
		h.HealthyStreak = 0
		if h.SlowStreak >= cfg.SlowRequestCount {
			h.State = "cooling"
			h.CooldownUntil = now.Add(time.Duration(cfg.CooldownMinutes) * time.Minute)
			h.NextProbeAt = h.CooldownUntil
		}
	} else {
		if h.HealthyStreak == 0 {
			h.StreakStartedAt = now
		}
		h.SlowStreak = 0
		if h.State == "recovering" {
			h.HealthyStreak++
			if h.HealthyStreak >= cfg.HealthyRequestCount {
				h.State = "normal"
				h.CooldownUntil = time.Time{}
				h.NextProbeAt = time.Time{}
			}
		} else {
			h.HealthyStreak = 0
		}
	}
}

// acquireProbe 在同一进程内保证每个观察账号最多一个在途请求，并限制放行间隔。
func (s *AccountTimeRotationService) acquireProbe(id int64, now time.Time) (func(), bool) {
	if s == nil {
		return func() {}, true
	}
	s.snapshotMu.Lock()
	cfg := s.healthPolicyLocked(id, now)
	if cfg == nil {
		s.snapshotMu.Unlock()
		return func() {}, true
	}
	h := s.healthLocked(id, now)
	if h.State != "recovering" {
		s.snapshotMu.Unlock()
		return func() {}, true
	}
	if h.ProbeInFlight || now.Before(h.NextProbeAt) {
		s.snapshotMu.Unlock()
		return nil, false
	}
	h.ProbeInFlight = true
	h.NextProbeAt = now.Add(time.Duration(cfg.ProbeIntervalSeconds) * time.Second)
	s.snapshotMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.snapshotMu.Lock()
			h.ProbeInFlight = false
			s.snapshotMu.Unlock()
		})
	}, true
}

func (s *AccountTimeRotationService) IsDegraded(id int64, now time.Time) bool {
	if s == nil {
		return false
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	return s.healthPolicyLocked(id, now) != nil && s.healthLocked(id, now).State != "normal"
}

func (s *AccountTimeRotationService) IsRecovering(id int64, now time.Time) bool {
	if s == nil {
		return false
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	return s.healthPolicyLocked(id, now) != nil && s.healthLocked(id, now).State == "recovering"
}

func (s *AccountTimeRotationService) restoreHealth(ctx context.Context, cfg *AccountTimeRotationConfig) error {
	repo, ok := s.repo.(AccountSmartRotationHealthRepository)
	if !ok {
		return nil
	}
	s.snapshotMu.RLock()
	loaded := s.health != nil
	s.snapshotMu.RUnlock()
	if loaded {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	saved, err := repo.LoadHealth(ctx)
	if err != nil {
		return err
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if s.health != nil {
		return nil
	}
	s.health = make(map[int64]*AccountSmartRotationHealth)
	if saved != nil && saved.Revision == cfg.Revision {
		for id, value := range saved.Accounts {
			h := value
			h.ProbeInFlight = false
			s.health[id] = &h
		}
	}
	return nil
}

func (s *AccountTimeRotationService) persistHealth(ctx context.Context) error {
	repo, ok := s.repo.(AccountSmartRotationHealthRepository)
	if !ok {
		return nil
	}
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	s.snapshotMu.RLock()
	if s.snapshot == nil {
		s.snapshotMu.RUnlock()
		return nil
	}
	saved := &AccountSmartRotationHealthSnapshot{Revision: s.snapshot.Revision, Accounts: make(map[int64]AccountSmartRotationHealth, len(s.health))}
	for id, h := range s.health {
		saved.Accounts[id] = *h
	}
	s.snapshotMu.RUnlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return repo.SaveHealth(ctx, saved)
}
