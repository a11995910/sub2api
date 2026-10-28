package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type pelicanExcelRepo struct {
	*excelGrantRepoStub
	disableCalls int
}

func (r *pelicanExcelRepo) DisableExcelBPSOn403(context.Context, *Account, string) (bool, error) {
	r.disableCalls++
	r.account.Extra["openai_excel_bps"] = false
	return true, nil
}

func pelicanExcelFixture(t *testing.T, upstream *httpUpstreamRecorder, authorized bool) (*AccountTestService, *pelicanExcelRepo, *OpenAITokenInfo) {
	t.Helper()
	oauth, grants, _ := excelGrantService(t)
	grants.account.Credentials["access_token"] = "synthetic-primary-codex-token"
	grants.account.Extra["openai_excel_bps_auto_disable_on_403"] = true
	repo := &pelicanExcelRepo{excelGrantRepoStub: grants}
	info := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
	if authorized {
		require.NoError(t, oauth.saveExcelGrant(context.Background(), grants.account, "", info))
	}
	gateway := openAIClientToolsTestService(upstream)
	gateway.accountRepo = repo
	gateway.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, cfg: gateway.cfg, openaiGatewayService: gateway}
	return svc, repo, info
}

func pelicanExcelWire() string {
	return "data: {\"type\":\"response.output_text.delta\",\"delta\":\"<svg>鹈鹕</svg>\"}\n\n" + excelTestWire()
}

func TestPelicanExcelBPSUsesIndependentGrantAndMappedModel(t *testing.T) {
	u := excelTestUpstream(pelicanExcelWire(), http.StatusOK)
	svc, repo, info := pelicanExcelFixture(t, u, true)
	a := repo.account
	a.Credentials["model_mapping"] = map[string]any{"alias": "gpt-6-astra", "gpt-6-astra": "must-not-map-twice"}
	a.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	a.Proxy = &Proxy{Protocol: "http", Host: "127.0.0.1", Port: 8888}
	c, rec := excelTestContext("/api/v1/admin/accounts/300/test")
	c.Request.Header.Set("Cookie", "synthetic-cookie")
	c.Request.Header.Set("x-codex-turn-state", "synthetic-ticket")
	collector := &pelicanTestUsageCollector{}
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), pelicanTestUsageKey{}, collector))
	previousWriter := c.Writer
	require.NoError(t, svc.TestPelicanAccountConnection(c, a.ID, "alias", "画出鹈鹕", "max"))
	require.Same(t, previousWriter, c.Writer)
	require.Len(t, u.requests, 1)
	require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
	require.Equal(t, "Bearer "+info.AccessToken, u.lastReq.Header.Get("Authorization"))
	require.Equal(t, a.Proxy.URL(), u.lastProxyURL)
	require.Empty(t, u.lastReq.Header.Get("Cookie"))
	require.Empty(t, u.lastReq.Header.Get("x-codex-turn-state"))
	require.Equal(t, "gpt-6-astra", gjson.GetBytes(u.lastBody, "model").String())
	require.Equal(t, "xhigh", gjson.GetBytes(u.lastBody, "reasoning_effort").String())
	require.Contains(t, string(u.lastBody), "画出鹈鹕")
	output, message := parsePelicanOutput(rec.Body.String())
	require.Empty(t, message)
	require.Contains(t, output, "<svg>鹈鹕</svg>")
	require.Len(t, collector.requests, 1)
	require.True(t, collector.requests[0].complete)
	require.Equal(t, 10, collector.requests[0].tokens.InputTokens)
	require.Equal(t, 2, collector.requests[0].tokens.OutputTokens)
	require.Equal(t, true, a.Extra["openai_excel_bps_auto_disable_on_403"])
}

func TestPelicanExcelBPSFailuresNeverUseCodexOrDisableProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, wire, want string
		status           int
		transport        error
	}{
		{"鉴权失败", `{"error":{"message":"secret-upstream-token"}}`, "HTTP 401", 401, nil},
		{"普通拒绝", `{"error":{"message":"secret-upstream-token"}}`, "HTTP 403", 403, nil},
		{"模型权限", `{"error":{"code":"model_not_allowed","message":"secret-upstream-token"}}`, "HTTP 403", 403, nil},
		{"限流", `{"error":{"message":"secret-upstream-token"}}`, "HTTP 429", 429, nil},
		{"连接失败", "", "connection failed", 200, errors.New("secret-upstream-token")},
		{"中断", "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\"}}\n\n", "中断", 200, nil},
		{"流内失败", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\",\"error\":{\"code\":\"rate_limit_exceeded\",\"message\":\"secret-upstream-token\"}}}\n\n", "rate_limit_exceeded", 200, nil},
		{"未完成", "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n", "未完成", 200, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := excelTestUpstream(tc.wire, tc.status)
			u.err = tc.transport
			svc, repo, _ := pelicanExcelFixture(t, u, true)
			c, rec := excelTestContext("/internal/pelican-test")
			require.Error(t, svc.TestPelicanAccountConnection(c, repo.account.ID, "", "画出鹈鹕", ""))
			require.Len(t, u.requests, 1)
			require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
			require.Contains(t, rec.Body.String(), tc.want)
			require.NotContains(t, rec.Body.String(), "secret-upstream-token")
			require.NotContains(t, rec.Body.String(), `"success":true`)
			require.Zero(t, repo.disableCalls)
			require.Equal(t, true, repo.account.Extra["openai_excel_bps"])
		})
	}
}

func TestPelicanExcelBPSPrerequisitesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		configure  func(*AccountTestService, *Account)
	}{
		{"未授权", "OAuth 凭据不可用", func(*AccountTestService, *Account) {}},
		{"模型未选择", "模型范围", func(_ *AccountTestService, a *Account) { a.Extra["openai_excel_bps_models"] = []string{"other"} }},
		{"全局关闭", "全局开关已关闭", func(s *AccountTestService, _ *Account) {
			s.openaiGatewayService.settingService = &SettingService{settingRepo: &excelSettingsRepo{raw: `{"enabled":false}`}}
		}},
		{"配置读取失败", "配置读取失败", func(s *AccountTestService, _ *Account) {
			s.openaiGatewayService.settingService = &SettingService{settingRepo: &excelSettingsRepo{err: errors.New("配置不可用")}}
		}},
		{"服务缺失", "转发服务不可用", func(s *AccountTestService, _ *Account) { s.openaiGatewayService = nil }},
		{"账号不支持", "账号类型不支持", func(_ *AccountTestService, a *Account) { a.Credentials["plan_type"] = "free" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			u := excelTestUpstream(pelicanExcelWire(), 200)
			svc, repo, _ := pelicanExcelFixture(t, u, false)
			tc.configure(svc, repo.account)
			c, rec := excelTestContext("/internal/pelican-test")
			require.Error(t, svc.TestPelicanAccountConnection(c, repo.account.ID, "", "画出鹈鹕", ""))
			require.Empty(t, u.requests)
			require.Contains(t, rec.Body.String(), tc.want)
			require.NotContains(t, rec.Body.String(), `"success":true`)
		})
	}
}

func TestPelicanExcelBPSBackgroundAndPrimaryExcel(t *testing.T) {
	for _, primary := range []bool{false, true} {
		t.Run(map[bool]string{false: "独立授权", true: "专用账号"}[primary], func(t *testing.T) {
			u := excelTestUpstream(pelicanExcelWire(), 200)
			svc, repo, _ := pelicanExcelFixture(t, u, true)
			if primary {
				repo.account.Credentials["client_id"] = openai.ExcelClientID
				svc.openaiGatewayService.openAITokenProvider = nil
			}
			result, err := svc.RunPelicanBackground(context.Background(), repo.account.ID, "", &PelicanTestConfig{Prompt: "画出鹈鹕"})
			require.NoError(t, err)
			require.Equal(t, "success", result.Status)
			require.Contains(t, result.ResponseText, "<svg>")
			require.Equal(t, "gpt-6-astra", result.PelicanConfig.ModelID)
			require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
		})
	}
}

func TestPelicanWithoutExcelKeepsCodexRoute(t *testing.T) {
	u := excelTestUpstream(pelicanExcelWire(), 200)
	svc, repo, _ := pelicanExcelFixture(t, u, false)
	repo.account.Extra["openai_excel_bps"] = false
	c, rec := excelTestContext("/internal/pelican-test")
	require.NoError(t, svc.TestPelicanAccountConnection(c, repo.account.ID, "", "画出鹈鹕", ""))
	require.Equal(t, "chatgpt.com", u.lastReq.URL.Host)
	require.Equal(t, "Bearer synthetic-primary-codex-token", u.lastReq.Header.Get("Authorization"))
	require.True(t, strings.Contains(rec.Body.String(), `"success":true`))
}
