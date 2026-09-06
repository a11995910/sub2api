package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
)

// VideoAccountStatsSnapshot 保存提交时的成本基数和账号倍率；Cost 为 nil 时沿用客户基价。
// 旧任务没有此快照，继续使用结算时的账号统计规则。
type VideoAccountStatsSnapshot struct {
	Cost           *float64 `json:"cost"`
	RateMultiplier float64  `json:"rate_multiplier"`
}

// EstimateVideoAccountStats 在预留余额前固定成本；ZYCA 必须命中明确的视频成本配置。
func (s *OpenAIGatewayService) EstimateVideoAccountStats(ctx context.Context, input VideoTaskReserveInput, totalCost float64) (*VideoAccountStatsSnapshot, error) {
	if input.Account == nil || input.Account.ID != input.AccountID {
		return nil, errors.New("视频成本账号信息不完整")
	}
	rate := input.Account.BillingRateMultiplier()
	if !validVideoStatsAmount(rate) {
		return nil, errors.New("视频账号成本倍率无效")
	}
	groupID := int64(0)
	if input.GroupID != nil {
		groupID = *input.GroupID
	}
	model := strings.TrimSpace(input.UpstreamModel)
	if model == "" {
		model = input.Model
	}
	required := ResolveOpenAIVideoRequestProfile(input.Account) == OpenAIVideoRequestProfileZYCA
	cost, err := resolveVideoAccountStatsCost(ctx, s.channelService, input.AccountID, groupID,
		model, input.Resolution, 1, input.DurationSeconds, totalCost, required)
	if err != nil {
		return nil, err
	}
	return &VideoAccountStatsSnapshot{Cost: cost, RateMultiplier: rate}, nil
}

func resolveVideoAccountStatsCost(
	ctx context.Context, cs *ChannelService, accountID, groupID int64,
	model, resolution string, count, duration int, totalCost float64, required bool,
) (*float64, error) {
	missing := func() (*float64, error) {
		if required {
			return nil, fmt.Errorf("视频上游模型 %s 的 %s 尚未配置明确的每秒成本", model, resolution)
		}
		return nil, nil
	}
	if cs == nil || groupID <= 0 || strings.TrimSpace(model) == "" {
		return missing()
	}
	channel, err := cs.GetChannelForGroup(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("读取视频账号成本规则失败: %w", err)
	}
	if channel == nil {
		return missing()
	}
	platform := channelLookupPlatform(ctx, cs.GetGroupPlatform(ctx, groupID))
	for _, rule := range channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, groupID) {
			continue
		}
		pricing := findPricingForModel(rule.Pricing, platform, strings.ToLower(strings.TrimSpace(model)))
		if pricing == nil {
			continue
		}
		if pricing.BillingMode == BillingModeVideo {
			// 第一条命中的模型规则缺档时不可借用后续规则或客户售价。
			cost, err := calculateVideoStatsCost(pricing, resolution, count, duration)
			if err != nil {
				return nil, fmt.Errorf("视频上游模型 %s: %w", model, err)
			}
			return cost, nil
		}
		if required {
			return missing()
		}
		// 保留其他协议历史按次计费规则，视频不尝试 token 默认价。
		if pricing.BillingMode == BillingModePerRequest || pricing.BillingMode == BillingModeImage {
			return calculatePerRequestStatsCost(pricing, count), nil
		}
	}
	if !required && channel.ApplyPricingToAccountStats && validVideoStatsAmount(totalCost) {
		return &totalCost, nil
	}
	return missing()
}

func calculateVideoStatsCost(pricing *ChannelModelPricing, resolution string, count, duration int) (*float64, error) {
	resolution, known := LookupVideoBillingResolution(resolution)
	if !known || count <= 0 || duration <= 0 || pricing == nil || pricing.BillingMode != BillingModeVideo {
		return nil, errors.New("视频成本计费参数无效")
	}
	price := pricing.PerRequestPrice
	for _, tier := range pricing.Intervals {
		if normalized, ok := LookupVideoBillingResolution(tier.TierLabel); ok && normalized == resolution {
			price = tier.PerRequestPrice
			break
		}
	}
	if price == nil || !validVideoStatsAmount(*price) {
		return nil, fmt.Errorf("视频 %s 尚未配置有效的每秒成本", resolution)
	}
	cost := *price * float64(count) * float64(duration)
	if !validVideoStatsAmount(cost) {
		return nil, errors.New("视频成本超出有效范围")
	}
	return &cost, nil
}

func validVideoStatsAmount(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}
