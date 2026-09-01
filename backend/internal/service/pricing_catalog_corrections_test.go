package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

const legacyGPT56SolCatalogJSON = `{
	"gpt-5.6-sol": {
		"litellm_provider": "openai",
		"mode": "chat",
		"supports_service_tier": true,
		"input_cost_per_token": 5e-06,
		"input_cost_per_token_priority": 1e-05,
		"input_cost_per_token_flex": 2.5e-06,
		"input_cost_per_token_batches": 2.5e-06,
		"output_cost_per_token": 3e-05,
		"output_cost_per_token_priority": 6e-05,
		"output_cost_per_token_flex": 1.5e-05,
		"output_cost_per_token_batches": 1.5e-05,
		"cache_creation_input_token_cost": 6.25e-06,
		"cache_creation_input_token_cost_priority": 1.25e-05,
		"cache_creation_input_token_cost_flex": 3.125e-06,
		"cache_creation_input_token_cost_batches": 3.125e-06,
		"cache_read_input_token_cost": 5e-07,
		"cache_read_input_token_cost_priority": 1e-06,
		"cache_read_input_token_cost_flex": 2.5e-07,
		"cache_read_input_token_cost_batches": 2.5e-07
	}
}`

const legacyGPT56SolAboveCatalogJSON = `{
	"gpt-5.6-sol": {
		"litellm_provider": "openai",
		"mode": "chat",
		"input_cost_per_token": 5e-06,
		"input_cost_per_token_priority": 1e-05,
		"input_cost_per_token_flex": 2.5e-06,
		"input_cost_per_token_batches": 2.5e-06,
		"output_cost_per_token": 3e-05,
		"output_cost_per_token_priority": 6e-05,
		"output_cost_per_token_flex": 1.5e-05,
		"output_cost_per_token_batches": 1.5e-05,
		"cache_creation_input_token_cost": 6.25e-06,
		"cache_creation_input_token_cost_priority": 1.25e-05,
		"cache_creation_input_token_cost_flex": 3.125e-06,
		"cache_creation_input_token_cost_batches": 3.125e-06,
		"cache_read_input_token_cost": 5e-07,
		"cache_read_input_token_cost_priority": 1e-06,
		"cache_read_input_token_cost_flex": 2.5e-07,
		"cache_read_input_token_cost_batches": 2.5e-07,
		"input_cost_per_token_above_272k_tokens": 1e-05,
		"input_cost_per_token_above_272k_tokens_priority": 2e-05,
		"input_cost_per_token_above_272k_tokens_flex": 5e-06,
		"input_cost_per_token_above_272k_tokens_batches": 5e-06,
		"output_cost_per_token_above_272k_tokens": 4.5e-05,
		"output_cost_per_token_above_272k_tokens_priority": 9e-05,
		"output_cost_per_token_above_272k_tokens_flex": 2.25e-05,
		"output_cost_per_token_above_272k_tokens_batches": 2.25e-05,
		"cache_creation_input_token_cost_above_272k_tokens": 1.25e-05,
		"cache_creation_input_token_cost_above_272k_tokens_priority": 2.5e-05,
		"cache_creation_input_token_cost_above_272k_tokens_flex": 6.25e-06,
		"cache_creation_input_token_cost_above_272k_tokens_batches": 6.25e-06,
		"cache_read_input_token_cost_above_272k_tokens": 1e-06,
		"cache_read_input_token_cost_above_272k_tokens_priority": 2e-06,
		"cache_read_input_token_cost_above_272k_tokens_flex": 5e-07,
		"cache_read_input_token_cost_above_272k_tokens_batches": 5e-07
	}
}`

const legacyGPT55FastCatalogJSON = `{
	"gpt-5.5": {
		"litellm_provider": "openai",
		"mode": "chat",
		"input_cost_per_token": 5e-06,
		"input_cost_per_token_priority": 1e-05,
		"output_cost_per_token": 3e-05,
		"output_cost_per_token_priority": 6e-05,
		"cache_read_input_token_cost": 5e-07,
		"cache_read_input_token_cost_priority": 1e-06,
		"input_cost_per_token_above_272k_tokens": 1e-05,
		"output_cost_per_token_above_272k_tokens": 4.5e-05,
		"cache_read_input_token_cost_above_272k_tokens": 1e-06
	}
}`

func TestKnownGPT56SolCatalogCorrectionMigratesLegacySnapshots(t *testing.T) {
	for name, body := range map[string]string{
		"无长上下文字段":         legacyGPT56SolCatalogJSON,
		"完整 above_272k 档": legacyGPT56SolAboveCatalogJSON,
	} {
		t.Run(name, func(t *testing.T) {
			var rawData map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(body), &rawData))
			corrected := applyKnownPricingCatalogCorrections(rawData)

			var fields map[string]any
			require.NoError(t, json.Unmarshal(corrected["gpt-5.6-sol"], &fields))
			require.InDelta(t, 4e-6, fields["input_cost_per_token"], 1e-15)
			require.InDelta(t, 8e-6, fields["input_cost_per_token_priority"], 1e-15)
			require.InDelta(t, 2e-6, fields["input_cost_per_token_batches"], 1e-15)
			require.InDelta(t, 20e-6, fields["output_cost_per_token"], 1e-15)
			require.InDelta(t, 5e-6, fields["cache_creation_input_token_cost"], 1e-15)
			require.InDelta(t, 0.4e-6, fields["cache_read_input_token_cost"], 1e-15)
			require.InDelta(t, 272000, fields["long_context_input_token_threshold"], 1e-15)
			require.InDelta(t, 2, fields["long_context_input_cost_multiplier"], 1e-15)
			require.InDelta(t, 1.5, fields["long_context_output_cost_multiplier"], 1e-15)
		})
	}
}

func TestKnownGPT55FastCatalogCorrectionMigratesOnlyLegacyFingerprint(t *testing.T) {
	var legacy map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(legacyGPT55FastCatalogJSON), &legacy))
	legacy["gpt-5.5-2026-04-23"] = append(json.RawMessage(nil), legacy["gpt-5.5"]...)
	body, err := json.Marshal(legacy)
	require.NoError(t, err)

	svc := &PricingService{}
	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	for _, model := range []string{"gpt-5.5", "gpt-5.5-2026-04-23"} {
		pricing := data[model]
		require.NotNil(t, pricing, model)
		require.InDelta(t, 12.5e-6, pricing.InputCostPerTokenPriority, 1e-15, model)
		require.InDelta(t, 75e-6, pricing.OutputCostPerTokenPriority, 1e-15, model)
		require.InDelta(t, 1.25e-6, pricing.CacheReadInputTokenCostPriority, 1e-15, model)
		require.Equal(t, 272000, pricing.LongContextInputTokenThreshold, model)
	}
	svc.pricingData = data
	breakdown, err := NewBillingService(&config.Config{}, svc).CalculateCostWithServiceTier(
		"gpt-5.5-2026-04-23",
		UsageTokens{InputTokens: 2, OutputTokens: 1, CacheReadTokens: 3},
		1,
		"priority",
	)
	require.NoError(t, err)
	require.InDelta(t, 2*12.5e-6+75e-6+3*1.25e-6, breakdown.TotalCost, 1e-15)

	var future map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(`{"gpt-5.5": {
		"input_cost_per_token": 5e-06,
		"input_cost_per_token_priority": 1.1e-05,
		"output_cost_per_token": 3e-05,
		"output_cost_per_token_priority": 6.1e-05,
		"cache_read_input_token_cost": 5e-07,
		"cache_read_input_token_cost_priority": 1.1e-06
	}}`), &future))
	future["gpt-5.5-2026-04-23"] = append(json.RawMessage(nil), future["gpt-5.5"]...)
	corrected := applyKnownPricingCatalogCorrections(future)
	for _, model := range []string{"gpt-5.5", "gpt-5.5-2026-04-23"} {
		var fields map[string]any
		require.NoError(t, json.Unmarshal(corrected[model], &fields))
		require.InDelta(t, 11e-6, fields["input_cost_per_token_priority"], 1e-15, model)
		require.InDelta(t, 61e-6, fields["output_cost_per_token_priority"], 1e-15, model)
	}
}

func TestKnownGPT55FastCatalogCorrectionKeepsOperatorOverrideHighestPriority(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.5": {
		"input_cost_per_token_priority": 1.1e-05,
		"output_cost_per_token_priority": 6.1e-05,
		"cache_read_input_token_cost_priority": 1.1e-06,
		"long_context_input_token_threshold": 0
	}}`)
	data, err := svc.parsePricingData([]byte(legacyGPT55FastCatalogJSON))
	require.NoError(t, err)
	require.InDelta(t, 11e-6, data["gpt-5.5"].InputCostPerTokenPriority, 1e-15)
	require.InDelta(t, 61e-6, data["gpt-5.5"].OutputCostPerTokenPriority, 1e-15)
	require.InDelta(t, 1.1e-6, data["gpt-5.5"].CacheReadInputTokenCostPriority, 1e-15)
	require.Zero(t, data["gpt-5.5"].LongContextInputTokenThreshold)
}

func TestKnownGPT56SolCatalogCorrectionDoesNotPinFuturePrices(t *testing.T) {
	body := []byte(`{"gpt-5.6-sol": {
		"litellm_provider": "openai", "mode": "chat",
		"input_cost_per_token": 4.5e-06,
		"output_cost_per_token": 2.1e-05,
		"cache_read_input_token_cost": 4.5e-07
	}}`)
	var rawData map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &rawData))
	corrected := applyKnownPricingCatalogCorrections(rawData)

	var fields map[string]any
	require.NoError(t, json.Unmarshal(corrected["gpt-5.6-sol"], &fields))
	require.InDelta(t, 4.5e-6, fields["input_cost_per_token"], 1e-15)
	require.InDelta(t, 21e-6, fields["output_cost_per_token"], 1e-15)
	require.NotContains(t, fields, "long_context_input_token_threshold")
}

func TestKnownGPT56SolCatalogCorrectionKeepsOperatorOverrideHighestPriority(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.6-sol": {
		"input_cost_per_token": 9e-06,
		"input_cost_per_token_priority": 1.8e-05,
		"output_cost_per_token": 9e-05,
		"output_cost_per_token_priority": 1.8e-04,
		"cache_creation_input_token_cost": 1.125e-05,
		"cache_creation_input_token_cost_priority": 2.25e-05,
		"cache_read_input_token_cost": 9e-07,
		"cache_read_input_token_cost_priority": 1.8e-06,
		"long_context_input_token_threshold": 0
	}}`)
	data, err := svc.parsePricingData([]byte(legacyGPT56SolCatalogJSON))
	require.NoError(t, err)
	svc.pricingData = data

	pricing, err := NewBillingService(&config.Config{}, svc).GetModelPricing("gpt-5.6-sol")
	require.NoError(t, err)
	require.InDelta(t, 9e-6, pricing.InputPricePerToken, 1e-15)
	require.InDelta(t, 18e-6, pricing.InputPricePerTokenPriority, 1e-15)
	require.InDelta(t, 90e-6, pricing.OutputPricePerToken, 1e-15)
	require.InDelta(t, 180e-6, pricing.OutputPricePerTokenPriority, 1e-15)
	require.InDelta(t, 11.25e-6, pricing.CacheCreationPricePerToken, 1e-15)
	require.InDelta(t, 0.9e-6, pricing.CacheReadPricePerToken, 1e-15)
	require.Zero(t, pricing.LongContextInputThreshold)
}

func TestKnownGPT56SolCatalogCorrectionBillsAllTokenComponents(t *testing.T) {
	pricingSvc := &PricingService{}
	data, err := pricingSvc.parsePricingData([]byte(legacyGPT56SolCatalogJSON))
	require.NoError(t, err)
	pricingSvc.pricingData = data
	billing := NewBillingService(&config.Config{}, pricingSvc)
	resolver := NewModelPricingResolver(nil, billing)

	tests := []struct {
		name        string
		tokens      UsageTokens
		serviceTier string
		input       float64
		cacheRead   float64
		cacheWrite  float64
		output      float64
		longContext bool
	}{
		{name: "短上下文/Standard", tokens: UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72000, OutputTokens: 100}, input: 4e-6, cacheRead: 0.4e-6, cacheWrite: 5e-6, output: 20e-6},
		{name: "短上下文/Fast", tokens: UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72000, OutputTokens: 100}, serviceTier: "priority", input: 8e-6, cacheRead: 0.8e-6, cacheWrite: 10e-6, output: 40e-6},
		{name: "长上下文/Standard", tokens: UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72001, OutputTokens: 100}, input: 8e-6, cacheRead: 0.8e-6, cacheWrite: 10e-6, output: 30e-6, longContext: true},
		{name: "长上下文/Fast", tokens: UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72001, OutputTokens: 100}, serviceTier: "priority", input: 16e-6, cacheRead: 1.6e-6, cacheWrite: 20e-6, output: 60e-6, longContext: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacy, err := billing.CalculateCostWithServiceTier("gpt-5.6-sol", tt.tokens, 1, tt.serviceTier)
			require.NoError(t, err)
			unified, err := billing.CalculateCostUnified(CostInput{
				Ctx:            context.Background(),
				Model:          "gpt-5.6-sol",
				Tokens:         tt.tokens,
				RateMultiplier: 1,
				ServiceTier:    tt.serviceTier,
				Resolver:       resolver,
			})
			require.NoError(t, err)

			for entry, cost := range map[string]*CostBreakdown{"旧入口": legacy, "统一入口": unified} {
				t.Run(entry, func(t *testing.T) {
					require.InDelta(t, float64(tt.tokens.InputTokens)*tt.input, cost.InputCost, 1e-12)
					require.InDelta(t, float64(tt.tokens.CacheReadTokens)*tt.cacheRead, cost.CacheReadCost, 1e-12)
					require.InDelta(t, float64(tt.tokens.CacheCreationTokens)*tt.cacheWrite, cost.CacheCreationCost, 1e-12)
					require.InDelta(t, float64(tt.tokens.OutputTokens)*tt.output, cost.OutputCost, 1e-12)
					require.Equal(t, tt.longContext, cost.LongContextBillingApplied)
				})
			}
		})
	}
}
