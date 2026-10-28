package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountRestWSReplayPreservesCurrentTurnAndToolContext(t *testing.T) {
	replay := &accountRestWSReplay{}
	replay.begin([]byte(`{"type":"response.create","model":"公开模型","input":[{"role":"user","content":"第一轮"}]}`), "公开模型")
	replay.observe([]byte(`{"type":"response.completed","response":{"id":"resp_first","status":"completed","output":[{"type":"function_call","call_id":"call_1","name":"读取文件","arguments":"{}"}]}}`))
	replay.begin([]byte(`{"type":"response.create","previous_response_id":"resp_first","input":[{"type":"function_call_output","call_id":"call_1","output":"文件内容"}]}`), "公开模型")
	for _, code := range []string{"account_resting", "account_random_draining", "account_random_resting"} {
		payload, current := OpenAIWSCurrentTurnRetryPayload(replay.wrap(accountProtectionError(code, "休息", 1)))
		require.True(t, current)
		require.NotEmpty(t, payload)
		require.False(t, gjson.GetBytes(payload, "previous_response_id").Exists())
		require.Equal(t, "公开模型", gjson.GetBytes(payload, "model").String())
		require.Equal(t, int64(3), gjson.GetBytes(payload, "input.#").Int())
		require.Equal(t, "function_call_output", gjson.GetBytes(payload, "input.2.type").String())
		require.True(t, AnalyzeToolCallOutputContextCoverageBytes(payload).ContextCoversAllCallIDs)
	}
	payload, current := OpenAIWSCurrentTurnRetryPayload(replay.wrap(accountProtectionWaitError("account_rate_limit_wait_timeout", "限速超时")))
	require.True(t, current)
	require.Empty(t, payload)
}

func TestAccountRestWSReplayRejectsIncompleteHistory(t *testing.T) {
	for _, scenario := range []string{"跨连接", "未知响应", "缺失输出", "孤立工具", "超过容量", "会话更新"} {
		t.Run(scenario, func(t *testing.T) {
			replay := &accountRestWSReplay{}
			replay.begin([]byte(`{"type":"response.create","input":[{"role":"user","content":"第一轮"}]}`), "模型")
			replay.observe([]byte(`{"type":"response.completed","response":{"id":"resp_first","status":"completed","output":[]}}`))
			switch scenario {
			case "跨连接":
				replay = &accountRestWSReplay{}
			case "未知响应":
				replay.responseID = "resp_unknown"
			case "缺失输出":
				replay.observe([]byte(`{"type":"response.completed","response":{"id":"resp_first","status":"completed"}}`))
			case "会话更新":
				replay.invalidate()
			}
			next := `{"type":"response.create","previous_response_id":"resp_first","input":[{"role":"user","content":"当前轮"}]}`
			if scenario == "孤立工具" {
				next = `{"type":"response.create","input":[{"type":"function_call_output","call_id":"unknown","output":"结果"}]}`
			}
			if scenario == "超过容量" {
				next = `{"type":"response.create","input":"` + strings.Repeat("a", accountRestWSReplayMaxBytes) + `"}`
			}
			replay.begin([]byte(next), "模型")
			payload, current := OpenAIWSCurrentTurnRetryPayload(replay.wrap(accountProtectionError("account_resting", "休息", 1)))
			require.True(t, current)
			require.Empty(t, payload)
		})
	}
}
