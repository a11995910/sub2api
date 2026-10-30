package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/redis/go-redis/v9"
)

const accountProtectionReason GatewayFailureReason = "account_request_protection"

// AccountProtectionClientError 让本地拒绝保留明确原因，不被上游错误映射覆盖。
func (e *UpstreamFailoverError) AccountProtectionClientError() (code, message string, ok bool) {
	if e == nil || e.Reason != accountProtectionReason {
		return "", "", false
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(e.ResponseBody, &body)
	return body.Error.Code, e.ClientMessage, true
}

func accountProtectionError(code, message string, retry int) error {
	status := http.StatusServiceUnavailable
	if code == "account_rate_limited" || code == "account_rate_limit_wait_timeout" || code == "account_rate_limit_queue_full" {
		status = http.StatusTooManyRequests
	}
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "type": "server_error", "message": message}})
	return &UpstreamFailoverError{StatusCode: status, ClientStatusCode: status, ClientMessage: message, ResponseBody: body,
		ResponseHeaders: http.Header{"Retry-After": []string{strconv.Itoa(retry)}}, Scope: GatewayFailureScopeAccount,
		Reason: accountProtectionReason, NextAccountAction: NextAccountRetry}
}

func isAccountProtectionError(err error) bool {
	var failure *UpstreamFailoverError
	return errors.As(err, &failure) && failure.Reason == accountProtectionReason
}

// IsAccountRestError 表示账号已停止接单，应解除绑定并重新选号；存储故障不属于休息。
func IsAccountRestError(err error) bool {
	var failure *UpstreamFailoverError
	if !errors.As(err, &failure) {
		return false
	}
	code, _, ok := failure.AccountProtectionClientError()
	return ok && (code == "account_resting" || code == "account_random_draining" || code == "account_random_resting")
}

// 调度和发送前复核共用休息规则，不在这里检查 RPM，避免破坏原账号排队。
func (s *AccountTimeRotationService) checkSessionRest(ctx context.Context, account *Account) error {
	if err := s.checkRequestProtection(account, time.Now(), false); err != nil {
		return err
	}
	return s.checkRandomRest(ctx, account)
}

func protectionRootID(account *Account) int64 {
	if account.ParentAccountID != nil {
		return *account.ParentAccountID
	}
	return account.ID
}

// 过期配置不降级为无限制；启动尚未加载配置时也拒绝 OAuth 新请求。
func (s *AccountTimeRotationService) protectionConfig(now time.Time) (*AccountTimeRotationConfig, error) {
	if s == nil {
		return nil, nil
	}
	s.snapshotMu.RLock()
	defer s.snapshotMu.RUnlock()
	if s.snapshot == nil || now.Sub(s.refreshedAt) > 45*time.Second {
		return nil, accountProtectionError("account_protection_unavailable", "账号保护配置尚未就绪或已过期，请稍后重试", 15)
	}
	return s.snapshot, nil
}

func (s *AccountTimeRotationService) CheckRequestProtection(account *Account, now time.Time) error {
	return s.checkRequestProtection(account, now, true)
}

func (s *AccountTimeRotationService) checkRequestProtection(account *Account, now time.Time, checkLimit bool) error {
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil
	}
	cfg, err := s.protectionConfig(now)
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	id := protectionRootID(account)
	if cfg.Mode == "paced" {
		if pacedAccountSelected(cfg.Paced, id) {
			return cfg.Paced.scheduleError(now)
		}
		return nil
	}
	if value, ok := s.protectionBlocks.Load(id); checkLimit && ok {
		blocked := value.(accountProtectionBlock)
		if blocked.revision == cfg.Revision && now.Before(blocked.until) {
			return accountProtectionError("account_rate_limited", "账号滚动 60 秒总请求次数已达上限", int(blocked.until.Sub(now).Seconds())+1)
		}
	}
	if !cfg.StrictRest {
		return nil
	}
	local := now.In(accountRotationLocation)
	minute := local.Hour()*60 + local.Minute()
	for _, slot := range cfg.Slots {
		for _, selected := range slot.AccountIDs {
			if selected != id {
				continue
			}
			start, _ := rotationMinute(slot.Start, false)
			end, _ := rotationMinute(slot.End, true)
			active := minute >= start && minute < end
			if end < start {
				active = minute >= start || minute < end
			}
			if !active {
				return accountProtectionError("account_resting", "账号处于严格休息时段，请使用其他可用账号", 60)
			}
			return nil
		}
	}
	return nil
}

// Redis 时间与原子滑动窗口保证多进程、分组、用户及影子账号共用总次数。
// 仅保存最近一分钟的准入记录，失败尝试不返还次数。
var accountRequestWindow = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now-60000)
redis.call('ZREMRANGEBYSCORE', KEYS[3], '-inf', now)
local head = redis.call('ZRANGE', KEYS[3], 0, 0)
if #head > 0 and head[1] ~= ARGV[2] then return 1 end
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[1]) then
 local first = redis.call('ZRANGE', KEYS[1], 0, 0, 'WITHSCORES')
 return math.max(1, math.ceil((tonumber(first[2])+60000-now)/1000))
end
local seq = redis.call('INCR', KEYS[2])
redis.call('ZADD', KEYS[1], now, tostring(seq))
redis.call('PEXPIRE', KEYS[1], 61000)
redis.call('PEXPIRE', KEYS[2], 61000)
if ARGV[2] ~= '' then redis.call('ZREM', KEYS[3], ARGV[2]) end
return 0
`)

func (s *AccountTimeRotationService) AdmitRequest(ctx context.Context, account *Account) error {
	return s.admitRequest(ctx, account, "")
}

func (s *AccountTimeRotationService) admitRequest(ctx context.Context, account *Account, waiter string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkRequestProtection(account, time.Now(), false); err != nil {
		return err
	}
	if s == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth {
		return nil
	}
	cfg, err := s.protectionConfig(time.Now())
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if err := s.checkRandomRest(ctx, account); err != nil {
		return err
	}
	id := protectionRootID(account)
	for _, limit := range cfg.RequestLimits {
		if limit.AccountID != id {
			continue
		}
		if s.protectionRedis == nil {
			return accountProtectionError("account_protection_unavailable", "账号限速存储不可用，请稍后重试", 15)
		}
		key := fmt.Sprintf("account_request_limit:{%d}", id)
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		retry, err := accountRequestWindow.Run(checkCtx, s.protectionRedis, []string{key, key + ":seq", key + ":waiters"}, limit.RPM, waiter).Int()
		if err != nil {
			return accountProtectionError("account_protection_unavailable", "账号限速存储不可用，请稍后重试", 15)
		}
		if retry > 0 {
			s.protectionBlocks.Store(id, accountProtectionBlock{revision: cfg.Revision, until: time.Now().Add(time.Duration(retry) * time.Second)})
			return accountProtectionError("account_rate_limited", "账号滚动 60 秒总请求次数已达上限", retry)
		}
		break
	}
	return s.checkRequestProtection(account, time.Now(), false)
}

// 包装实际 HTTP 出口，每次重试单独计数，响应体关闭或读完后释放在途记录。
type protectedHTTPUpstream struct {
	inner    HTTPUpstream
	rotation *AccountTimeRotationService
	accounts AccountRepository
}

func NewProtectedHTTPUpstream(inner HTTPUpstream, rotation *AccountTimeRotationService, accounts AccountRepository) HTTPUpstream {
	return &protectedHTTPUpstream{inner: inner, rotation: rotation, accounts: accounts}
}

func (p *protectedHTTPUpstream) begin(ctx context.Context, id int64) (func(), error) {
	noop := func() {}
	if id <= 0 {
		return noop, nil
	}
	cfg, err := p.rotation.protectionConfig(time.Now())
	if err == nil && (cfg == nil || !cfg.Enabled || len(cfg.ProtectionAccountIDs()) == 0) {
		return noop, nil
	}
	account, lookupErr := p.accounts.GetByID(ctx, id)
	if lookupErr != nil || account == nil {
		return noop, accountProtectionError("account_protection_unavailable", "无法核实账号保护归属，请稍后重试", 15)
	}
	return p.rotation.BeginProtectedRequest(ctx, account)
}

func protectedResponse(resp *http.Response, err error, done func()) (*http.Response, error) {
	if err != nil || resp == nil || resp.Body == nil {
		done()
	} else {
		resp.Body = &accountProtectionBody{ReadCloser: resp.Body, done: done}
	}
	return resp, err
}

func (p *protectedHTTPUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	done, err := p.begin(req.Context(), id)
	if err != nil {
		return nil, err
	}
	resp, err := p.inner.Do(req, proxy, id, concurrency)
	return protectedResponse(resp, err, done)
}

func (p *protectedHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	done, err := p.begin(req.Context(), id)
	if err != nil {
		return nil, err
	}
	resp, err := p.inner.DoWithTLS(req, proxy, id, concurrency, profile)
	return protectedResponse(resp, err, done)
}

type accountProtectionBlock struct {
	revision int64
	until    time.Time
}

type AccountProtectionStatus struct {
	Paced           *AccountPacedStatus `json:"paced,omitempty"`
	AccountID       int64               `json:"account_id"`
	State           string              `json:"state"`
	RPM             int                 `json:"rpm"`
	Used            *int                `json:"used"`
	RandomState     string              `json:"random_state,omitempty"`
	RandomRestUntil *time.Time          `json:"random_rest_until,omitempty"`
	InFlight        int                 `json:"in_flight"`
}

var accountRequestUsage = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1])*1000 + math.floor(tonumber(t[2])/1000)
return redis.call('ZCOUNT', KEYS[1], '('..tostring(now-60000), '+inf')
`)

// 状态读取不领取次数、不延长限速窗口。
func (s *AccountTimeRotationService) ProtectionStatus(ctx context.Context, cfg *AccountTimeRotationConfig, account *Account, now time.Time) AccountProtectionStatus {
	if cfg.Mode == "paced" && pacedAccountSelected(cfg.Paced, protectionRootID(account)) {
		return s.pacedStatus(ctx, cfg, account, now)
	}
	result := AccountProtectionStatus{AccountID: account.ID, State: "available"}
	for _, limit := range cfg.RequestLimits {
		if limit.AccountID == account.ID {
			result.RPM = limit.RPM
			break
		}
	}
	if !cfg.Enabled {
		result.State = "disabled"
		return result
	}
	if err := s.CheckRequestProtection(account, now); err != nil {
		var failover *UpstreamFailoverError
		if errors.As(err, &failover) {
			code, _, _ := failover.AccountProtectionClientError()
			result.State = code
		} else {
			result.State = "account_protection_unavailable"
		}
	}
	if result.RPM > 0 {
		if s.protectionRedis == nil {
			result.State = "account_protection_unavailable"
			return result
		}
		readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		used, err := accountRequestUsage.Run(readCtx, s.protectionRedis, []string{fmt.Sprintf("account_request_limit:{%d}", account.ID)}).Int()
		if err != nil {
			result.State = "account_protection_unavailable"
			return result
		}
		result.Used = &used
		if used >= result.RPM && result.State == "available" {
			result.State = "account_rate_limited"
		}
	}
	if policy := s.randomRestPolicy(account); policy != nil {
		state, err := s.runRandomRest(ctx, protectionRootID(account), policy, "check", "")
		if err != nil {
			result.State = "account_protection_unavailable"
		} else {
			result.RandomState, result.InFlight = state.phase, state.active
			if state.phase == "resting" {
				result.RandomRestUntil = &state.until
			}
			if result.State == "available" || result.State == "account_rate_limited" {
				if state.phase == "draining" {
					result.State = "account_random_draining"
				}
				if state.phase == "resting" {
					result.State = "account_random_resting"
				}
			}
		}
	}
	return result
}

// 插件协议无法逐次拦截插件内部重试；受保护账号必须使用核心传输。
func (s *AccountTimeRotationService) checkPluginRequestProtection(account *Account) error {
	if s == nil || account == nil {
		return nil
	}
	cfg, err := s.protectionConfig(time.Now())
	if err != nil {
		return err
	}
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	id := protectionRootID(account)
	for _, protectedID := range cfg.ProtectionAccountIDs() {
		if protectedID == id {
			return accountProtectionError("account_protection_unavailable", "受保护账号暂不支持插件转发，请关闭该账号的插件路由", 15)
		}
	}
	return nil
}
