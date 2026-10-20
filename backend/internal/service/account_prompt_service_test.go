//go:build unit

package service

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestPromptAccountUsesSelectedIdentityAndActualQuestion(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
		t.Run(model, func(t *testing.T) {
			account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3,
				Credentials: map[string]any{"access_token": "fixture-token", "chatgpt_account_id": "fixture-workspace"}}
			repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{71: account}}}
			resp := newJSONResponse(http.StatusOK, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"原始回答\"}\n\ndata: {\"type\":\"response.completed\"}\n\n")
			upstream := &queuedHTTPUpstream{responses: []*http.Response{resp}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
			c, recorder := newTestContext()
			require.NoError(t, svc.TestPromptAccountConnection(c, 71, model, "固定逻辑题\n请说明理由", "high"))
			require.Len(t, upstream.requests, 1)
			request := upstream.requests[0]
			require.Equal(t, "Bearer fixture-token", request.Header.Get("Authorization"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.Equal(t, "固定逻辑题\n请说明理由", gjson.GetBytes(body, "input.0.content.0.text").String())
			require.Equal(t, "high", gjson.GetBytes(body, "reasoning.effort").String())
			require.Equal(t, model, gjson.GetBytes(body, "model").String())
			require.Contains(t, recorder.Body.String(), `"prompt_mode":true`)
			require.Contains(t, recorder.Body.String(), "原始回答")
			require.Contains(t, recorder.Body.String(), `"test_complete"`)
			require.Zero(t, repo.clearedErrorID)
		})
	}
}

func TestPromptAccountRejectsWrongPlatformBeforeSending(t *testing.T) {
	account := &Account{ID: 71, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{71: account}}}
	upstream := &queuedHTTPUpstream{}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream}
	c, _ := newTestContext()
	require.Error(t, svc.TestPromptAccountConnection(c, 71, "gpt-6-astra", "逻辑题", ""))
	require.Empty(t, upstream.requests)
}

func TestPromptMarkerDoesNotAffectDefaultOrPelicanTests(t *testing.T) {
	svc := &AccountTestService{}
	c, recorder := newTestContext()
	svc.sendEvent(c, TestEvent{Type: "test_start", Model: "gpt-6-astra"})
	require.False(t, strings.Contains(recorder.Body.String(), "prompt_mode"))
}
