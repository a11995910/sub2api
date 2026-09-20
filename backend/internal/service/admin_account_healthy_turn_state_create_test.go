//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminCreateOAuthHealthyTurnStateSwitchFromJSON(t *testing.T) {
	for _, tc := range []struct {
		name        string
		extra       string
		plan        any
		platform    string
		accountType string
		enabled     bool
		ticket      bool
		preflight   bool
		invalid     bool
	}{
		{name: "推送时直接开启", extra: `{"openai_healthy_turn_state_replace":true}`, enabled: true},
		{name: "明确关闭不采集", extra: `{"openai_healthy_turn_state_replace":false}`},
		{name: "未提供策略默认首发", extra: `{}`, enabled: true, preflight: true},
		{name: "字符串开关不能误启用", extra: `{"openai_healthy_turn_state_replace":"true"}`, invalid: true},
		{name: "高级商务新账号默认首发", extra: `{}`, plan: "self_serve_business_prolite", enabled: true, preflight: true},
		{name: "高级商务未提供额外字段默认首发", extra: `null`, plan: "self_serve_business_prolite", enabled: true, preflight: true},
		{name: "默认首发不依赖档位格式", extra: `{}`, plan: " Self-Serve_Business Prolite ", enabled: true, preflight: true},
		{name: "高级商务显式关闭优先", extra: `{"openai_healthy_turn_state_replace":false}`, plan: "self_serve_business_prolite"},
		{name: "高级商务非法开关仍拒绝", extra: `{"openai_healthy_turn_state_replace":"false"}`, plan: "self_serve_business_prolite", invalid: true},
		{name: "标准商务默认首发", extra: `{}`, plan: "team", enabled: true, preflight: true},
		{name: "个人轻量专业版默认首发", extra: `{}`, plan: "prolite", enabled: true, preflight: true},
		{name: "未知档位默认首发", extra: `{}`, plan: "Business Premium", enabled: true, preflight: true},
		{name: "非字符串档位也不影响默认首发", extra: `{}`, plan: 42, enabled: true, preflight: true},
		{name: "其他平台不启用", extra: `{}`, plan: "self_serve_business_prolite", platform: PlatformAnthropic},
		{name: "密钥账号不启用", extra: `{}`, plan: "self_serve_business_prolite", accountType: AccountTypeAPIKey},
		{name: "初始化令牌默认首发", extra: `{}`, accountType: AccountTypeSetupToken, enabled: true, preflight: true},
		{name: "显式异常补试优先", extra: `{"openai_turn_state_mode":"healthy_retry"}`, enabled: true},
		{name: "显式首发优先", extra: `{"openai_turn_state_mode":"healthy_preflight"}`, enabled: true, preflight: true},
		{name: "显式关闭优先", extra: `{"openai_turn_state_mode":"off"}`},
		{name: "显式门票覆盖旧开关", extra: `{"openai_turn_state_mode":"codex_ticket","openai_healthy_turn_state_replace":true}`, ticket: true},
		{name: "非法模式拒绝", extra: `{"openai_turn_state_mode":"unknown"}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var extra map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.extra), &extra))
			platform, accountType := tc.platform, tc.accountType
			if platform == "" {
				platform = PlatformOpenAI
			}
			if accountType == "" {
				accountType = AccountTypeOAuth
			}
			repo := &upstreamBillingProbeAccountRepo{}
			admin := &adminServiceImpl{accountRepo: repo}
			created, err := admin.CreateAccount(context.Background(), &CreateAccountInput{
				Name: "管理员接口导入验证", Platform: platform, Type: accountType,
				Credentials: map[string]any{"plan_type": tc.plan}, Extra: extra, SkipDefaultGroupBind: true,
			})
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, created)
				return
			}
			require.NoError(t, err)
			persisted, err := repo.GetByID(context.Background(), created.ID)
			require.NoError(t, err)
			require.Equal(t, tc.enabled, persisted.OpenAIHealthyTurnStateReplaceEnabled())
			require.Equal(t, tc.ticket, persisted.OpenAICodexTicketEnabled())
			require.Equal(t, tc.preflight, persisted.OpenAITurnStateMode() == OpenAITurnStateHealthyPreflight)
			if tc.preflight {
				require.Equal(t, true, persisted.Extra[openAIHealthyTurnStateReplaceKey], "首发模式必须启用健康池维护")
				require.False(t, persisted.OpenAIHealthyTurnStateFailClosed(), "新建默认缺头时继续原请求")
			}
			if tc.ticket {
				require.Equal(t, false, persisted.Extra[openAIHealthyTurnStateReplaceKey], "门票模式必须关闭健康池维护")
			}
			require.Equal(t, tc.enabled, healthyDynamicMaintenanceEnabled(persisted), "创建链路必须保留开关供无页面后台维护使用")
		})
	}
}

func TestTurnStateCreateDefaultSkipsShadowAndExistingReads(t *testing.T) {
	parentID := int64(1)
	shadow := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
	applyOpenAITurnStateCreateDefault(shadow)
	require.Empty(t, shadow.Extra)
	legacy := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.Equal(t, OpenAITurnStateOff, legacy.OpenAITurnStateMode(), "读取旧账号不能套用新建默认")
	require.Empty(t, legacy.Extra)
}

func TestCRSSyncTurnStateDefaultsOnlyNewAccounts(t *testing.T) {
	for _, tc := range []struct {
		name          string
		existingExtra map[string]any
		sourceExtra   map[string]any
		mode          string
	}{
		{name: "新账号默认首发", mode: OpenAITurnStateHealthyPreflight},
		{name: "新账号显式保留旧补试", sourceExtra: map[string]any{openAIHealthyTurnStateReplaceKey: true}, mode: OpenAITurnStateHealthyRetry},
		{name: "旧账号不补默认", existingExtra: map[string]any{}, mode: OpenAITurnStateOff},
		{name: "旧账号保留补试", existingExtra: map[string]any{openAIHealthyTurnStateReplaceKey: true}, mode: OpenAITurnStateHealthyRetry},
		{name: "旧账号保留门票", existingExtra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateCodexTicket}, mode: OpenAITurnStateCodexTicket},
		{name: "旧账号保留首发", existingExtra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateHealthyPreflight}, mode: OpenAITurnStateHealthyPreflight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var existing *Account
			if tc.existingExtra != nil {
				existing = &Account{ID: 41, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: mergeMap(tc.existingExtra, map[string]any{"crs_account_id": "crs-openai-1"})}
			}
			repo := newCRSLongContextAccountRepo(existing)
			result := runCRSOpenAILongContextSync(t, repo, crsOpenAILongContextSource{collection: "openaiOAuthAccounts", credentials: map[string]any{"access_token": "测试令牌"}, extra: tc.sourceExtra})
			require.Zero(t, result.Failed)
			require.Equal(t, tc.mode, repo.accounts["crs-openai-1"].OpenAITurnStateMode())
		})
	}
}
