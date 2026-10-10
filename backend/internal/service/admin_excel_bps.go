package service

import (
	"context"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *adminServiceImpl) validateExcelBPSAccount(ctx context.Context, a *Account) error {
	if !hasExcelBPSExtra(a.Extra) {
		return nil
	}
	bad := func(message string) error { return infraerrors.BadRequest("OPENAI_EXCEL_BPS_INVALID", message) }
	for _, key := range []string{"openai_excel_bps", "openai_excel_bps_omit_unsupported_tools", "openai_excel_bps_ignore_encrypted_content", "openai_excel_bps_auto_disable_on_403", "openai_excel_bps_auto_recover_on_403", "openai_excel_bps_cache_creation_as_input"} {
		if v, ok := a.Extra[key]; ok {
			if _, valid := v.(bool); !valid {
				return bad(key + " 必须为布尔值")
			}
		}
	}
	if a.Extra["openai_excel_bps"] == true && !excelBPSAccountEligible(a) {
		return bad("仅支持非免费套餐的普通 OpenAI OAuth 账号")
	}
	if a.Extra["openai_excel_bps_mihomo"] == true {
		return bad("当前部署使用账号已有代理，不支持来源站点的 Mihomo 会话代理池")
	}
	if a.Extra[ExcelBPSAutoRecoverOn403Key] == true && a.Extra["openai_excel_bps_auto_disable_on_403"] != true {
		return bad("恢复探测需同时开启 403 自动关闭")
	}
	if raw, ok := a.Extra["openai_excel_bps_models"]; ok {
		var models []string
		switch v := raw.(type) {
		case []string:
			models = v
		case []any:
			for _, item := range v {
				name, ok := item.(string)
				if !ok {
					return bad("模型名称必须为字符串")
				}
				models = append(models, name)
			}
		default:
			return bad("模型范围必须为数组")
		}
		if len(models) == 0 || len(models) > 100 {
			return bad("请选择 1 至 100 个模型，全部模型需移除范围字段")
		}
		for _, model := range models {
			if strings.TrimSpace(model) == "" || len(model) > 200 {
				return bad("模型名称无效")
			}
		}
	}
	return s.validateExcelBPS403GroupSettings(ctx, a)
}
