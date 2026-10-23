package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// 在途集合不按租约自动删除：进程崩溃不能被误判成上游已结束。
// 异常遗留会保守阻止休息结束，需运维核实旧连接结束后清理。
var accountRandomRestScript = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
local op = ARGV[1]
local interval = tonumber(ARGV[3])
local probability = tonumber(ARGV[4])
local duration = tonumber(ARGV[6])
local maxwork = tonumber(ARGV[7])
if op == 'release' then redis.call('SREM', KEYS[2], ARGV[2]) end
local count = redis.call('SCARD', KEYS[2])
local phase = redis.call('HGET', KEYS[1], 'phase') or 'working'
local untilAt = tonumber(redis.call('HGET', KEYS[1], 'until') or '0')
local started = tonumber(redis.call('HGET', KEYS[1], 'started') or '0')
local nextAt = tonumber(redis.call('HGET', KEYS[1], 'next') or '0')
if phase == 'resting' and now >= untilAt then
 phase = 'working'; started = 0; nextAt = 0; untilAt = 0
end
if phase == 'working' and started > 0 then
 if (maxwork > 0 and now-started >= maxwork) or (now >= nextAt and tonumber(ARGV[5]) < probability) then
  phase = 'draining'
  redis.call('HSET', KEYS[1], 'duration', duration)
 elseif now >= nextAt then
  nextAt = now+interval
 end
end
if phase == 'draining' and count == 0 then
 phase = 'resting'
 untilAt = now+tonumber(redis.call('HGET', KEYS[1], 'duration') or duration)
end
if phase == 'working' and op == 'admit' then
 if started == 0 then started = now; nextAt = now+interval end
 redis.call('SADD', KEYS[2], ARGV[2]); count = redis.call('SCARD', KEYS[2])
end
-- 查看未使用的账号不会启动工作时钟。
redis.call('HSET', KEYS[1], 'phase', phase, 'until', untilAt, 'started', started, 'next', nextAt)
return {phase, tostring(untilAt), tostring(count), tostring(now)}
`)

func (s *AccountTimeRotationService) randomRestPolicy(account *Account) *AccountRandomRestConfig {
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil
	}
	cfg, err := s.protectionConfig(time.Now())
	if err != nil || cfg == nil || !cfg.Enabled || cfg.RandomRest == nil || !cfg.RandomRest.Enabled {
		return nil
	}
	for _, id := range cfg.RandomRest.AccountIDs {
		if id == protectionRootID(account) {
			return cfg.RandomRest
		}
	}
	return nil
}

type randomRestState struct {
	phase  string
	until  time.Time
	active int
	now    time.Time
}

func (s *AccountTimeRotationService) runRandomRest(ctx context.Context, id int64, policy *AccountRandomRestConfig, op, token string) (randomRestState, error) {
	var state randomRestState
	unavailable := func() (randomRestState, error) {
		return state, accountRandomRestError("account_protection_unavailable", "随机休息存储不可用，请稍后重试", 15)
	}
	if s.protectionRedis == nil {
		return unavailable()
	}
	duration := policy.MinRestMinutes
	if policy.MaxRestMinutes > duration {
		duration += mathrand.IntN(policy.MaxRestMinutes - duration + 1)
	}
	key := fmt.Sprintf("account_random_rest:{%d}", id)
	opCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	values, err := accountRandomRestScript.Run(opCtx, s.protectionRedis, []string{key, key + ":active"}, op, token,
		policy.CheckIntervalMinutes*60000, policy.ProbabilityPercent, mathrand.IntN(100), duration*60000, policy.MaxWorkMinutes*60000).StringSlice()
	if err != nil || len(values) != 4 {
		return unavailable()
	}
	state.phase = values[0]
	var until, now int64
	if _, err = fmt.Sscan(values[1], &until); err != nil {
		return unavailable()
	}
	if _, err = fmt.Sscan(values[2], &state.active); err != nil {
		return unavailable()
	}
	if _, err = fmt.Sscan(values[3], &now); err != nil {
		return unavailable()
	}
	state.until, state.now = time.UnixMilli(until), time.UnixMilli(now)
	return state, nil
}

func accountRandomRestError(code, message string, retry int) error {
	err := accountProtectionError(code, message, retry)
	if !IsAccountRestError(err) {
		err.(*UpstreamFailoverError).NextAccountAction = NextAccountStop
	}
	return err
}

func (state randomRestState) admissionError() error {
	switch state.phase {
	case "draining":
		return accountRandomRestError("account_random_draining", "原账号已暂停接单，请使用其他可用账号", 15)
	case "resting":
		return accountRandomRestError("account_random_resting", "原账号随机休息中，请使用其他可用账号", max(1, int(state.until.Sub(state.now).Seconds())+1))
	}
	return nil
}

func (s *AccountTimeRotationService) checkRandomRest(ctx context.Context, account *Account) error {
	policy := s.randomRestPolicy(account)
	if policy == nil {
		return nil
	}
	state, err := s.runRandomRest(ctx, protectionRootID(account), policy, "check", "")
	if err != nil {
		return err
	}
	return state.admissionError()
}

// BeginProtectedRequest 必须与实际请求结束配对；限速排队不算在途请求。
func (s *AccountTimeRotationService) BeginProtectedRequest(ctx context.Context, account *Account) (func(), error) {
	noop := func() {}
	if err := s.WaitAdmitRequest(ctx, account); err != nil {
		return noop, err
	}
	policy := s.randomRestPolicy(account)
	if policy == nil {
		return noop, nil
	}
	id, token := protectionRootID(account), rand.Text()
	release := func() {
		// 请求取消不取消清理；响应丢失时也用同一令牌幂等删除。
		for attempt := 0; attempt < 3; attempt++ {
			if _, err := s.runRandomRest(context.Background(), id, policy, "release", token); err == nil {
				return
			}
		}
		slog.Error("随机休息在途记录清理失败，账号保持保守排空", "account_id", id)
	}
	state, err := s.runRandomRest(ctx, id, policy, "admit", token)
	if err != nil {
		release()
		return noop, err
	}
	if err = state.admissionError(); err != nil {
		return noop, err
	}
	var once sync.Once
	done := func() { once.Do(release) }
	if err = s.checkRequestProtection(account, time.Now(), false); err != nil {
		done()
		return noop, err
	}
	if err = ctx.Err(); err != nil {
		done()
		return noop, err
	}
	return done, nil
}

func (s *AccountTimeRotationService) tickRandomRest(ctx context.Context, cfg *AccountTimeRotationConfig) {
	if !cfg.Enabled || cfg.RandomRest == nil || !cfg.RandomRest.Enabled {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, id := range cfg.RandomRest.AccountIDs {
		if ctx.Err() != nil {
			return
		}
		// 严格排班只影响准入；排空和已开始的休息继续完成。
		if _, err := s.runRandomRest(ctx, id, cfg.RandomRest, "check", ""); err != nil {
			slog.Warn("随机休息状态推进失败", "account_id", id)
		}
	}
}

type accountProtectionBody struct {
	io.ReadCloser
	done func()
}

func (b *accountProtectionBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.done()
	}
	return n, err
}
func (b *accountProtectionBody) Close() error { err := b.ReadCloser.Close(); b.done(); return err }

// 透传模式一次只有一个活动轮次，终态和新一轮发送可能位于不同协程。
type accountProtectionTurn struct {
	mu      sync.Mutex
	pending []func()
	closed  bool
}

func (t *accountProtectionTurn) finish() {
	t.mu.Lock()
	var done func()
	if len(t.pending) > 0 {
		done = t.pending[0]
		t.pending = t.pending[1:]
	}
	t.mu.Unlock()
	if done != nil {
		done()
	}
}
func (t *accountProtectionTurn) finishAll() {
	t.mu.Lock()
	pending := t.pending
	t.pending = nil
	t.closed = true
	t.mu.Unlock()
	for _, done := range pending {
		done()
	}
}
func (t *accountProtectionTurn) begin(ctx context.Context, s *AccountTimeRotationService, account *Account) error {
	done, err := s.BeginProtectedRequest(ctx, account)
	if err != nil {
		return err
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		done()
		return context.Canceled
	}
	t.pending = append(t.pending, done)
	t.mu.Unlock()
	return nil
}

func (s *AccountTimeRotationService) preservesProtectedSession(account *Account) bool {
	return s.requestLimit(account) > 0 || s.randomRestPolicy(account) != nil
}
