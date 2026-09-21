package service

import (
	"context"
	"errors"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	OpenAITurnStateModeKey              = "openai_turn_state_mode"
	OpenAITurnStateOff                  = "off"
	OpenAITurnStateHealthyRetry         = "healthy_retry"
	OpenAITurnStateHealthyPreflight     = "healthy_preflight"
	OpenAITurnStateCodexTicket          = "codex_ticket"
	OpenAIHealthyTurnStateFailClosedKey = "openai_healthy_turn_state_fail_closed"
)

// 新建独立 OpenAI OAuth 类账号默认关闭状态头策略；显式模式和旧开关始终优先。
// 仅供创建入口调用，读取或更新已有账号时不得重新套用默认值。
func applyOpenAITurnStateCreateDefault(account *Account) {
	if !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return
	}
	if _, exists := account.Extra[OpenAITurnStateModeKey]; exists {
		return
	}
	if _, exists := account.Extra[openAIHealthyTurnStateReplaceKey]; exists {
		return
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateOff
	account.Extra[openAIHealthyTurnStateReplaceKey] = false
}

// OpenAITurnStateMode 保留旧开关语义；无法识别的存量模式按关闭处理。
func (a *Account) OpenAITurnStateMode() string {
	if a == nil || a.Platform != PlatformOpenAI {
		return OpenAITurnStateOff
	}
	if raw, exists := a.Extra[OpenAITurnStateModeKey]; exists {
		mode, ok := raw.(string)
		if ok && validOpenAITurnStateMode(mode) {
			return mode
		}
		return OpenAITurnStateOff
	}
	if enabled, _ := a.Extra[openAIHealthyTurnStateReplaceKey].(bool); enabled {
		return OpenAITurnStateHealthyRetry
	}
	return OpenAITurnStateOff
}

func validOpenAITurnStateMode(mode string) bool {
	switch mode {
	case OpenAITurnStateOff, OpenAITurnStateHealthyRetry, OpenAITurnStateHealthyPreflight, OpenAITurnStateCodexTicket:
		return true
	}
	return false
}

func (a *Account) OpenAICodexTicketEnabled() bool {
	return a.OpenAITurnStateMode() == OpenAITurnStateCodexTicket
}

func (a *Account) OpenAIHealthyTurnStateFailClosed() bool {
	if a.OpenAITurnStateMode() != OpenAITurnStateHealthyPreflight {
		return false
	}
	// 首发模式未设置缺头策略时默认暂停；显式关闭仍允许原请求继续。
	enabled, configured := a.Extra[OpenAIHealthyTurnStateFailClosedKey].(bool)
	return !configured || enabled
}

func validateOpenAITurnStatePolicy(extra map[string]any) error {
	if raw, exists := extra[OpenAIHealthyTurnStateFailClosedKey]; exists {
		if _, ok := raw.(bool); !ok {
			return infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_SETTING", OpenAIHealthyTurnStateFailClosedKey+" 必须为布尔值")
		}
	}
	if raw, exists := extra[OpenAITurnStateModeKey]; exists {
		mode, ok := raw.(string)
		if !ok || !validOpenAITurnStateMode(mode) {
			return infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_SETTING", "状态头模式必须是 off、healthy_retry、healthy_preflight 或 codex_ticket")
		}
		extra[openAIHealthyTurnStateReplaceKey] = mode == OpenAITurnStateHealthyRetry || mode == OpenAITurnStateHealthyPreflight
	} else if enabled, exists := extra[openAIHealthyTurnStateReplaceKey].(bool); exists {
		// 旧客户端的局部更新也同步模式，避免数据库键级合并保留旧模式导致开关失效。
		extra[OpenAITurnStateModeKey] = OpenAITurnStateOff
		if enabled {
			extra[OpenAITurnStateModeKey] = OpenAITurnStateHealthyRetry
		}
	}
	return nil
}

var ErrOpenAIHealthyTurnStateUnavailable = errors.New("当前账号模型没有可领取的健康状态头")
var ErrOpenAIHealthyTurnStateBudgetExhausted = errors.New("本次请求在当前账号的健康头尝试次数已用完")

// OpenAIHealthyTurnStateUnavailableError 供调度层换号；不包含状态值或底层存储错误。
type OpenAIHealthyTurnStateUnavailableError struct {
	AccountID int64
	Model     string
}

func (e *OpenAIHealthyTurnStateUnavailableError) Error() string {
	return fmt.Sprintf("账号 %d 的模型 %s 没有可领取的健康状态头", e.AccountID, e.Model)
}
func (e *OpenAIHealthyTurnStateUnavailableError) Unwrap() error {
	return ErrOpenAIHealthyTurnStateUnavailable
}

// HasOpenAIHealthyTurnState 只检查库存；实际发送前仍须原子领取，以处理并发竞争。
func (s *OpenAIGatewayService) HasOpenAIHealthyTurnState(account *Account, model string) bool {
	if s == nil || account == nil {
		return false
	}
	_, ok := s.openaiHealthyTurnStates.get(openAIHealthyTurnStateScope{accountID: account.ID, model: model})
	return ok
}

type openAITurnStateProbeContextKey struct{}

// WithOpenAITurnStateProbe 标记独立采集，所有状态头注入器都必须跳过。
func WithOpenAITurnStateProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, openAITurnStateProbeContextKey{}, true)
}
func IsOpenAITurnStateProbe(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(openAITurnStateProbeContextKey{}).(bool)
	return enabled
}
