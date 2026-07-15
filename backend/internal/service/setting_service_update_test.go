//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type settingUpdateRepoStub struct {
	updates map[string]string
}

func (s *settingUpdateRepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *settingUpdateRepoStub) GetValue(ctx context.Context, key string) (string, error) {
	panic("unexpected GetValue call")
}

func (s *settingUpdateRepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingUpdateRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *settingUpdateRepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	s.updates = make(map[string]string, len(settings))
	for k, v := range settings {
		s.updates[k] = v
	}
	return nil
}

func (s *settingUpdateRepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingUpdateRepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

type settingAntigravityUARepoStub struct {
	values map[string]string
}

func (s *settingAntigravityUARepoStub) Get(ctx context.Context, key string) (*Setting, error) {
	panic("unexpected Get call")
}

func (s *settingAntigravityUARepoStub) GetValue(ctx context.Context, key string) (string, error) {
	if value, ok := s.values[key]; ok {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (s *settingAntigravityUARepoStub) Set(ctx context.Context, key, value string) error {
	panic("unexpected Set call")
}

func (s *settingAntigravityUARepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	panic("unexpected GetMultiple call")
}

func (s *settingAntigravityUARepoStub) SetMultiple(ctx context.Context, settings map[string]string) error {
	panic("unexpected SetMultiple call")
}

func (s *settingAntigravityUARepoStub) GetAll(ctx context.Context) (map[string]string, error) {
	panic("unexpected GetAll call")
}

func (s *settingAntigravityUARepoStub) Delete(ctx context.Context, key string) error {
	panic("unexpected Delete call")
}

type settingsGroupReaderStub struct {
	byID  map[int64]*Group
	errBy map[int64]error
	calls []int64
}

func (s *settingsGroupReaderStub) GetByID(ctx context.Context, id int64) (*Group, error) {
	s.calls = append(s.calls, id)
	if err, ok := s.errBy[id]; ok {
		return nil, err
	}
	if g, ok := s.byID[id]; ok {
		return g, nil
	}
	return nil, ErrGroupNotFound
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_ValidGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			11: {ID: 11, SubscriptionType: SubscriptionTypeSubscription},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 11, ValidityDays: 30},
		},
	})
	require.NoError(t, err)
	require.Equal(t, []int64{11}, groupReader.calls)

	raw, ok := repo.updates[SettingKeyDefaultSubscriptions]
	require.True(t, ok)

	var got []DefaultSubscriptionSetting
	require.NoError(t, json.Unmarshal([]byte(raw), &got))
	require.Equal(t, []DefaultSubscriptionSetting{
		{GroupID: 11, ValidityDays: 30},
	}, got)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsNonSubscriptionGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			12: {ID: 12, SubscriptionType: SubscriptionTypeStandard},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 12, ValidityDays: 7},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_INVALID", infraerrors.Reason(err))
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsNotFoundGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		errBy: map[int64]error{
			13: ErrGroupNotFound,
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 13, ValidityDays: 7},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_INVALID", infraerrors.Reason(err))
	require.Equal(t, "13", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsDuplicateGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			11: {ID: 11, SubscriptionType: SubscriptionTypeSubscription},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 11, ValidityDays: 30},
			{GroupID: 11, ValidityDays: 60},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_DUPLICATE", infraerrors.Reason(err))
	require.Equal(t, "11", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_DefaultSubscriptions_RejectsDuplicateGroupWithoutGroupReader(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		DefaultSubscriptions: []DefaultSubscriptionSetting{
			{GroupID: 11, ValidityDays: 30},
			{GroupID: 11, ValidityDays: 60},
		},
	})
	require.Error(t, err)
	require.Equal(t, "DEFAULT_SUBSCRIPTION_GROUP_DUPLICATE", infraerrors.Reason(err))
	require.Equal(t, "11", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_APIKeyDefaultGroup_ValidGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			21: {ID: 21, Status: StatusActive},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		APIKeyDefaultGroupID: 21,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{21}, groupReader.calls)
	require.Equal(t, "21", repo.updates[SettingKeyAPIKeyDefaultGroupID])
}

func TestSettingService_UpdateSettings_APIKeyDefaultGroup_RejectsInactive(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			21: {ID: 21, Status: StatusDisabled},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		APIKeyDefaultGroupID: 21,
	})
	require.Error(t, err)
	require.Equal(t, "API_KEY_DEFAULT_GROUP_INVALID", infraerrors.Reason(err))
	require.Equal(t, "21", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_AffiliateSubscriptionRewardGroup_ValidGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			31: {ID: 31, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AffiliateSubscriptionRewardGroupID: 31,
		AffiliateSubscriptionRewardDays:    7,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{31}, groupReader.calls)
	require.Equal(t, "31", repo.updates[SettingKeyAffiliateSubscriptionRewardGroup])
	require.Equal(t, "7", repo.updates[SettingKeyAffiliateSubscriptionRewardDays])
}

func TestSettingService_UpdateSettings_AffiliateSubscriptionRewardGroup_ValidSubscriptionGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			31: {ID: 31, Status: StatusActive, SubscriptionType: SubscriptionTypeSubscription},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AffiliateSubscriptionRewardGroupID: 31,
		AffiliateSubscriptionRewardDays:    7,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{31}, groupReader.calls)
	require.Equal(t, "31", repo.updates[SettingKeyAffiliateSubscriptionRewardGroup])
	require.Equal(t, "7", repo.updates[SettingKeyAffiliateSubscriptionRewardDays])
}

func TestSettingService_UpdateSettings_AffiliateSubscriptionRewardGroup_RejectsInvalidGroup(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	groupReader := &settingsGroupReaderStub{
		byID: map[int64]*Group{
			32: {ID: 32, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard},
		},
	}
	svc := NewSettingService(repo, &config.Config{})
	svc.SetSettingsGroupReader(groupReader)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AffiliateSubscriptionRewardGroupID: 32,
		AffiliateSubscriptionRewardDays:    7,
	})
	require.Error(t, err)
	require.Equal(t, "AFFILIATE_SUBSCRIPTION_REWARD_GROUP_INVALID", infraerrors.Reason(err))
	require.Equal(t, "32", infraerrors.FromError(err).Metadata["group_id"])
	require.Nil(t, repo.updates)
}

func TestSettingService_UpdateSettings_RegistrationEmailSuffixWhitelist_Normalized(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		RegistrationEmailSuffixWhitelist: []string{"example.com", "@EXAMPLE.com", " @foo.bar ", "*.EDU.CN"},
	})
	require.NoError(t, err)
	require.Equal(t, `["@example.com","@foo.bar","*.edu.cn"]`, repo.updates[SettingKeyRegistrationEmailSuffixWhitelist])
}

func TestSettingService_UpdateSettings_RegistrationEmailSuffixWhitelist_Invalid(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		RegistrationEmailSuffixWhitelist: []string{"@invalid_domain"},
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_REGISTRATION_EMAIL_SUFFIX_WHITELIST", infraerrors.Reason(err))
}

func TestParseDefaultSubscriptions_NormalizesValues(t *testing.T) {
	got := parseDefaultSubscriptions(`[{"group_id":11,"validity_days":30},{"group_id":11,"validity_days":60},{"group_id":0,"validity_days":10},{"group_id":12,"validity_days":99999}]`)
	require.Equal(t, []DefaultSubscriptionSetting{
		{GroupID: 11, ValidityDays: 30},
		{GroupID: 11, ValidityDays: 60},
		{GroupID: 12, ValidityDays: MaxValidityDays},
	}, got)
}

func TestSettingService_GetAffiliateSubscriptionRewardConfig_ClampsAndDefaults(t *testing.T) {
	svc := NewSettingService(&settingAntigravityUARepoStub{values: map[string]string{
		SettingKeyAffiliateSubscriptionRewardGroup: "41",
		SettingKeyAffiliateSubscriptionRewardDays:  "999999",
	}}, &config.Config{})

	groupID, days := svc.GetAffiliateSubscriptionRewardConfig(context.Background())
	require.Equal(t, int64(41), groupID)
	require.Equal(t, AffiliateSubscriptionRewardDaysMax, days)

	emptySvc := NewSettingService(&settingAntigravityUARepoStub{values: map[string]string{}}, &config.Config{})
	groupID, days = emptySvc.GetAffiliateSubscriptionRewardConfig(context.Background())
	require.Equal(t, int64(0), groupID)
	require.Equal(t, 0, days)
}

func TestSettingService_UpdateSettings_TablePreferences(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		TableDefaultPageSize: 50,
		TablePageSizeOptions: []int{20, 50, 100},
	})
	require.NoError(t, err)
	require.Equal(t, "50", repo.updates[SettingKeyTableDefaultPageSize])
	require.Equal(t, "[20,50,100]", repo.updates[SettingKeyTablePageSizeOptions])

	err = svc.UpdateSettings(context.Background(), &SystemSettings{
		TableDefaultPageSize: 1000,
		TablePageSizeOptions: []int{20, 100},
	})
	require.NoError(t, err)
	require.Equal(t, "1000", repo.updates[SettingKeyTableDefaultPageSize])
	require.Equal(t, "[20,100]", repo.updates[SettingKeyTablePageSizeOptions])
}

func TestSettingService_UpdateSettings_QuickLink(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		QuickLinkEnabled: true,
		QuickLinkText:    "  联系客服：点击进入 aibang.click，务必保存网址  ",
		QuickLinkURL:     "  https://aibang.click  ",
	})
	require.NoError(t, err)
	require.Equal(t, "true", repo.updates[SettingKeyQuickLinkEnabled])
	require.Equal(t, "联系客服：点击进入 aibang.click，务必保存网址", repo.updates[SettingKeyQuickLinkText])
	require.Equal(t, "https://aibang.click", repo.updates[SettingKeyQuickLinkURL])
}

func TestSettingService_UpdateSettings_QuickLinkRejectsInvalidEnabledConfig(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		QuickLinkEnabled: true,
		QuickLinkText:    "",
		QuickLinkURL:     "https://aibang.click",
	})
	require.Error(t, err)

	err = svc.UpdateSettings(context.Background(), &SystemSettings{
		QuickLinkEnabled: true,
		QuickLinkText:    "联系客服",
		QuickLinkURL:     "javascript:alert(1)",
	})
	require.Error(t, err)
}

func TestSettingService_UpdateSettings_PaymentVisibleMethodsAndAdvancedScheduler(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		PaymentVisibleMethodAlipaySource:  "alipay",
		PaymentVisibleMethodWxpaySource:   "easypay",
		PaymentVisibleMethodAlipayEnabled: true,
		PaymentVisibleMethodWxpayEnabled:  false,
		OpenAIAdvancedSchedulerEnabled:    true,
	})
	require.NoError(t, err)
	require.Equal(t, VisibleMethodSourceOfficialAlipay, repo.updates[SettingPaymentVisibleMethodAlipaySource])
	require.Equal(t, VisibleMethodSourceEasyPayWechat, repo.updates[SettingPaymentVisibleMethodWxpaySource])
	require.Equal(t, "true", repo.updates[SettingPaymentVisibleMethodAlipayEnabled])
	require.Equal(t, "false", repo.updates[SettingPaymentVisibleMethodWxpayEnabled])
	require.Equal(t, "true", repo.updates[openAIAdvancedSchedulerSettingKey])
}

func TestSettingService_UpdateSettings_AntigravityUserAgentVersion(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		AntigravityUserAgentVersion: "1.23.2",
	})
	require.NoError(t, err)
	require.Equal(t, "1.23.2", repo.updates[SettingKeyAntigravityUserAgentVersion])
}

func TestSettingService_UpdateSettings_APIKeyACLTrustForwardedIPRefreshesConfig(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	cfg := &config.Config{}
	svc := NewSettingService(repo, cfg)

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		APIKeyACLTrustForwardedIP: true,
	})
	require.NoError(t, err)
	require.Equal(t, "true", repo.updates[SettingKeyAPIKeyACLTrustForwardedIP])
	require.True(t, cfg.Security.TrustForwardedIPForAPIKeyACL)
	require.True(t, cfg.TrustForwardedIPForAPIKeyACL())
}

func TestSettingService_ParseSettings_APIKeyACLTrustForwardedIPFallsBackToConfigWhenMissing(t *testing.T) {
	cfg := &config.Config{}
	cfg.Security.TrustForwardedIPForAPIKeyACL = true
	svc := NewSettingService(&settingUpdateRepoStub{}, cfg)

	got := svc.parseSettings(map[string]string{})

	require.True(t, got.APIKeyACLTrustForwardedIP)
}

func TestSettingService_UpdateAndParseCheckinSettings(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		CheckinEnabled:       true,
		CheckinContent:       "  每日签到领灵石  ",
		CheckinDailyReward:   0.25,
		CheckinExtraReward4:  1,
		CheckinExtraReward16: 5,
	})
	require.NoError(t, err)
	require.Equal(t, "true", repo.updates[SettingKeyCheckinEnabled])
	require.Equal(t, "每日签到领灵石", repo.updates[SettingKeyCheckinContent])
	require.Equal(t, "0.25000000", repo.updates[SettingKeyCheckinDailyReward])
	require.Equal(t, "1.00000000", repo.updates[SettingKeyCheckinExtraReward4])
	require.Equal(t, "5.00000000", repo.updates[SettingKeyCheckinExtraReward16])

	got := svc.parseSettings(repo.updates)
	require.True(t, got.CheckinEnabled)
	require.Equal(t, "每日签到领灵石", got.CheckinContent)
	require.InDelta(t, 0.25, got.CheckinDailyReward, 0.0001)
	require.InDelta(t, 1, got.CheckinExtraReward4, 0.0001)
	require.InDelta(t, 5, got.CheckinExtraReward16, 0.0001)
}

func TestSettingService_UpdateSettings_RejectsEnabledCheckinWithoutReward(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{CheckinEnabled: true})
	require.Error(t, err)
	require.Equal(t, "INVALID_CHECKIN_REWARD", infraerrors.Reason(err))
	require.Nil(t, repo.updates)
}

func TestSettingService_GetAntigravityUserAgentVersion_Precedence(t *testing.T) {
	t.Run("后台设置优先", func(t *testing.T) {
		svc := NewSettingService(&settingAntigravityUARepoStub{values: map[string]string{
			SettingKeyAntigravityUserAgentVersion: "1.24.0",
		}}, &config.Config{})

		require.Equal(t, "1.24.0", svc.GetAntigravityUserAgentVersion(context.Background()))
	})

	t.Run("空值回退配置默认值", func(t *testing.T) {
		svc := NewSettingService(&settingAntigravityUARepoStub{values: map[string]string{
			SettingKeyAntigravityUserAgentVersion: "",
		}}, &config.Config{})

		require.Equal(t, antigravity.GetDefaultUserAgentVersion(), svc.GetAntigravityUserAgentVersion(context.Background()))
	})

	t.Run("缺失回退配置默认值", func(t *testing.T) {
		svc := NewSettingService(&settingAntigravityUARepoStub{values: map[string]string{}}, &config.Config{})

		require.Equal(t, antigravity.GetDefaultUserAgentVersion(), svc.GetAntigravityUserAgentVersion(context.Background()))
	})
}

func TestSettingService_UpdateSettings_RejectsInvalidPaymentVisibleMethodSource(t *testing.T) {
	repo := &settingUpdateRepoStub{}
	svc := NewSettingService(repo, &config.Config{})

	err := svc.UpdateSettings(context.Background(), &SystemSettings{
		PaymentVisibleMethodAlipaySource: "not-a-provider",
	})
	require.Error(t, err)
	require.Equal(t, "INVALID_PAYMENT_VISIBLE_METHOD_SOURCE", infraerrors.Reason(err))
	require.Nil(t, repo.updates)
}
