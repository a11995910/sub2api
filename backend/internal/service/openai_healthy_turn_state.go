package service

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

const (
	openAIHealthyTurnStateRecordKey  = "openai_healthy_turn_state_record"
	openAIHealthyTurnStateReplaceKey = "openai_healthy_turn_state_replace"
	// 这是本地使用期限，不是上游状态的官方有效期。
	openAIHealthyTurnStateTTL        = 40 * time.Minute
	openAIHealthyTurnStateMaxEntries = 1024
	openAIHealthyTurnStateMaxBytes   = 16 << 10
	openAIHealthyTurnStateBudgetKey  = "openai_healthy_turn_state_budget"
)

func (a *Account) OpenAIHealthyTurnStateReplaceEnabled() bool {
	mode := a.OpenAITurnStateMode()
	return mode == OpenAITurnStateHealthyRetry || mode == OpenAITurnStateHealthyPreflight
}

// ValidateOpenAIHealthyTurnStateExtra 校验替换开关，并移除已停用的普通请求记录配置。
func ValidateOpenAIHealthyTurnStateExtra(extra map[string]any) error {
	delete(extra, openAIHealthyTurnStateRecordKey)
	if value, exists := extra[openAIHealthyTurnStateReplaceKey]; exists {
		if _, ok := value.(bool); !ok {
			return infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_SETTING", openAIHealthyTurnStateReplaceKey+" 必须为布尔值")
		}
	}
	return validateOpenAITurnStatePolicy(extra)
}

type openAIHealthyTurnStateScope struct {
	accountID int64
	model     string
	// 同账号、同模型共用记录；凭据、出口和传输方式不参与领取范围。
	identity  [32]byte
	transport string
	proxyID   int64
}

func (s openAIHealthyTurnStateScope) shared() openAIHealthyTurnStateScope {
	return openAIHealthyTurnStateScope{accountID: s.accountID, model: strings.TrimSpace(s.model)}
}

type openAIHealthyTurnStateEntry struct {
	value      string
	expiresAt  time.Time
	leaseToken string
}

type openAIHealthyTurnStateRejected struct {
	scope  openAIHealthyTurnStateScope
	digest [32]byte
}

// 正式服务使用持久化仓储；内存实现仅供独立单测使用。
type openAIHealthyTurnStateCache struct {
	repo     HealthyTurnStateRepository
	mu       sync.Mutex
	entries  map[openAIHealthyTurnStateScope]openAIHealthyTurnStateEntry
	rejected map[openAIHealthyTurnStateRejected]time.Time
	held     map[openAIHealthyTurnStateRejected]time.Time
}

func (c *openAIHealthyTurnStateCache) sweepLocked(now time.Time) {
	for key, value := range c.entries {
		if !now.Before(value.expiresAt) {
			delete(c.entries, key)
		}
	}
	for key, expires := range c.rejected {
		if !now.Before(expires) {
			delete(c.rejected, key)
		}
	}
	for key, expires := range c.held {
		if !now.Before(expires) {
			delete(c.held, key)
		}
	}
}

func (c *openAIHealthyTurnStateCache) store(scope openAIHealthyTurnStateScope, entry openAIHealthyTurnStateEntry) bool {
	if entry.value == "" || len(entry.value) > openAIHealthyTurnStateMaxBytes || strings.ContainsAny(entry.value, "\r\n") {
		return false
	}
	if c.repo != nil {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		stored, err := c.repo.Save(ctx, scope.persistent(), entry.persistent())
		healthyTurnStateStoreError("save", scope.accountID, err)
		return err == nil && stored
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	scope = scope.shared()
	now := time.Now()
	c.sweepLocked(now)
	if !now.Before(entry.expiresAt) {
		return false
	}
	if _, rejected := c.rejected[openAIHealthyTurnStateRejected{scope, sha256.Sum256([]byte(entry.value))}]; rejected {
		return false
	}
	if _, held := c.held[openAIHealthyTurnStateRejected{scope, sha256.Sum256([]byte(entry.value))}]; held {
		return false
	}
	if c.entries == nil {
		c.entries = make(map[openAIHealthyTurnStateScope]openAIHealthyTurnStateEntry)
	}
	if old, ok := c.entries[scope]; ok {
		// 同一个不透明值反复返回时，不能无限延长其本地寿命。
		if old.value == entry.value {
			return false
		}
		// 重试归还的旧记录不能覆盖更晚收到的新记录。
		if old.expiresAt.After(entry.expiresAt) {
			return false
		}
	} else if len(c.entries)+len(c.held) >= openAIHealthyTurnStateMaxEntries {
		return false
	}
	c.entries[scope] = entry
	return true
}

func (c *openAIHealthyTurnStateCache) claim(scope openAIHealthyTurnStateScope, current string) (openAIHealthyTurnStateEntry, bool) {
	if c.repo != nil {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		value, err := c.repo.Claim(ctx, scope.persistent(), current)
		healthyTurnStateStoreError("claim", scope.accountID, err)
		if err != nil || value == nil {
			return openAIHealthyTurnStateEntry{}, false
		}
		return openAIHealthyTurnStateEntry{value.Value, value.ExpiresAt, value.LeaseToken}, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	scope = scope.shared()
	c.sweepLocked(time.Now())
	entry, ok := c.entries[scope]
	if !ok || entry.value == strings.TrimSpace(current) {
		return openAIHealthyTurnStateEntry{}, false
	}
	delete(c.entries, scope)
	if c.held == nil {
		c.held = make(map[openAIHealthyTurnStateRejected]time.Time)
	}
	c.held[openAIHealthyTurnStateRejected{scope, sha256.Sum256([]byte(entry.value))}] = entry.expiresAt
	return entry, true
}

func (c *openAIHealthyTurnStateCache) release(scope openAIHealthyTurnStateScope, entry openAIHealthyTurnStateEntry) {
	if c.repo != nil {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		healthyTurnStateStoreError("release", scope.accountID, c.repo.Release(ctx, scope.persistent(), entry.persistent()))
		return
	}
	c.mu.Lock()
	scope = scope.shared()
	delete(c.held, openAIHealthyTurnStateRejected{scope, sha256.Sum256([]byte(entry.value))})
	c.mu.Unlock()
}

func (c *openAIHealthyTurnStateCache) reject(scope openAIHealthyTurnStateScope, entry openAIHealthyTurnStateEntry) {
	if entry.value == "" {
		return
	}
	if c.repo != nil {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		healthyTurnStateStoreError("reject", scope.accountID, c.repo.Reject(ctx, scope.persistent(), entry.value))
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	scope = scope.shared()
	c.sweepLocked(time.Now())
	if old, ok := c.entries[scope]; ok && old.value == entry.value {
		delete(c.entries, scope)
	}
	if c.rejected == nil {
		c.rejected = make(map[openAIHealthyTurnStateRejected]time.Time)
	}
	key := openAIHealthyTurnStateRejected{scope, sha256.Sum256([]byte(entry.value))}
	delete(c.held, key)
	if _, exists := c.rejected[key]; exists || len(c.rejected) < openAIHealthyTurnStateMaxEntries {
		c.rejected[key] = time.Now().Add(openAIHealthyTurnStateTTL)
	}
}

type openAIHealthyTurnStateBudget struct {
	mu   sync.Mutex
	used bool
}

type openAIHealthyTurnStateAttempt struct {
	cache         *openAIHealthyTurnStateCache
	scope         openAIHealthyTurnStateScope
	budget        *openAIHealthyTurnStateBudget
	clientContext context.Context
	replace       bool
	preflight     bool
	failClosed    bool
	borrowed      openAIHealthyTurnStateEntry
	sent          bool
	httpStatus    int
	startedAt     time.Time
	currentState  string
}

const openAIHealthyTurnStateFirstOutputLimit = 5 * time.Second

func (a *openAIHealthyTurnStateAttempt) markStarted() {
	if a != nil && a.startedAt.IsZero() {
		a.startedAt = time.Now()
	}
}

func (s *OpenAIGatewayService) newOpenAIHealthyTurnStateAttempt(c *gin.Context, account *Account, model, endpoint, proxyURL string, headers http.Header) *openAIHealthyTurnStateAttempt {
	if s == nil || account == nil || account.ID <= 0 || account.Platform != PlatformOpenAI {
		return nil
	}
	// 门票模式由独立链路处理，避免健康池的模型预读、淘汰和补试介入。
	if account.OpenAICodexTicketEnabled() {
		return nil
	}
	budget := &openAIHealthyTurnStateBudget{}
	clientCtx := context.Background()
	if c != nil {
		if existing, ok := c.Get(openAIHealthyTurnStateBudgetKey); ok {
			budget, _ = existing.(*openAIHealthyTurnStateBudget)
		}
		if budget == nil {
			budget = &openAIHealthyTurnStateBudget{}
		}
		c.Set(openAIHealthyTurnStateBudgetKey, budget)
		if c.Request != nil {
			clientCtx = c.Request.Context()
		}
	}
	transport, proxyID := "http", int64(0)
	if strings.HasPrefix(endpoint, "ws:") {
		transport = "websocket"
	}
	if account.ProxyID != nil {
		proxyID = *account.ProxyID
	}
	return &openAIHealthyTurnStateAttempt{
		cache:  &s.openaiHealthyTurnStates,
		scope:  openAIHealthyTurnStateScope{account.ID, model, [32]byte{}, transport, proxyID},
		budget: budget, clientContext: clientCtx, currentState: headers.Get(openAICodexTurnStateHeader),
		replace:    account.OpenAIHealthyTurnStateReplaceEnabled(),
		preflight:  account.OpenAITurnStateMode() == OpenAITurnStateHealthyPreflight,
		failClosed: account.OpenAIHealthyTurnStateFailClosed(),
	}
}

// claimPreflight 首发使用同一原子租约和一次预算；用过的头不在本次请求中再次补试。
func (a *openAIHealthyTurnStateAttempt) claimPreflight(ctx context.Context, headers http.Header) (bool, error) {
	if a == nil || !a.preflight {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := a.clientContext.Err(); err != nil {
		return false, err
	}
	a.budget.mu.Lock()
	defer a.budget.mu.Unlock()
	if a.budget.used {
		if a.failClosed {
			return false, wrapOpenAITurnStateUnavailable(ErrOpenAIHealthyTurnStateBudgetExhausted)
		}
		return false, nil
	}
	// 首发允许领取与客户端同值的头，以确保该值也有独占租约。
	entry, ok := a.cache.claim(a.scope, "")
	if !ok {
		if a.failClosed {
			return false, wrapOpenAITurnStateUnavailable(&OpenAIHealthyTurnStateUnavailableError{AccountID: a.scope.accountID, Model: a.scope.model})
		}
		return false, nil
	}
	a.borrowed = entry
	if err := ctx.Err(); err != nil {
		a.restore()
		return false, err
	}
	if err := a.clientContext.Err(); err != nil {
		a.restore()
		return false, err
	}
	if !a.started(0) {
		if a.failClosed {
			return false, wrapOpenAITurnStateUnavailable(&OpenAIHealthyTurnStateUnavailableError{AccountID: a.scope.accountID, Model: a.scope.model})
		}
		return false, nil
	}
	a.budget.used = true
	headers.Set(openAICodexTurnStateHeader, entry.value)
	return true, nil
}

// claimRetry 仅处理真实 HTTP／握手 429、503；模型不一致走独立入口。
func (a *openAIHealthyTurnStateAttempt) claimRetry(ctx context.Context, status int, responseHeaders http.Header, current string) (bool, error) {
	if a == nil || !a.replace || (status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable) {
		return false, nil
	}
	return a.claimReplacement(ctx, status, responseHeaders, current)
}

func (a *openAIHealthyTurnStateAttempt) claimModelMismatchRetry(ctx context.Context, headers http.Header, current string) (bool, error) {
	if a == nil || !a.replace {
		return false, nil
	}
	return a.claimReplacement(ctx, http.StatusBadGateway, headers, current)
}

func (a *openAIHealthyTurnStateAttempt) claimReplacement(ctx context.Context, status int, responseHeaders http.Header, current string) (bool, error) {
	// 状态头替换仍独立遵守上游等待时间，不复用普通重试的截断策略。
	delay := time.Second
	now := time.Now()
	if resetAt := parseRetryAfterResetTime(responseHeaders, now); resetAt != nil {
		if wait := resetAt.Sub(now); wait > delay {
			delay = wait
		}
	}
	if delay >= openAIOAuth429RetryWindow {
		return false, nil
	}
	for _, candidate := range []context.Context{ctx, a.clientContext} {
		if err := candidate.Err(); err != nil {
			return false, err
		}
		if deadline, ok := candidate.Deadline(); ok && !time.Now().Add(delay).Before(deadline) {
			return false, nil
		}
	}
	a.budget.mu.Lock()
	if a.budget.used {
		a.budget.mu.Unlock()
		return false, nil
	}
	entry, ok := a.cache.claim(a.scope, current)
	if !ok {
		a.budget.mu.Unlock()
		return false, nil
	}
	a.budget.used = true
	a.budget.mu.Unlock()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		a.cache.release(a.scope, entry)
		a.cache.store(a.scope, entry)
		return false, ctx.Err()
	case <-a.clientContext.Done():
		a.cache.release(a.scope, entry)
		a.cache.store(a.scope, entry)
		return false, a.clientContext.Err()
	case <-timer.C:
	}
	if !time.Now().Before(entry.expiresAt) {
		a.cache.release(a.scope, entry)
		return false, nil
	}
	a.borrowed = entry
	slog.Info("openai_healthy_turn_state", "action", "replace", "account_id", a.scope.accountID, "model", a.scope.model, "status", status)
	return true, nil
}

func (a *openAIHealthyTurnStateAttempt) failed() {
	if a == nil || a.borrowed.value == "" {
		return
	}
	a.completed(false)
	slog.Info("openai_healthy_turn_state", "action", "evict", "account_id", a.scope.accountID, "model", a.scope.model)
	a.borrowed = openAIHealthyTurnStateEntry{}
}

func (a *openAIHealthyTurnStateAttempt) restore() {
	if a == nil || a.borrowed.value == "" {
		return
	}
	a.cache.release(a.scope, a.borrowed)
	if a.cache.repo == nil {
		a.cache.store(a.scope, a.borrowed)
	}
	a.borrowed = openAIHealthyTurnStateEntry{}
}

type openAIHealthyTurnStateContextKey struct{}

func (s *OpenAIGatewayService) prepareOpenAIHealthyTurnStateRequest(c *gin.Context, account *Account, req *http.Request, model string) *http.Request {
	// compact、搜索和用量接口不作为首字健康来源，也不使用推理请求的状态。
	if req == nil || req.URL == nil || !strings.HasSuffix(strings.TrimRight(req.URL.Path, "/"), "/responses") {
		return req
	}
	proxyURL := ""
	if account != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	attempt := s.newOpenAIHealthyTurnStateAttempt(c, account, model, "http:"+req.URL.String(), proxyURL, req.Header)
	if attempt == nil {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), openAIHealthyTurnStateContextKey{}, attempt))
}

func openAIHealthyTurnStateAttemptFromRequest(req *http.Request) *openAIHealthyTurnStateAttempt {
	if req == nil {
		return nil
	}
	attempt, _ := req.Context().Value(openAIHealthyTurnStateContextKey{}).(*openAIHealthyTurnStateAttempt)
	return attempt
}
