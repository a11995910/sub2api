package service

import "strings"

// RedactLegacyPrivateAccountExtra 清理已退役门票和 Excel 集成留下的凭据字段。
// 仅用于返回或导出账号数据，退役功能本身不会再读取或写入这些字段。
func RedactLegacyPrivateAccountExtra(extra map[string]any) map[string]any {
	if extra == nil {
		return nil
	}
	redacted := make(map[string]any, len(extra))
	for key, value := range extra {
		if key == "codex_harvest_proxy_url" || strings.HasPrefix(key, "codex_turn_ticket:") {
			continue
		}
		redacted[key] = value
	}
	return redacted
}
