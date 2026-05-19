package service

import "strings"

const openAIRequiredGPT55Model = "gpt-5.5"

// ensureRequiredOpenAIModelMappings 为 OpenAI GPT-5 系列白名单补齐必须透传的模型。
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
	if mappingSupportsRequestedModel(mapping, openAIRequiredGPT55Model) {
		return credentials
	}
	if !isOpenAIGPT5FamilyMapping(mapping) {
		return credentials
	}

	updatedMapping := cloneRawModelMapping(rawMapping)
	if updatedMapping == nil {
		return credentials
	}
	updatedMapping[openAIRequiredGPT55Model] = openAIRequiredGPT55Model

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
	return !mappingSupportsRequestedModel(mapping, openAIRequiredGPT55Model) && isOpenAIGPT5FamilyMapping(mapping)
}

func isOpenAIGPT5FamilyMapping(mapping map[string]string) bool {
	for key := range mapping {
		model := strings.TrimSpace(key)
		if strings.HasPrefix(model, "gpt-5.") || strings.HasPrefix(model, "gpt-5-") || model == "gpt-5" {
			return true
		}
	}
	return false
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
