//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestZYCAFixedVideoPriceValidation(t *testing.T) {
	for _, price := range []float64{0, 2.4, 7.25} {
		got, err := zycaFixedVideoPrice(&ChannelModelPricing{BillingMode: BillingModePerRequest, PerRequestPrice: &price})
		require.NoError(t, err)
		require.Equal(t, price, got)
	}
	for _, pricing := range []*ChannelModelPricing{
		nil,
		{BillingMode: BillingModePerRequest},
		{BillingMode: BillingModeImage, PerRequestPrice: testPtrFloat64(1)},
		{BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(-1)},
		{BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(math.NaN())},
		{BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(math.Inf(1))},
		{BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(2), Intervals: []PricingInterval{{TierLabel: "720p", PerRequestPrice: testPtrFloat64(3)}}},
	} {
		_, err := zycaFixedVideoPrice(pricing)
		require.Error(t, err)
	}
}

func TestZYCAFixedVideoUsesConfiguredPriceWithoutDurationMultiplier(t *testing.T) {
	for _, source := range []string{PricingSourceChannel, PricingSourceGroup} {
		for _, price := range []float64{0, 3.6, 7.25} {
			pricing := ChannelModelPricing{Platform: "anthropic", Models: []string{"wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: &price}
			svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
			svc.resolver = newResolverWithChannel(t, []ChannelModelPricing{pricing})
			key := &APIKey{Group: &Group{ID: 100}}
			if source == PricingSourceGroup {
				key.Group.ModelPricing = []ChannelModelPricing{pricing}
			}
			for _, resolution := range []string{"480p", "720p"} {
				for _, duration := range []int{5, 30} {
					cost, err := svc.EstimateVideoCostForAccount(context.Background(), zycaTestAccount(), key, "wan-3.0", resolution, duration)
					require.NoError(t, err)
					require.InDelta(t, price, cost.TotalCost, 1e-9)
					require.Equal(t, string(BillingModePerRequest), cost.BillingMode)
				}
			}
			account := zycaTestAccount()
			for _, protocol := range []string{"legacy", "unified_json"} {
				account.Credentials["video_request_profile"] = protocol
				_, err := svc.EstimateVideoCostForAccount(context.Background(), account, key, "wan-3.0", "4K", 5)
				require.ErrorContains(t, err, "每秒价格")
			}
		}
	}
}

func TestZYCAFixedAccountCostDoesNotUseCustomerPrice(t *testing.T) {
	cs := videoStatsTestChannelService([]AccountStatsPricingRule{{GroupIDs: []int64{10}, Pricing: []ChannelModelPricing{{
		Models: []string{"wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(2.4),
	}}}})
	for _, duration := range []int{5, 30} {
		cost, err := resolveVideoAccountStatsCost(context.Background(), cs, 20, 10, "wan-3.0", "720p", 1, duration, 99, true)
		require.NoError(t, err)
		require.InDelta(t, 2.4, *cost, 1e-9)
	}
}

func TestZYCAFixedVideoPreservesLegacyGroupVideoPrice(t *testing.T) {
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	svc.resolver = newResolverWithChannel(t, []ChannelModelPricing{{
		Platform: "anthropic", Models: []string{"wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(3.6),
	}})
	key := &APIKey{Group: &Group{ID: 100, VideoModelPrices: map[string]map[string]float64{"wan-3.0": {"720p": 0.2}}}}
	cost, err := svc.EstimateVideoCostForAccount(context.Background(), zycaTestAccount(), key, "wan-3.0", "720p", 5)
	require.NoError(t, err)
	require.InDelta(t, 1.0, cost.TotalCost, 1e-9)
}

func TestZYCAReservationRejectsMissingAccountEstimator(t *testing.T) {
	account := zycaTestAccount()
	repo := &fakeVideoTaskBillingRepo{}
	estimator := &fakeVideoTaskCostEstimator{cost: &CostBreakdown{ActualCost: 8}}
	svc := NewVideoTaskBillingService(repo, estimator, nil)
	_, err := svc.Reserve(context.Background(), VideoTaskReserveInput{
		RequestID: "缺少协议计费器", Platform: PlatformOpenAI, UserID: 7, APIKeyID: 11,
		AccountID: account.ID, Account: account, APIKey: &APIKey{ID: 11}, Model: "wan-3.0", Resolution: "720p", DurationSeconds: 30,
	})
	require.Error(t, err)
	require.Zero(t, estimator.calls)
	require.Nil(t, repo.task)
}

type zycaReservationEstimator struct {
	fakeVideoTaskCostEstimator
	accountCalls int
}

func (estimator *zycaReservationEstimator) EstimateVideoCostForAccount(context.Context, *Account, *APIKey, string, string, int) (*CostBreakdown, error) {
	estimator.accountCalls++
	return &CostBreakdown{TotalCost: 3.6, ActualCost: 3.6, BillingMode: string(BillingModePerRequest)}, nil
}

func TestVideoReservationUsesAccountPricingOnlyForZYCA(t *testing.T) {
	for _, protocol := range []string{"zyca", "legacy", "unified_json"} {
		t.Run(protocol, func(t *testing.T) {
			account := zycaTestAccount()
			account.Credentials["video_request_profile"] = protocol
			estimator := &zycaReservationEstimator{fakeVideoTaskCostEstimator: fakeVideoTaskCostEstimator{cost: &CostBreakdown{ActualCost: 8}}}
			svc := NewVideoTaskBillingService(&fakeVideoTaskBillingRepo{}, estimator, nil)
			task, err := svc.Reserve(context.Background(), VideoTaskReserveInput{
				RequestID: "隔离测试", Platform: PlatformOpenAI, UserID: 7, APIKeyID: 11,
				AccountID: account.ID, Account: account, APIKey: &APIKey{ID: 11}, Model: "wan-3.0", Resolution: "720p", DurationSeconds: 30,
			})
			require.NoError(t, err)
			if protocol == "zyca" {
				require.Equal(t, 1, estimator.accountCalls)
				require.Zero(t, estimator.calls)
				require.Equal(t, 3.6, task.EstimatedCost)
			} else {
				require.Zero(t, estimator.accountCalls)
				require.Equal(t, 1, estimator.calls)
				require.Equal(t, 8.0, task.EstimatedCost)
			}
		})
	}
}

func TestZYCAAliasPriceAndUpstreamCostRemainSeparate(t *testing.T) {
	account := zycaTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"zyca-wan-3.0": "wan-3.0"}
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	svc.resolver = newResolverWithChannel(t, []ChannelModelPricing{
		{Platform: "anthropic", Models: []string{"zyca-wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(3.6)},
		{Platform: "anthropic", Models: []string{"wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(9)},
	})
	key := &APIKey{GroupID: groupIDPtr(), Group: &Group{ID: 100, RateMultiplier: 1}}
	for _, multiplier := range []float64{1, 1.5} {
		key.Group.RateMultiplier = multiplier
		cost, err := svc.EstimateVideoCostForAccount(context.Background(), account, key, "zyca-wan-3.0", "720p", 30)
		require.NoError(t, err)
		require.InDelta(t, 3.6, cost.TotalCost, 1e-9)
		require.InDelta(t, 3.6*multiplier, cost.ActualCost, 1e-9)
	}
	svc.channelService = videoStatsTestChannelService([]AccountStatsPricingRule{{
		AccountIDs: []int64{account.ID}, GroupIDs: []int64{10}, Pricing: []ChannelModelPricing{
			{Models: []string{"wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(2.4)},
			{Models: []string{"zyca-wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(8)},
		},
	}})
	groupID := int64(10)
	snapshot, err := svc.EstimateVideoAccountStats(context.Background(), VideoTaskReserveInput{
		Account: account, AccountID: account.ID, GroupID: &groupID,
		Model: "zyca-wan-3.0", UpstreamModel: account.GetMappedModel("zyca-wan-3.0"),
		Resolution: "720p", DurationSeconds: 30,
	}, 3.6)
	require.NoError(t, err)
	require.NotNil(t, snapshot.Cost)
	require.InDelta(t, 2.4, *snapshot.Cost, 1e-9)
}

func TestZYCAAliasMissingPriceDoesNotBorrowUpstreamPrice(t *testing.T) {
	account := zycaTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"zyca-wan-3.0": "wan-3.0"}
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	svc.resolver = newResolverWithChannel(t, []ChannelModelPricing{{
		Platform: "anthropic", Models: []string{"wan-3.0"}, BillingMode: BillingModePerRequest, PerRequestPrice: testPtrFloat64(3.6),
	}})
	key := &APIKey{GroupID: groupIDPtr(), Group: &Group{ID: 100, RateMultiplier: 1}}
	_, err := svc.EstimateVideoCostForAccount(context.Background(), account, key, "zyca-wan-3.0", "720p", 30)
	require.Error(t, err)
}

func TestZYCAReservationRejectsMismatchedAccount(t *testing.T) {
	account := zycaTestAccount()
	repo := &fakeVideoTaskBillingRepo{}
	estimator := &zycaReservationEstimator{}
	svc := NewVideoTaskBillingService(repo, estimator, nil)
	_, err := svc.Reserve(context.Background(), VideoTaskReserveInput{
		RequestID: "账号不匹配", Platform: PlatformOpenAI, UserID: 7, APIKeyID: 11,
		AccountID: account.ID + 1, Account: account, APIKey: &APIKey{ID: 11}, Model: "wan-3.0", Resolution: "720p", DurationSeconds: 30,
	})
	require.ErrorContains(t, err, "账号不匹配")
	require.Zero(t, estimator.accountCalls)
	require.Zero(t, estimator.calls)
	require.Nil(t, repo.task)
}
