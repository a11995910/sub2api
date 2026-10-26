package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// AccountPacedConfig 将一个根账号的接单时段和发送容量放在同一策略中。
type AccountPacedConfig struct {
	AccountID     int64  `json:"account_id"`
	Start         string `json:"start"`
	End           string `json:"end"`
	RPM           int    `json:"rpm"`
	SustainedRPM  int    `json:"sustained_rpm"`
	BurstCapacity int    `json:"burst_capacity"`
	// 只兼容读取此前的长窗口配置，校验后转换并清除。
	RequestsPer15Min int `json:"requests_per_15_minutes,omitempty"`
	RequestsPerHour  int `json:"requests_per_hour,omitempty"`
	MaxConcurrency   int `json:"max_concurrency"`
	MaxWaitSeconds   int `json:"max_wait_seconds"`
}

func (p *AccountPacedConfig) validate(enabled bool) error {
	if p == nil {
		return fmt.Errorf("请填写单账号接单规则")
	}
	if p.AccountID < 0 || (enabled && p.AccountID == 0) {
		return fmt.Errorf("启用前请选择一个独立 OpenAI OAuth 根账号")
	}
	start, err := rotationMinute(p.Start, false)
	if err != nil {
		return err
	}
	end, err := rotationMinute(p.End, true)
	if err != nil {
		return err
	}
	if start == end || (start == 0 && end == 1440) {
		return fmt.Errorf("接单时间必须保留休息时段，起止时间不能相同")
	}
	if p.SustainedRPM == 0 && p.BurstCapacity == 0 && p.RequestsPer15Min > 0 && p.RequestsPerHour > 0 {
		p.SustainedRPM = max(1, min(p.RPM, p.RequestsPer15Min/15, p.RequestsPerHour/60))
		p.BurstCapacity = p.RPM
	}
	if p.RPM < 1 || p.RPM > 10000 || p.SustainedRPM < 1 || p.SustainedRPM > p.RPM || p.BurstCapacity < 1 || p.BurstCapacity > p.RPM {
		return fmt.Errorf("60 秒上限须为 1–10000；持续 RPM 与突发额度须为正整数且不超过 60 秒上限")
	}
	p.RequestsPer15Min, p.RequestsPerHour = 0, 0
	if p.MaxConcurrency < 1 || p.MaxConcurrency > 1000 || p.MaxWaitSeconds < 0 || p.MaxWaitSeconds > 65 {
		return fmt.Errorf("在途并发须为 1–1000，最长等待须为 0–65 秒")
	}
	return nil
}

func (p *AccountPacedConfig) window(now time.Time) (bool, time.Time) {
	local := now.In(accountRotationLocation)
	start, _ := rotationMinute(p.Start, false)
	end, _ := rotationMinute(p.End, true)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, accountRotationLocation)
	begin, finish := midnight.Add(time.Duration(start)*time.Minute), midnight.Add(time.Duration(end)*time.Minute)
	if end < start {
		if local.Before(finish) {
			begin = begin.Add(-24 * time.Hour)
		} else {
			finish = finish.Add(24 * time.Hour)
		}
	}
	if !local.Before(begin) && local.Before(finish) {
		return true, finish
	}
	if !local.Before(begin) {
		begin = begin.Add(24 * time.Hour)
	}
	return false, begin
}

func (p *AccountPacedConfig) scheduleError(now time.Time) error {
	active, next := p.window(now)
	if active {
		return nil
	}
	return accountProtectionError("account_resting", "账号不在接单时段，请在下次接单时间重试", max(1, int(next.Sub(now).Seconds())))
}

func (s *AccountTimeRotationService) pacedPolicy(account *Account) (*AccountPacedConfig, error) {
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil, nil
	}
	cfg, err := s.protectionConfig(time.Now())
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled || cfg.Mode != "paced" || cfg.Paced == nil || cfg.Paced.AccountID != protectionRootID(account) {
		return nil, nil
	}
	return cfg.Paced, nil
}

// 同一根身份的令牌桶、滚动60秒上限与真实发送在途在一个事务中领取。
// 每个令牌使用60000个整数单位，按毫秒补充，避免反复轮询丢失小数额度。
// 在途只按实际结束释放；状态查询只计算、不持久化、不领取额度。
var accountPacedWindow = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
local op, token = ARGV[1], ARGV[2]
local rpm, rate, capacity = tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5])
local used = redis.call('ZCOUNT', KEYS[1], '('..tostring(now-60000), '+inf')
local state = redis.call('HMGET', KEYS[4], 'units', 'updated', 'rate', 'capacity')
local units, updated
if state[1] then
 local oldRate, oldCapacity = tonumber(state[3]), tonumber(state[4])
 units, updated = tonumber(state[1]), tonumber(state[2])
 if not units or not updated or not oldRate or not oldCapacity or units < 0 or oldRate <= 0 or oldCapacity <= 0 then
  return redis.error_reply('invalid pacing bucket')
 end
 -- 先按已保存速度结算空闲时间，再应用新容量，提速或扩容不会凭空发放令牌。
 local elapsed = math.min(math.max(0, now-updated), math.ceil(oldCapacity*60000/oldRate))
 units = math.min(capacity*60000, oldCapacity*60000, units+elapsed*oldRate)
 updated = math.max(now, updated)
else
 if state[2] or state[3] or state[4] then return redis.error_reply('incomplete pacing bucket') end
 -- 存在旧准入记录但没有桶时，从空桶开始；不能因升级或状态缺失再发一次突发额度。
 units = redis.call('EXISTS', KEYS[1]) == 1 and 0 or capacity*60000
 updated = now
end
local reason, retry = 'available', 0
if units < 60000 then
 reason = 'sustained'
 retry = math.max(1, math.ceil(((60000-units)/rate + math.max(0, updated-now))/1000))
end
if used >= rpm then
 local first = redis.call('ZRANGEBYSCORE', KEYS[1], '('..tostring(now-60000), '+inf', 'WITHSCORES', 'LIMIT', used-rpm, 1)
 local wait = math.max(1, math.ceil((tonumber(first[2])+60000-now)/1000))
 if wait >= retry then reason = 'rpm'; retry = wait end
end
local active = redis.call('ZCARD', KEYS[2])
if active >= tonumber(ARGV[6]) and retry == 0 then reason = 'concurrency'; retry = 1 end
local queue = redis.call('ZCOUNT', KEYS[3], '('..tostring(now), '+inf')
if op == 'read' and queue > 0 and reason == 'available' then reason = 'queue'; retry = 1 end
local minute = math.floor(now/60000 + 480)%1440
local firstMinute, lastMinute = tonumber(ARGV[7]), tonumber(ARGV[8])
local working = (firstMinute < lastMinute and minute >= firstMinute and minute < lastMinute) or (firstMinute > lastMinute and (minute >= firstMinute or minute < lastMinute))
if not working then reason = 'resting' end
if op == 'admit' then
 redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now-60000)
 redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now)
 if redis.call('ZSCORE', KEYS[2], token) then
  -- 响应丢失后重放同一准入，不重复扣令牌或登记在途。
  reason = 'admitted'
 elseif working then
  local head = redis.call('ZRANGE', KEYS[3], 0, 0)
  if #head > 0 and head[1] ~= token then reason = 'queue'; retry = math.max(retry,1) end
  if reason == 'available' then
   units = units-60000
   redis.call('ZADD', KEYS[1], now, token)
   redis.call('PEXPIRE', KEYS[1], 61000)
   redis.call('ZADD', KEYS[2], now, token)
   redis.call('ZREM', KEYS[3], token)
   reason = 'admitted'; active = active+1; used = used+1
  end
 end
 -- 拒绝的请求也结算补充时间和配置变更，但不消耗令牌。
 -- 桶不设过期时间，重启、停用再启用和调整设置不能重置额度。
 redis.call('HSET', KEYS[4], 'units', tostring(units), 'updated', tostring(updated), 'rate', tostring(rate), 'capacity', tostring(capacity))
end
return {reason, tostring(retry), tostring(used), tostring(units), tostring(active), tostring(queue)}
`)

type AccountPacedStatus struct {
	AvailableTokens float64   `json:"available_tokens"`
	SustainedRPM    int       `json:"sustained_rpm"`
	BurstCapacity   int       `json:"burst_capacity"`
	MaxConcurrency  int       `json:"max_concurrency"`
	Waiting         int       `json:"waiting"`
	BlockedBy       string    `json:"blocked_by"`
	RetryAfter      int       `json:"retry_after"`
	NextChangeAt    time.Time `json:"next_change_at"`
	ScheduleOpen    bool      `json:"schedule_open"`
}

type pacedUsage struct {
	reason                                string
	retry, minute, units, active, waiting int
}

func pacedKeys(id int64) []string {
	key := fmt.Sprintf("account_pacing:{%d}", id)
	return []string{key + ":history", key + ":active", key + ":waiters", key + ":bucket"}
}

func (s *AccountTimeRotationService) runPaced(ctx context.Context, p *AccountPacedConfig, op, token string) (pacedUsage, error) {
	var usage pacedUsage
	unavailable := func() (pacedUsage, error) {
		return usage, accountProtectionWaitError("account_protection_unavailable", "账号接单状态暂不可用，请稍后重试")
	}
	if s.protectionRedis == nil {
		return unavailable()
	}
	start, _ := rotationMinute(p.Start, false)
	end, _ := rotationMinute(p.End, true)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := accountPacedWindow.Run(ctx, s.protectionRedis, pacedKeys(p.AccountID), op, token, p.RPM, p.SustainedRPM, p.BurstCapacity, p.MaxConcurrency, start, end).StringSlice()
	if err != nil || len(values) != 6 {
		return unavailable()
	}
	usage.reason = values[0]
	for i, dst := range []*int{&usage.retry, &usage.minute, &usage.units, &usage.active, &usage.waiting} {
		value, err := strconv.Atoi(values[i+1])
		if err != nil {
			return unavailable()
		}
		*dst = value
	}
	return usage, nil
}

func (s *AccountTimeRotationService) releasePaced(id int64, token string) {
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := s.protectionRedis.ZRem(ctx, pacedKeys(id)[1], token).Err()
		cancel()
		if err == nil {
			return
		}
	}
	s.pacedPendingReleases.Store(token, id)
	slog.Error("账号接单在途释放失败，已加入后台补偿", "account_id", id)
}

func (s *AccountTimeRotationService) retryPacedReleases(ctx context.Context) {
	if s.protectionRedis == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	s.pacedPendingReleases.Range(func(key, value any) bool {
		if ctx.Err() != nil {
			return false
		}
		if s.protectionRedis.ZRem(ctx, pacedKeys(value.(int64))[1], key.(string)).Err() == nil {
			s.pacedPendingReleases.Delete(key)
		}
		return true
	})
}

func pacedCapacityError(code string, retry int) error {
	message := "账号接单容量已满，请稍后重试"
	if code == "account_rate_limit_wait_timeout" {
		message = "账号接单等待超时，请稍后重试"
	}
	if code == "account_rate_limit_queue_full" {
		message = "账号接单等待队列已满，请稍后重试"
	}
	err := accountProtectionError(code, message, max(1, retry)).(*UpstreamFailoverError)
	err.NextAccountAction = NextAccountStop
	return err
}

func (s *AccountTimeRotationService) beginPacedRequest(ctx context.Context, account *Account, initial *AccountPacedConfig) (func(), error) {
	noop := func() {}
	ctx, endClientWait := accountProtectionClientWaitContext(ctx)
	defer endClientWait()
	started := time.Now()
	deadline := started.Add(time.Duration(initial.MaxWaitSeconds) * time.Second)
	token, id := rand.Text(), initial.AccountID
	queued, attempted, admitted := false, false, false
	lastRetry := 1
	defer func() {
		if queued {
			if stats, ok := ctx.Value(accountProtectionWaitStatsKey{}).(*atomic.Int64); ok {
				stats.Add(int64(time.Since(started)))
			}
			cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.protectionRedis.ZRem(cleanup, pacedKeys(id)[2], token).Err()
		}
		// 超时响应可能丢失，但该请求不会向上游发送，清理同一令牌即可。
		if attempted && !admitted {
			s.releasePaced(id, token)
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return noop, err
		}
		policy, err := s.pacedPolicy(account)
		if err != nil {
			return noop, err
		}
		if policy == nil {
			return s.BeginProtectedRequest(ctx, account)
		}
		if err := policy.scheduleError(time.Now()); err != nil {
			return noop, err
		}
		// 缩短等待设置立即生效，延长不能延长当前请求原来的截止时间。
		deadline = pacedEarlierTime(deadline, started.Add(time.Duration(policy.MaxWaitSeconds)*time.Second))
		if queued && !time.Now().Before(deadline) {
			return noop, pacedCapacityError("account_rate_limit_wait_timeout", lastRetry)
		}
		attempted = s.protectionRedis != nil
		opCtx := ctx
		opCancel := func() {}
		if queued {
			opCtx, opCancel = context.WithDeadline(ctx, deadline)
		}
		usage, err := s.runPaced(opCtx, policy, "admit", token)
		opCancel()
		if err != nil {
			if ctx.Err() != nil {
				return noop, ctx.Err()
			}
			if queued && !time.Now().Before(deadline) {
				return noop, pacedCapacityError("account_rate_limit_wait_timeout", lastRetry)
			}
			return noop, err
		}
		if usage.reason == "admitted" {
			if err := ctx.Err(); err != nil {
				return noop, err
			}
			if err := s.checkSessionRest(ctx, account); err != nil {
				return noop, err
			}
			var once sync.Once
			admitted = true
			return func() { once.Do(func() { s.releasePaced(id, token) }) }, nil
		}
		attempted = false // 明确未领取在途，无需额外释放或补偿。
		if usage.reason == "resting" {
			return noop, accountProtectionError("account_resting", "账号不在接单时段", 60)
		}
		lastRetry = usage.retry
		remaining := time.Until(deadline)
		if policy.MaxWaitSeconds == 0 {
			return noop, pacedCapacityError("account_rate_limited", usage.retry)
		}
		if remaining <= 0 {
			return noop, pacedCapacityError("account_rate_limit_wait_timeout", usage.retry)
		}
		if !queued {
			// 入队响应丢失也尝试清理；租约覆盖最大等待时间。
			queued = true
			opCtx, cancel := context.WithTimeout(ctx, min(remaining, 2*time.Second))
			ok, err := accountRequestEnqueue.Run(opCtx, s.protectionRedis, []string{pacedKeys(id)[2]}, min(policy.RPM, 32), token).Int()
			cancel()
			if err != nil {
				return noop, accountProtectionWaitError("account_protection_unavailable", "账号接单队列暂不可用")
			}
			if ok == 0 {
				return noop, pacedCapacityError("account_rate_limit_queue_full", usage.retry)
			}
			continue
		}
		timer := time.NewTimer(min(250*time.Millisecond, remaining))
		select {
		case <-ctx.Done():
			timer.Stop()
			return noop, ctx.Err()
		case <-timer.C:
		}
	}
}

func pacedEarlierTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (s *AccountTimeRotationService) pacedStatus(ctx context.Context, cfg *AccountTimeRotationConfig, account *Account, now time.Time) AccountProtectionStatus {
	p := cfg.Paced
	open, next := p.window(now)
	result := AccountProtectionStatus{AccountID: account.ID, State: "disabled", RPM: p.RPM,
		Paced: &AccountPacedStatus{SustainedRPM: p.SustainedRPM, BurstCapacity: p.BurstCapacity, MaxConcurrency: p.MaxConcurrency, ScheduleOpen: open, NextChangeAt: next}}
	if !cfg.Enabled {
		return result
	}
	usage, err := s.runPaced(ctx, p, "read", "")
	if err != nil {
		result.State = "account_protection_unavailable"
		return result
	}
	result.Used = &usage.minute
	result.InFlight = usage.active
	result.Paced.AvailableTokens = float64(usage.units) / 60000
	result.Paced.Waiting = usage.waiting
	result.Paced.BlockedBy, result.Paced.RetryAfter = usage.reason, usage.retry
	result.State = "available"
	if usage.reason != "available" {
		result.State = "account_rate_limited"
	}
	if usage.reason == "resting" {
		result.State = "account_resting"
	}
	if err := s.checkRequestProtection(account, now, false); err != nil {
		result.State = "account_protection_unavailable"
		if !open {
			result.State = "account_resting"
		}
	}
	return result
}
