//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeTokenBreakdown_ImageInputZeroPriceSemantics(t *testing.T) {
	tokens := UsageTokens{InputTokens: 100, ImageInputTokens: 20}

	t.Run("未配置时回退文本输入价", func(t *testing.T) {
		pricing := &ModelPricing{InputPricePerToken: 0.001}

		cost := new(BillingService).computeTokenBreakdown(pricing, tokens, 1, "", false)

		require.InDelta(t, 0.08, cost.InputCost, 1e-12)
		require.InDelta(t, 0.02, cost.ImageInputCost, 1e-12)
	})

	t.Run("显式零价不回退", func(t *testing.T) {
		pricing := &ModelPricing{
			InputPricePerToken:      0.001,
			ImageInputPriceExplicit: true,
		}

		cost := new(BillingService).computeTokenBreakdown(pricing, tokens, 1, "", false)

		require.InDelta(t, 0.08, cost.InputCost, 1e-12)
		require.Zero(t, cost.ImageInputCost)
	})
}

func TestApplyChannelImageInputPrice_PreservesExplicitZero(t *testing.T) {
	zero := 0.0
	pricing := &ModelPricing{ImageInputPricePerToken: 1}

	applyChannelImageInputPrice(&ChannelModelPricing{ImageInputPrice: &zero}, pricing)

	require.Zero(t, pricing.ImageInputPricePerToken)
	require.True(t, pricing.ImageInputPriceExplicit)

	applyChannelImageInputPrice(&ChannelModelPricing{}, pricing)

	require.Zero(t, pricing.ImageInputPricePerToken)
	require.False(t, pricing.ImageInputPriceExplicit)
}
