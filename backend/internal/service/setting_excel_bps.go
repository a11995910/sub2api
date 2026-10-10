package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

const SettingKeyExcelBPSEnabled = "excel_bps_enabled"
const SettingKeyExcelBPSDefaults = "excel_bps_defaults"

// ExcelBPSSettings 单独保存协议配置，避免系统设置表单覆盖其他配置。
type ExcelBPSSettings struct {
	ExcelBPSEnabled               bool   `json:"excel_bps_enabled"`
	ExcelBPSImageMode             string `json:"excel_bps_image_mode"`
	ExcelBPSImageRelayEnabled     bool   `json:"excel_bps_image_relay_enabled"`
	ExcelBPSImageBaseURL          string `json:"excel_bps_image_base_url"`
	ExcelBPSImageBodyLimitMiB     int    `json:"excel_bps_image_body_limit_mib"`
	ExcelBPSImageBudgetMiB        int    `json:"excel_bps_image_budget_mib"`
	ExcelBPSImageMaxRequests      int    `json:"excel_bps_image_max_requests"`
	ExcelBPSImageMaxImageMiB      int    `json:"excel_bps_image_max_image_mib"`
	ExcelBPSImageMaxImages        int    `json:"excel_bps_image_max_images"`
	ExcelBPSImageLimitPolicy      string `json:"excel_bps_image_limit_policy"`
	ExcelBPSImageWarningRemaining int    `json:"excel_bps_image_warning_remaining"`
	ExcelBPSImageCompactReserve   int    `json:"excel_bps_image_compact_reserve"`
	ExcelBPSImageMaxTotalMiB      int    `json:"excel_bps_image_max_total_mib"`
	ExcelBPSImageStorageMiB       int    `json:"excel_bps_image_storage_mib"`
	ExcelBPSImageStorageEntries   int    `json:"excel_bps_image_storage_entries"`
	ExcelBPSImageTTLMinutes       int    `json:"excel_bps_image_ttl_minutes"`
}

func (s *SettingService) GetExcelBPSSettings(ctx context.Context) (*ExcelBPSSettings, error) {
	settings, err := s.settingRepo.GetAll(ctx)
	if err != nil {
		return nil, err
	}
	result := &ExcelBPSSettings{ExcelBPSEnabled: settings[SettingKeyExcelBPSEnabled] == "true"}
	result.ExcelBPSImageMode = settings[SettingKeyExcelBPSImageMode]
	if result.ExcelBPSImageMode == "" {
		result.ExcelBPSImageMode = ExcelBPSImageModeNative
	}
	result.ExcelBPSImageRelayEnabled = settings[SettingKeyExcelBPSImageRelayEnabled] == "true"
	result.ExcelBPSImageBaseURL = settings[SettingKeyExcelBPSImageBaseURL]
	result.ExcelBPSImageBodyLimitMiB, _ = parseExcelBPSImageCapacity(settings[SettingKeyExcelBPSImageBodyLimitMiB], DefaultExcelBPSImageBodyLimitMiB)
	result.ExcelBPSImageBudgetMiB, _ = parseExcelBPSImageCapacity(settings[SettingKeyExcelBPSImageBudgetMiB], DefaultExcelBPSImageBudgetMiB)
	result.ExcelBPSImageMaxRequests, _ = parseExcelBPSImageCapacity(settings[SettingKeyExcelBPSImageMaxRequests], DefaultExcelBPSImageMaxRequests)
	if validateExcelBPSImageCapacity(result.ExcelBPSImageBodyLimitMiB, result.ExcelBPSImageBudgetMiB, result.ExcelBPSImageMaxRequests) != nil {
		result.ExcelBPSImageBodyLimitMiB = DefaultExcelBPSImageBodyLimitMiB
		result.ExcelBPSImageBudgetMiB = DefaultExcelBPSImageBudgetMiB
		result.ExcelBPSImageMaxRequests = DefaultExcelBPSImageMaxRequests
	}

	imageLimits, imageLimitsErr := parseExcelBPSImageLimits(settings)
	if imageLimitsErr != nil {
		imageLimits = basispoints.DefaultImageRelayLimits()
	}
	result.ExcelBPSImageMaxImageMiB = imageLimits.MaxImageMiB
	result.ExcelBPSImageMaxImages = imageLimits.MaxImages
	result.ExcelBPSImageLimitPolicy = settings[SettingKeyExcelBPSImageLimitPolicy]
	if result.ExcelBPSImageLimitPolicy == "" {
		result.ExcelBPSImageLimitPolicy = "off"
	}
	result.ExcelBPSImageWarningRemaining, _ = parseExcelBPSImageCapacity(settings[SettingKeyExcelBPSImageWarningRemaining], 8)
	result.ExcelBPSImageCompactReserve, _ = parseExcelBPSImageCapacity(settings[SettingKeyExcelBPSImageCompactReserve], 3)
	result.ExcelBPSImageMaxTotalMiB = imageLimits.MaxTotalMiB
	result.ExcelBPSImageStorageMiB = imageLimits.StorageMiB
	result.ExcelBPSImageStorageEntries = imageLimits.StorageEntries
	result.ExcelBPSImageTTLMinutes = imageLimits.TTLMinutes
	return result, nil
}
func (s *SettingService) SaveExcelBPSSettings(ctx context.Context, settings *ExcelBPSSettings) error {
	updates, err := buildExcelBPSSettings(settings)
	if err != nil {
		return err
	}
	return s.settingRepo.SetMultiple(ctx, updates)
}
func buildExcelBPSSettings(settings *ExcelBPSSettings) (map[string]string, error) {
	imageRelay, err := normalizeExcelBPSImageRelaySettings(settings.ExcelBPSImageRelayEnabled, settings.ExcelBPSImageBaseURL, settings.ExcelBPSImageMode)
	if err != nil {
		return nil, err
	}
	settings.ExcelBPSImageBaseURL = imageRelay.BaseURL
	if settings.ExcelBPSImageBodyLimitMiB == 0 {
		settings.ExcelBPSImageBodyLimitMiB = DefaultExcelBPSImageBodyLimitMiB
	}
	if settings.ExcelBPSImageBudgetMiB == 0 {
		settings.ExcelBPSImageBudgetMiB = DefaultExcelBPSImageBudgetMiB
	}
	if settings.ExcelBPSImageMaxRequests == 0 {
		settings.ExcelBPSImageMaxRequests = DefaultExcelBPSImageMaxRequests
	}
	if settings.ExcelBPSImageMaxImageMiB == 0 {
		settings.ExcelBPSImageMaxImageMiB = imageRelay.Limits.MaxImageMiB
	}
	if settings.ExcelBPSImageMaxImages == 0 {
		settings.ExcelBPSImageMaxImages = imageRelay.Limits.MaxImages
	}
	if settings.ExcelBPSImageMaxTotalMiB == 0 {
		settings.ExcelBPSImageMaxTotalMiB = imageRelay.Limits.MaxTotalMiB
	}
	if settings.ExcelBPSImageStorageMiB == 0 {
		settings.ExcelBPSImageStorageMiB = imageRelay.Limits.StorageMiB
	}
	if settings.ExcelBPSImageStorageEntries == 0 {
		settings.ExcelBPSImageStorageEntries = imageRelay.Limits.StorageEntries
	}
	if settings.ExcelBPSImageTTLMinutes == 0 {
		settings.ExcelBPSImageTTLMinutes = imageRelay.Limits.TTLMinutes
	}
	if settings.ExcelBPSImageLimitPolicy == "" {
		settings.ExcelBPSImageLimitPolicy = "off"
	}
	if settings.ExcelBPSImageWarningRemaining == 0 {
		settings.ExcelBPSImageWarningRemaining = 8
	}
	if settings.ExcelBPSImageCompactReserve == 0 {
		settings.ExcelBPSImageCompactReserve = 3
	}
	if err := validateExcelBPSImagePolicy(settings.ExcelBPSImageLimitPolicy, settings.ExcelBPSImageWarningRemaining, settings.ExcelBPSImageCompactReserve, settings.ExcelBPSImageMaxImages); err != nil {
		return nil, infraerrors.BadRequest("INVALID_EXCEL_BPS_IMAGE_POLICY", err.Error())
	}
	if err := settings.imageRelayLimits().Validate(); err != nil {
		return nil, infraerrors.BadRequest("INVALID_EXCEL_BPS_IMAGE_LIMITS", err.Error())
	}
	if err := validateExcelBPSImageCapacity(settings.ExcelBPSImageBodyLimitMiB, settings.ExcelBPSImageBudgetMiB, settings.ExcelBPSImageMaxRequests); err != nil {
		return nil, err
	}

	updates := map[string]string{SettingKeyExcelBPSEnabled: strconv.FormatBool(settings.ExcelBPSEnabled)}
	updates[SettingKeyExcelBPSImageMode] = imageRelay.Mode
	updates[SettingKeyExcelBPSImageRelayEnabled] = strconv.FormatBool(imageRelay.Enabled)
	updates[SettingKeyExcelBPSImageBaseURL] = imageRelay.BaseURL
	updates[SettingKeyExcelBPSImageBodyLimitMiB] = strconv.Itoa(settings.ExcelBPSImageBodyLimitMiB)
	updates[SettingKeyExcelBPSImageBudgetMiB] = strconv.Itoa(settings.ExcelBPSImageBudgetMiB)
	updates[SettingKeyExcelBPSImageMaxRequests] = strconv.Itoa(settings.ExcelBPSImageMaxRequests)
	updates[SettingKeyExcelBPSImageMaxImageMiB] = strconv.Itoa(settings.ExcelBPSImageMaxImageMiB)
	updates[SettingKeyExcelBPSImageLimitPolicy] = settings.ExcelBPSImageLimitPolicy
	updates[SettingKeyExcelBPSImageWarningRemaining] = strconv.Itoa(settings.ExcelBPSImageWarningRemaining)
	updates[SettingKeyExcelBPSImageCompactReserve] = strconv.Itoa(settings.ExcelBPSImageCompactReserve)
	updates[SettingKeyExcelBPSImageMaxImages] = strconv.Itoa(settings.ExcelBPSImageMaxImages)
	updates[SettingKeyExcelBPSImageMaxTotalMiB] = strconv.Itoa(settings.ExcelBPSImageMaxTotalMiB)
	updates[SettingKeyExcelBPSImageStorageMiB] = strconv.Itoa(settings.ExcelBPSImageStorageMiB)
	updates[SettingKeyExcelBPSImageStorageEntries] = strconv.Itoa(settings.ExcelBPSImageStorageEntries)
	updates[SettingKeyExcelBPSImageTTLMinutes] = strconv.Itoa(settings.ExcelBPSImageTTLMinutes)
	return updates, nil
}
func (s *ExcelBPSSettings) imageRelayLimits() basispoints.ImageRelayLimits {
	return basispoints.ImageRelayLimits{
		MaxImageMiB:    s.ExcelBPSImageMaxImageMiB,
		MaxImages:      s.ExcelBPSImageMaxImages,
		MaxTotalMiB:    s.ExcelBPSImageMaxTotalMiB,
		StorageMiB:     s.ExcelBPSImageStorageMiB,
		StorageEntries: s.ExcelBPSImageStorageEntries,
		TTLMinutes:     s.ExcelBPSImageTTLMinutes,
	}
}

type ExcelBPSDefaults struct {
	AllModels               bool     `json:"all_models"`
	Models                  []string `json:"models"`
	OmitUnsupportedTools    bool     `json:"omit_unsupported_tools"`
	IgnoreEncryptedContent  bool     `json:"ignore_encrypted_content"`
	AutoDisableOn403        bool     `json:"auto_disable_on_403"`
	AutoRecoverOn403        bool     `json:"auto_recover_on_403"`
	RecoveryIntervalMinutes int      `json:"recovery_interval_minutes"`
	AutoMoveOn403           bool     `json:"auto_move_on_403"`
	TargetGroupID           int64    `json:"target_group_id"`
	CacheCreationAsInput    bool     `json:"cache_creation_as_input"`
}

func DefaultExcelBPSDefaults() ExcelBPSDefaults {
	return ExcelBPSDefaults{
		TargetGroupID:          -1,
		Models:                 []string{"gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra"},
		IgnoreEncryptedContent: true, AutoDisableOn403: true, CacheCreationAsInput: true,
		RecoveryIntervalMinutes: DefaultExcelBPS403RecoveryIntervalMinutes,
	}
}

func validateExcelBPSDefaults(b ExcelBPSDefaults) error {
	bad := func(message string) error { return infraerrors.BadRequest("AUTO_CONFIG_BPS_INVALID", message) }
	if len(b.Models) > 100 {
		return bad("at most 100 BPS models allowed")
	}
	for _, model := range b.Models {
		if strings.TrimSpace(model) == "" || len(model) > 200 {
			return bad("invalid BPS model name")
		}
	}
	if !b.AllModels && len(b.Models) == 0 {
		return bad("select at least one BPS model")
	}
	if b.RecoveryIntervalMinutes < 1 || b.RecoveryIntervalMinutes > MaxExcelBPS403RecoveryIntervalMinutes {
		return bad("BPS recovery interval must be between 1 and 10080 minutes")
	}
	if b.AutoRecoverOn403 && !b.AutoDisableOn403 {
		return bad("BPS recovery requires automatic disabling on 403")
	}
	if b.AutoMoveOn403 && b.TargetGroupID < 0 {
		return bad("select a BPS 403 target group")
	}
	return nil
}

func (s *SettingService) GetExcelBPSDefaults(ctx context.Context) (ExcelBPSDefaults, error) {
	result := DefaultExcelBPSDefaults()
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyExcelBPSDefaults})
	if err != nil {
		return result, err
	}
	if raw := values[SettingKeyExcelBPSDefaults]; raw != "" {
		if err = json.Unmarshal([]byte(raw), &result); err != nil {
			return result, err
		}
	}
	return result, nil
}
func (s *SettingService) SaveExcelBPSDefaults(ctx context.Context, b ExcelBPSDefaults) error {
	if err := validateExcelBPSDefaults(b); err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	return s.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyExcelBPSDefaults: string(raw)})
}
