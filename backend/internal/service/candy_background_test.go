//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCandyBackgroundRequiresCompletedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, terminal string
		success        bool
	}{
		{"完整输出", `{"type":"response.completed","response":{"status":"completed","output":[]}}`, true},
		{"完整done", `{"type":"response.done","response":{"status":"completed","output":[]}}`, true},
		{"截断done", `{"type":"response.done","response":{"status":"incomplete","incomplete_details":{"reason":"max_output_tokens"}}}`, false},
		{"失败done", `{"type":"response.done","response":{"status":"failed"}}`, false},
		{"缺少结束状态", `{"type":"response.done"}`, false},
		{"completed附带截断", `{"type":"response.completed","response":{"status":"completed","incomplete_details":{"reason":"max_output_tokens"}}}`, false},
		{"拒答", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","status":"completed","content":[{"type":"refusal","refusal":"无法回答"}]}]}}`, false},
		{"输出未完成", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","status":"incomplete","content":[]}]}}`, false},
		{"无结束事件", ``, false},
		{"提前DONE", `[DONE]`, false},
		{"拒答事件", `{"type":"response.refusal.delta","delta":"拒绝"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{ID: 91, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "测试令牌"}}
			body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\n"
			if tc.terminal != "" {
				body += "data: " + tc.terminal + "\n\n"
			}
			upstream := &queuedHTTPUpstream{responses: []*http.Response{newJSONResponse(http.StatusOK, body)}}
			repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{91: account}}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
			result, err := svc.RunCandyBackground(context.Background(), 91, "", &PelicanTestConfig{Prompt: "测试题目", ReasoningEffort: "high"})
			require.NoError(t, err)
			require.Equal(t, tc.success, result.Status == "success", result.ErrorMessage)
			require.Equal(t, "21", result.ResponseText)
			if !tc.success {
				require.NotEmpty(t, result.ErrorMessage)
			}
			require.Len(t, upstream.requests, 1)
			requestBody, err := io.ReadAll(upstream.requests[0].Body)
			require.NoError(t, err)
			require.Equal(t, "gpt-6-astra", gjson.GetBytes(requestBody, "model").String())
			prompt := gjson.GetBytes(requestBody, "input.0.content.0.text").String()
			require.Contains(t, prompt, "测试题目")
			require.Contains(t, prompt, `{"answer":整数}`)
			require.NotContains(t, prompt, "21")
			require.NotContains(t, prompt, "HTML")
			require.Equal(t, "high", gjson.GetBytes(requestBody, "reasoning.effort").String())
		})
	}
}

func TestCandyBackgroundRejectsMappedImageModel(t *testing.T) {
	account := &Account{ID: 92, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "测试令牌", "model_mapping": map[string]any{"别名": "gpt-image-2"}}}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{92: account}}}
	upstream := &queuedHTTPUpstream{}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
	result, err := svc.RunCandyBackground(context.Background(), 92, "别名", &PelicanTestConfig{Prompt: "测试题目", ReasoningEffort: "high"})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.True(t, strings.Contains(result.ErrorMessage, "图像模型"))
	require.Empty(t, upstream.requests)
}

func TestCandyBackgroundSkipsUnavailableWithoutSending(t *testing.T) {
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
	for _, tc := range []struct {
		name   string
		change func(*Account)
	}{
		{"429", func(a *Account) { a.RateLimitResetAt = &future }},
		{"停用", func(a *Account) { a.Status = StatusDisabled }},
		{"错误", func(a *Account) { a.Status = StatusError }},
		{"暂停调度", func(a *Account) { a.Schedulable = false }},
		{"过载", func(a *Account) { a.OverloadUntil = &future }},
		{"临时故障", func(a *Account) { a.TempUnschedulableUntil = &future }},
		{"账号过期", func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past }},
		{"模型冷却", func(a *Account) {
			a.Extra = map[string]any{"model_rate_limits": map[string]any{AccountIntelligenceDefaultModel: map[string]any{"rate_limit_reset_at": future.Format(time.RFC3339)}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := protectionTestAccount(91)
			tc.change(account)
			upstream := &queuedHTTPUpstream{}
			repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{91: account}}}
			svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}}
			result, err := svc.RunCandyBackground(context.Background(), 91, "", &PelicanTestConfig{Prompt: "测试题"})
			require.NoError(t, err)
			require.Equal(t, "skipped", result.Status, result.ErrorMessage)
			require.NotEmpty(t, result.ErrorMessage)
			require.Empty(t, result.ResponseText)
			require.Empty(t, upstream.requests)
		})
	}
}

func TestCandyBackgroundSkipsRestingAccount(t *testing.T) {
	rotation, mr, _ := protectionWaitFixture(t, 0)
	randomRestTestConfig(rotation, 100, 0)
	account := protectionTestAccount(1)
	now := time.Now()
	mr.SetTime(now)
	done, err := rotation.BeginProtectedRequest(context.Background(), account)
	require.NoError(t, err)
	done()
	mr.SetTime(now.Add(time.Minute))
	upstream := &queuedHTTPUpstream{}
	repo := &openAIAccountTestRepo{mockAccountRepoForGemini: mockAccountRepoForGemini{accountsByID: map[int64]*Account{1: account}}}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: &config.Config{}, openaiGatewayService: &OpenAIGatewayService{accountTimeRotation: rotation}}
	result, err := svc.RunCandyBackground(context.Background(), 1, "", &PelicanTestConfig{Prompt: "测试题"})
	require.NoError(t, err)
	require.Equal(t, "skipped", result.Status)
	require.Empty(t, upstream.requests)
}
