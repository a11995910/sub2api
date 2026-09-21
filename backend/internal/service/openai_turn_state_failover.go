package service

import (
	"encoding/json"
	"errors"
	"net/http"
)

const openAITurnStateUnavailableReason GatewayFailureReason = "openai_turn_state_unavailable"

const openAITurnStateUnavailableMessage = "当前账号模型暂时没有可用状态头，请稍后重试"

// OpenAITurnStateClientError 只返回本地定义的状态头错误，避免被上游错误映射覆盖。
func (e *UpstreamFailoverError) OpenAITurnStateClientError() (code, message string, ok bool) {
	if e == nil || e.Reason != openAITurnStateUnavailableReason {
		return "", "", false
	}
	return "turn_state_unavailable", openAITurnStateUnavailableMessage, true
}

// 本地缺票不属于代理故障或上游限流，允许换号但不扣减账号健康度。
type openAITurnStateFailoverError struct {
	cause    error
	failover *UpstreamFailoverError
}

func (e *openAITurnStateFailoverError) Error() string   { return e.failover.ClientMessage }
func (e *openAITurnStateFailoverError) Unwrap() []error { return []error{e.cause, e.failover} }

func wrapOpenAITurnStateUnavailable(err error) error {
	if err == nil {
		return nil
	}
	var existing *openAITurnStateFailoverError
	if errors.As(err, &existing) {
		return err
	}
	code, message := "turn_state_unavailable", openAITurnStateUnavailableMessage
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"type": "server_error", "code": code, "message": message}})
	return &openAITurnStateFailoverError{cause: err, failover: &UpstreamFailoverError{
		StatusCode:        http.StatusServiceUnavailable,
		ResponseBody:      body,
		Scope:             GatewayFailureScopeAccount,
		Reason:            openAITurnStateUnavailableReason,
		NextAccountAction: NextAccountRetry,
		ClientStatusCode:  http.StatusServiceUnavailable,
		ClientMessage:     message,
	}}
}
