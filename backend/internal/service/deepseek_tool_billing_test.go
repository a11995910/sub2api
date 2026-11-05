package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestDeepSeekToolPriceMustBeExplicit(t *testing.T) {
	for _, price := range []*float64{nil, deepSeekPrice(-1), deepSeekPrice(math.NaN()), deepSeekPrice(math.Inf(1))} {
		_, err := CalculateDeepSeekToolCost(&Group{SearchPricePer1k: price}, 1, 1)
		require.ErrorIs(t, err, ErrDeepSeekToolPricingRequired)
	}
	free, err := CalculateDeepSeekToolCost(&Group{SearchPricePer1k: deepSeekPrice(0)}, 3, 2)
	require.NoError(t, err)
	require.Zero(t, free.ActualCost)
	require.Equal(t, string(BillingModePerRequest), free.BillingMode)
	failed, err := CalculateDeepSeekToolCost(nil, 0, 1)
	require.NoError(t, err)
	require.Zero(t, failed.TotalCost)
}

func TestDeepSeekToolBillingOnlyAndTokenSurcharge(t *testing.T) {
	ctx := context.Background()
	pricing := newStubPricingServiceFromJSON(t, `{"test-deepseek":{"input_cost_per_token":0.001,"output_cost_per_token":0.002,"litellm_provider":"deepseek","mode":"chat"}}`)
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, pricing)}
	key := &APIKey{Group: &Group{SearchPricePer1k: deepSeekPrice(2)}}
	calc := func(result *OpenAIForwardResult, models []string, tokens UsageTokens) (*CostBreakdown, error) {
		return svc.calculateOpenAIRecordUsageCost(ctx, result, key, models, 3, 1, 1, 2, tokens, "", nil, false, time.Time{})
	}
	// 独立工具即使带有未知展示模型和 token，也必须只按成功次数结算。
	only, err := calc(&OpenAIForwardResult{DeepSeekToolOnly: true, DeepSeekToolCalls: 2}, []string{"unpriced-tool"}, UsageTokens{InputTokens: 900})
	require.NoError(t, err)
	require.InDelta(t, 0.004, only.TotalCost, 1e-12)
	require.InDelta(t, 0.008, only.ActualCost, 1e-12)
	require.Equal(t, string(BillingModePerRequest), only.BillingMode)
	tokens := UsageTokens{InputTokens: 10, OutputTokens: 5}
	baseline, err := calc(&OpenAIForwardResult{}, []string{"test-deepseek"}, tokens)
	require.NoError(t, err)
	mixed, err := calc(&OpenAIForwardResult{DeepSeekToolCalls: 2}, []string{"test-deepseek"}, tokens)
	require.NoError(t, err)
	require.InDelta(t, baseline.TotalCost+0.004, mixed.TotalCost, 1e-12)
	require.InDelta(t, baseline.ActualCost+0.008, mixed.ActualCost, 1e-12)
	require.Equal(t, baseline.InputCost, mixed.InputCost)
	require.Equal(t, baseline.OutputCost, mixed.OutputCost)
	key.Group.SearchPricePer1k = nil
	_, err = calc(&OpenAIForwardResult{DeepSeekToolOnly: true, DeepSeekToolCalls: 1}, nil, UsageTokens{})
	require.ErrorIs(t, err, ErrDeepSeekToolPricingRequired)
}

func TestDeepSeekMissingModelPriceKeepsKnownToolCharge(t *testing.T) {
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	key := &APIKey{Group: &Group{SearchPricePer1k: deepSeekPrice(2)}}
	for _, rounds := range [][]OpenAIUsage{nil, {{InputTokens: 10}, {InputTokens: 15}}} {
		result := &OpenAIForwardResult{DeepSeekToolCalls: 2, DeepSeekModelUsages: rounds}
		cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"definitely-unpriced-model"}, 1, 1, 1, 2, UsageTokens{InputTokens: 25}, "", nil, false, time.Time{})
		require.NoError(t, err)
		require.InDelta(t, 0.004, cost.TotalCost, 1e-12)
		require.InDelta(t, 0.008, cost.ActualCost, 1e-12)
		require.Zero(t, cost.InputCost)
	}
}

func TestDeepSeekToolInflightUsesBaseRateAndAllRounds(t *testing.T) {
	ctx := context.Background()
	groupID := int64(1)
	key := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 2, SearchPricePer1k: deepSeekPrice(1)}}
	deps := inflightEstimateDeps{billing: NewBillingService(&config.Config{}, nil)}
	cost, priced := deps.estimate(ctx, key, InflightEstimateRequest{Kind: InflightEstimateDeepSeekTool, DeepSeekToolCalls: 3})
	require.True(t, priced)
	require.InDelta(t, 0.006, cost, 1e-12)
	req := InflightEstimateRequest{Kind: InflightEstimateToken, Model: "claude-sonnet-4-5", MaxTokens: 10, BodyBytes: 40}
	one := deps.estimateOne(ctx, key, req.Model, req, 3, 2)
	req.TokenRounds, req.DeepSeekToolCalls = 4, 2
	all := deps.estimateOne(ctx, key, req.Model, req, 3, 2)
	require.InDelta(t, one*4+0.004, all, 1e-12)
	key.Group.SearchPricePer1k = deepSeekPrice(0)
	cost, priced = deps.estimate(ctx, key, InflightEstimateRequest{Kind: InflightEstimateDeepSeekTool, DeepSeekToolCalls: 1})
	require.True(t, priced)
	require.Zero(t, cost)
}

func TestDeepSeekModelRoundsKeepContextTiersSeparate(t *testing.T) {
	pricing := newStubPricingServiceFromJSON(t, openAILadderCatalogJSON)
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, pricing)}
	key := &APIKey{Group: &Group{SearchPricePer1k: deepSeekPrice(2)}}
	calc := func(result *OpenAIForwardResult, tokens UsageTokens) *CostBreakdown {
		cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"gpt-5.4"}, 1, 1, 1, 1, tokens, "", nil, false, time.Time{})
		require.NoError(t, err)
		return cost
	}
	one := calc(&OpenAIForwardResult{}, UsageTokens{InputTokens: 200000, OutputTokens: 10})
	aggregated := calc(&OpenAIForwardResult{}, UsageTokens{InputTokens: 400000, OutputTokens: 20})
	rounds := calc(&OpenAIForwardResult{DeepSeekToolCalls: 1, DeepSeekModelUsages: []OpenAIUsage{
		{InputTokens: 200000, OutputTokens: 10}, {InputTokens: 200000, OutputTokens: 10},
	}}, UsageTokens{InputTokens: 400000, OutputTokens: 20})
	require.InDelta(t, one.TotalCost*2+0.002, rounds.TotalCost, 1e-12)
	require.Greater(t, aggregated.TotalCost, rounds.TotalCost, "合计上下文不能误触发长上下文高价")
	require.False(t, rounds.LongContextBillingApplied)
}

func TestDeepSeekModelRoundsBillPerRequestPerRound(t *testing.T) {
	billing := NewBillingService(&config.Config{}, nil)
	svc := &OpenAIGatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	key := &APIKey{Group: &Group{ID: 1, SearchPricePer1k: deepSeekPrice(1), ModelPricing: []ChannelModelPricing{{
		Models: []string{"per-request-test"}, BillingMode: BillingModePerRequest, PerRequestPrice: deepSeekPrice(0.02),
	}}}}
	result := &OpenAIForwardResult{DeepSeekToolCalls: 1, DeepSeekModelUsages: []OpenAIUsage{{InputTokens: 10}, {InputTokens: 15}}}
	cost, err := svc.calculateOpenAIRecordUsageCost(context.Background(), result, key, []string{"per-request-test"}, 1, 1, 1, 1, UsageTokens{InputTokens: 25}, "", nil, false, time.Time{})
	require.NoError(t, err)
	require.InDelta(t, 0.041, cost.ActualCost, 1e-12)
}

func TestDeepSeekToolsSchedulingCapability(t *testing.T) {
	a := &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test"}}
	require.True(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityDeepSeekTools))
	a.Platform = PlatformOpenAI
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityDeepSeekTools))
	a.Platform, a.Type = PlatformDeepseek, AccountTypeOAuth
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityDeepSeekTools))
	a.Type = AccountTypeAPIKey
	a.Credentials = map[string]any{"api_key": "test", "api_protocol": "adaptive", "api_base_urls": map[string]any{"anthropic": "https://third-party.example"}}
	require.False(t, a.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityDeepSeekTools), "不能把只配置第三方 Anthropic 的 Key 送往默认官方工具接口")
}

func TestDeepSeekRecordUsageKeepsRoundCostsAndAggregateCacheBuckets(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	billingRepo := &openAIRecordUsageBillingRepoStub{}
	svc := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	group := &Group{ID: 1, Platform: PlatformDeepseek, RateMultiplier: 2, SearchPricePer1k: deepSeekPrice(2), ModelPricing: []ChannelModelPricing{{
		Models: []string{"deepseek-flash"}, BillingMode: BillingModeToken,
		InputPrice: deepSeekPrice(0.001), OutputPrice: deepSeekPrice(0.002), CacheReadPrice: deepSeekPrice(0.0001),
	}}}
	cache := newEmptyChannelCache()
	cache.channelByGroupID[group.ID] = &Channel{ID: 1, Status: StatusActive, AccountStatsPricingRules: []AccountStatsPricingRule{{
		GroupIDs: []int64{group.ID}, Pricing: []ChannelModelPricing{
			{Models: []string{"deepseek-flash"}, BillingMode: BillingModePerRequest, PerRequestPrice: deepSeekPrice(0.1)},
			{Models: []string{"deepseek-tools-fetch"}, BillingMode: BillingModePerRequest, PerRequestPrice: deepSeekPrice(0.003)},
		},
	}}}
	cache.groupPlatform[group.ID], cache.loadedAt = PlatformDeepseek, time.Now()
	svc.channelService = &ChannelService{}
	svc.channelService.cache.Store(cache)
	svc.resolver = NewModelPricingResolver(svc.channelService, svc.billingService)
	result := &OpenAIForwardResult{Model: "deepseek-flash", UpstreamModel: "deepseek-flash", RequestID: "deepseek-round-cost-test",
		Usage: OpenAIUsage{InputTokens: 250, CacheReadInputTokens: 180, OutputTokens: 30}, DeepSeekToolCalls: 2,
		DeepSeekModelUsages: []OpenAIUsage{{InputTokens: 100, CacheReadInputTokens: 80, OutputTokens: 10}, {InputTokens: 150, CacheReadInputTokens: 100, OutputTokens: 20}},
	}
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: result,
		APIKey: &APIKey{ID: 2, GroupID: &group.ID, Group: group}, User: &User{ID: 3}, Account: &Account{ID: 4, Platform: PlatformDeepseek, Type: AccountTypeAPIKey},
	})
	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, 70, usageRepo.lastLog.InputTokens)
	require.Equal(t, 180, usageRepo.lastLog.CacheReadTokens)
	require.Equal(t, 30, usageRepo.lastLog.OutputTokens)
	require.InDelta(t, 0.152, usageRepo.lastLog.TotalCost, 1e-12)
	require.InDelta(t, 0.304, usageRepo.lastLog.ActualCost, 1e-12)
	require.NotNil(t, usageRepo.lastLog.AccountStatsCost)
	require.InDelta(t, 0.206, *usageRepo.lastLog.AccountStatsCost, 1e-12)
	require.Equal(t, "deepseek_tools:2", *usageRepo.lastLog.BillingTier)
	require.Equal(t, 1, billingRepo.calls)
}

func deepSeekPrice(v float64) *float64 { return &v }
