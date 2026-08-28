package handler

import (
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const keyBillingInfoSchemaVersion = 1

type keyBillingInfoResponse struct {
	Object                  string     `json:"object"`
	SchemaVersion           int        `json:"schema_version"`
	BillingScope            string     `json:"billing_scope"`
	GroupRateMultiplier     float64    `json:"group_rate_multiplier"`
	UserRateMultiplier      *float64   `json:"user_rate_multiplier,omitempty"`
	ResolvedRateMultiplier  float64    `json:"resolved_rate_multiplier"`
	PeakRateEnabled         bool       `json:"peak_rate_enabled"`
	PeakStart               *string    `json:"peak_start,omitempty"`
	PeakEnd                 *string    `json:"peak_end,omitempty"`
	PeakRateMultiplier      *float64   `json:"peak_rate_multiplier,omitempty"`
	AppliedPeakMultiplier   *float64   `json:"applied_peak_multiplier,omitempty"`
	PromoDiscountEnabled    bool       `json:"promo_discount_enabled"`
	PromoDiscountStart      *time.Time `json:"promo_discount_start,omitempty"`
	PromoDiscountEnd        *time.Time `json:"promo_discount_end,omitempty"`
	PromoDiscountRate       *float64   `json:"promo_discount_rate,omitempty"`
	AppliedPromoMultiplier  *float64   `json:"applied_promo_multiplier,omitempty"`
	EffectiveRateMultiplier float64    `json:"effective_rate_multiplier"`
	Timezone                *string    `json:"timezone,omitempty"`
	ObservedAt              time.Time  `json:"observed_at"`
}

// KeyBillingInfo returns the token billing multiplier effective for the authenticated API key.
// GET /v1/sub2api/billing
func (h *GatewayHandler) KeyBillingInfo(c *gin.Context) {
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if h.cfg != nil && h.cfg.RunMode == config.RunModeSimple {
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "Billing information is not supported in simple mode")
		return
	}
	if apiKey.GroupID == nil {
		h.errorResponse(c, http.StatusForbidden, "permission_error", "API key is not assigned to a group")
		return
	}
	if apiKey.Group == nil {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "Billing information is unavailable")
		return
	}

	resolvedRate, ok := h.resolveKeyBillingRate(c, apiKey)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "Billing information is unavailable")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, buildKeyBillingInfo(apiKey, resolvedRate, timezone.Now()))
}

func (h *GatewayHandler) resolveKeyBillingRate(c *gin.Context, apiKey *service.APIKey) (float64, bool) {
	groupRate := apiKey.Group.RateMultiplier
	switch apiKey.Group.Platform {
	case service.PlatformOpenAI, service.PlatformGrok:
		if h.openAIGatewayService == nil {
			return 0, false
		}
		return h.openAIGatewayService.ResolveUserGroupRateMultiplier(c.Request.Context(), apiKey.UserID, *apiKey.GroupID, groupRate), true
	default:
		if h.gatewayService == nil {
			return 0, false
		}
		return h.gatewayService.ResolveUserGroupRateMultiplier(c.Request.Context(), apiKey.UserID, *apiKey.GroupID, groupRate), true
	}
}

func buildKeyBillingInfo(apiKey *service.APIKey, resolvedRate float64, now time.Time) keyBillingInfoResponse {
	groupRate := apiKey.Group.RateMultiplier
	var userRate *float64
	if resolvedRate != groupRate {
		userRate = &resolvedRate
	}
	appliedPeak := apiKey.Group.PeakMultiplierAt(now)
	// 活动折扣为分组整体让利：有效倍率 =（用户专属 ?? 分组默认）× 高峰因子 × 活动折扣，
	// 与计费路径 computePeakAwareMultipliers 的叠加顺序保持一致。
	appliedPromo := apiKey.Group.PromoDiscountMultiplierAt(now)

	response := keyBillingInfoResponse{
		Object:                  "sub2api.key_billing",
		SchemaVersion:           keyBillingInfoSchemaVersion,
		BillingScope:            "token",
		GroupRateMultiplier:     groupRate,
		UserRateMultiplier:      userRate,
		ResolvedRateMultiplier:  resolvedRate,
		PeakRateEnabled:         apiKey.Group.PeakRateEnabled,
		PromoDiscountEnabled:    apiKey.Group.PromoDiscountEnabled,
		EffectiveRateMultiplier: resolvedRate * appliedPeak * appliedPromo,
		ObservedAt:              now.UTC(),
	}
	if apiKey.Group.PeakRateEnabled {
		response.PeakStart = &apiKey.Group.PeakStart
		response.PeakEnd = &apiKey.Group.PeakEnd
		response.PeakRateMultiplier = &apiKey.Group.PeakRateMultiplier
		response.AppliedPeakMultiplier = &appliedPeak
		tz := timezone.Location().String()
		response.Timezone = &tz
	}
	if apiKey.Group.PromoDiscountEnabled {
		response.PromoDiscountStart = apiKey.Group.PromoDiscountStart
		response.PromoDiscountEnd = apiKey.Group.PromoDiscountEnd
		response.PromoDiscountRate = &apiKey.Group.PromoDiscountRate
		response.AppliedPromoMultiplier = &appliedPromo
	}
	return response
}
