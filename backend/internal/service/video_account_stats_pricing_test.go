//go:build unit

package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoAccountStatsResolutionPricing(t *testing.T) {
	pricing := &ChannelModelPricing{BillingMode: BillingModeVideo, Intervals: []PricingInterval{
		{TierLabel: "768p", PerRequestPrice: testPtrFloat64(0.12)},
		{TierLabel: "1080p", PerRequestPrice: testPtrFloat64(0.24)},
		{TierLabel: "2K", PerRequestPrice: testPtrFloat64(0.24)},
		{TierLabel: "4K", PerRequestPrice: testPtrFloat64(0.26)},
	}}
	for _, tt := range []struct {
		resolution string
		want       float64
	}{
		{"768p", 1.2}, {"1080p", 2.4}, {"2k", 2.4}, {"4K", 2.6},
	} {
		t.Run(tt.resolution, func(t *testing.T) {
			cost, err := calculateVideoStatsCost(pricing, tt.resolution, 2, 5)
			require.NoError(t, err)
			require.InDelta(t, tt.want, *cost, 1e-9)
		})
	}
	_, err := calculateVideoStatsCost(pricing, "720p", 1, 5)
	require.ErrorContains(t, err, "每秒成本")
	pricing.PerRequestPrice = testPtrFloat64(0)
	cost, err := calculateVideoStatsCost(pricing, "720p", 1, 5)
	require.NoError(t, err)
	require.Zero(t, *cost)
	// 显式零价分档优先于默认价。
	pricing.PerRequestPrice = testPtrFloat64(9)
	pricing.Intervals[0].PerRequestPrice = testPtrFloat64(0)
	cost, err = calculateVideoStatsCost(pricing, "768p", 1, 5)
	require.NoError(t, err)
	require.Zero(t, *cost)
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		pricing.PerRequestPrice = &value
		_, err := calculateVideoStatsCost(pricing, "720p", 1, 5)
		require.Error(t, err)
	}
	for _, resolution := range []string{"", "8K"} {
		_, err := calculateVideoStatsCost(pricing, resolution, 1, 5)
		require.Error(t, err)
	}
	_, err = calculateVideoStatsCost(pricing, "4K", 1, 0)
	require.Error(t, err)
	require.Nil(t, calculateStatsCost(pricing, UsageTokens{OutputTokens: 100}, 1))
}

func videoStatsTestChannelService(rules []AccountStatsPricingRule) *ChannelService {
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{10},
		ApplyPricingToAccountStats: true, AccountStatsPricingRules: rules}
	return newTestChannelService(makeStandardRepo(channel, map[int64]string{10: PlatformOpenAI}))
}

func TestVideoAccountStatsMatchesUpstreamModelAndScope(t *testing.T) {
	rules := []AccountStatsPricingRule{
		{AccountIDs: []int64{99}, Pricing: []ChannelModelPricing{{Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(9)}}},
		{AccountIDs: []int64{20}, Pricing: []ChannelModelPricing{
			{Platform: PlatformGrok, Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(8)},
			{Platform: PlatformOpenAI, Models: []string{"minimax-*"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.24)},
			{Platform: PlatformOpenAI, Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.26)},
		}},
	}
	cs := videoStatsTestChannelService(rules)
	cost, err := resolveVideoAccountStatsCost(context.Background(), cs, 20, 10, "MiniMax-H3", "4K", 1, 5, 9, true)
	require.NoError(t, err)
	require.InDelta(t, 1.3, *cost, 1e-9)
	_, err = resolveVideoAccountStatsCost(context.Background(), cs, 20, 10, "public-video", "4K", 1, 5, 9, true)
	require.Error(t, err)
	_, err = resolveVideoAccountStatsCost(context.Background(), cs, 21, 10, "minimax-h3", "4K", 1, 5, 9, true)
	require.Error(t, err)
	rules[1].GroupIDs = []int64{10}
	cs = videoStatsTestChannelService(rules)
	cost, err = resolveVideoAccountStatsCost(context.Background(), cs, 21, 10, "minimax-h4", "4K", 1, 5, 9, true)
	require.NoError(t, err)
	require.InDelta(t, 1.2, *cost, 1e-9)
}

func TestVideoAccountStatsMissingPriceDoesNotBorrowCustomerPrice(t *testing.T) {
	for _, mode := range []BillingMode{BillingModeToken, BillingModeImage, BillingModePerRequest, BillingModeVideo} {
		t.Run(string(mode), func(t *testing.T) {
			cs := videoStatsTestChannelService([]AccountStatsPricingRule{
				{GroupIDs: []int64{10}, Pricing: []ChannelModelPricing{{Models: []string{"minimax-h3"}, BillingMode: mode,
					Intervals: []PricingInterval{{TierLabel: "768p", PerRequestPrice: testPtrFloat64(0.12)}}}}},
				{GroupIDs: []int64{10}, Pricing: []ChannelModelPricing{{Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, PerRequestPrice: testPtrFloat64(0.01)}}},
			})
			_, err := resolveVideoAccountStatsCost(context.Background(), cs, 20, 10, "minimax-h3", "4K", 1, 5, 99, true)
			require.Error(t, err)
		})
	}
	cs := videoStatsTestChannelService(nil)
	_, err := resolveVideoAccountStatsCost(context.Background(), cs, 20, 10, "minimax-h3", "4K", 1, 5, 99, true)
	require.Error(t, err)
	cost, err := resolveVideoAccountStatsCost(context.Background(), cs, 20, 10, "legacy-video", "720p", 1, 5, 0, false)
	require.NoError(t, err)
	require.NotNil(t, cost)
	require.Zero(t, *cost)
}

func TestVideoAccountStatsSnapshotKeepsCostAfterConfigChange(t *testing.T) {
	price := 0.26
	rules := []AccountStatsPricingRule{{AccountIDs: []int64{20}, Pricing: []ChannelModelPricing{{
		Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo, PerRequestPrice: &price,
	}}}}
	account := &Account{ID: 20, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		RateMultiplier: testPtrFloat64(0.5), Credentials: map[string]any{"video_request_profile": "zyca"}}
	svc := &OpenAIGatewayService{channelService: videoStatsTestChannelService(rules)}
	snapshot, err := svc.EstimateVideoAccountStats(context.Background(), VideoTaskReserveInput{
		Account: account, AccountID: 20, GroupID: ptrVideoInt64(10), Model: "public-video",
		UpstreamModel: "minimax-h3", Resolution: "4K", DurationSeconds: 5,
	}, 9)
	require.NoError(t, err)
	encoded, err := json.Marshal(VideoTaskUsageContext{AccountStatsSnapshot: snapshot})
	require.NoError(t, err)
	price = 100
	account.RateMultiplier = testPtrFloat64(2)
	var restored VideoTaskUsageContext
	require.NoError(t, json.Unmarshal(encoded, &restored))
	require.InDelta(t, 1.3, *restored.AccountStatsSnapshot.Cost, 1e-9)
	require.Equal(t, 0.5, restored.AccountStatsSnapshot.RateMultiplier)
}

func TestVideoAccountStatsRulesValidateTiers(t *testing.T) {
	for _, tt := range []struct {
		name, tier string
		price      *float64
		valid      bool
	}{
		{"768p", "768p", testPtrFloat64(0.12), true},
		{"2K", "2K", testPtrFloat64(0.24), true},
		{"零价", "4K", testPtrFloat64(0), true},
		{"缺价", "4K", nil, false},
		{"非法档位", "8K", testPtrFloat64(1), false},
		{"负价", "4K", testPtrFloat64(-1), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rules := []AccountStatsPricingRule{{AccountIDs: []int64{20}, Pricing: []ChannelModelPricing{{
				Models: []string{"minimax-h3"}, BillingMode: BillingModeVideo,
				Intervals: []PricingInterval{{TierLabel: tt.tier, PerRequestPrice: tt.price}},
			}}}}
			for _, err := range []error{validateAccountStatsPricingRules(rules), validateAccountStatsPricingRulesUpdate(nil, rules)} {
				if tt.valid {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			}
		})
	}
}
