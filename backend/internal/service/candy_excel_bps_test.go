package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCandyExcelBPSValidatesCompletionAndNeverFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name, terminal, want string
	}{
		{"完整输出", `{"type":"response.completed","response":{"status":"completed","output":[]}}`, "success"},
		{"无结束事件", "", "failed"},
		{"缺少结束状态", `{"type":"response.completed","response":{"output":[]}}`, "failed"},
		{"截断", `{"type":"response.incomplete","response":{"status":"incomplete"}}`, "failed"},
		{"completed附带截断", `{"type":"response.completed","response":{"status":"completed","incomplete_details":{"reason":"max_output_tokens"}}}`, "failed"},
		{"拒答", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","content":[{"type":"refusal","refusal":"拒答"}]}]}}`, "failed"},
		{"拒答事件后完成", `{"type":"response.refusal.delta","delta":"拒答"}` + "\n\ndata: " + `{"type":"response.completed","response":{"status":"completed","output":[]}}`, "failed"},
		{"输出未完成", `{"type":"response.completed","response":{"status":"completed","output":[{"type":"message","status":"incomplete","content":[]}]}}`, "failed"},
		{"流内错误", `{"type":"response.failed","response":{"status":"failed","error":{"code":"rate_limit_exceeded","message":"secret-upstream-token"}}}`, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"21\"}\n\n"
			if tc.terminal != "" {
				wire += "data: " + tc.terminal + "\n\n"
			}
			u := excelTestUpstream(wire, http.StatusOK)
			svc, repo, info := pelicanExcelFixture(t, u, true)
			result, err := svc.RunCandyBackground(context.Background(), repo.account.ID, "", &PelicanTestConfig{Prompt: "测试题目", ReasoningEffort: "high"})
			require.NoError(t, err)
			require.Equal(t, tc.want, result.Status, result.ErrorMessage)
			require.Equal(t, "21", result.ResponseText)
			require.Len(t, u.requests, 1)
			require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
			require.Equal(t, "Bearer "+info.AccessToken, u.lastReq.Header.Get("Authorization"))
			require.Contains(t, string(u.lastBody), "测试题目")
			require.Contains(t, string(u.lastBody), `answer`)
			require.NotContains(t, string(u.lastBody), "HTML")
			require.NotContains(t, result.ErrorMessage, "secret-upstream-token")
			require.Zero(t, repo.disableCalls)
			if tc.want == "failed" {
				require.NotEmpty(t, result.ErrorMessage)
			}
		})
	}
}

func TestCandyExcelBPSAuthAndHTTPErrorsKeepExcel(t *testing.T) {
	for _, status := range []int{0, 401, 403, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			u := excelTestUpstream(`{"error":{"message":"secret-upstream-token"}}`, status)
			svc, repo, _ := pelicanExcelFixture(t, u, status != 0)
			result, err := svc.RunCandyBackground(context.Background(), repo.account.ID, "", &PelicanTestConfig{Prompt: "测试题目"})
			require.NoError(t, err)
			require.Equal(t, "failed", result.Status)
			require.Contains(t, result.ErrorMessage, "Excel")
			require.NotContains(t, result.ErrorMessage, "secret-upstream-token")
			require.Zero(t, repo.disableCalls)
			require.Equal(t, true, repo.account.Extra["openai_excel_bps"])
			if status == 0 {
				require.Empty(t, u.requests)
			} else {
				require.Len(t, u.requests, 1)
				require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
			}
		})
	}
}

func TestCandyExcelBPSRejectsMappedImageBeforeSending(t *testing.T) {
	u := excelTestUpstream("", 200)
	svc, repo, _ := pelicanExcelFixture(t, u, true)
	repo.account.Credentials["model_mapping"] = map[string]any{"别名": "gpt-image-2"}
	result, err := svc.RunCandyBackground(context.Background(), repo.account.ID, "别名", &PelicanTestConfig{Prompt: "测试题目"})
	require.NoError(t, err)
	require.Equal(t, "failed", result.Status)
	require.True(t, strings.Contains(result.ErrorMessage, "图像模型"))
	require.Empty(t, u.requests)
}
