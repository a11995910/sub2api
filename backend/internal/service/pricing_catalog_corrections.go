package service

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const gpt56SolCurrentCatalogPatch = `{
	"input_cost_per_token": 0.000004,
	"input_cost_per_token_priority": 0.000008,
	"input_cost_per_token_flex": 0.000002,
	"input_cost_per_token_batches": 0.000002,
	"output_cost_per_token": 0.00002,
	"output_cost_per_token_priority": 0.00004,
	"output_cost_per_token_flex": 0.00001,
	"output_cost_per_token_batches": 0.00001,
	"cache_creation_input_token_cost": 0.000005,
	"cache_creation_input_token_cost_priority": 0.00001,
	"cache_creation_input_token_cost_flex": 0.0000025,
	"cache_creation_input_token_cost_batches": 0.0000025,
	"cache_read_input_token_cost": 0.0000004,
	"cache_read_input_token_cost_priority": 0.0000008,
	"cache_read_input_token_cost_flex": 0.0000002,
	"cache_read_input_token_cost_batches": 0.0000002,
	"input_cost_per_token_above_272k_tokens": 0.000008,
	"input_cost_per_token_above_272k_tokens_priority": 0.000016,
	"input_cost_per_token_above_272k_tokens_flex": 0.000004,
	"input_cost_per_token_above_272k_tokens_batches": 0.000004,
	"output_cost_per_token_above_272k_tokens": 0.00003,
	"output_cost_per_token_above_272k_tokens_priority": 0.00006,
	"output_cost_per_token_above_272k_tokens_flex": 0.000015,
	"output_cost_per_token_above_272k_tokens_batches": 0.000015,
	"cache_creation_input_token_cost_above_272k_tokens": 0.00001,
	"cache_creation_input_token_cost_above_272k_tokens_priority": 0.00002,
	"cache_creation_input_token_cost_above_272k_tokens_flex": 0.000005,
	"cache_creation_input_token_cost_above_272k_tokens_batches": 0.000005,
	"cache_read_input_token_cost_above_272k_tokens": 0.0000008,
	"cache_read_input_token_cost_above_272k_tokens_priority": 0.0000016,
	"cache_read_input_token_cost_above_272k_tokens_flex": 0.0000004,
	"cache_read_input_token_cost_above_272k_tokens_batches": 0.0000004,
	"long_context_input_token_threshold": 272000,
	"long_context_input_cost_multiplier": 2,
	"long_context_output_cost_multiplier": 1.5,
	"supports_service_tier": true
}`

const gpt55CurrentFastCatalogPatch = `{
	"input_cost_per_token_priority": 0.0000125,
	"output_cost_per_token_priority": 0.000075,
	"cache_read_input_token_cost_priority": 0.00000125,
	"supports_service_tier": true
}`

// applyKnownPricingCatalogCorrections 只迁移已确认的旧目录指纹。修正在运营者
// override 之前执行，因此 override_file 仍拥有最高优先级。远端一旦发布不同价卡，
// 指纹不再匹配，本修正会自动停止，避免把未来价格永久钉死在代码中。
func applyKnownPricingCatalogCorrections(rawData map[string]json.RawMessage) map[string]json.RawMessage {
	corrections := []struct {
		model      string
		name       string
		patch      json.RawMessage
		matchesOld func(json.RawMessage) bool
	}{
		{
			model:      "gpt-5.6-sol",
			name:       "gpt-5.6-sol-2026-promo",
			patch:      json.RawMessage(gpt56SolCurrentCatalogPatch),
			matchesOld: isKnownLegacyGPT56SolCatalogEntry,
		},
		{
			model:      "gpt-5.5",
			name:       "gpt-5.5-fast-2.5x",
			patch:      json.RawMessage(gpt55CurrentFastCatalogPatch),
			matchesOld: isKnownLegacyGPT55FastCatalogEntry,
		},
		{
			model:      "gpt-5.5-2026-04-23",
			name:       "gpt-5.5-2026-04-23-fast-2.5x",
			patch:      json.RawMessage(gpt55CurrentFastCatalogPatch),
			matchesOld: isKnownLegacyGPT55FastCatalogEntry,
		},
	}
	for _, correction := range corrections {
		raw, ok := rawData[correction.model]
		if !ok || !correction.matchesOld(raw) {
			continue
		}
		merged, ok := mergePricingOverrideEntry(raw, correction.patch)
		if !ok {
			continue
		}
		rawData[correction.model] = merged
		logger.L().Warn(
			"pricing.catalog_correction_applied",
			zap.String("model", correction.model),
			zap.String("correction", correction.name),
		)
	}
	return rawData
}

func isKnownLegacyGPT55FastCatalogEntry(raw json.RawMessage) bool {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return false
	}
	required := map[string]float64{
		"input_cost_per_token":                 5e-6,
		"input_cost_per_token_priority":        10e-6,
		"output_cost_per_token":                30e-6,
		"output_cost_per_token_priority":       60e-6,
		"cache_read_input_token_cost":          0.5e-6,
		"cache_read_input_token_cost_priority": 1e-6,
	}
	for name, want := range required {
		if !catalogNumericFieldMatches(fields, name, want, false) {
			return false
		}
	}
	optional := map[string]float64{
		"input_cost_per_token_above_272k_tokens":        10e-6,
		"output_cost_per_token_above_272k_tokens":       45e-6,
		"cache_read_input_token_cost_above_272k_tokens": 1e-6,
		"long_context_input_token_threshold":            272000,
		"long_context_input_cost_multiplier":            2,
		"long_context_output_cost_multiplier":           1.5,
	}
	for name, want := range optional {
		if !catalogNumericFieldMatches(fields, name, want, true) {
			return false
		}
	}
	for name := range fields {
		if !strings.Contains(name, "_above_") || !strings.Contains(name, "_cost") {
			continue
		}
		if _, known := optional[name]; !known {
			return false
		}
	}
	return true
}

func isKnownLegacyGPT56SolCatalogEntry(raw json.RawMessage) bool {
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return false
	}

	required := map[string]float64{
		"input_cost_per_token":        5e-6,
		"output_cost_per_token":       30e-6,
		"cache_read_input_token_cost": 0.5e-6,
	}
	for name, want := range required {
		if !catalogNumericFieldMatches(fields, name, want, false) {
			return false
		}
	}

	// 旧目录在不同快照中可能缺少 priority/cache-write/长上下文字段，也可能带
	// 完整的 above_272k 旧档。字段缺失可接受；字段存在时必须完整匹配已知旧值。
	optional := map[string]float64{
		"input_cost_per_token_priority":                              10e-6,
		"input_cost_per_token_flex":                                  2.5e-6,
		"input_cost_per_token_batches":                               2.5e-6,
		"output_cost_per_token_priority":                             60e-6,
		"output_cost_per_token_flex":                                 15e-6,
		"output_cost_per_token_batches":                              15e-6,
		"cache_creation_input_token_cost":                            6.25e-6,
		"cache_creation_input_token_cost_priority":                   12.5e-6,
		"cache_creation_input_token_cost_flex":                       3.125e-6,
		"cache_creation_input_token_cost_batches":                    3.125e-6,
		"cache_read_input_token_cost_priority":                       1e-6,
		"cache_read_input_token_cost_flex":                           0.25e-6,
		"cache_read_input_token_cost_batches":                        0.25e-6,
		"input_cost_per_token_above_272k_tokens":                     10e-6,
		"input_cost_per_token_above_272k_tokens_priority":            20e-6,
		"input_cost_per_token_above_272k_tokens_flex":                5e-6,
		"input_cost_per_token_above_272k_tokens_batches":             5e-6,
		"output_cost_per_token_above_272k_tokens":                    45e-6,
		"output_cost_per_token_above_272k_tokens_priority":           90e-6,
		"output_cost_per_token_above_272k_tokens_flex":               22.5e-6,
		"output_cost_per_token_above_272k_tokens_batches":            22.5e-6,
		"cache_creation_input_token_cost_above_272k_tokens":          12.5e-6,
		"cache_creation_input_token_cost_above_272k_tokens_priority": 25e-6,
		"cache_creation_input_token_cost_above_272k_tokens_flex":     6.25e-6,
		"cache_creation_input_token_cost_above_272k_tokens_batches":  6.25e-6,
		"cache_read_input_token_cost_above_272k_tokens":              1e-6,
		"cache_read_input_token_cost_above_272k_tokens_priority":     2e-6,
		"cache_read_input_token_cost_above_272k_tokens_flex":         0.5e-6,
		"cache_read_input_token_cost_above_272k_tokens_batches":      0.5e-6,
		"long_context_input_token_threshold":                         272000,
		"long_context_input_cost_multiplier":                         2,
		"long_context_output_cost_multiplier":                        1.5,
	}
	for name, want := range optional {
		if !catalogNumericFieldMatches(fields, name, want, true) {
			return false
		}
	}

	// 未知阈值的 input/output/cache above 字段表示远端已进入另一版价卡，不能覆盖。
	for name := range fields {
		if !strings.Contains(name, "_above_") || !strings.Contains(name, "_cost") {
			continue
		}
		if _, known := optional[name]; !known {
			return false
		}
	}
	return true
}

func catalogNumericFieldMatches(fields map[string]any, name string, want float64, optional bool) bool {
	value, ok := fields[name]
	if !ok {
		return optional
	}
	number, ok := value.(float64)
	return ok && math.Abs(number-want) <= 1e-15
}
