package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeKnownOpenAICodexModel_BareGPT56RoutesToSol(t *testing.T) {
	tests := map[string]string{
		"gpt-5.6":            "gpt-5.6-sol",
		"openai/gpt-5.6":     "gpt-5.6-sol",
		"gpt5.6":             "gpt-5.6-sol",
		"gpt-5.6-high":       "gpt-5.6-sol",
		"gpt-5.6-max":        "gpt-5.6-sol",
		"gpt-5.6-2026-07-09": "gpt-5.6-sol",
		"openai/gpt-5.6-max": "gpt-5.6-sol",
	}

	for input, expected := range tests {
		t.Run(input, func(t *testing.T) {
			require.Equal(t, expected, normalizeKnownOpenAICodexModel(input))
		})
	}
}

func TestUsageBillingModelCandidates_BareGPT56IncludesSol(t *testing.T) {
	require.Equal(t,
		[]string{"gpt-5.6", "gpt-5.6-sol"},
		usageBillingModelCandidates("gpt-5.6"),
	)
	require.Equal(t,
		[]string{"openai/gpt-5.6", "gpt-5.6", "gpt-5.6-sol"},
		usageBillingModelCandidates("openai/gpt-5.6"),
	)
}

func TestResolveOpenAIFastModelPolicy_OfficialSKUs(t *testing.T) {
	tests := map[string]struct {
		canonical string
		ratio     float64
	}{
		"gpt-5.6-sol":       {canonical: "gpt-5.6-sol", ratio: 2},
		"gpt-5.6-terra":     {canonical: "gpt-5.6-terra", ratio: 2},
		"gpt-5.6-luna":      {canonical: "gpt-5.6-luna", ratio: 2},
		"gpt-5.5":           {canonical: "gpt-5.5", ratio: 2.5},
		"gpt-5.4":           {canonical: "gpt-5.4", ratio: 2},
		"gpt-5.4-mini":      {canonical: "gpt-5.4-mini", ratio: 2},
		"gpt-5.2":           {canonical: "gpt-5.2", ratio: 2},
		"gpt-5.1":           {canonical: "gpt-5.1", ratio: 2},
		"gpt-5":             {canonical: "gpt-5", ratio: 2},
		"gpt-5-mini":        {canonical: "gpt-5-mini", ratio: 1.8},
		"gpt-4.1":           {canonical: "gpt-4.1", ratio: 1.75},
		"gpt-4.1-mini":      {canonical: "gpt-4.1-mini", ratio: 1.75},
		"gpt-4.1-nano":      {canonical: "gpt-4.1-nano", ratio: 2},
		"gpt-4o":            {canonical: "gpt-4o", ratio: 1.7},
		"gpt-4o-2024-05-13": {canonical: "gpt-4o-2024-05-13", ratio: 1.75},
		"gpt-4o-mini":       {canonical: "gpt-4o-mini", ratio: 5.0 / 3.0},
		"o3":                {canonical: "o3", ratio: 1.75},
		"o4-mini":           {canonical: "o4-mini", ratio: 20.0 / 11.0},
		"gpt-5.3-codex":     {canonical: "gpt-5.3-codex", ratio: 2},
	}

	for model, expected := range tests {
		t.Run(model, func(t *testing.T) {
			policy, ok := resolveOpenAIFastModelPolicy(model)
			require.True(t, ok)
			require.Equal(t, expected.canonical, policy.CanonicalSKU)
			require.InDelta(t, expected.ratio, policy.FallbackRatio, 1e-12)
		})
	}
}

func TestResolveOpenAIFastModelPolicy_AcceptsOnlyKnownAliases(t *testing.T) {
	positive := map[string]string{
		" OpenAI/GPT5.4_MINI-HIGH ":        "gpt-5.4-mini",
		"openai/gpt-5.5-2026-08-31":        "gpt-5.5",
		"gpt 5.3 codex openai compact":     "gpt-5.3-codex",
		"gpt-5.4-chat-latest":              "gpt-5.4",
		"gpt-5.3":                          "gpt-5.3-codex",
		"gpt-5.3-none":                     "gpt-5.3-codex",
		"gpt-5.3-low":                      "gpt-5.3-codex",
		"gpt-5.3-medium":                   "gpt-5.3-codex",
		"gpt-5.3-high":                     "gpt-5.3-codex",
		"gpt-5.3-xhigh":                    "gpt-5.3-codex",
		"gpt-5-nano":                       "gpt-5.4",
		"gpt-5.2-codex":                    "gpt-5.2",
		"gpt-5.1-codex":                    "gpt-5.3-codex",
		"gpt-5.1-codex-max":                "gpt-5.3-codex",
		"gpt-5.1-codex-mini":               "gpt-5.3-codex",
		"codex-mini-latest":                "gpt-5.3-codex",
		"gpt-5-codex":                      "gpt-5.3-codex",
		"openai/o4_mini":                   "o4-mini",
		"gpt-5.6":                          "gpt-5.6-sol",
		"openai/gpt-5.6-max":               "gpt-5.6-sol",
		"openai/gpt-4o-2024-05-13":         "gpt-4o-2024-05-13",
		"provider/gpt-4.1-nano-2025-04-14": "gpt-4.1-nano",
	}
	for model, canonical := range positive {
		t.Run("positive/"+model, func(t *testing.T) {
			policy, ok := resolveOpenAIFastModelPolicy(model)
			require.True(t, ok)
			require.Equal(t, canonical, policy.CanonicalSKU)
		})
	}

	for _, model := range []string{
		"gpt-5.4-pro",
		"gpt-5.4-nano",
		"gpt-5.5-pro",
		"gpt-5.6-cyber",
		"gpt-5.6-sol-preview",
		"gpt-5.4-mini-preview",
		"gpt-4.1-turbo",
		"o3-pro",
		"gpt-5.3-codex-spark",
		"gpt-5.2-codex-high",
		"gpt-5.1-codex-preview",
		"codex-mini-latest-high",
		"gpt-5.4-chat-latest-preview",
		"gpt-5-nano-preview",
		"company-gpt-5.5",
		"gpt-5.5-2026-99-99",
	} {
		t.Run("negative/"+model, func(t *testing.T) {
			_, ok := resolveOpenAIFastModelPolicy(model)
			require.False(t, ok)
		})
	}
}

func TestNormalizeKnownOpenAICodexModel_GPT54ProDoesNotCollapseToGPT54(t *testing.T) {
	for _, model := range []string{"gpt-5.4-pro", "gpt5.4-pro", "openai/gpt-5.4-pro-high"} {
		require.Equal(t, "gpt-5.4-pro", normalizeKnownOpenAICodexModel(model), model)
	}
}
