//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminCreateOAuthTurnStateOnlySupportsTickets(t *testing.T) {
	for _, tc := range []struct {
		name, extra, accountType, mode string
		invalid                        bool
	}{
		{name: "新建默认关闭", extra: `{}`, mode: OpenAITurnStateOff},
		{name: "初始化令牌默认关闭", extra: `null`, accountType: AccountTypeSetupToken, mode: OpenAITurnStateOff},
		{name: "旧开关不再启用采集", extra: `{"openai_healthy_turn_state_replace":true}`, mode: OpenAITurnStateOff},
		{name: "旧开关不覆盖门票", extra: `{"openai_turn_state_mode":"codex_ticket","openai_healthy_turn_state_replace":true}`, mode: OpenAITurnStateCodexTicket},
		{name: "明确关闭", extra: `{"openai_turn_state_mode":"off"}`, mode: OpenAITurnStateOff},
		{name: "拒绝补试模式", extra: `{"openai_turn_state_mode":"healthy_retry"}`, invalid: true},
		{name: "拒绝首发模式", extra: `{"openai_turn_state_mode":"healthy_preflight"}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var extra map[string]any
			require.NoError(t, json.Unmarshal([]byte(tc.extra), &extra))
			accountType := tc.accountType
			if accountType == "" {
				accountType = AccountTypeOAuth
			}
			repo := &upstreamBillingProbeAccountRepo{}
			admin := &adminServiceImpl{accountRepo: repo}
			created, err := admin.CreateAccount(context.Background(), &CreateAccountInput{
				Name: "门票策略验证", Platform: PlatformOpenAI, Type: accountType,
				Credentials: map[string]any{"plan_type": "team"}, Extra: extra, SkipDefaultGroupBind: true,
			})
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, created)
				return
			}
			require.NoError(t, err)
			persisted, err := repo.GetByID(context.Background(), created.ID)
			require.NoError(t, err)
			require.Equal(t, tc.mode, persisted.OpenAITurnStateMode())
			require.Equal(t, tc.mode, persisted.Extra[OpenAITurnStateModeKey])
			require.NotContains(t, persisted.Extra, "openai_healthy_turn_state_replace")
		})
	}
}

func TestTurnStateCreateDefaultSkipsShadowAndExistingReads(t *testing.T) {
	parentID := int64(1)
	shadow := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}
	applyOpenAITurnStateCreateDefault(shadow)
	require.Empty(t, shadow.Extra)
	legacy := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	require.Equal(t, OpenAITurnStateOff, legacy.OpenAITurnStateMode())
	require.Empty(t, legacy.Extra, "只读旧账号不改写存储")
}

func TestCRSSyncTurnStateDefaultsAndRetiredModes(t *testing.T) {
	for _, tc := range []struct {
		name          string
		existingExtra map[string]any
		sourceExtra   map[string]any
		mode          string
	}{
		{name: "新账号默认关闭", mode: OpenAITurnStateOff},
		{name: "新账号旧开关不生效", sourceExtra: map[string]any{"openai_healthy_turn_state_replace": true}, mode: OpenAITurnStateOff},
		{name: "新账号明确门票", sourceExtra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateCodexTicket}, mode: OpenAITurnStateCodexTicket},
		{name: "旧账号旧开关停用", existingExtra: map[string]any{"openai_healthy_turn_state_replace": true}, mode: OpenAITurnStateOff},
		{name: "旧账号门票保留", existingExtra: map[string]any{OpenAITurnStateModeKey: OpenAITurnStateCodexTicket}, mode: OpenAITurnStateCodexTicket},
		{name: "旧账号首发停用", existingExtra: map[string]any{OpenAITurnStateModeKey: "healthy_preflight"}, mode: OpenAITurnStateOff},
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
