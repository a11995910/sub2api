package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExcelBPSSettingsSaveAndImmediateDisable(t *testing.T) {
	ctx := context.Background()
	repo := &excelBPSSettingsRepo{values: map[string]string{"unrelated_setting": "preserve"}}
	settings := &SettingService{settingRepo: repo}
	gateway := &OpenAIGatewayService{settingService: settings}
	initial, err := settings.GetExcelBPSSettings(ctx)
	require.NoError(t, err)
	require.False(t, initial.ExcelBPSEnabled)
	require.False(t, initial.ExcelBPSImageRelayEnabled)
	initial.ExcelBPSEnabled = true
	initial.ExcelBPSImageRelayEnabled = true
	require.NoError(t, settings.SaveExcelBPSSettings(ctx, initial))
	require.True(t, gateway.excelBPSGloballyEnabled(ctx))
	relay, err := settings.GetExcelBPSImageRelaySettings(ctx)
	require.NoError(t, err)
	require.True(t, relay.Enabled)
	require.Equal(t, ExcelBPSImageModeNative, relay.Mode)
	initial.ExcelBPSEnabled = false
	require.NoError(t, settings.SaveExcelBPSSettings(ctx, initial))
	require.False(t, gateway.excelBPSGloballyEnabled(ctx))
	// 关闭总开关后，遗留图片配置不能阻止原协议请求。
	repo.values[SettingKeyExcelBPSImageMode] = "invalid"
	relay, err = settings.GetExcelBPSImageRelaySettings(ctx)
	require.NoError(t, err)
	require.False(t, relay.Enabled)
	require.Equal(t, "preserve", repo.values["unrelated_setting"])
}

func TestExcelBPSSettingsRejectInvalidWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	repo := &excelBPSSettingsRepo{values: map[string]string{}}
	settings := &SettingService{settingRepo: repo}
	defaults, err := settings.GetExcelBPSSettings(ctx)
	require.NoError(t, err)
	for _, change := range []func(*ExcelBPSSettings){
		func(s *ExcelBPSSettings) { s.ExcelBPSImageMode = "unsupported" },
		func(s *ExcelBPSSettings) {
			s.ExcelBPSImageRelayEnabled = true
			s.ExcelBPSImageMode = "relay"
			s.ExcelBPSImageBaseURL = "http://example.com"
		},
		func(s *ExcelBPSSettings) { s.ExcelBPSImageBudgetMiB = 1 },
		func(s *ExcelBPSSettings) { s.ExcelBPSImageMaxRequests = -1 },
		func(s *ExcelBPSSettings) { s.ExcelBPSImageLimitPolicy = "unsupported" },
	} {
		value := *defaults
		change(&value)
		require.Error(t, settings.SaveExcelBPSSettings(ctx, &value))
		require.Empty(t, repo.values)
	}
}

func TestExcelBPSDefaultsRoundTripAndValidation(t *testing.T) {
	ctx := context.Background()
	repo := &excelBPSSettingsRepo{values: map[string]string{}}
	settings := &SettingService{settingRepo: repo}
	defaults, err := settings.GetExcelBPSDefaults(ctx)
	require.NoError(t, err)
	defaults.AutoRecoverOn403 = true
	defaults.RecoveryIntervalMinutes = 42
	defaults.AutoMoveOn403 = true
	defaults.TargetGroupID = 0
	require.NoError(t, settings.SaveExcelBPSDefaults(ctx, defaults))
	got, err := settings.GetExcelBPSDefaults(ctx)
	require.NoError(t, err)
	require.Equal(t, defaults, got)
	require.Empty(t, repo.values[SettingKeyExcelBPSEnabled], "保存模板不启用全局协议")
	for _, change := range []func(*ExcelBPSDefaults){
		func(s *ExcelBPSDefaults) { s.AutoDisableOn403 = false },
		func(s *ExcelBPSDefaults) { s.RecoveryIntervalMinutes = 0 },
		func(s *ExcelBPSDefaults) { s.TargetGroupID = -1 },
		func(s *ExcelBPSDefaults) { s.Models = nil },
	} {
		value := defaults
		change(&value)
		require.Error(t, settings.SaveExcelBPSDefaults(ctx, value))
		got, err = settings.GetExcelBPSDefaults(ctx)
		require.NoError(t, err)
		require.Equal(t, defaults, got)
	}
}

func TestExcelBPSCompactOverridesOnlySelectedRoute(t *testing.T) {
	for _, advanced := range []bool{true, false} {
		t.Run(strconv.FormatBool(advanced), func(t *testing.T) {
			account := excelAccount()
			account.Status = StatusActive
			account.Schedulable = true
			account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
			account.Extra["openai_compact_supported"] = false
			require.Equal(t, 0, openAICompactSupportTier(account))
			cfg := &config.Config{}
			cfg.Gateway.Scheduling.LoadBatchEnabled = true
			svc := &OpenAIGatewayService{
				accountRepo: schedulerTestOpenAIAccountRepo{accounts: []Account{*account}},
				cache:       &schedulerTestGatewayCache{}, cfg: cfg,
				rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(strconv.FormatBool(advanced)),
				concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
				settingService:     excelBPSTestService(nil).settingService,
			}
			groupID := int64(101301)
			selection, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, true)
			require.NoError(t, err)
			require.NotNil(t, selection)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			_, _, err = svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, true)
			require.Error(t, err, "范围外模型保留原生 compact 限制")
			svc.settingService = nil
			_, _, err = svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, true)
			require.Error(t, err, "全局关闭后恢复原生 compact 限制")
		})
	}
}
