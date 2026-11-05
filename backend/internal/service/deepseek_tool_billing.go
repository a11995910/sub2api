package service

import (
	"errors"
	"math"
)

var ErrDeepSeekToolPricingRequired = errors.New("DeepSeek Search/Fetch requires an explicit group search_price_per_1k; set 0 to allow free calls")

func deepSeekUsageTokens(usage OpenAIUsage) UsageTokens {
	return UsageTokens{
		InputTokens:  max(0, usage.InputTokens-usage.CacheReadInputTokens-usage.CacheCreationInputTokens),
		OutputTokens: usage.OutputTokens, CacheReadTokens: usage.CacheReadInputTokens,
		CacheCreationTokens:  usage.CacheCreationInputTokens,
		ImageInputTokens:     max(0, usage.ImageInputTokens-usage.ImageCacheReadTokens),
		ImageCacheReadTokens: usage.ImageCacheReadTokens, ImageOutputTokens: usage.ImageOutputTokens,
	}
}

// ValidateDeepSeekToolPricing 在调用上游前校验管理员售价，避免套用其他平台默认价。
func ValidateDeepSeekToolPricing(group *Group) error {
	if group == nil || group.SearchPricePer1k == nil || *group.SearchPricePer1k < 0 ||
		math.IsNaN(*group.SearchPricePer1k) || math.IsInf(*group.SearchPricePer1k, 0) {
		return ErrDeepSeekToolPricingRequired
	}
	return nil
}

// CalculateDeepSeekToolCost 使用显式单价和基础倍率，成功次数以真实工具响应为准。
func CalculateDeepSeekToolCost(group *Group, calls int, multiplier float64) (*CostBreakdown, error) {
	if calls <= 0 {
		return &CostBreakdown{BillingMode: string(BillingModePerRequest)}, nil
	}
	if err := ValidateDeepSeekToolPricing(group); err != nil {
		return nil, err
	}
	if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return nil, errors.New("invalid DeepSeek tool rate multiplier")
	}
	total := *group.SearchPricePer1k / 1000 * float64(calls)
	return &CostBreakdown{TotalCost: total, ActualCost: total * math.Max(0, multiplier), BillingMode: string(BillingModePerRequest)}, nil
}
