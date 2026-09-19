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
		invalid     bool
	}{
		{name: "推送时直接开启", extra: `{"openai_healthy_turn_state_replace":true}`, enabled: true},
		{name: "明确关闭不采集", extra: `{"openai_healthy_turn_state_replace":false}`},
		{name: "未提供开关不采集", extra: `{}`},
		{name: "字符串开关不能误启用", extra: `{"openai_healthy_turn_state_replace":"true"}`, invalid: true},
		{name: "高级商务新账号默认开启", extra: `{}`, plan: "self_serve_business_prolite", enabled: true},
		{name: "高级商务未提供额外字段默认开启", extra: `null`, plan: "self_serve_business_prolite", enabled: true},
		{name: "高级商务档位兼容大小写与分隔符", extra: `{}`, plan: " Self-Serve_Business Prolite ", enabled: true},
		{name: "高级商务显式关闭优先", extra: `{"openai_healthy_turn_state_replace":false}`, plan: "self_serve_business_prolite"},
		{name: "高级商务非法开关仍拒绝", extra: `{"openai_healthy_turn_state_replace":"false"}`, plan: "self_serve_business_prolite", invalid: true},
		{name: "标准商务保持默认关闭", extra: `{}`, plan: "team"},
		{name: "个人轻量专业版保持默认关闭", extra: `{}`, plan: "prolite"},
		{name: "未知展示名不能作为档位", extra: `{}`, plan: "Business Premium"},
		{name: "非字符串档位不启用", extra: `{}`, plan: 42},
		{name: "其他平台不启用", extra: `{}`, plan: "self_serve_business_prolite", platform: PlatformAnthropic},
		{name: "密钥账号不启用", extra: `{}`, plan: "self_serve_business_prolite", accountType: AccountTypeAPIKey},
		{name: "初始化令牌不启用", extra: `{}`, plan: "self_serve_business_prolite", accountType: AccountTypeSetupToken},
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
			require.Equal(t, tc.enabled, healthyDynamicMaintenanceEnabled(persisted), "创建链路必须保留开关供无页面后台维护使用")
		})
	}
}
