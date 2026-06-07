//go:build unit

package service

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type affiliateRepoSourceStub struct {
	summaries              map[int64]*AffiliateSummary
	invitees               []AffiliateInvitee
	accrueSourceOrderID    *int64
	accrueSourceRedeemCode *int64
}

func (r *affiliateRepoSourceStub) EnsureUserAffiliate(_ context.Context, userID int64) (*AffiliateSummary, error) {
	if summary, ok := r.summaries[userID]; ok {
		return summary, nil
	}
	return nil, ErrAffiliateProfileNotFound
}

func (r *affiliateRepoSourceStub) GetAffiliateByCode(context.Context, string) (*AffiliateSummary, error) {
	panic("unexpected GetAffiliateByCode call")
}

func (r *affiliateRepoSourceStub) BindInviter(context.Context, int64, int64) (bool, error) {
	panic("unexpected BindInviter call")
}

func (r *affiliateRepoSourceStub) AccrueQuota(_ context.Context, _ int64, _ int64, _ float64, _ int, sourceOrderID, sourceRedeemCodeID *int64) (bool, error) {
	r.accrueSourceOrderID = sourceOrderID
	r.accrueSourceRedeemCode = sourceRedeemCodeID
	return true, nil
}

func (r *affiliateRepoSourceStub) GetAccruedRebateFromInvitee(context.Context, int64, int64) (float64, error) {
	return 0, nil
}

func (r *affiliateRepoSourceStub) ThawFrozenQuota(context.Context, int64) (float64, error) {
	return 0, nil
}

func (r *affiliateRepoSourceStub) TransferQuotaToBalance(context.Context, int64) (float64, float64, error) {
	panic("unexpected TransferQuotaToBalance call")
}

func (r *affiliateRepoSourceStub) ListInvitees(context.Context, int64, int) ([]AffiliateInvitee, error) {
	return r.invitees, nil
}

func (r *affiliateRepoSourceStub) UpdateUserAffCode(context.Context, int64, string) error {
	panic("unexpected UpdateUserAffCode call")
}

func (r *affiliateRepoSourceStub) ResetUserAffCode(context.Context, int64) (string, error) {
	panic("unexpected ResetUserAffCode call")
}

func (r *affiliateRepoSourceStub) SetUserRebateRate(context.Context, int64, *float64) error {
	panic("unexpected SetUserRebateRate call")
}

func (r *affiliateRepoSourceStub) BatchSetUserRebateRate(context.Context, []int64, *float64) error {
	panic("unexpected BatchSetUserRebateRate call")
}

func (r *affiliateRepoSourceStub) ListUsersWithCustomSettings(context.Context, AffiliateAdminFilter) ([]AffiliateAdminEntry, int64, error) {
	panic("unexpected ListUsersWithCustomSettings call")
}

func (r *affiliateRepoSourceStub) ListAffiliateInviteRecords(context.Context, AffiliateRecordFilter) ([]AffiliateInviteRecord, int64, error) {
	panic("unexpected ListAffiliateInviteRecords call")
}

func (r *affiliateRepoSourceStub) ListAffiliateRebateRecords(context.Context, AffiliateRecordFilter) ([]AffiliateRebateRecord, int64, error) {
	panic("unexpected ListAffiliateRebateRecords call")
}

func (r *affiliateRepoSourceStub) ListAffiliateTransferRecords(context.Context, AffiliateRecordFilter) ([]AffiliateTransferRecord, int64, error) {
	panic("unexpected ListAffiliateTransferRecords call")
}

func (r *affiliateRepoSourceStub) GetAffiliateUserOverview(context.Context, int64) (*AffiliateUserOverview, error) {
	panic("unexpected GetAffiliateUserOverview call")
}

func TestAccrueInviteRebateForRedeemPassesRedeemSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inviterID := int64(10)
	inviteeID := int64(20)
	redeemCodeID := int64(30)
	repo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
		inviteeID: {UserID: inviteeID, InviterID: &inviterID, CreatedAt: time.Now()},
		inviterID: {UserID: inviterID, CreatedAt: time.Now()},
	}}
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled: "true",
	}}, &config.Config{})
	svc := &AffiliateService{repo: repo, settingService: settingSvc}

	rebate, err := svc.AccrueInviteRebateForRedeem(ctx, inviteeID, 100, &redeemCodeID)
	require.NoError(t, err)
	require.InDelta(t, 20, rebate, 1e-9)
	require.Nil(t, repo.accrueSourceOrderID)
	require.NotNil(t, repo.accrueSourceRedeemCode)
	require.Equal(t, redeemCodeID, *repo.accrueSourceRedeemCode)
}

// TestResolveRebateRatePercent_PerUserOverride verifies that per-inviter
// AffRebateRatePercent overrides the global rate, that NULL falls back to the
// global rate, and that out-of-range exclusive rates are clamped silently.
//
// SettingService is left nil here so globalRebateRatePercent returns the
// documented default (AffiliateRebateRateDefault = 20%) — this exercises the
// fallback path without spinning up a settings stub.
func TestResolveRebateRatePercent_PerUserOverride(t *testing.T) {
	t.Parallel()
	svc := &AffiliateService{}

	// nil exclusive rate → falls back to global default (20%)
	require.InDelta(t, AffiliateRebateRateDefault,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{}), 1e-9)

	// exclusive rate set → overrides global
	rate := 50.0
	require.InDelta(t, 50.0,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &rate}), 1e-9)

	// exclusive rate 0 → returns 0 (no rebate, intentional)
	zero := 0.0
	require.InDelta(t, 0.0,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &zero}), 1e-9)

	// exclusive rate above max → clamped to Max
	tooHigh := 250.0
	require.InDelta(t, AffiliateRebateRateMax,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &tooHigh}), 1e-9)

	// exclusive rate below min → clamped to Min
	tooLow := -5.0
	require.InDelta(t, AffiliateRebateRateMin,
		svc.resolveRebateRatePercent(context.Background(), &AffiliateSummary{AffRebateRatePercent: &tooLow}), 1e-9)
}

// TestIsEnabled_NilSettingServiceReturnsDefault verifies that IsEnabled
// safely handles a nil settingService dependency by returning the default
// (off). This protects callers from nil-pointer crashes in misconfigured
// environments.
func TestIsEnabled_NilSettingServiceReturnsDefault(t *testing.T) {
	t.Parallel()
	svc := &AffiliateService{}
	require.False(t, svc.IsEnabled(context.Background()))
	require.Equal(t, AffiliateEnabledDefault, svc.IsEnabled(context.Background()))
}

func TestAffiliateSubscriptionRewardConfigRequiresEnabledAffiliate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                 "false",
		SettingKeyAffiliateSubscriptionRewardGroup: "9",
		SettingKeyAffiliateSubscriptionRewardDays:  "3",
	}}, &config.Config{})
	svc := &AffiliateService{settingService: settingSvc}

	require.Equal(t, AffiliateSubscriptionRewardConfig{}, svc.GetSubscriptionRewardConfig(ctx))
}

func TestAffiliateSubscriptionRewardConfigAndInviter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	inviterID := int64(10)
	inviteeID := int64(20)
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                 "true",
		SettingKeyAffiliateSubscriptionRewardGroup: "9",
		SettingKeyAffiliateSubscriptionRewardDays:  "3",
	}}, &config.Config{})
	repo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
		inviteeID: {UserID: inviteeID, InviterID: &inviterID, CreatedAt: time.Now()},
	}}
	svc := &AffiliateService{repo: repo, settingService: settingSvc}

	require.Equal(t, AffiliateSubscriptionRewardConfig{GroupID: 9, ValidityDays: 3}, svc.GetSubscriptionRewardConfig(ctx))
	gotInviterID, err := svc.ResolveInviterID(ctx, inviteeID)
	require.NoError(t, err)
	require.Equal(t, inviterID, gotInviterID)
}

type affiliateRewardGroupReaderStub struct {
	group *Group
}

func (s affiliateRewardGroupReaderStub) GetByID(_ context.Context, id int64) (*Group, error) {
	if s.group == nil || s.group.ID != id {
		return nil, ErrGroupNotFound
	}
	return s.group, nil
}

type affiliateGroupAccessReaderStub struct {
	items map[int64]UserGroupAccessMeta
}

func (s affiliateGroupAccessReaderStub) ListActiveUserGroupAccessMeta(context.Context, int64) (map[int64]UserGroupAccessMeta, error) {
	return s.items, nil
}

func TestGetAffiliateDetailIncludesPaymentReward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	cases := []struct {
		name       string
		group      *Group
		wantMode   string
		wantRate   float64
		wantGroup  string
		wantUserID int64
	}{
		{
			name: "标准专属分组按倍率扣余额",
			group: &Group{
				ID:               9,
				Name:             "VIP 专线",
				IsExclusive:      true,
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeStandard,
				RateMultiplier:   0.7,
			},
			wantMode:   "standard_group_access",
			wantRate:   0.7,
			wantGroup:  "VIP 专线",
			wantUserID: 10,
		},
		{
			name: "订阅分组按订阅额度消耗",
			group: &Group{
				ID:               12,
				Name:             "Claude 订阅",
				Status:           StatusActive,
				SubscriptionType: SubscriptionTypeSubscription,
				RateMultiplier:   1,
			},
			wantMode:   "subscription_quota",
			wantRate:   1,
			wantGroup:  "Claude 订阅",
			wantUserID: 11,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
				SettingKeyAffiliateEnabled:                 "true",
				SettingKeyAffiliateSubscriptionRewardGroup: strconv.FormatInt(tc.group.ID, 10),
				SettingKeyAffiliateSubscriptionRewardDays:  "5",
			}}, &config.Config{})
			repo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
				tc.wantUserID: {UserID: tc.wantUserID, AffCode: "AFF-CODE", CreatedAt: time.Now()},
			}}
			svc := NewAffiliateService(repo, settingSvc, nil, nil)
			svc.SetRewardGroupReader(affiliateRewardGroupReaderStub{group: tc.group})

			detail, err := svc.GetAffiliateDetail(ctx, tc.wantUserID)
			require.NoError(t, err)
			require.NotNil(t, detail.PaymentReward)
			require.Equal(t, tc.group.ID, detail.PaymentReward.GroupID)
			require.Equal(t, tc.wantGroup, detail.PaymentReward.GroupName)
			require.Equal(t, 5, detail.PaymentReward.ValidityDays)
			require.Equal(t, tc.wantMode, detail.PaymentReward.RewardMode)
			require.InDelta(t, tc.wantRate, detail.PaymentReward.RateMultiplier, 1e-9)
		})
	}
}

func TestGetAffiliateDetailIncludesCurrentRewardExpiresAt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	expiresAt := time.Now().Add(5 * 24 * time.Hour)
	group := &Group{
		ID:               9,
		Name:             "邀请奖励分组",
		IsExclusive:      true,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
		RateMultiplier:   0.7,
	}
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                 "true",
		SettingKeyAffiliateSubscriptionRewardGroup: strconv.FormatInt(group.ID, 10),
		SettingKeyAffiliateSubscriptionRewardDays:  "5",
	}}, &config.Config{})
	repo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
		10: {UserID: 10, AffCode: "AFF-CODE", CreatedAt: time.Now()},
	}}
	svc := NewAffiliateService(repo, settingSvc, nil, nil)
	svc.SetRewardGroupReader(affiliateRewardGroupReaderStub{group: group})
	svc.SetGroupAccessReader(affiliateGroupAccessReaderStub{items: map[int64]UserGroupAccessMeta{
		group.ID: {
			GroupID:   group.ID,
			Source:    UserAllowedGroupSourceAffiliatePaymentReward,
			ExpiresAt: &expiresAt,
		},
	}})

	detail, err := svc.GetAffiliateDetail(ctx, 10)
	require.NoError(t, err)
	require.NotNil(t, detail.PaymentReward)
	require.NotNil(t, detail.PaymentReward.CurrentExpiresAt)
	require.WithinDuration(t, expiresAt, *detail.PaymentReward.CurrentExpiresAt, time.Second)
	require.Equal(t, UserAllowedGroupSourceAffiliatePaymentReward, detail.PaymentReward.AccessSource)
}

func TestGetAffiliateDetailOmitsCountdownForPermanentRewardAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	group := &Group{
		ID:               9,
		Name:             "邀请奖励分组",
		IsExclusive:      true,
		Status:           StatusActive,
		SubscriptionType: SubscriptionTypeStandard,
		RateMultiplier:   0.7,
	}
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                 "true",
		SettingKeyAffiliateSubscriptionRewardGroup: strconv.FormatInt(group.ID, 10),
		SettingKeyAffiliateSubscriptionRewardDays:  "5",
	}}, &config.Config{})
	repo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
		10: {UserID: 10, AffCode: "AFF-CODE", CreatedAt: time.Now()},
	}}
	svc := NewAffiliateService(repo, settingSvc, nil, nil)
	svc.SetRewardGroupReader(affiliateRewardGroupReaderStub{group: group})
	svc.SetGroupAccessReader(affiliateGroupAccessReaderStub{items: map[int64]UserGroupAccessMeta{
		group.ID: {
			GroupID:   group.ID,
			Source:    UserAllowedGroupSourceManual,
			Permanent: true,
		},
	}})

	detail, err := svc.GetAffiliateDetail(ctx, 10)
	require.NoError(t, err)
	require.NotNil(t, detail.PaymentReward)
	require.Nil(t, detail.PaymentReward.CurrentExpiresAt)
	require.Empty(t, detail.PaymentReward.AccessSource)
}

// TestValidateExclusiveRate_BoundaryAndInvalid covers the validator used by
// admin-facing rate setters: nil is always valid (clear), in-range values
// are accepted, NaN/Inf and out-of-range values produce a typed BadRequest.
func TestValidateExclusiveRate_BoundaryAndInvalid(t *testing.T) {
	t.Parallel()
	require.NoError(t, validateExclusiveRate(nil))

	for _, v := range []float64{0, 0.01, 50, 99.99, 100} {
		v := v
		require.NoError(t, validateExclusiveRate(&v), "value %v should be valid", v)
	}

	for _, v := range []float64{-0.01, 100.01, -100, 200} {
		v := v
		require.Error(t, validateExclusiveRate(&v), "value %v should be rejected", v)
	}

	nan := math.NaN()
	require.Error(t, validateExclusiveRate(&nan))
	posInf := math.Inf(1)
	require.Error(t, validateExclusiveRate(&posInf))
	negInf := math.Inf(-1)
	require.Error(t, validateExclusiveRate(&negInf))
}

func TestMaskEmail(t *testing.T) {
	t.Parallel()
	require.Equal(t, "a***@g***.com", maskEmail("alice@gmail.com"))
	require.Equal(t, "x***@d***", maskEmail("x@domain"))
	require.Equal(t, "", maskEmail(""))
}

func TestIsValidAffiliateCodeFormat(t *testing.T) {
	t.Parallel()

	// 邀请码格式校验同时服务于：
	// 1) 系统自动生成的 12 位随机码（A-Z 去 I/O，2-9 去 0/1）
	// 2) 管理员设置的自定义专属码（如 "VIP2026"、"NEW_USER-1"）
	// 因此校验放宽到 [A-Z0-9_-]{4,32}（要求调用方先 ToUpper）。
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"valid canonical 12-char", "ABCDEFGHJKLM", true},
		{"valid all digits 2-9", "234567892345", true},
		{"valid mixed", "A2B3C4D5E6F7", true},
		{"valid admin custom short", "VIP1", true},
		{"valid admin custom with hyphen", "NEW-USER", true},
		{"valid admin custom with underscore", "VIP_2026", true},
		{"valid 32-char max", "ABCDEFGHIJKLMNOPQRSTUVWXYZ012345", true},
		// Previously-excluded chars (I/O/0/1) are now allowed since admins may use them.
		{"letter I now allowed", "IBCDEFGHJKLM", true},
		{"letter O now allowed", "OBCDEFGHJKLM", true},
		{"digit 0 now allowed", "0BCDEFGHJKLM", true},
		{"digit 1 now allowed", "1BCDEFGHJKLM", true},
		{"too short (3 chars)", "ABC", false},
		{"too long (33 chars)", "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456", false},
		{"lowercase rejected (caller must ToUpper first)", "abcdefghjklm", false},
		{"empty", "", false},
		{"utf8 non-ascii", "ÄÄÄÄÄÄ", false}, // bytes out of charset
		{"ascii punctuation .", "ABCDEFGHJK.M", false},
		{"whitespace", "ABCDEFGHJK M", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, isValidAffiliateCodeFormat(tc.in))
		})
	}
}
