package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// 存储接口中的头仅在服务端使用，不作为管理接口响应。
type HealthyTurnStateScope struct {
	AccountID             int64
	Key, Model, Transport string
	ProxyID               int64
}

type HealthyTurnStateValue struct {
	Value      string
	ExpiresAt  time.Time
	LeaseToken string
}

type HealthyTurnStateRecord struct {
	Model          string     `json:"model"`
	Transport      string     `json:"transport"`
	ProxyID        int64      `json:"proxy_id"`
	Status         string     `json:"status"`
	Captures       int64      `json:"captures"`
	Attempts       int64      `json:"attempts"`
	Successes      int64      `json:"successes"`
	Failures       int64      `json:"failures"`
	ExpiresAt      *time.Time `json:"expires_at"`
	LastCapturedAt *time.Time `json:"last_captured_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	LastSuccessAt  *time.Time `json:"last_success_at"`
	LastFailureAt  *time.Time `json:"last_failure_at"`
	LastOutcome    string     `json:"last_outcome"`
	LastHTTPStatus int        `json:"last_http_status"`
}

type HealthyTurnStateProbeLog struct {
	Model      string    `json:"model"`
	Transport  string    `json:"transport"`
	ProxyID    int64     `json:"proxy_id"`
	Status     string    `json:"status"`
	HTTPStatus int       `json:"http_status"`
	CreatedAt  time.Time `json:"created_at"`
}

type HealthyTurnStateStats struct {
	Available int64                      `json:"available"`
	InUse     int64                      `json:"in_use"`
	Captures  int64                      `json:"captures"`
	Attempts  int64                      `json:"attempts"`
	Successes int64                      `json:"successes"`
	Failures  int64                      `json:"failures"`
	Records   []HealthyTurnStateRecord   `json:"records"`
	Probes    []HealthyTurnStateProbeLog `json:"probes"`
}

type HealthyTurnStateRepository interface {
	Save(context.Context, HealthyTurnStateScope, HealthyTurnStateValue) (bool, error)
	Get(context.Context, HealthyTurnStateScope) (*HealthyTurnStateValue, error)
	Claim(context.Context, HealthyTurnStateScope, string) (*HealthyTurnStateValue, error)
	Release(context.Context, HealthyTurnStateScope, HealthyTurnStateValue) error
	Start(context.Context, HealthyTurnStateScope, HealthyTurnStateValue, int) error
	Complete(context.Context, HealthyTurnStateScope, HealthyTurnStateValue, bool, int) error
	Reject(context.Context, HealthyTurnStateScope, string) error
	RecordProbe(context.Context, int64, HealthyTurnStateProbeLog) error
	Stats(context.Context, int64) (*HealthyTurnStateStats, error)
}

func (s openAIHealthyTurnStateScope) persistent() HealthyTurnStateScope {
	// 状态头属于 OpenAI 全局共享池，账号、模型、传输方式和代理不参与领取范围。
	return HealthyTurnStateScope{Key: "openai_global"}
}

func (e openAIHealthyTurnStateEntry) persistent() HealthyTurnStateValue {
	return HealthyTurnStateValue{e.value, e.expiresAt, e.leaseToken}
}

// 流结束或客户端取消后仍需保存结果；单次数据库操作最多等待两秒。
func healthyTurnStateStoreContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Second)
}

func healthyTurnStateStoreError(operation string, accountID int64, err error) {
	if err != nil {
		// 数据库和加密错误可能包含参数，日志只输出固定操作名。
		slog.Warn("openai_healthy_turn_state_storage_failed", "operation", operation, "account_id", accountID)
	}
}

func (c *openAIHealthyTurnStateCache) get(scope openAIHealthyTurnStateScope) (openAIHealthyTurnStateEntry, bool) {
	if c.repo != nil {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		value, err := c.repo.Get(ctx, scope.persistent())
		healthyTurnStateStoreError("get", scope.accountID, err)
		if err != nil || value == nil {
			return openAIHealthyTurnStateEntry{}, false
		}
		return openAIHealthyTurnStateEntry{value.Value, value.ExpiresAt, value.LeaseToken}, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepLocked(time.Now())
	value, ok := c.entries[scope.shared()]
	return value, ok
}

func (a *openAIHealthyTurnStateAttempt) started(status int) bool {
	if a == nil || a.borrowed.value == "" {
		return false
	}
	if a.sent {
		return true
	}
	// 替换请求在等待 429／503 后重新开始计时。
	a.startedAt = time.Now()
	a.httpStatus = status
	if a.cache.repo != nil {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		err := a.cache.repo.Start(ctx, a.scope.persistent(), a.borrowed.persistent(), status)
		healthyTurnStateStoreError("start", a.scope.accountID, err)
		if err != nil {
			a.restore()
			return false
		}
	}
	a.sent = true
	return true
}

func (a *openAIHealthyTurnStateAttempt) completed(success bool) {
	if a == nil || a.borrowed.value == "" {
		return
	}
	if a.cache.repo != nil && a.sent {
		ctx, cancel := healthyTurnStateStoreContext()
		defer cancel()
		healthyTurnStateStoreError("complete", a.scope.accountID, a.cache.repo.Complete(ctx, a.scope.persistent(), a.borrowed.persistent(), success, a.httpStatus))
		a.borrowed = openAIHealthyTurnStateEntry{}
		return
	}
	if success {
		a.restore()
	} else {
		a.cache.reject(a.scope, a.borrowed)
		a.borrowed = openAIHealthyTurnStateEntry{}
	}
}

func (s *AccountTestService) HealthyTurnStateStats(ctx context.Context, accountID int64) (*HealthyTurnStateStats, error) {
	if s.openaiGatewayService == nil || s.openaiGatewayService.openaiHealthyTurnStates.repo == nil {
		return nil, fmt.Errorf("健康状态头持久化服务暂不可用")
	}
	return s.openaiGatewayService.openaiHealthyTurnStates.repo.Stats(ctx, accountID)
}
