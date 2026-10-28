package service

import (
	"context"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

func zycaFixedVideoPrice(pricing *ChannelModelPricing) (float64, error) {
	if pricing == nil || pricing.BillingMode != BillingModePerRequest || pricing.PerRequestPrice == nil ||
		!validVideoStatsAmount(*pricing.PerRequestPrice) || len(pricing.Intervals) != 0 {
		return 0, errors.New("ZYCA 按次视频必须配置有效的默认单价，且不能包含分档价格")
	}
	return *pricing.PerRequestPrice, nil
}

func (s *OpenAIGatewayService) estimateZYCAVideoCost(ctx context.Context, apiKey *APIKey, model, resolution string, durationSeconds int) (*CostBreakdown, error) {
	if s == nil || s.billingService == nil || apiKey == nil || strings.TrimSpace(model) == "" || durationSeconds <= 0 {
		return nil, errors.New("ZYCA 视频计费配置不可用")
	}
	apiKey = s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey)
	resolved := s.resolveOpenAIChannelPricing(ctx, strings.TrimSpace(model), apiKey)
	if resolved == nil || resolved.Mode != BillingModePerRequest {
		return s.estimateZYCAVideoSecondsCost(ctx, apiKey, model, resolution, durationSeconds)
	}
	if resolved.Source == PricingSourceChannel && apiKeyHasConfiguredVideoPrice(apiKey, model, resolution) {
		return s.estimateZYCAVideoSecondsCost(ctx, apiKey, model, resolution, durationSeconds)
	}
	if _, err := zycaFixedVideoPrice(resolved.channelPricing); err != nil {
		return nil, err
	}
	if s.resolver == nil {
		return nil, errors.New("ZYCA 视频价格解析器不可用")
	}
	baseMultiplier := 1.0
	if s.cfg != nil {
		baseMultiplier = s.cfg.Default.RateMultiplier
	}
	if apiKey.GroupID != nil && apiKey.Group != nil {
		baseMultiplier = s.ResolveUserGroupRateMultiplier(ctx, apiKey.UserID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	}
	pricingAt := OpenAIPricingAtFromContext(ctx)
	if pricingAt.IsZero() {
		pricingAt = timezone.Now()
	}
	cost, err := s.billingService.CalculateCostUnified(CostInput{
		Ctx: ctx, Model: strings.TrimSpace(model), GroupID: apiKey.GroupID, Group: apiKey.Group,
		RequestCount: 1, UsageUnits: 1, PricingAt: pricingAt,
		RateMultiplier: resolvePromoDiscountedVideoRateMultiplier(apiKey, baseMultiplier, pricingAt),
		Resolver:       s.resolver, Resolved: resolved,
	})
	if err != nil {
		return nil, err
	}
	if cost == nil || !validVideoStatsAmount(cost.TotalCost) || !validVideoStatsAmount(cost.ActualCost) {
		return nil, errors.New("ZYCA 视频费用超出有效范围")
	}
	return cost, nil
}

func (s *OpenAIGatewayService) estimateZYCAVideoSecondsCost(ctx context.Context, apiKey *APIKey, model, resolution string, durationSeconds int) (*CostBreakdown, error) {
	cost, err := s.estimateVideoCost(ctx, apiKey, model, resolution, 1, true)
	if err != nil {
		return nil, err
	}
	if cost == nil || cost.BillingMode != string(BillingModeVideo) {
		return nil, errors.New("ZYCA 按秒视频价格配置无效")
	}
	applyCostBreakdownMultiplier(cost, float64(durationSeconds))
	return cost, nil
}
