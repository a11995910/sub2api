package service

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type excelAutoRepoStub struct {
	*excelGrantRepoStub
	record *ExcelAutoRecord
	reason string
	result string
}

func (r *excelAutoRepoStub) GetExcelAuto(context.Context, int64) (*ExcelAutoRecord, error) {
	if r.record == nil {
		return nil, nil
	}
	v := *r.record
	return &v, nil
}
func (r *excelAutoRepoStub) SaveExcelAuto(_ context.Context, id, revision int64, cipher string, enabled bool) (bool, error) {
	if r.record != nil && r.record.Revision != revision {
		return false, nil
	}
	state := "queued"
	if !enabled {
		state = "disabled"
	}
	r.record = &ExcelAutoRecord{AccountID: id, Revision: revision + 1, Ciphertext: cipher, Enabled: enabled, State: state}
	return true, nil
}
func (r *excelAutoRepoStub) ClaimExcelAuto(context.Context, string) (*ExcelAutoRecord, error) {
	return nil, nil
}
func (r *excelAutoRepoStub) FinishExcelAuto(_ context.Context, _ *ExcelAutoRecord, _ *Account, _, replacement, reason string) (bool, error) {
	r.reason, r.result = reason, replacement
	return true, nil
}
func (r *excelAutoRepoStub) RetryExcelAuto(context.Context, int64) error { return nil }

type excelAutoRunnerStub struct {
	token  *OpenAITokenInfo
	code   string
	input  ExcelAutoLogin
	calls  int
	during func()
}

func (r *excelAutoRunnerStub) Ready() bool { return true }
func (r *excelAutoRunnerStub) Login(_ context.Context, input ExcelAutoLogin) (*OpenAITokenInfo, string) {
	r.calls++
	r.input = input
	if r.during != nil {
		r.during()
	}
	return r.token, r.code
}
func autoTestService(t *testing.T) (*OpenAIOAuthService, *excelAutoRepoStub, *excelAutoRunnerStub) {
	s, grants, _ := excelGrantService(t)
	repo := &excelAutoRepoStub{excelGrantRepoStub: grants}
	s.excelAccountRepo = repo
	runner := &excelAutoRunnerStub{token: excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))}
	s.excelAuto = &excelAutoWorker{runner: runner, settings: &SettingService{}}
	// 测试不启动后台循环。
	s.excelAuto.cancel = func() {}
	s.excelAuto.done = make(chan struct{})
	close(s.excelAuto.done)
	return s, repo, runner
}
func TestExcelAutoConfigurationSecretsAndDisable(t *testing.T) {
	s, r, _ := autoTestService(t)
	ctx := context.Background()
	input := ExcelAutoInput{Enabled: true, Email: "test@example.invalid", Password: "synthetic-password", TOTPSecret: "JBSWY3DPEHPK3PXP"}
	require.NoError(t, s.SaveExcelAuto(ctx, 300, input))
	require.NotContains(t, r.record.Ciphertext, "synthetic-password")
	view, err := s.ExcelAutoStatus(ctx, 300)
	require.NoError(t, err)
	require.True(t, view.Configured)
	require.True(t, view.TOTPConfigured)
	raw, _ := json.Marshal(view)
	require.NotContains(t, string(raw), input.Password)
	require.NotContains(t, string(raw), input.TOTPSecret)
	input.Password = ""
	input.TOTPSecret = ""
	require.NoError(t, s.SaveExcelAuto(ctx, 300, input))
	stored, err := s.decodeAuto(r.record)
	require.NoError(t, err)
	require.Equal(t, "synthetic-password", stored.Password)
	input.Email = "new@example.invalid"
	require.ErrorContains(t, s.SaveExcelAuto(ctx, 300, input), "重新填写密码")
	require.NoError(t, s.SaveExcelAuto(ctx, 300, ExcelAutoInput{}))
	require.Empty(t, r.record.Ciphertext)
	require.False(t, r.record.Enabled)
}
func TestExcelAutoLoginPreservesCodexAndValidatesIdentity(t *testing.T) {
	for _, test := range []struct {
		name, workspace, code string
		valid                 bool
	}{
		{"同账号成功", "test-workspace", "", true}, {"工作区不一致", "other", "", false}, {"要求额外验证", "test-workspace", "verification_required", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, r, runner := autoTestService(t)
			ctx := context.Background()
			require.NoError(t, s.SaveExcelAuto(ctx, 300, ExcelAutoInput{Enabled: true, Email: "test@example.invalid", Password: "synthetic"}))
			runner.token = excelGrantToken(test.workspace, "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
			runner.code = test.code
			original := maps.Clone(r.account.Credentials)
			s.runExcelAutoJob(ctx, r, r.record)
			require.Equal(t, original, r.account.Credentials)
			if test.valid {
				require.Empty(t, r.reason)
				require.NotEmpty(t, r.result)
				raw, err := s.excelEncryptor.Decrypt(r.result)
				require.NoError(t, err)
				var grant excelOAuthGrant
				require.NoError(t, json.Unmarshal([]byte(raw), &grant))
				require.Equal(t, openai.ExcelClientID, grant.Token.ClientID)
			} else {
				require.NotEmpty(t, r.reason)
				require.Empty(t, r.result)
			}
		})
	}
}
func TestExcelAutoGrantChangeAndExistingAuthorization(t *testing.T) {
	s, r, runner := autoTestService(t)
	ctx := context.Background()
	require.NoError(t, s.SaveExcelAuto(ctx, 300, ExcelAutoInput{Enabled: true, Email: "test@example.invalid", Password: "synthetic"}))
	require.NoError(t, s.saveExcelGrant(ctx, r.account, "", runner.token))
	s.runExcelAutoJob(ctx, r, r.record)
	require.Zero(t, runner.calls)
	require.Equal(t, r.ciphertext, r.result)
	r.ciphertext = ""
	runner.during = func() { r.account.Credentials["chatgpt_user_id"] = "other-user" }
	s.runExcelAutoJob(ctx, r, r.record)
	require.Equal(t, "identity_mismatch", r.reason)
	require.Empty(t, r.result)
}
func TestExcelAutoRejectsIneligibleAndInvalidSecrets(t *testing.T) {
	s, r, _ := autoTestService(t)
	ctx := context.Background()
	input := ExcelAutoInput{Enabled: true, Email: "test@example.invalid", Password: "synthetic", TOTPSecret: "123456"}
	require.ErrorContains(t, s.SaveExcelAuto(ctx, 300, input), "长期 TOTP")
	r.account.Credentials["plan_type"] = "free"
	require.Error(t, s.SaveExcelAuto(ctx, 300, input))
	require.NotContains(t, excelAutoErrorMessage("secret-raw-upstream"), "secret")
	require.Error(t, validateExcelOAuthIdentity(r.account, nil, false))
}

func TestExcelAutoPendingKeepsNativeRoute(t *testing.T) {
	s, r, runner := autoTestService(t)
	ctx := context.Background()
	require.NoError(t, s.SaveExcelAuto(ctx, 300, ExcelAutoInput{Enabled: true, Email: "test@example.invalid", Password: "synthetic"}))
	gateway := &OpenAIGatewayService{openAITokenProvider: &OpenAITokenProvider{openAIOAuthService: s}}
	enabled, err := gateway.excelBPSRouteEnabledForUpstreamModel(ctx, r.account, "gpt-6-astra")
	require.NoError(t, err)
	require.False(t, enabled)
	require.NoError(t, s.saveExcelGrant(ctx, r.account, "", runner.token))
	enabled, err = gateway.excelBPSRouteEnabledForUpstreamModel(ctx, r.account, "gpt-6-astra")
	require.NoError(t, err)
	require.True(t, enabled)
}
