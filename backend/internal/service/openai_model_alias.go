package service

import (
	"sort"
	"strings"
	"time"
)

type openAIFastModelPolicy struct {
	CanonicalSKU  string
	FallbackRatio float64
}

// 源列表保持可读，初始化时按 SKU 长度降序排列，避免 mini/nano 和日期快照
// 被较短的基础型号提前匹配。
var openAIFastModelPolicies = func() []openAIFastModelPolicy {
	policies := []openAIFastModelPolicy{
		{CanonicalSKU: "gpt-5.6-sol", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5.6-terra", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5.6-luna", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5.5", FallbackRatio: 2.5},
		{CanonicalSKU: "gpt-5.4", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5.4-mini", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5.2", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5.1", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5", FallbackRatio: 2},
		{CanonicalSKU: "gpt-5-mini", FallbackRatio: 1.8},
		{CanonicalSKU: "gpt-4.1", FallbackRatio: 1.75},
		{CanonicalSKU: "gpt-4.1-mini", FallbackRatio: 1.75},
		{CanonicalSKU: "gpt-4.1-nano", FallbackRatio: 2},
		{CanonicalSKU: "gpt-4o", FallbackRatio: 1.7},
		{CanonicalSKU: "gpt-4o-2024-05-13", FallbackRatio: 1.75},
		{CanonicalSKU: "gpt-4o-mini", FallbackRatio: 5.0 / 3.0},
		{CanonicalSKU: "o3", FallbackRatio: 1.75},
		{CanonicalSKU: "o4-mini", FallbackRatio: 20.0 / 11.0},
		{CanonicalSKU: "gpt-5.3-codex", FallbackRatio: 2},
	}
	sort.SliceStable(policies, func(i, j int) bool {
		return len(policies[i].CanonicalSKU) > len(policies[j].CanonicalSKU)
	})
	return policies
}()

func lastOpenAIModelSegment(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	if strings.Contains(model, "/") {
		parts := strings.Split(model, "/")
		model = parts[len(parts)-1]
	}
	return strings.TrimSpace(model)
}

func canonicalizeOpenAIModelSpelling(model string) string {
	model = strings.ToLower(lastOpenAIModelSegment(model))
	if model == "" {
		return ""
	}

	normalized := strings.ReplaceAll(model, "_", "-")
	normalized = strings.Join(strings.Fields(normalized), "-")
	for strings.Contains(normalized, "--") {
		normalized = strings.ReplaceAll(normalized, "--", "-")
	}

	if strings.HasPrefix(normalized, "gpt5") {
		normalized = "gpt-5" + strings.TrimPrefix(normalized, "gpt5")
	}
	replacements := []struct {
		from string
		to   string
	}{
		{"gpt-5.4mini", "gpt-5.4-mini"},
		{"gpt-5.4nano", "gpt-5.4-nano"},
		{"gpt-5.3-codexspark", "gpt-5.3-codex-spark"},
		{"gpt-5.3codexspark", "gpt-5.3-codex-spark"},
		{"gpt-5.3codex", "gpt-5.3-codex"},
	}
	for _, replacement := range replacements {
		normalized = strings.ReplaceAll(normalized, replacement.from, replacement.to)
	}
	return normalized
}

func canonicalizeOpenAIModelAliasSpelling(model string) string {
	normalized := canonicalizeOpenAIModelSpelling(model)
	if !strings.HasPrefix(normalized, "gpt-") && !strings.Contains(normalized, "codex") {
		return ""
	}
	return normalized
}

func isOpenAIFastAliasSuffix(suffix string) bool {
	switch suffix {
	case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra", "openai-compact":
		return true
	}
	if !isCodexDateSuffix(suffix) {
		return false
	}
	_, err := time.Parse("2006-01-02", suffix)
	return err == nil
}

func openAIModelMatchesFastSKU(normalized, sku string) bool {
	if normalized == sku {
		return true
	}
	suffix, ok := strings.CutPrefix(normalized, sku+"-")
	return ok && isOpenAIFastAliasSuffix(suffix)
}

// resolveOpenAIFastModelPolicy 是 Fast 能力声明和缺失价格兜底共用的 fail-closed
// 入口。允许 provider 前缀、拼写变体、日期快照、reasoning effort 与 compact
// 别名；官方 SKU 表之外的产品后缀一律不匹配。
func resolveOpenAIFastModelPolicy(model string) (openAIFastModelPolicy, bool) {
	normalized := canonicalizeOpenAIModelSpelling(model)
	if normalized == "" {
		return openAIFastModelPolicy{}, false
	}
	for _, policy := range openAIFastModelPolicies {
		if openAIModelMatchesFastSKU(normalized, policy.CanonicalSKU) {
			return policy, true
		}
	}
	// 仅继承 codexModelMap 中的精确已知映射；这样 gpt-5.2-codex、
	// codex-mini-latest 等实际会改写到官方 Fast SKU 的兼容别名保持一致，
	// preview、Pro、Spark 和未知产品后缀仍然 fail-closed。
	if mapped := getNormalizedCodexModel(normalized); mapped != "" && mapped != normalized {
		for _, policy := range openAIFastModelPolicies {
			if mapped == policy.CanonicalSKU {
				return policy, true
			}
		}
	}
	if normalized == "gpt-5.6" {
		return openAIFastModelPolicy{CanonicalSKU: "gpt-5.6-sol", FallbackRatio: 2}, true
	}
	if suffix, ok := strings.CutPrefix(normalized, "gpt-5.6-"); ok && isOpenAIFastAliasSuffix(suffix) {
		return openAIFastModelPolicy{CanonicalSKU: "gpt-5.6-sol", FallbackRatio: 2}, true
	}
	return openAIFastModelPolicy{}, false
}

func normalizeKnownOpenAICodexModel(model string) string {
	normalized := canonicalizeOpenAIModelAliasSpelling(model)
	if normalized == "" {
		return ""
	}

	if mapped := getNormalizedCodexModel(normalized); mapped != "" {
		return mapped
	}
	if strings.HasSuffix(normalized, "-openai-compact") {
		if mapped := getNormalizedCodexModel(strings.TrimSuffix(normalized, "-openai-compact")); mapped != "" {
			return mapped
		}
	}

	switch {
	case openAIModelMatchesFastSKU(normalized, "gpt-5.6-sol"):
		return "gpt-5.6-sol"
	case openAIModelMatchesFastSKU(normalized, "gpt-5.6-terra"):
		return "gpt-5.6-terra"
	case openAIModelMatchesFastSKU(normalized, "gpt-5.6-luna"):
		return "gpt-5.6-luna"
	case normalized == "gpt-5.6":
		return "gpt-5.6-sol"
	case strings.HasPrefix(normalized, "gpt-5.6-"):
		suffix := strings.TrimPrefix(normalized, "gpt-5.6-")
		if suffix == "max" || isKnownCodexModelSuffix(suffix) {
			return "gpt-5.6-sol"
		}
		return ""
	case strings.Contains(normalized, "gpt-5.5-pro"):
		return "gpt-5.5-pro"
	case strings.Contains(normalized, "gpt-5.5"):
		return "gpt-5.5"
	case strings.Contains(normalized, "gpt-5.4-pro"):
		return "gpt-5.4-pro"
	case strings.Contains(normalized, "gpt-5.4-mini"):
		return "gpt-5.4-mini"
	case strings.Contains(normalized, "gpt-5.4-nano"):
		return "gpt-5.4-nano"
	case strings.Contains(normalized, "gpt-5.4"):
		return "gpt-5.4"
	case strings.Contains(normalized, "gpt-5.2"):
		return "gpt-5.2"
	case strings.Contains(normalized, "gpt-5.3-codex-spark"):
		return "gpt-5.3-codex-spark"
	case strings.Contains(normalized, "gpt-5.3-codex"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "gpt-5.3"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "codex"):
		return "gpt-5.3-codex"
	case strings.Contains(normalized, "gpt-5"):
		return "gpt-5.4"
	default:
		return ""
	}
}

// isOpenAIGPT56Model 判断是否 GPT-5.6 系列模型；入参可为原始模型名
// （含大小写/路径/后缀变体）或已归一化的基名，两者均能正确识别。
func isOpenAIGPT56Model(model string) bool {
	normalized := canonicalizeOpenAIModelAliasSpelling(model)
	if normalized == "gpt-5.6" {
		return true
	}
	if suffix, ok := strings.CutPrefix(normalized, "gpt-5.6-"); ok && (suffix == "max" || isKnownCodexModelSuffix(suffix)) {
		return true
	}
	for _, prefix := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		if normalized == prefix || strings.HasPrefix(normalized, prefix+"-") {
			return true
		}
	}
	return false
}

func appendUsageBillingModelCandidate(candidates []string, seen map[string]struct{}, model string) []string {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return candidates
	}
	add := func(candidate string) {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return
		}
		key := strings.ToLower(candidate)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		candidates = append(candidates, candidate)
	}

	add(trimmed)
	if canonical := canonicalizeOpenAIModelAliasSpelling(trimmed); canonical != "" {
		add(canonical)
	}
	if normalized := normalizeKnownOpenAICodexModel(trimmed); normalized != "" {
		add(normalized)
	}
	return candidates
}

func usageBillingModelCandidates(primary string, alternates ...string) []string {
	seen := make(map[string]struct{}, 1+len(alternates))
	candidates := appendUsageBillingModelCandidate(nil, seen, primary)
	for _, alternate := range alternates {
		candidates = appendUsageBillingModelCandidate(candidates, seen, alternate)
	}
	return candidates
}

func firstUsageBillingModel(candidates []string) string {
	for _, candidate := range candidates {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
