package service

import "time"

func resolveImageRateMultiplier(apiKey *APIKey, effectiveGroupMultiplier float64) float64 {
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.ImageRateIndependent {
		if apiKey.Group.ImageRateMultiplier < 0 {
			return 0
		}
		return apiKey.Group.ImageRateMultiplier
	}
	return effectiveGroupMultiplier
}

func resolveVideoRateMultiplier(apiKey *APIKey, effectiveGroupMultiplier float64) float64 {
	if apiKey != nil && apiKey.Group != nil && apiKey.Group.VideoRateIndependent {
		if apiKey.Group.VideoRateMultiplier < 0 {
			return 0
		}
		return apiKey.Group.VideoRateMultiplier
	}
	return effectiveGroupMultiplier
}

// resolvePromoDiscountedVideoRateMultiplier 先解析视频独立倍率，再叠加请求时刻生效的
// 分组活动折扣。视频不受 token 高峰倍率影响，但与图片按次计费一样参与分组整体让利。
func resolvePromoDiscountedVideoRateMultiplier(apiKey *APIKey, effectiveGroupMultiplier float64, pricingAt time.Time) float64 {
	multiplier := resolveVideoRateMultiplier(apiKey, effectiveGroupMultiplier)
	if apiKey == nil || apiKey.Group == nil {
		return multiplier
	}
	return multiplier * apiKey.Group.PromoDiscountMultiplierAt(pricingAt)
}
