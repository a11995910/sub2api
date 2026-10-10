package service

import "strings"

func (a *Account) IsExcelBPSEnabled() bool {
	if a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.IsShadow() || a.IsOpenAIAgentIdentity() || a.IsOpenAIPersonalAccessToken() {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(a.GetCredential("plan_type")), "free") {
		return false
	}
	enabled, _ := a.Extra["openai_excel_bps"].(bool)
	return enabled
}

const ExcelBPSIgnoreEncryptedContentKey = "openai_excel_bps_ignore_encrypted_content"

// IsExcelBPSIgnoreEncryptedContentEnabled opts into replacing ciphertext that
// BPS cannot forward, such as sub-agent messages in an old Codex conversation,
// with an omission notice instead of rejecting the whole request.
func (a *Account) IsExcelBPSIgnoreEncryptedContentEnabled() bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	enabled, _ := a.Extra[ExcelBPSIgnoreEncryptedContentKey].(bool)
	return enabled
}

// IsExcelBPSCacheCreationAsInputEnabled controls local billing and downstream usage.
// The setting has no effect unless this account uses the Excel/BPS protocol.
func (a *Account) IsExcelBPSCacheCreationAsInputEnabled() bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	enabled, _ := a.Extra["openai_excel_bps_cache_creation_as_input"].(bool)
	return enabled
}

// IsExcelBPSAutoDisableOn403Enabled opts into disabling BPS after a generic 403.
func (a *Account) IsExcelBPSAutoDisableOn403Enabled() bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	enabled, _ := a.Extra["openai_excel_bps_auto_disable_on_403"].(bool)
	return enabled
}

// isExcelBPSAllModelsEnabled preserves legacy account-wide routing. An explicit
// list, including an empty or malformed list, never enables BPS for all models.
func (a *Account) isExcelBPSAllModelsEnabled() bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	_, scoped := a.Extra["openai_excel_bps_models"]
	return !scoped
}

// IsExcelBPSEnabledForModel selects the protocol after account model mapping.
// The list selects a protocol; it does not restrict access to other models.
func (a *Account) IsExcelBPSEnabledForModel(requestedModel string) bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	return a.isExcelBPSUpstreamModelEnabled(a.GetMappedModel(requestedModel))
}

func (a *Account) isExcelBPSUpstreamModelEnabled(model string) bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	raw, scoped := a.Extra["openai_excel_bps_models"]
	if !scoped {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	switch models := raw.(type) {
	case []string:
		for _, selected := range models {
			if strings.TrimSpace(selected) == model {
				return true
			}
		}
	case []any:
		for _, selected := range models {
			if name, ok := selected.(string); ok && strings.TrimSpace(name) == model {
				return true
			}
		}
	}
	return false
}

// 保存前检查适用账号和模型范围，禁止通过 API 绕过页面约束。
func hasExcelBPSExtra(extra map[string]any) bool {
	for key := range extra {
		if strings.HasPrefix(key, "openai_excel_bps") {
			return true
		}
	}
	return false
}
