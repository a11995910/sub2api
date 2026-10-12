package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSAuditNativeCredentialIsolation(t *testing.T) {
	a := excelTestAccount()
	s := openAIClientToolsTestService(excelTestUpstream("", 200))
	token, _, err := s.GetAccessToken(context.Background(), a)
	require.Error(t, err)
	require.Empty(t, token)
	require.False(t, a.SupportsOpenAIImageCapability(OpenAIImagesCapabilityBasic))
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityLive))
	c, rec := excelTestContext("/v1/responses/input_tokens")
	require.NoError(t, s.ForwardResponsesInputTokens(context.Background(), c, a, []byte(`{"model":"gpt-6-astra","input":"你好"}`)))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "input_tokens")
}

func TestExcelBPSAuditMappedCompatibilityModels(t *testing.T) {
	for _, format := range []string{"anthropic", "chat"} {
		t.Run(format, func(t *testing.T) {
			u := excelTestUpstream(excelTestWire(), 200)
			s := openAIClientToolsTestService(u)
			a := excelTestAccount()
			a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
			c, rec := excelTestContext("/v1/" + format)
			body := []byte(`{"model":"claude-alias","messages":[{"role":"user","content":"你好"}],"max_tokens":100}`)
			var result *OpenAIForwardResult
			var err error
			if format == "anthropic" {
				result, err = s.ForwardAsAnthropic(context.Background(), c, a, body, "", "gpt-6-astra")
			} else {
				result, err = s.ForwardAsChatCompletions(context.Background(), c, a, body, "", "gpt-6-astra")
			}
			require.NoError(t, err, rec.Body.String())
			require.Equal(t, "bps.openai.com", u.lastReq.URL.Host)
			require.Equal(t, "claude-alias", result.Model)
		})
	}
}

func TestExcelBPSAuditSelectionGates(t *testing.T) {
	a := excelTestAccount()
	a.Extra["openai_excel_bps_models"] = []any{"gpt-6-astra"}
	repo := &excelSettingsRepo{}
	s := &OpenAIGatewayService{settingService: &SettingService{settingRepo: repo}}
	scheduler := &defaultOpenAIAccountScheduler{service: s}
	require.False(t, excelBPSSelectionAllows(context.Background(), a, "gpt-6-astra", false))
	for _, enabled := range []bool{true, false} {
		cfg := DefaultExcelBPSConfig()
		cfg.Enabled = enabled
		require.NoError(t, s.settingService.SaveExcelBPSConfig(context.Background(), cfg))
		for _, model := range []string{"gpt-6-astra", "other-model"} {
			ctx := s.withExcelBPSSelection(context.Background())
			want := enabled && model == "gpt-6-astra"
			require.Equal(t, want, isOpenAICompatibleAccountEligibleForRequestBeforeProfit(ctx, a, PlatformOpenAI, model, false, ""))
			got, _ := scheduler.isAccountRequestCompatibleReason(ctx, a, OpenAIAccountScheduleRequest{RequestedModel: model})
			require.Equal(t, want, got)
		}
	}
}

type excelAuditUnreadableSettings struct{ SettingRepository }

func (*excelAuditUnreadableSettings) GetValue(context.Context, string) (string, error) {
	panic("随机图片地址不能读取数据库")
}
func TestExcelBPSAuditUnknownImageNeverReadsSettings(t *testing.T) {
	s := &OpenAIGatewayService{settingService: &SettingService{settingRepo: &excelAuditUnreadableSettings{}}}
	for _, id := range []string{"invalid", strings.Repeat("a", 43)} {
		c, rec := excelTestContext("/api/bps-images/" + id)
		c.Request = httptest.NewRequest("GET", "/api/bps-images/"+id, nil)
		s.ServeExcelBPSImage(c)
		require.Equal(t, 404, rec.Code)
		require.Nil(t, s.excelBPSImages)
	}
}

type excelAuditBusyTokenCache struct{ OpenAITokenCache }

func (*excelAuditBusyTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}
func TestExcelOAuthAuditBusyLockUsesUnexpiredToken(t *testing.T) {
	s, repo, client := excelGrantService(t)
	info := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Minute))
	require.NoError(t, s.saveExcelGrant(context.Background(), repo.account, "", info))
	s.excelTokenCache = &excelAuditBusyTokenCache{}
	token, err := s.ExcelAccessToken(context.Background(), repo.account.ID)
	require.NoError(t, err)
	require.Equal(t, info.AccessToken, token)
	require.Zero(t, client.calls.Load())
}

func TestExcelOAuthAuditPrimaryRotationDoesNotLoseGrant(t *testing.T) {
	for _, changeIdentity := range []bool{false, true} {
		t.Run(map[bool]string{false: "主令牌正常轮换", true: "身份替换"}[changeIdentity], func(t *testing.T) {
			s, repo, client := excelGrantService(t)
			ctx := context.Background()
			old := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Minute))
			require.NoError(t, s.saveExcelGrant(ctx, repo.account, "", old))
			previous := repo.ciphertext
			fresh := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
			client.response = &openai.TokenResponse{AccessToken: fresh.AccessToken, IDToken: fresh.IDToken, RefreshToken: "rotated-excel"}
			client.beforeRefresh = func() {
				repo.mu.Lock()
				defer repo.mu.Unlock()
				repo.account.Credentials["access_token"] = "rotated-primary"
				if changeIdentity {
					repo.account.Credentials["chatgpt_user_id"] = "other-user"
				}
			}
			token, err := s.ExcelAccessToken(ctx, repo.account.ID)
			if changeIdentity {
				require.Error(t, err)
				require.Equal(t, previous, repo.ciphertext)
			} else {
				require.NoError(t, err)
				require.Equal(t, fresh.AccessToken, token)
				_, _, grant, err := s.readExcelGrant(ctx, repo.account.ID)
				require.NoError(t, err)
				require.Equal(t, "rotated-excel", grant.RefreshToken)
			}
			require.EqualValues(t, 1, client.calls.Load())
		})
	}
}

func (r *excelGrantRepoStub) DisableExcelBPSOn403(_ context.Context, a *Account, expectedGrant string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if expectedGrant != r.ciphertext {
		return false, nil
	}
	if a.ID != r.account.ID {
		return false, errors.New("账号不匹配")
	}
	return true, nil
}
func TestExcelOAuthAudit403UsesActualGrantSnapshot(t *testing.T) {
	oauth, repo, client := excelGrantService(t)
	repo.account.Extra["openai_excel_bps_auto_disable_on_403"] = true
	ctx := context.Background()
	old := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Minute))
	require.NoError(t, oauth.saveExcelGrant(ctx, repo.account, "", old))
	fresh := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
	client.response = &openai.TokenResponse{AccessToken: fresh.AccessToken, IDToken: fresh.IDToken, RefreshToken: "rotated-excel"}
	token, err := oauth.ExcelAccessToken(ctx, repo.account.ID)
	require.NoError(t, err)
	gateway := &OpenAIGatewayService{accountRepo: repo, openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: oauth}}
	requestCtx := gateway.withExcelBPSCredentialSnapshot(ctx, repo.account, token)
	require.True(t, gateway.disableExcelBPSOn403(requestCtx, repo.account), "本次刷新后的 403 仍应关闭")
	require.NoError(t, oauth.saveExcelGrant(ctx, repo.account, repo.ciphertext, excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(2*time.Hour))))
	require.False(t, gateway.disableExcelBPSOn403(requestCtx, repo.account), "新授权不能被旧请求关闭")
}

func TestExcelBPSAuditAdminNativeProbesDoNotForward(t *testing.T) {
	a := excelTestAccount()
	tester := &AccountTestService{}
	for _, mode := range []string{"", AccountTestModeCompact} {
		c, rec := excelTestContext("/admin/accounts/test")
		require.Error(t, tester.testOpenAIAccountConnection(c, a, "gpt-6-astra", "你好", mode))
		require.Contains(t, rec.Body.String(), "Excel OAuth")
	}
	c, rec := excelTestContext("/admin/accounts/test")
	require.Error(t, tester.testOpenAIImageOAuth(c, context.Background(), a, "gpt-image-1", "图片"))
	require.Contains(t, rec.Body.String(), "Excel OAuth")
	request, err := tester.buildOpenAIOAuthUpstreamModelsRequest(context.Background(), a)
	require.Error(t, err)
	require.Nil(t, request)
	gateway := &OpenAIGatewayService{}
	_, err = gateway.FetchCodexModelsManifest(context.Background(), a, "", "")
	require.ErrorContains(t, err, "Excel OAuth")
	usage := &AccountUsageService{}
	snapshot, err := usage.probeOpenAICodexSnapshot(context.Background(), a)
	require.NoError(t, err)
	require.Nil(t, snapshot)
}
