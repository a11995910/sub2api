//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIVideoUsageMissingStatsTierStillBills(t *testing.T) {
	for _, subscribed := range []bool{false, true} {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		billingRepo := &openAIRecordUsageBillingRepoStub{}
		svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		svc.channelService = videoStatsTestChannelService([]AccountStatsPricingRule{{GroupIDs: []int64{10}, Pricing: []ChannelModelPricing{{
			Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo,
			Intervals: []PricingInterval{{TierLabel: "768p", PerRequestPrice: testPtrFloat64(0.12)}},
		}}}})
		key := &APIKey{ID: 11, GroupID: ptrVideoInt64(10), Group: &Group{ID: 10, RateMultiplier: 1,
			VideoModelPrices: map[string]map[string]float64{"minimax-h3": {"1080p": 0.5}},
		}}
		var subscription *UserSubscription
		if subscribed {
			key.Group.SubscriptionType = SubscriptionTypeSubscription
			subscription = &UserSubscription{ID: 3}
		}
		err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{RequestID: "video-missing-stats", Model: "minimax-h3", VideoCount: 1, VideoResolution: "1080p", VideoDurationSeconds: 5},
			APIKey: key, User: &User{ID: 7}, Account: &Account{ID: 17}, Subscription: subscription,
		})
		require.NoError(t, err)
		require.Equal(t, 1, billingRepo.calls, "统计缺档不能跳过客户账本")
		if subscribed {
			require.InDelta(t, 2.5, billingRepo.lastCmd.SubscriptionCost, 1e-9)
		} else {
			require.InDelta(t, 2.5, billingRepo.lastCmd.BalanceCost, 1e-9)
		}
		require.Equal(t, 1, usageRepo.calls)
		require.Nil(t, usageRepo.lastLog.AccountStatsCost, "缺失的成本不伪造为已配置成本")
		require.InDelta(t, 2.5, usageRepo.lastLog.ActualCost, 1e-9)
	}
}

func TestOpenAIVideoUsageSubscriptionUsesOriginalPriceSnapshot(t *testing.T) {
	for _, price := range []float64{0, 0.5} {
		usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
		billingRepo := &openAIRecordUsageBillingRepoStub{}
		svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
		key := &APIKey{ID: 11, GroupID: ptrVideoInt64(10), Group: &Group{
			ID: 10, SubscriptionType: SubscriptionTypeSubscription, RateMultiplier: 1,
			VideoModelPrices: map[string]map[string]float64{"public-video": {"1080p": price}},
		}}
		account := zycaTestAccount()
		account.Extra = map[string]any{"quota_limit": 100.0}
		cost, err := svc.EstimateVideoCostForAccount(context.Background(), account, key, "public-video", "1080p", 5)
		require.NoError(t, err)
		stats := &VideoAccountStatsSnapshot{Cost: testPtrFloat64(0.6), RateMultiplier: 0.5}
		// 提交后改价、改倍率并移除成本规则，扣费仍使用提交时快照。
		key.Group.VideoModelPrices = map[string]map[string]float64{"minimax-h3": {"1080p": 9}}
		account.RateMultiplier = testPtrFloat64(8)
		err = svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
			Result: &OpenAIForwardResult{RequestID: "video-subscription-snapshot", Model: "minimax-h3", BillingModel: "minimax-h3", UpstreamModel: "minimax-h3",
				VideoCount: 1, VideoResolution: "1080p", VideoDurationSeconds: 5},
			APIKey: key, User: &User{ID: 7}, Account: account, Subscription: &UserSubscription{ID: 3},
			ChannelUsageFields: ChannelUsageFields{OriginalModel: "public-video", ChannelMappedModel: "minimax-h3", BillingModelSource: BillingModelSourceChannelMapped},
			PrecalculatedCost:  cost, VideoAccountStatsSnapshot: stats,
		})
		require.NoError(t, err)
		require.Equal(t, 1, billingRepo.calls)
		require.InDelta(t, price*5, billingRepo.lastCmd.SubscriptionCost, 1e-9)
		require.InDelta(t, price*5*0.5, billingRepo.lastCmd.AccountQuotaCost, 1e-9)
		require.Zero(t, billingRepo.lastCmd.BalanceCost)
		require.Equal(t, 1, usageRepo.calls)
		require.InDelta(t, price*5, usageRepo.lastLog.TotalCost, 1e-9)
		require.InDelta(t, price*5, usageRepo.lastLog.ActualCost, 1e-9)
		require.Equal(t, "public-video", usageRepo.lastLog.RequestedModel)
		require.Equal(t, "minimax-h3", *usageRepo.lastLog.UpstreamModel)
		require.Equal(t, 0.6, *usageRepo.lastLog.AccountStatsCost)
		require.Equal(t, 0.5, *usageRepo.lastLog.AccountRateMultiplier)
	}
}
