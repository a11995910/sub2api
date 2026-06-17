//go:build unit

package service

import (
	"context"
	"database/sql"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/enttest"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

type redeemAffiliateRewardUserRepo struct {
	*mockUserRepo
	grants []TemporaryAllowedGroupGrantInput
}

func (r *redeemAffiliateRewardUserRepo) GrantTemporaryAllowedGroup(_ context.Context, input TemporaryAllowedGroupGrantInput) (*TemporaryAllowedGroupGrantResult, error) {
	r.grants = append(r.grants, input)
	expiresAt := time.Now().UTC().AddDate(0, 0, input.ValidityDays)
	return &TemporaryAllowedGroupGrantResult{
		UserID:    input.UserID,
		GroupID:   input.GroupID,
		ExpiresAt: &expiresAt,
	}, nil
}

func (r *redeemAffiliateRewardUserRepo) ExpireTemporaryAllowedGroups(context.Context, ExpireTemporaryAllowedGroupsInput) ([]ExpiredTemporaryAllowedGroupResult, error) {
	return nil, nil
}

func TestRedeemAffiliateRebateGrantsRewardGroupAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newAffiliateRewardTestClient(t, "redeem_reward_first")
	inviterID := int64(23)
	inviteeID := int64(351)
	redeemCodeID := int64(243)
	rewardGroupID := int64(31)

	affiliateRepo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
		inviteeID: {UserID: inviteeID, InviterID: &inviterID, CreatedAt: time.Now()},
		inviterID: {UserID: inviterID, CreatedAt: time.Now()},
	}}
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                 "true",
		SettingKeyAffiliateSubscriptionRewardGroup: "31",
		SettingKeyAffiliateSubscriptionRewardDays:  "5",
	}}, &config.Config{})
	affiliateSvc := NewAffiliateService(affiliateRepo, settingSvc, nil, nil)
	affiliateSvc.SetRewardGroupReader(affiliateRewardGroupReaderStub{group: &Group{
		ID:               rewardGroupID,
		Name:             "邀请奖励分组",
		Status:           StatusActive,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
	}})

	userRepo := &redeemAffiliateRewardUserRepo{mockUserRepo: &mockUserRepo{}}
	svc := &RedeemService{
		userRepo:         userRepo,
		entClient:        client,
		affiliateService: affiliateSvc,
	}

	svc.tryAccrueAffiliateRebateForRedeem(ctx, inviteeID, 50, redeemCodeID)

	require.Len(t, userRepo.grants, 1)
	grant := userRepo.grants[0]
	require.Equal(t, inviterID, grant.UserID)
	require.Equal(t, rewardGroupID, grant.GroupID)
	require.Equal(t, 5, grant.ValidityDays)
	require.Equal(t, UserAllowedGroupSourceAffiliatePaymentReward, grant.Source)
	require.Nil(t, grant.SourceOrderID)
	require.Contains(t, grant.Notes, "兑换码 243")
	require.Nil(t, affiliateRepo.accrueSourceOrderID)
	require.NotNil(t, affiliateRepo.accrueSourceRedeemCode)
	require.Equal(t, redeemCodeID, *affiliateRepo.accrueSourceRedeemCode)
}

func TestRedeemAffiliateRebateDoesNotGrantRewardGroupTwiceForSameInvitee(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client := newAffiliateRewardTestClient(t, "redeem_reward_once")
	inviterID := int64(23)
	inviteeID := int64(351)
	rewardGroupID := int64(31)

	affiliateRepo := &affiliateRepoSourceStub{summaries: map[int64]*AffiliateSummary{
		inviteeID: {UserID: inviteeID, InviterID: &inviterID, CreatedAt: time.Now()},
		inviterID: {UserID: inviterID, CreatedAt: time.Now()},
	}}
	settingSvc := NewSettingService(&settingRepoStub{values: map[string]string{
		SettingKeyAffiliateEnabled:                 "true",
		SettingKeyAffiliateSubscriptionRewardGroup: "31",
		SettingKeyAffiliateSubscriptionRewardDays:  "5",
	}}, &config.Config{})
	affiliateSvc := NewAffiliateService(affiliateRepo, settingSvc, nil, nil)
	affiliateSvc.SetRewardGroupReader(affiliateRewardGroupReaderStub{group: &Group{
		ID:               rewardGroupID,
		Name:             "邀请奖励分组",
		Status:           StatusActive,
		IsExclusive:      true,
		SubscriptionType: SubscriptionTypeStandard,
	}})

	userRepo := &redeemAffiliateRewardUserRepo{mockUserRepo: &mockUserRepo{}}
	svc := &RedeemService{
		userRepo:         userRepo,
		entClient:        client,
		affiliateService: affiliateSvc,
	}

	svc.tryAccrueAffiliateRebateForRedeem(ctx, inviteeID, 50, 243)
	svc.tryAccrueAffiliateRebateForRedeem(ctx, inviteeID, 60, 244)

	require.Len(t, userRepo.grants, 1)
	require.Equal(t, inviterID, userRepo.grants[0].UserID)
	require.Equal(t, rewardGroupID, userRepo.grants[0].GroupID)

	count, err := client.PaymentAuditLog.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func newAffiliateRewardTestClient(t *testing.T, name string) *dbent.Client {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	drv := entsql.OpenDB(dialect.SQLite, db)
	client := enttest.NewClient(t, enttest.WithOptions(dbent.Driver(drv)))
	t.Cleanup(func() { _ = client.Close() })
	return client
}
