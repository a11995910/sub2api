package service

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
)

// 账号开关只影响普通 OpenAI OAuth 根账号，按映射后的模型选择协议。
func (a *Account) IsExcelBPSEnabled() bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Type == AccountTypeOAuth && !a.IsShadow() && !a.IsOpenAIAgentIdentity() && !a.IsOpenAIPersonalAccessToken() && !strings.EqualFold(a.GetCredential("plan_type"), "free") && a.Extra["openai_excel_bps"] == true
}
func (a *Account) IsExcelBPSEnabledForModel(model string) bool {
	return a != nil && a.isExcelBPSEnabledForUpstreamModel(a.GetMappedModel(model))
}

func (a *Account) isExcelBPSEnabledForUpstreamModel(model string) bool {
	if !a.IsExcelBPSEnabled() {
		return false
	}
	raw, exists := a.Extra["openai_excel_bps_models"]
	if !exists {
		return true
	}
	switch values := raw.(type) {
	case []string:
		for _, v := range values {
			if strings.TrimSpace(v) == model {
				return true
			}
		}
	case []any:
		for _, v := range values {
			if s, ok := v.(string); ok && strings.TrimSpace(s) == model {
				return true
			}
		}
	}
	return false
}
func (a *Account) IsExcelBPSIgnoreEncryptedContentEnabled() bool {
	return a.IsExcelBPSEnabled() && a.Extra["openai_excel_bps_ignore_encrypted_content"] == true
}
func (a *Account) IsExcelBPSCacheCreationAsInputEnabled() bool {
	return a.IsExcelBPSEnabled() && a.Extra["openai_excel_bps_cache_creation_as_input"] == true
}
func (a *Account) excelBPSNativeFallbackReason(body []byte) string {
	if a.Extra["openai_excel_bps_omit_unsupported_tools"] == true {
		return ""
	}
	return basispoints.NativeFallbackReason(body)
}

func (a *Account) IsExcelOAuth() bool {
	return a != nil && a.IsOpenAIOAuthLike() && a.GetCredential("client_id") == openai.ExcelClientID
}
func validateExcelBPSAccount(a *Account) error {
	if a.Extra["openai_excel_bps"] != true {
		return nil
	}
	if !a.IsExcelBPSEnabled() {
		return fmt.Errorf("Excel/BPS 仅支持非免费套餐的普通 OpenAI OAuth 根账号")
	}
	if raw, ok := a.Extra["openai_excel_bps_models"]; ok {
		valid := false
		switch v := raw.(type) {
		case []string:
			valid = len(v) > 0
			for _, name := range v {
				valid = valid && strings.TrimSpace(name) != ""
			}
		case []any:
			valid = len(v) > 0
			for _, name := range v {
				str, ok := name.(string)
				valid = valid && ok && strings.TrimSpace(str) != ""
			}
		}
		if !valid {
			return fmt.Errorf("启用 Excel/BPS 时必须选择模型或开启所有模型")
		}
	}
	return nil
}

type AccountExcelBPSRepository interface {
	DisableExcelBPSOn403(context.Context, *Account, string) (bool, error)
}

func (a *Account) IsExcelBPSAutoDisableOn403Enabled() bool {
	return a.IsExcelBPSEnabled() && a.Extra["openai_excel_bps_auto_disable_on_403"] == true
}

func writeExcelOAuthRouteError(c *gin.Context) error {
	err := fmt.Errorf("Excel OAuth 仅支持已启用的 Excel/BPS 路由，请检查全局开关和账号模型范围")
	c.JSON(400, gin.H{"error": gin.H{"type": "invalid_request_error", "code": "excel_oauth_route_unavailable", "message": err.Error()}})
	return err
}

// 一次选号惰性读取一次全局门禁，避免每个普通账号都查询设置。
type excelBPSSelectionKey struct{}
type excelBPSSelectionConfig struct {
	once     sync.Once
	settings *SettingService
	enabled  bool
}

func (s *OpenAIGatewayService) withExcelBPSSelection(ctx context.Context) context.Context {
	if _, ok := ctx.Value(excelBPSSelectionKey{}).(*excelBPSSelectionConfig); ok {
		return ctx
	}
	return context.WithValue(ctx, excelBPSSelectionKey{}, &excelBPSSelectionConfig{settings: s.settingService})
}
func excelBPSSelectionAllows(ctx context.Context, a *Account, model string, compact bool) bool {
	if !a.IsExcelOAuth() {
		return true
	}
	if compact || !a.IsExcelBPSEnabledForModel(model) {
		return false
	}
	if cfg, ok := ctx.Value(excelBPSSelectionKey{}).(*excelBPSSelectionConfig); ok {
		cfg.once.Do(func() { value, err := cfg.settings.GetExcelBPSConfig(ctx); cfg.enabled = err == nil && value.Enabled })
		return cfg.enabled
	}
	// 未安装选号门禁上下文时保守排除专用账号。
	return false
}
