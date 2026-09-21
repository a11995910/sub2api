package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTurnStateRetiredModesDoNotInjectRetryOrBlock(t *testing.T) {
	for _, mode := range []string{"healthy_retry", "healthy_preflight", ""} {
		t.Run(mode, func(t *testing.T) {
			account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Extra: map[string]any{
					"openai_healthy_turn_state_replace":     true,
					"openai_healthy_turn_state_fail_closed": true,
				},
			}
			if mode != "" {
				account.Extra[OpenAITurnStateModeKey] = mode
			}
			calls := 0
			upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "客户端原有状态", req.Header.Get(openAICodexTurnStateHeader))
				return &http.Response{StatusCode: http.StatusServiceUnavailable}, nil
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			require.Equal(t, OpenAITurnStateOff, account.OpenAITurnStateMode())
			require.False(t, svc.isOpenAIAccountRequestRuntimeBlocked(account, "gpt-6-astra"))
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
			require.NoError(t, err)
			req.Header.Set(openAICodexTurnStateHeader, "客户端原有状态")
			response, err := svc.doOpenAIUpstream(req, "", account)
			require.NoError(t, err)
			require.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
			require.Equal(t, 1, calls, "旧模式不再增加健康头补试")
		})
	}
}

func TestTurnStatePolicyRejectsRemovedModesAndCleansLegacyFlags(t *testing.T) {
	for _, mode := range []any{"healthy_retry", "healthy_preflight", "unknown", true, nil} {
		require.Error(t, validateOpenAITurnStatePolicy(map[string]any{OpenAITurnStateModeKey: mode}))
	}
	for _, mode := range []string{OpenAITurnStateOff, OpenAITurnStateCodexTicket} {
		extra := map[string]any{OpenAITurnStateModeKey: mode, "openai_codex_ticket_fail_closed": false,
			"openai_healthy_turn_state_record": true, "openai_healthy_turn_state_replace": true,
			"openai_healthy_turn_state_fail_closed": true,
		}
		require.NoError(t, validateOpenAITurnStatePolicy(extra))
		require.Equal(t, map[string]any{OpenAITurnStateModeKey: mode, "openai_codex_ticket_fail_closed": false}, extra)
	}
}
