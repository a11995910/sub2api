package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func integrityTestAccount() *Account {
	return &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "测试令牌"}}
}

func TestOpenAIRequestIntegrityModesAndValidation(t *testing.T) {
	a := integrityTestAccount()
	require.Equal(t, "observe", a.OpenAIRequestIntegrityMode())
	for _, value := range []any{"strict", "enforce", "", true, nil} {
		require.Error(t, ValidateOpenAIRequestIntegrityExtra(map[string]any{openAIRequestIntegrityModeKey: value}))
	}
	require.NoError(t, ValidateOpenAIRequestIntegrityExtra(nil))
	for _, mode := range []string{"observe", "off"} {
		a.Extra = map[string]any{openAIRequestIntegrityModeKey: mode}
		require.NoError(t, ValidateOpenAIRequestIntegrityExtra(a.Extra))
		require.Equal(t, mode, a.OpenAIRequestIntegrityMode())
	}
	require.Nil(t, newOpenAIRequestIntegritySnapshot(a, []byte(`无效JSON`)))
	a.Platform = PlatformAnthropic
	a.Extra = nil
	require.Nil(t, newOpenAIRequestIntegritySnapshot(a, []byte(`{}`)))
	require.Equal(t, "off", (*Account)(nil).OpenAIRequestIntegrityMode())
}

func TestOpenAIRequestIntegrityDetectsSemanticLoss(t *testing.T) {
	a := integrityTestAccount()
	tests := []struct{ name, before, after, field string }{
		{"用户上下文", `{"input":"原始上下文"}`, `{"input":"缩减上下文"}`, "input"},
		{"推理档位", `{"reasoning":{"effort":"high"}}`, `{}`, "reasoning"},
		{"工具定义", `{"tools":[{"type":"function","name":"run","parameters":{"type":"object"}}]}`, `{"tools":[]}`, "tools"},
		{"关联标识", `{"input":[{"type":"function_call_output","call_id":"call_a","output":"结果"}]}`, `{"input":[{"type":"function_call_output","call_id":"call_b","output":"结果"}]}`, "input"},
		{"工具参数中的同名元数据", `{"input":[{"type":"function_call","call_id":"call_a","arguments":{"internal_chat_message_metadata_passthrough":"正文值"}}]}`, `{"input":[{"type":"function_call","call_id":"call_a","arguments":{}}]}`, "input"},
		{"工具参数精度", `{"input":[{"type":"function_call","call_id":"call_a","arguments":{"n":9007199254740993}}]}`, `{"input":[{"type":"function_call","call_id":"call_a","arguments":{"n":9007199254740992}}]}`, "input"},
		{"推理内容", `{"input":[{"type":"reasoning","encrypted_content":"cipher-a"}]}`, `{"input":[{"type":"reasoning","encrypted_content":"cipher-b"}]}`, "input"},
		{"历史关联", `{"previous_response_id":"resp_original"}`, `{}`, "previous_response_id"},
		{"系统指令", `{"instructions":"保留完整解释"}`, `{"instructions":"简略回答"}`, "instructions"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := newOpenAIRequestIntegritySnapshot(a, []byte(tt.before)).compare(a, []byte(tt.after), "test")
			require.Equal(t, "changed", report.Status)
			require.Contains(t, report.Fields, tt.field)
		})
	}
}

func TestOpenAIRequestIntegrityAcceptsEquivalentCodexRepresentations(t *testing.T) {
	a := integrityTestAccount()
	tests := []struct{ name, before, after string }{
		{"文本消息", `{"input":"hello"}`, `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`},
		{"系统提升", `{"input":[{"role":"system","content":"规则"},{"role":"user","content":"问题"}]}`, `{"instructions":"规则","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"问题"}]}]}`},
		{"旧工具协议", `{"functions":[{"name":"run","parameters":{"type":"object"}}],"function_call":{"name":"run"}}`, `{"tools":[{"type":"function","name":"run","parameters":{"type":"object"}}],"tool_choice":{"type":"function","name":"run"}}`},
		{"函数嵌套", `{"tools":[{"type":"function","function":{"name":"run","description":"执行"}}]}`, `{"tools":[{"type":"function","name":"run","description":"执行"}]}`},
		{"不支持的预算", `{"max_output_tokens":100,"max_completion_tokens":200}`, `{}`},
		{"输入项内部元数据", `{"input":[{"role":"user","content":"hello","internal_chat_message_metadata_passthrough":{"ignored":"metadata"}}]}`, `{"input":[{"role":"user","content":"hello"}]}`},
		{"身份元数据", `{"input":"hello","client_metadata":{"session_id":"a"},"prompt_cache_key":"a"}`, `{"input":"hello","client_metadata":{"session_id":"b"},"prompt_cache_key":"b"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, "unchanged", newOpenAIRequestIntegritySnapshot(a, []byte(tt.before)).compare(a, []byte(tt.after), "test").Status)
		})
	}
	a.Type = AccountTypeAPIKey
	require.Equal(t, []string{"max_output_tokens"}, newOpenAIRequestIntegritySnapshot(a, []byte(`{"max_output_tokens":100}`)).compare(a, []byte(`{}`), "test").Fields)
}

func TestOpenAIRequestIntegrityMappedModelsAndSkippedPayloads(t *testing.T) {
	a := integrityTestAccount()
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-5.4"}
	a.Credentials["compact_model_mapping"] = map[string]any{"alias": "gpt-5.4-openai-compact"}
	for _, compact := range []bool{false, true} {
		model := "gpt-5.4"
		if compact {
			model = "gpt-5.4-openai-compact"
		}
		report := newOpenAIRequestIntegritySnapshot(a, []byte(`{"model":"alias"}`), compact).compare(a, []byte(`{"model":"`+model+`"}`), "test")
		require.Equal(t, "unchanged", report.Status)
	}
	for _, payload := range []string{`null`, `[]`, `{"input":`, `{} {}`} {
		snapshot := newOpenAIRequestIntegritySnapshot(a, []byte(payload))
		require.Equal(t, "skipped_invalid_json", snapshot.compare(a, []byte(`{}`), "test").Status)
	}
	oversized := []byte(strings.Repeat(" ", openAIRequestIntegrityMaxBytes+1))
	require.Equal(t, "skipped_size_limit", newOpenAIRequestIntegritySnapshot(a, oversized).compare(a, []byte(`{}`), "test").Status)
	require.Equal(t, "skipped_size_limit", newOpenAIRequestIntegritySnapshot(a, []byte(`{}`)).compare(a, oversized, "test").Status)
}

func TestOpenAIRequestIntegrityLogsOnlyFieldsAndResetsOnFailover(t *testing.T) {
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	defer slog.SetDefault(old)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	a := integrityTestAccount()
	before := []byte(`{"input":"私密原文-abc123","tools":[{"name":"私密工具"}]}`)
	stageOpenAIRequestIntegrity(c, a, before)
	for i := 0; i < 2; i++ {
		observeStagedOpenAIRequestIntegrity(c, a, []byte(`{"input":"改变的原文","tools":[]}`), "test")
	}
	require.Equal(t, 1, strings.Count(log.String(), "openai_request_integrity_observation"))
	require.NotContains(t, log.String(), "私密")
	require.NotContains(t, log.String(), "改变的原文")
	require.NotContains(t, log.String(), "测试令牌")
	require.Contains(t, log.String(), `"fields":["input","tools"]`)
	a.ID++
	observeStagedOpenAIRequestIntegrity(c, a, []byte(`{}`), "test")
	require.Equal(t, 1, strings.Count(log.String(), "openai_request_integrity_observation"))
	a.Extra = map[string]any{openAIRequestIntegrityModeKey: "off"}
	stageOpenAIRequestIntegrity(c, a, before)
	observeStagedOpenAIRequestIntegrity(c, a, []byte(`{}`), "test")
	report, _ := c.Get(openAIRequestIntegrityReportKey)
	require.Nil(t, report)
}

func TestOpenAIRequestIntegrityForwardObservesWithoutBlocking(t *testing.T) {
	for _, mode := range []string{"observe", "off"} {
		t.Run(mode, func(t *testing.T) {
			body := []byte(`{"model":"gpt-5.5","parallel_tool_calls":true,"input":[{"type":"reasoning","id":"rs_old","summary":[]},{"type":"item_reference","id":"rs_old"},{"type":"message","role":"user","content":"compact this"}]}`)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", bytes.NewReader(body))
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"id":"resp_compact","status":"completed","output":[{"type":"compaction","encrypted_content":"cipher"}],"usage":{"input_tokens":1,"output_tokens":1}}`))}}
			svc := openAIClientToolsTestService(upstream)
			a := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}, Extra: map[string]any{openAIRequestIntegrityModeKey: mode}}
			result, err := svc.Forward(context.Background(), c, a, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.True(t, json.Valid(upstream.lastBody))
			value, _ := c.Get(openAIRequestIntegrityReportKey)
			if mode == "off" {
				require.Nil(t, value)
				return
			}
			report := value.(openAIRequestIntegrityReport)
			require.Equal(t, "changed", report.Status)
			require.Contains(t, report.Fields, "input")
			require.Contains(t, report.Fields, "parallel_tool_calls")
		})
	}
}

func TestOpenAIRequestIntegrityHTTPFingerprintDoesNotReportSemanticChange(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		for _, mode := range []string{"off", "device", "session", "full"} {
			name := mode
			if passthrough {
				name += "透传"
			}
			t.Run(name, func(t *testing.T) {
				body := []byte(`{"model":"gpt-5.1","stream":true,"instructions":"完整回答","input":[{"role":"user","content":"解释算法"}],"reasoning":{"effort":"high"},"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"q":{"type":"string"}}}}],"client_metadata":{"session_id":"client-session"}}`)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body))
				c.Request.Header.Set("session_id", "original-session")
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"))}}
				svc := openAIClientToolsTestService(upstream)
				a := integrityTestAccount()
				a.Extra = map[string]any{"openai_passthrough": passthrough, "codex_fingerprint_mode": mode, codexFingerprintSeedExtraKey: testCodexFingerprintSeed}
				_, err := svc.Forward(context.Background(), c, a, body)
				require.NoError(t, err)
				value, exists := c.Get(openAIRequestIntegrityReportKey)
				require.True(t, exists)
				report := value.(openAIRequestIntegrityReport)
				require.Equal(t, "unchanged", report.Status, "fields=%v", report.Fields)
				if mode != "off" {
					require.NotNil(t, stagedCodexFingerprintIDs(c, a), "必须实际启用收敛后再验证观察结果")
				}
			})
		}
	}
}
