package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestTurnStateUnavailableClientErrorsPreserveLocalReason(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cause   error
		code    string
		message string
	}{
		{"库存不足", ErrOpenAIHealthyTurnStateUnavailable, "turn_state_unavailable", openAITurnStateUnavailableMessage},
		{"预算耗尽", ErrOpenAIHealthyTurnStateBudgetExhausted, "turn_state_budget_exhausted", ErrOpenAIHealthyTurnStateBudgetExhausted.Error()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := wrapOpenAITurnStateUnavailable(fmt.Errorf("构造请求失败: %w", tc.cause))
			require.ErrorIs(t, err, tc.cause)
			require.Same(t, err, wrapOpenAITurnStateUnavailable(err))
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.Equal(t, http.StatusServiceUnavailable, failover.StatusCode)
			code, message, ok := failover.OpenAITurnStateClientError()
			require.True(t, ok)
			require.Equal(t, tc.code, code)
			require.Equal(t, tc.message, message)
			require.Equal(t, tc.code, gjson.GetBytes(failover.ResponseBody, "error.code").String())
			require.Equal(t, tc.message, gjson.GetBytes(failover.ResponseBody, "error.message").String())
			require.Equal(t, "server_error", gjson.GetBytes(failover.ResponseBody, "error.type").String())
			require.True(t, failover.ShouldRetryNextAccount())
			require.False(t, failover.ShouldReportAccountScheduleFailure())
			for _, wrapped := range []error{err, wrapOpenAIWSFallback("acquire_conn", err)} {
				reason, retry := classifyOpenAIWSReconnectReason(wrapped)
				require.Equal(t, tc.code, reason)
				require.False(t, retry)
			}
		})
	}
	for _, failover := range []*UpstreamFailoverError{nil, {StatusCode: http.StatusServiceUnavailable}} {
		_, _, ok := failover.OpenAITurnStateClientError()
		require.False(t, ok)
	}
	_, message, ok := (&UpstreamFailoverError{Reason: openAITurnStateUnavailableReason, ClientMessage: "内部敏感信息"}).OpenAITurnStateClientError()
	require.True(t, ok)
	require.Equal(t, openAITurnStateUnavailableMessage, message)
	reason, retry := classifyOpenAIWSReconnectReason(wrapOpenAIWSFallback("acquire_conn", errors.New("网络暂时不可用")))
	require.Equal(t, "acquire_conn", reason)
	require.True(t, retry)
}

func TestTurnStateUnavailablePreservesFailoverAndCause(t *testing.T) {
	cause := errors.New("本地库存为空")
	err := fmt.Errorf("构造请求失败: %w", wrapOpenAITurnStateUnavailable(cause))
	require.ErrorIs(t, err, cause)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Equal(t, http.StatusServiceUnavailable, failover.ClientStatusCode)
	require.True(t, failover.ShouldRetryNextAccount())
	require.False(t, failover.RetryableOnSameAccount)
	require.False(t, failover.ShouldReportAccountScheduleFailure())
	// nil 账号与上下文证明缺票不会进入需要账号/代理的网络故障分支。
	svc := &OpenAIGatewayService{}
	require.Same(t, err, svc.handleOpenAIUpstreamTransportError(context.Background(), nil, nil, err, false))
}

func TestHealthyPreflightSchedulingUsesMappedModelAndSkipsCompact(t *testing.T) {
	svc := &OpenAIGatewayService{}
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Extra:       map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight, OpenAIHealthyTurnStateFailClosedKey: true},
		Credentials: map[string]any{"model_mapping": map[string]any{"alias": "gpt-5.5"}},
	}
	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", false))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", true))
	require.True(t, svc.openaiHealthyTurnStates.store(openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-5.5"}, openAIHealthyTurnStateEntry{value: "healthy", expiresAt: time.Now().Add(time.Minute)}))
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", false))
	_, claimed := svc.openaiHealthyTurnStates.claim(openAIHealthyTurnStateScope{accountID: account.ID, model: "gpt-5.5"}, "")
	require.True(t, claimed)
	require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", false))
	account.Extra[OpenAIHealthyTurnStateFailClosedKey] = false
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", false))
	account.Extra[OpenAIHealthyTurnStateFailClosedKey] = true
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateHealthyRetry
	require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "alias", false))
}
