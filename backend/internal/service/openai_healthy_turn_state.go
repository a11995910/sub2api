package service

import (
	"context"
	"crypto/sha256"
	"fmt"
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
	// 这是本地缓存保留时间，不是上游状态的官方有效期。
	openAIHealthyTurnStateTTL        = 40 * time.Minute
	openAIHealthyTurnStateMaxEntries = 1024
	openAIHealthyTurnStateMaxBytes   = 16 << 10
	openAIHealthyTurnStateBudgetKey  = "openai_healthy_turn_state_budget"
)

func (a *Account) OpenAIHealthyTurnStateRecordEnabled() bool {
	if a == nil || a.Platform != PlatformOpenAI {
		return false
	}
	value, exists := a.Extra[openAIHealthyTurnStateRecordKey]
	if !exists {
		return true
	}
	enabled, _ := value.(bool)
	return enabled
}

func (a *Account) OpenAIHealthyTurnStateReplaceEnabled() bool {
	if a == nil || a.Platform != PlatformOpenAI {
		return false
	}
	enabled, _ := a.Extra[openAIHealthyTurnStateReplaceKey].(bool)
	return enabled
}

// ValidateOpenAIHealthyTurnStateExtra 保证两个独立开关只接受布尔值。
func ValidateOpenAIHealthyTurnStateExtra(extra map[string]any) error {
	for _, key := range []string{openAIHealthyTurnStateRecordKey, openAIHealthyTurnStateReplaceKey} {
		if value, exists := extra[key]; exists {
			if _, ok := value.(bool); !ok {
				return infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_SETTING", key+" 必须为布尔值")
			}
		}
	}
	return nil
}

type openAIHealthyTurnStateScope struct {
	accountID int64
	model     string
	// 凭据、出口、地址和传输方式只参与内存内散列，不能进入日志。
	identity [32]byte
}

type openAIHealthyTurnStateEntry struct {
	value     string
	expiresAt time.Time
}

type openAIHealthyTurnStateRejected struct {
	scope  openAIHealthyTurnStateScope
	digest [32]byte
}

// 缓存不持久化；领取即移除，防止并发请求同时使用同一条健康记录。
type openAIHealthyTurnStateCache struct {
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
	c.mu.Lock()
	defer c.mu.Unlock()
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
	c.mu.Lock()
	defer c.mu.Unlock()
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
	c.mu.Lock()
	delete(c.held, openAIHealthyTurnStateRejected{scope, sha256.Sum256([]byte(entry.value))})
	c.mu.Unlock()
}

func (c *openAIHealthyTurnStateCache) reject(scope openAIHealthyTurnStateScope, entry openAIHealthyTurnStateEntry) {
	if entry.value == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
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
	used map[int64]bool
}

type openAIHealthyTurnStateAttempt struct {
	cache           *openAIHealthyTurnStateCache
	scope           openAIHealthyTurnStateScope
	budget          *openAIHealthyTurnStateBudget
	clientContext   context.Context
	record, replace bool
	borrowed        openAIHealthyTurnStateEntry
}

func (s *OpenAIGatewayService) newOpenAIHealthyTurnStateAttempt(c *gin.Context, account *Account, model, endpoint, proxyURL string, headers http.Header) *openAIHealthyTurnStateAttempt {
	if s == nil || account == nil || account.ID <= 0 || (!account.OpenAIHealthyTurnStateRecordEnabled() && !account.OpenAIHealthyTurnStateReplaceEnabled()) {
		return nil
	}
	budget := &openAIHealthyTurnStateBudget{used: make(map[int64]bool)}
	clientCtx := context.Background()
	if c != nil {
		if existing, ok := c.Get(openAIHealthyTurnStateBudgetKey); ok {
			budget, _ = existing.(*openAIHealthyTurnStateBudget)
		}
		if budget == nil {
			budget = &openAIHealthyTurnStateBudget{used: make(map[int64]bool)}
		}
		c.Set(openAIHealthyTurnStateBudgetKey, budget)
		if c.Request != nil {
			clientCtx = c.Request.Context()
		}
	}
	identity := strings.Join([]string{endpoint, proxyURL, headers.Get("Authorization"), headers.Get("ChatGPT-Account-Id"), fmt.Sprint(account.Extra["codex_fingerprint_mode"])}, "\x00")
	return &openAIHealthyTurnStateAttempt{
		cache:  &s.openaiHealthyTurnStates,
		scope:  openAIHealthyTurnStateScope{account.ID, model, sha256.Sum256([]byte(identity))},
		budget: budget, clientContext: clientCtx,
		record: account.OpenAIHealthyTurnStateRecordEnabled(), replace: account.OpenAIHealthyTurnStateReplaceEnabled(),
	}
}

// claimRetry 只允许真实 HTTP／握手 429、503 在同账号补试一次，保留 Retry-After。
func (a *openAIHealthyTurnStateAttempt) claimRetry(ctx context.Context, status int, responseHeaders http.Header, current string) (bool, error) {
	if a == nil || !a.replace || (status != http.StatusTooManyRequests && status != http.StatusServiceUnavailable) {
		return false, nil
	}
	delay := openAIOAuth429SameAccountRetryDelay(responseHeaders)
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
	if a.budget.used[a.scope.accountID] {
		a.budget.mu.Unlock()
		return false, nil
	}
	entry, ok := a.cache.claim(a.scope, current)
	if !ok {
		a.budget.mu.Unlock()
		return false, nil
	}
	a.budget.used[a.scope.accountID] = true
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
	a.cache.reject(a.scope, a.borrowed)
	slog.Info("openai_healthy_turn_state", "action", "evict", "account_id", a.scope.accountID, "model", a.scope.model)
	a.borrowed = openAIHealthyTurnStateEntry{}
}

func (a *openAIHealthyTurnStateAttempt) restore() {
	if a == nil || a.borrowed.value == "" {
		return
	}
	a.cache.release(a.scope, a.borrowed)
	a.cache.store(a.scope, a.borrowed)
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
