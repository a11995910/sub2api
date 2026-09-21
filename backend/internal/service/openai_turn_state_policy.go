package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	OpenAITurnStateModeKey     = "openai_turn_state_mode"
	OpenAITurnStateOff         = "off"
	OpenAITurnStateCodexTicket = "codex_ticket"
)

// 新建独立 OpenAI OAuth 类账号默认关闭门票策略。
func applyOpenAITurnStateCreateDefault(account *Account) {
	if !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return
	}
	if _, exists := account.Extra[OpenAITurnStateModeKey]; exists {
		return
	}
	if account.Extra == nil {
		account.Extra = make(map[string]any)
	}
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateOff
}

// 已移除的健康头模式与未知存量值按关闭处理，不自动启用门票采集。
func (a *Account) OpenAITurnStateMode() string {
	if a == nil || a.Platform != PlatformOpenAI {
		return OpenAITurnStateOff
	}
	if mode, _ := a.Extra[OpenAITurnStateModeKey].(string); mode == OpenAITurnStateCodexTicket {
		return mode
	}
	return OpenAITurnStateOff
}

func (a *Account) OpenAICodexTicketEnabled() bool {
	return a.OpenAITurnStateMode() == OpenAITurnStateCodexTicket
}

// 清除旧客户端携带的废弃开关，拒绝重新启用已移除的模式。
func validateOpenAITurnStatePolicy(extra map[string]any) error {
	for _, key := range []string{"openai_healthy_turn_state_record", "openai_healthy_turn_state_replace", "openai_healthy_turn_state_fail_closed"} {
		delete(extra, key)
	}
	if raw, exists := extra[OpenAITurnStateModeKey]; exists {
		mode, ok := raw.(string)
		if !ok || (mode != OpenAITurnStateOff && mode != OpenAITurnStateCodexTicket) {
			return infraerrors.BadRequest("INVALID_TURN_STATE_SETTING", "状态头模式仅支持 off（关闭）或 codex_ticket（292/332 门票）")
		}
	}
	return nil
}

type openAITurnStateProbeContextKey struct{}

// WithOpenAITurnStateProbe 标记独立采集，避免注入已有门票。
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
