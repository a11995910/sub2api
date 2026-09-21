package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 采集观测仅保存在进程内，绝不存储门票、代理地址或原始错误。
type openAICodexTicketHarvestEntry struct {
	collecting    bool
	lastAttemptAt time.Time
	lastResult    string
	httpStatus    int
	attemptIndex  int
	attemptTotal  int
}

type openAICodexTicketHarvestState struct {
	mu           sync.RWMutex
	entries      map[string]openAICodexTicketHarvestEntry
	cycleRunning bool
	nextCycleAt  time.Time
}

func (h *openAICodexTicketHarvestState) beginCycle() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cycleRunning, h.nextCycleAt = true, time.Time{}
}

func (h *openAICodexTicketHarvestState) endCycle() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cycleRunning = false
}

func (h *openAICodexTicketHarvestState) scheduleNextCycle(at time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextCycleAt = at
}

func (h *openAICodexTicketHarvestState) beginAttempt(accountID int64, model string, index, total int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = make(map[string]openAICodexTicketHarvestEntry)
	}
	h.entries[openAICodexTicketKey(accountID, model)] = openAICodexTicketHarvestEntry{
		collecting: true, lastAttemptAt: time.Now(), attemptIndex: index, attemptTotal: total,
	}
}

func (h *openAICodexTicketHarvestState) finishAttempt(accountID int64, model, result string, httpStatus int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := openAICodexTicketKey(accountID, model)
	entry := h.entries[key]
	entry.collecting, entry.lastResult, entry.httpStatus = false, result, httpStatus
	h.entries[key] = entry
}

func (h *openAICodexTicketHarvestState) recordSourceFailure(accountID int64, model string, startedAt time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.entries == nil {
		h.entries = make(map[string]openAICodexTicketHarvestEntry)
	}
	h.entries[openAICodexTicketKey(accountID, model)] = openAICodexTicketHarvestEntry{lastAttemptAt: startedAt, lastResult: "proxy_extract_failed"}
}

func (h *openAICodexTicketHarvestState) snapshot(accountID int64, model string) (openAICodexTicketHarvestEntry, time.Time) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	next := h.nextCycleAt
	if h.cycleRunning {
		next = time.Time{}
	}
	return h.entries[openAICodexTicketKey(accountID, model)], next
}

// OpenAICodexTicketStatuses 合并持久门票与采集器的实时摘要，供管理接口使用。
// next_attempt_at 只代表已安排的下一轮预计时间，轮内排队仍可能延后实际请求。
func (s *OpenAIGatewayService) OpenAICodexTicketStatuses(ctx context.Context, account *Account, now time.Time) []OpenAICodexTicketStatus {
	if s == nil || !isOpenAICodexTicketAccount(account) {
		return nil
	}
	cfg := s.openAICodexTicketConfig()
	cfg.Enabled = s.openAICodexTicketEnabledContext(ctx)
	if !cfg.Enabled {
		return nil
	}
	statuses := OpenAICodexTicketStatuses(account, cfg, now)
	configured := s.openAICodexTicketHarvestSourceConfigured(ctx)
	s.openaiCodexTicketLifecycleMu.Lock()
	stopped := s.openaiCodexTicketStopped
	s.openaiCodexTicketLifecycleMu.Unlock()
	for i := range statuses {
		status := &statuses[i]
		ticket := s.lookupOpenAICodexTicket(account, status.Model)
		status.Ready = ticket.valid(now, cfg.TargetLength)
		status.Blocked = openAICodexTicketFailClosed(account, cfg.FailClosed) && !status.Ready
		status.Length, status.RemainingSeconds, status.ExpiresAt = 0, 0, nil
		needsRefresh := true
		if status.Ready {
			status.Length = ticket.Length
			status.RemainingSeconds = int64(ticket.ExpiresAt.Sub(now) / time.Second)
			expiresAt := ticket.ExpiresAt
			status.ExpiresAt = &expiresAt
			refreshDue := expiresAt
			status.RefreshDueAt = &refreshDue
			needsRefresh = !now.Before(refreshDue)
		}
		entry, nextCycle := s.openaiCodexTicketHarvest.snapshot(account.ID, status.Model)
		if !entry.lastAttemptAt.IsZero() {
			attemptAt := entry.lastAttemptAt
			status.LastAttemptAt = &attemptAt
		}
		status.LastResult, status.LastHTTPStatus = entry.lastResult, entry.httpStatus
		status.AttemptIndex, status.AttemptTotal = entry.attemptIndex, entry.attemptTotal
		status.RetryIntervalSeconds = cfg.HarvestProbeIntervalSeconds
		status.AttemptTimeoutSeconds = cfg.HarvestAttemptTimeoutSeconds
		switch {
		case account.Status != StatusActive:
			status.HarvestStatus, status.PauseReason = "paused", "account_inactive"
		case !configured:
			status.HarvestStatus, status.PauseReason = "paused", "proxy_not_configured"
		case stopped || s.httpUpstream == nil:
			status.HarvestStatus, status.PauseReason = "paused", "collector_unavailable"
		case entry.collecting:
			status.HarvestStatus = "collecting"
		case !needsRefresh:
			status.HarvestStatus = "ready"
		default:
			status.HarvestStatus = "waiting"
			if nextCycle.After(now) {
				status.NextAttemptAt = &nextCycle
			}
		}
	}
	return statuses
}

func classifyOpenAICodexTicketProbeError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &netErr) && netErr.Timeout() {
		return "request_timeout"
	}
	// 只输出固定类别；匹配用的原文不得进入接口或日志。
	message := strings.ToLower(err.Error())
	for _, marker := range []string{"proxyconnect", "socks", "proxy authentication", "proxy error"} {
		if strings.Contains(message, marker) {
			return "proxy_error"
		}
	}
	return "upstream_error"
}

func classifyOpenAICodexTicketProbeResponse(status int) string {
	switch status {
	case http.StatusOK:
		return "invalid_format"
	case http.StatusUnauthorized:
		return "auth_failed"
	case http.StatusProxyAuthRequired:
		return "proxy_error"
	default:
		return "upstream_error"
	}
}
