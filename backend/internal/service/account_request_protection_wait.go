package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const accountRequestMaxWait = 65 * time.Second

type accountProtectionWaitStatsKey struct{}

// 只影响账号健康采样；用户侧耗时和计费仍保留完整时间。
func trackAccountProtectionWait(ctx context.Context) (context.Context, func(*OpenAIForwardResult)) {
	stats, _ := ctx.Value(accountProtectionWaitStatsKey{}).(*atomic.Int64)
	if stats == nil {
		stats = &atomic.Int64{}
		ctx = context.WithValue(ctx, accountProtectionWaitStatsKey{}, stats)
	}
	before := stats.Load()
	return ctx, func(result *OpenAIForwardResult) {
		if result != nil {
			result.LocalAdmissionWait = time.Duration(stats.Load() - before)
		}
	}
}

// 队列租约覆盖最长等待和清理时间；异常退出的实例不会永久占用队列。
var accountRequestEnqueue = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[1]) then return 0 end
redis.call('ZADD', KEYS[1], now+90000, ARGV[2])
redis.call('PEXPIRE', KEYS[1], 90000)
return 1
`)

func (s *AccountTimeRotationService) requestLimit(account *Account) int {
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return 0
	}
	cfg, err := s.protectionConfig(time.Now())
	if err != nil || cfg == nil || !cfg.Enabled {
		return 0
	}
	if cfg.Mode == "paced" && pacedAccountSelected(cfg.Paced, protectionRootID(account)) {
		return cfg.Paced.RPM
	}
	for _, limit := range cfg.RequestLimits {
		if limit.AccountID == protectionRootID(account) {
			return limit.RPM
		}
	}
	return 0
}

// 只在新会话候选筛选时调用；粘性绑定仍使用原账号并在发送前等待。
// 预留约 20% 的窗口容量，不增加实际发送上限；RPM=1 时不预留。
func (s *AccountTimeRotationService) allowsNewSession(ctx context.Context, account *Account) bool {
	if policy, err := s.pacedPolicy(account); err != nil {
		return false
	} else if policy != nil {
		// 单账号没有承接预留额度的其他账号；统一在实际发送前有界排队。
		return policy.scheduleError(time.Now()) == nil
	}
	if err := s.checkRandomRest(ctx, account); err != nil {
		return false
	}
	rpm := s.requestLimit(account)
	if rpm <= 0 {
		return true
	}
	if s.protectionRedis == nil {
		return false
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	used, err := accountRequestUsage.Run(readCtx, s.protectionRedis, []string{fmt.Sprintf("account_request_limit:{%d}", protectionRootID(account))}).Int()
	reserve := 0
	if rpm > 1 {
		reserve = max(1, rpm/5)
	}
	return err == nil && used < rpm-reserve
}

func accountProtectionWaitError(code, message string) error {
	err := accountProtectionError(code, message, 1)
	// 本地容量等待结束不能触发换号，否则会迁移整个会话的缓存归属。
	err.(*UpstreamFailoverError).NextAccountAction = NextAccountStop
	return err
}

// WaitAdmitRequest 保留已选择的账号，只在原根账号的有界队列等待。
// HTTP 重试、WS 每轮与预热共用同一个原子窗口，等待本身不消耗额度。
func (s *AccountTimeRotationService) WaitAdmitRequest(ctx context.Context, account *Account) error {
	if err := s.waitAdmitRequest(ctx, account, accountRequestMaxWait); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// 队列清理或心跳收尾也可能跨越排班边界，返回发送层前再次复核。
	return s.checkSessionRest(ctx, account)
}

func (s *AccountTimeRotationService) waitAdmitRequest(ctx context.Context, account *Account, maxWait time.Duration) error {
	started := time.Now()
	if s.requestLimit(account) > 0 {
		var cancelClientWait context.CancelFunc
		ctx, cancelClientWait = accountProtectionClientWaitContext(ctx)
		defer cancelClientWait()
	}
	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()
	waiter := ""
	defer func() {
		if waiter != "" {
			if stats, ok := ctx.Value(accountProtectionWaitStatsKey{}).(*atomic.Int64); ok {
				stats.Add(int64(time.Since(started)))
			}
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if waitCtx.Err() != nil {
			return accountProtectionWaitError("account_rate_limit_wait_timeout", "原账号限速等待超时，请稍后重试")
		}
		err := s.admitRequest(waitCtx, account, waiter)
		if err == nil {
			return ctx.Err()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if waitCtx.Err() != nil {
			return accountProtectionWaitError("account_rate_limit_wait_timeout", "原账号限速等待超时，请稍后重试")
		}
		var failure *UpstreamFailoverError
		if !errors.As(err, &failure) {
			return err
		}
		code, _, _ := failure.AccountProtectionClientError()
		if code != "account_rate_limited" {
			if waiter != "" && !IsAccountRestError(err) {
				failure.NextAccountAction = NextAccountStop
			}
			return err
		}
		if waiter == "" {
			rpm := s.requestLimit(account)
			if rpm <= 0 {
				continue // 管理员已移除限制，下一轮仍重新校验休息和配置。
			}
			waiter = rand.Text()
			key := fmt.Sprintf("account_request_limit:{%d}:waiters", protectionRootID(account))
			opCtx, opCancel := context.WithTimeout(waitCtx, 2*time.Second)
			added, enqueueErr := accountRequestEnqueue.Run(opCtx, s.protectionRedis, []string{key}, min(rpm, 32), waiter).Int()
			opCancel()
			// 即使入队响应丢失也清理租约，不留下不必要的阻塞。
			defer func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cleanupCancel()
				_ = s.protectionRedis.ZRem(cleanupCtx, key, waiter).Err()
			}()
			if enqueueErr != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if waitCtx.Err() != nil {
					return accountProtectionWaitError("account_rate_limit_wait_timeout", "原账号限速等待超时，请稍后重试")
				}
				return accountProtectionWaitError("account_protection_unavailable", "账号限速队列不可用，请稍后重试")
			}
			if added == 0 {
				return accountProtectionWaitError("account_rate_limit_queue_full", "原账号限速等待队列已满，请稍后重试")
			}
			continue
		}
		// 每秒复核配置和排班，不能睡过下班边界；Redis 脚本保证并发唤醒不超额。
		timer := time.NewTimer(time.Second)
		select {
		case <-waitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}
