//go:build unit

package admin

import (
	"bytes"
	"log/slog"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
)

// Saving settings is a whole-document PUT. A client that sends only the field it
// cares about must not reset everything else: a payload as small as
// `{"risk_control_enabled":true}` used to clear site_name, after which
// getStringOrDefault rendered the empty value as the built-in default and the
// login page silently changed name.

func TestUpdateSettingsPartialPayloadKeepsUnsentKeys(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName:                         "Example Gateway",
		service.SettingKeySiteSubtitle:                     "Example Gateway Platform",
		service.SettingKeySMTPHost:                         "smtp.example.com",
		service.SettingKeySMTPFrom:                         "noreply@example.com",
		service.SettingKeySMTPFallbacks:                    `[{"host":"backup.example.com","port":465,"username":"backup","password":"secret","from_email":"backup@example.com","from_name":"Backup","use_tls":true}]`,
		service.SettingKeyTurnstileEnabled:                 "true",
		service.SettingKeyQuickLinkEnabled:                 "true",
		service.SettingKeyQuickLinkText:                    "帮助中心",
		service.SettingKeyQuickLinkURL:                     "https://help.example.com",
		service.SettingKeyAffiliateSubscriptionRewardGroup: "9",
		service.SettingKeyAffiliateSubscriptionRewardDays:  "7",
		service.SettingKeyCheckinEnabled:                   "true",
		service.SettingKeyCheckinContent:                   "每日签到",
		service.SettingKeyCheckinDailyReward:               "1.50000000",
		service.SettingKeyCheckinExtraReward4:              "4.00000000",
		service.SettingKeyCheckinExtraReward16:             "16.00000000",
		service.SettingKeyAPIKeyDefaultGroupID:             "12",
	})

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "true", repo.values[service.SettingKeyRiskControlEnabled],
		"the field the caller actually sent must be written")

	require.Equal(t, "Example Gateway", repo.values[service.SettingKeySiteName])
	require.Equal(t, "Example Gateway Platform", repo.values[service.SettingKeySiteSubtitle])
	require.Equal(t, "smtp.example.com", repo.values[service.SettingKeySMTPHost])
	require.Equal(t, "noreply@example.com", repo.values[service.SettingKeySMTPFrom])
	require.Equal(t, "true", repo.values[service.SettingKeyTurnstileEnabled])
	require.JSONEq(t, `[{"host":"backup.example.com","port":465,"username":"backup","password":"secret","from_email":"backup@example.com","from_name":"Backup","use_tls":true}]`, repo.values[service.SettingKeySMTPFallbacks])
	require.Equal(t, "true", repo.values[service.SettingKeyQuickLinkEnabled])
	require.Equal(t, "帮助中心", repo.values[service.SettingKeyQuickLinkText])
	require.Equal(t, "https://help.example.com", repo.values[service.SettingKeyQuickLinkURL])
	require.Equal(t, "9", repo.values[service.SettingKeyAffiliateSubscriptionRewardGroup])
	require.Equal(t, "7", repo.values[service.SettingKeyAffiliateSubscriptionRewardDays])
	require.Equal(t, "true", repo.values[service.SettingKeyCheckinEnabled])
	require.Equal(t, "每日签到", repo.values[service.SettingKeyCheckinContent])
	require.Equal(t, "1.50000000", repo.values[service.SettingKeyCheckinDailyReward])
	require.Equal(t, "4.00000000", repo.values[service.SettingKeyCheckinExtraReward4])
	require.Equal(t, "16.00000000", repo.values[service.SettingKeyCheckinExtraReward16])
	require.Equal(t, "12", repo.values[service.SettingKeyAPIKeyDefaultGroupID])
}

func TestUpdateSettingsPartialPayloadAuditUsesPersistedValues(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySMTPFallbacks: `[{"host":"backup.example.com","port":465,"username":"backup","password":"secret","from_email":"backup@example.com","from_name":"Backup","use_tls":true}]`,
	})

	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, logs.String(), "risk_control_enabled")
	require.NotContains(t, logs.String(), "smtp_fallbacks")
}

func TestUpdateSettingsWritesPreservedCustomFields(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"smtp_fallbacks": []map[string]any{{
			"host":       "backup.example.com",
			"port":       465,
			"username":   "backup",
			"password":   "secret",
			"from_email": "backup@example.com",
			"from_name":  "Backup",
			"use_tls":    true,
		}},
		"quick_link_enabled":                     true,
		"quick_link_text":                        "帮助中心",
		"quick_link_url":                         "https://help.example.com",
		"affiliate_subscription_reward_group_id": 9,
		"affiliate_subscription_reward_days":     7,
		"checkin_enabled":                        true,
		"checkin_content":                        " 每日签到 ",
		"checkin_daily_reward":                   1.5,
		"checkin_extra_reward_4":                 4.0,
		"checkin_extra_reward_16":                16.0,
		"api_key_default_group_id":               12,
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `[{"host":"backup.example.com","port":465,"username":"backup","password":"secret","from_email":"backup@example.com","from_name":"Backup","use_tls":true}]`, repo.values[service.SettingKeySMTPFallbacks])
	require.Equal(t, "true", repo.values[service.SettingKeyQuickLinkEnabled])
	require.Equal(t, "帮助中心", repo.values[service.SettingKeyQuickLinkText])
	require.Equal(t, "https://help.example.com", repo.values[service.SettingKeyQuickLinkURL])
	require.Equal(t, "9", repo.values[service.SettingKeyAffiliateSubscriptionRewardGroup])
	require.Equal(t, "7", repo.values[service.SettingKeyAffiliateSubscriptionRewardDays])
	require.Equal(t, "true", repo.values[service.SettingKeyCheckinEnabled])
	require.Equal(t, "每日签到", repo.values[service.SettingKeyCheckinContent])
	require.Equal(t, "1.50000000", repo.values[service.SettingKeyCheckinDailyReward])
	require.Equal(t, "4.00000000", repo.values[service.SettingKeyCheckinExtraReward4])
	require.Equal(t, "16.00000000", repo.values[service.SettingKeyCheckinExtraReward16])
	require.Equal(t, "12", repo.values[service.SettingKeyAPIKeyDefaultGroupID])
}

// A full payload keeps whole-document semantics: fields explicitly set to their
// zero value are still cleared.
func TestUpdateSettingsFullPayloadStillClearsSentEmptyFields(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySiteName: "Example Gateway",
	})

	rec := doUpdateSettings(t, h, map[string]any{"site_name": ""}, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "", repo.values[service.SettingKeySiteName],
		"an explicitly sent empty value is a deliberate clear, not an omission")
}

// smtp_from_email is the one request field whose JSON name differs from its
// setting key; the alias keeps it from being treated as always-omitted.
func TestUpdateSettingsSMTPFromAliasIsWritable(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeySMTPFrom: "old@example.com",
	})

	rec := doUpdateSettings(t, h, map[string]any{"smtp_from_email": "new@example.com"}, nil)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, "new@example.com", repo.values[service.SettingKeySMTPFrom])
}

func TestUpdateSettingsGrokDefaultBaseURLModeIsWritable(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyGrokDefaultBaseURLMode: service.GrokDefaultBaseURLModeCLI,
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"grok_default_base_url_mode": service.GrokDefaultBaseURLModeEUWest1,
	}, nil)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.GrokDefaultBaseURLModeEUWest1, repo.values[service.SettingKeyGrokDefaultBaseURLMode])
}

func TestUpdateSettingsRejectsTwoCaptchaProviders(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyTurnstileEnabled:   "true",
		service.SettingKeyTurnstileSiteKey:   "site-key",
		service.SettingKeyTurnstileSecretKey: "turnstile-secret",
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"turnstile_enabled":                true,
		"turnstile_site_key":               "site-key",
		"turnstile_secret_key":             "turnstile-secret",
		"tencent_captcha_enabled":          true,
		"tencent_captcha_app_id":           "123456789",
		"tencent_captcha_app_secret_key":   "app-secret",
		"tencent_captcha_cloud_secret_id":  "cloud-secret-id",
		"tencent_captcha_cloud_secret_key": "cloud-secret-key",
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "cannot be enabled at the same time")
}

func TestUpdateSettingsRequiresFourTencentCaptchaCredentialsWhenEnabled(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"tencent_captcha_enabled": true,
		"tencent_captcha_app_id":  "123456789",
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "AppSecretKey")
}

func TestUpdateSettingsRetainsStoredTencentCaptchaCredentialsWhenInputsEmpty(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyTencentCaptchaAppSecretKey:   "stored-app-secret",
		service.SettingKeyTencentCaptchaCloudSecretID:  "stored-cloud-secret-id",
		service.SettingKeyTencentCaptchaCloudSecretKey: "stored-cloud-secret-key",
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"tencent_captcha_enabled":          true,
		"tencent_captcha_app_id":           "123456789",
		"tencent_captcha_app_secret_key":   "",
		"tencent_captcha_cloud_secret_id":  "",
		"tencent_captcha_cloud_secret_key": "",
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "stored-app-secret", repo.values[service.SettingKeyTencentCaptchaAppSecretKey])
	require.Equal(t, "stored-cloud-secret-id", repo.values[service.SettingKeyTencentCaptchaCloudSecretID])
	require.Equal(t, "stored-cloud-secret-key", repo.values[service.SettingKeyTencentCaptchaCloudSecretKey])
}

// 天御站点决定前端加载哪个 SDK 与服务端打哪个接入点，两端必须一致。
// 部分载荷把它重置回中国站，会让已配国际站的部署在下一次任意保存后整体失效。
func TestUpdateSettingsPartialPayloadKeepsTencentCaptchaRegion(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyTencentCaptchaRegion: service.TencentCaptchaRegionINTL,
	})

	rec := doUpdateSettings(t, h, map[string]any{"risk_control_enabled": true}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.TencentCaptchaRegionINTL,
		repo.values[service.SettingKeyTencentCaptchaRegion])
}

func TestUpdateSettingsNormalizesUnknownTencentCaptchaRegion(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyTencentCaptchaRegion: service.TencentCaptchaRegionINTL,
	})

	rec := doUpdateSettings(t, h, map[string]any{"tencent_captcha_region": "sgp"}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.TencentCaptchaRegionCN,
		repo.values[service.SettingKeyTencentCaptchaRegion],
		"未知站点必须落回中国站，不能写入无法识别的值")
}

func TestUpdateSettingsWritesTencentCaptchaRegionWhenSent(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{"tencent_captcha_region": "intl"}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, service.TencentCaptchaRegionINTL,
		repo.values[service.SettingKeyTencentCaptchaRegion])
}

func TestUpdateSettingsValidatesTencentCaptchaAppIDWhenEnabledFlagIsOmitted(t *testing.T) {
	h, _ := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyTencentCaptchaEnabled:        "true",
		service.SettingKeyTencentCaptchaAppID:          "123456789",
		service.SettingKeyTencentCaptchaAppSecretKey:   "stored-app-secret",
		service.SettingKeyTencentCaptchaCloudSecretID:  "stored-cloud-secret-id",
		service.SettingKeyTencentCaptchaCloudSecretKey: "stored-cloud-secret-key",
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"tencent_captcha_app_id": "not-a-number",
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "positive integer")
}
