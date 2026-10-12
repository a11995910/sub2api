package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

var ErrExcelBPSConfigSave = errors.New("Excel/BPS 配置保存失败")

const SettingKeyExcelBPSConfig = "excel_bps_config"
const ExcelBPSImageModeNative = "native"
const ExcelBPSImageModeRelay = "relay"

type ExcelBPSDefaults struct {
	AllModels              bool     `json:"all_models"`
	Models                 []string `json:"models"`
	OmitUnsupportedTools   bool     `json:"omit_unsupported_tools"`
	IgnoreEncryptedContent bool     `json:"ignore_encrypted_content"`
	AutoDisableOn403       bool     `json:"auto_disable_on_403"`
	CacheCreationAsInput   bool     `json:"cache_creation_as_input"`
}

type ExcelBPSImageRelaySettings struct {
	Enabled          bool                         `json:"enabled"`
	Mode             string                       `json:"mode"`
	BaseURL          string                       `json:"base_url"`
	BodyLimitMiB     int                          `json:"body_limit_mib"`
	BudgetMiB        int                          `json:"budget_mib"`
	MaxRequests      int                          `json:"max_requests"`
	Policy           string                       `json:"policy"`
	WarningRemaining int                          `json:"warning_remaining"`
	CompactReserve   int                          `json:"compact_reserve"`
	Limits           basispoints.ImageRelayLimits `json:"limits"`
}

type ExcelBPSConfig struct {
	Enabled  bool                       `json:"enabled"`
	Defaults ExcelBPSDefaults           `json:"defaults"`
	Images   ExcelBPSImageRelaySettings `json:"images"`
}

func DefaultExcelBPSConfig() ExcelBPSConfig {
	return ExcelBPSConfig{Enabled: true, Defaults: ExcelBPSDefaults{
		Models:                 []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra"},
		IgnoreEncryptedContent: true, AutoDisableOn403: true, CacheCreationAsInput: true,
	}, Images: ExcelBPSImageRelaySettings{Enabled: true, Mode: "native", BodyLimitMiB: 64, BudgetMiB: 1024, MaxRequests: 128,
		Policy: "off", WarningRemaining: 8, CompactReserve: 3, Limits: basispoints.DefaultImageRelayLimits()}}
}

func (s *SettingService) GetExcelBPSConfig(ctx context.Context) (ExcelBPSConfig, error) {
	cfg := DefaultExcelBPSConfig()
	if s == nil || s.settingRepo == nil {
		return cfg, nil
	}
	ctx, cancel := context.WithTimeout(ctx, gatewayForwardingDBTimeout)
	defer cancel()
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyExcelBPSConfig)
	if errors.Is(err, ErrSettingNotFound) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("Excel/BPS 配置损坏")
	}
	return cfg, validateExcelBPSConfig(&cfg)
}

func validateExcelBPSConfig(cfg *ExcelBPSConfig) error {
	if !cfg.Defaults.AllModels && len(cfg.Defaults.Models) == 0 {
		return fmt.Errorf("请至少选择一个默认模型")
	}
	seen := map[string]bool{}
	models := []string{}
	for _, model := range cfg.Defaults.Models {
		model = strings.TrimSpace(model)
		if model != "" && !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	cfg.Defaults.Models = models
	if !cfg.Defaults.AllModels && len(models) == 0 {
		return fmt.Errorf("默认模型不能为空")
	}
	x := &cfg.Images
	if x.Mode != "native" && x.Mode != "relay" {
		return fmt.Errorf("图片传输方式必须为 native 或 relay")
	}
	x.BaseURL = strings.TrimRight(strings.TrimSpace(x.BaseURL), "/")
	if x.BaseURL != "" || (x.Enabled && x.Mode == "relay") {
		if err := basispoints.ValidateImageRelayOrigin(x.BaseURL); err != nil {
			return err
		}
	}
	if x.BodyLimitMiB < 1 || x.BodyLimitMiB > basispoints.MaxImageBodyMiB || x.BudgetMiB < basispoints.MinImageBudgetMiB || x.BudgetMiB > basispoints.MaxImageBudgetMiB || x.BudgetMiB < x.BodyLimitMiB*8 || x.MaxRequests < 1 || x.MaxRequests > basispoints.MaxImageRequests {
		return fmt.Errorf("图片容量配置超出范围；总预算须至少为请求体上限的八倍")
	}
	if err := x.Limits.Validate(); err != nil {
		return err
	}
	if x.Policy != "off" && x.Policy != "warn" && x.Policy != "auto_compact" {
		return fmt.Errorf("未知图片限额策略")
	}
	if x.WarningRemaining < 1 || x.CompactReserve < 1 || (x.Policy == "warn" && (x.CompactReserve >= x.WarningRemaining || x.WarningRemaining >= x.Limits.MaxImages)) {
		return fmt.Errorf("图片阈值须满足：预留张数 < 提醒剩余张数 < 总张数")
	}
	return nil
}

func (s *SettingService) SaveExcelBPSConfig(ctx context.Context, cfg ExcelBPSConfig) error {
	if err := validateExcelBPSConfig(&cfg); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.settingRepo.Set(ctx, SettingKeyExcelBPSConfig, string(raw)); err != nil {
		return ErrExcelBPSConfigSave
	}
	return nil
}
func (s *SettingService) GetExcelBPSImageRelaySettings(ctx context.Context) (ExcelBPSImageRelaySettings, error) {
	cfg, err := s.GetExcelBPSConfig(ctx)
	cfg.Images.Enabled = cfg.Enabled && cfg.Images.Enabled
	return cfg.Images, err
}
func (s *OpenAIGatewayService) excelBPSGloballyEnabled(ctx context.Context) bool {
	cfg, err := s.settingService.GetExcelBPSConfig(ctx)
	return err == nil && cfg.Enabled
}

// 仅对选择了 BPS 的模型读取全局门禁；读取失败不能悄悄切回原生协议。
func (s *OpenAIGatewayService) excelBPSRouteEnabled(ctx context.Context, a *Account, model string) (bool, error) {
	if a == nil {
		return false, nil
	}
	return s.excelBPSRouteEnabledForUpstreamModel(ctx, a, a.GetMappedModel(model))
}

func (s *OpenAIGatewayService) excelBPSRouteEnabledForUpstreamModel(ctx context.Context, a *Account, model string) (bool, error) {
	if !a.isExcelBPSEnabledForUpstreamModel(model) {
		return false, nil
	}
	cfg, err := s.settingService.GetExcelBPSConfig(ctx)
	if err != nil {
		return false, fmt.Errorf("Excel/BPS 配置读取失败")
	}
	return cfg.Enabled, nil
}
