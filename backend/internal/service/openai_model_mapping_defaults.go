package service

import "strings"

const openAIRequiredGPT55Model = "gpt-5.5"

var openAIRequiredModelMappings = []string{
	"codex-auto-review",
	"gpt-4o-audio-preview",
	"gpt-4o-realtime-preview",
	"gpt-5.2",
	"gpt-5.2-2025-12-11",
	"gpt-5.2-chat-latest",
	"gpt-5.2-pro",
	"gpt-5.2-pro-2025-12-11",
	"gpt-5.3-codex",
	"gpt-5.3-codex-spark",
	"gpt-5.4",
	"gpt-5.4-2026-03-05",
	"gpt-5.4-mini",
	openAIRequiredGPT55Model,
	"gpt-image-1",
	"gpt-image-1.5",
	"gpt-image-2",
}

// ensureRequiredOpenAIModelMappings 为 OpenAI 固定模型白名单补齐必须透传的模型。
// 旧前端、外部同步或批量导入可能传入固定旧白名单，导致真实可用账号被本地调度过滤。
// 空 mapping 表示允许所有模型，不能写入白名单，否则会把全量能力收窄。
func ensureRequiredOpenAIModelMappings(platform string, credentials map[string]any) map[string]any {
	if platform != PlatformOpenAI || len(credentials) == 0 {
		return credentials
	}

	rawMapping, exists := credentials["model_mapping"]
	if !exists {
		return credentials
	}
	mapping := stringMappingFromRaw(rawMapping)
	if len(mapping) == 0 {
		return credentials
	}
	if !isOpenAIRequiredModelMappingCandidate(mapping) {
		return credentials
	}

	updatedMapping := cloneRawModelMapping(rawMapping)
	if updatedMapping == nil {
		return credentials
	}
	changed := false
	for _, model := range openAIRequiredModelMappings {
		if _, exists := mapping[model]; exists {
			continue
		}
		updatedMapping[model] = model
		changed = true
	}
	if !changed {
		return credentials
	}

	out := cloneStringAnyMap(credentials)
	out["model_mapping"] = updatedMapping
	return out
}

func credentialsMayNeedRequiredOpenAIModelMappings(credentials map[string]any) bool {
	if len(credentials) == 0 {
		return false
	}
	mapping := stringMappingFromRaw(credentials["model_mapping"])
	if len(mapping) == 0 {
		return false
	}
	if !isOpenAIRequiredModelMappingCandidate(mapping) {
		return false
	}
	for _, model := range openAIRequiredModelMappings {
		if _, exists := mapping[model]; !exists {
			return true
		}
	}
	return false
}

func isOpenAIRequiredModelMappingCandidate(mapping map[string]string) bool {
	for key := range mapping {
		model := strings.TrimSpace(key)
		if model == "codex-auto-review" || isOpenAIGPT5FamilyModel(model) {
			return true
		}
	}
	return false
}

func isOpenAIGPT5FamilyModel(model string) bool {
	return model == "gpt-5" || matchWildcard("gpt-5.*", model) || matchWildcard("gpt-5-*", model)
}

func cloneStringAnyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

func cloneRawModelMapping(raw any) map[string]any {
	switch mapping := raw.(type) {
	case map[string]any:
		out := make(map[string]any, len(mapping)+1)
		for key, value := range mapping {
			out[key] = value
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(mapping)+1)
		for key, value := range mapping {
			out[key] = value
		}
		return out
	default:
		return nil
	}
}

func allAccountsUsePlatform(accounts []*Account, expectedPlatform string, expectedCount int) bool {
	if len(accounts) != expectedCount || expectedCount == 0 {
		return false
	}
	for _, account := range accounts {
		if account == nil || account.Platform != expectedPlatform {
			return false
		}
	}
	return true
}
