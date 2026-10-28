package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyPelicanShowcaseEnabled    = "pelican_showcase_enabled"
	SettingKeyPelicanShowcaseConfig     = "pelican_showcase_config"
	SettingKeyPelicanShowcaseAPIEnabled = "pelican_showcase_api_enabled"
)

const (
	PelicanShowcaseDefaultMaxItems      = 20
	PelicanShowcaseMaxItemsLimit        = 100
	PelicanShowcaseDefaultRetentionDays = 7
	PelicanShowcaseRetentionDaysLimit   = 90
)

type PelicanShowcaseConfig struct {
	MaxItems      int  `json:"max_items"`
	AutoCleanup   bool `json:"auto_cleanup"`
	RetentionDays int  `json:"retention_days"`
}

func DefaultPelicanShowcaseConfig() PelicanShowcaseConfig {
	return PelicanShowcaseConfig{
		MaxItems:      PelicanShowcaseDefaultMaxItems,
		AutoCleanup:   true,
		RetentionDays: PelicanShowcaseDefaultRetentionDays,
	}
}

func NormalizePelicanShowcaseConfig(cfg PelicanShowcaseConfig) (PelicanShowcaseConfig, error) {
	if cfg.MaxItems == 0 {
		cfg.MaxItems = PelicanShowcaseDefaultMaxItems
	}
	if cfg.MaxItems < 1 || cfg.MaxItems > PelicanShowcaseMaxItemsLimit {
		return cfg, fmt.Errorf("showcase max items must be 1–%d", PelicanShowcaseMaxItemsLimit)
	}
	if cfg.RetentionDays == 0 {
		cfg.RetentionDays = PelicanShowcaseDefaultRetentionDays
	}
	if cfg.RetentionDays < 1 || cfg.RetentionDays > PelicanShowcaseRetentionDaysLimit {
		return cfg, fmt.Errorf("showcase retention must be 1–%d days", PelicanShowcaseRetentionDaysLimit)
	}
	return cfg, nil
}

func parsePelicanShowcaseConfig(raw string) (PelicanShowcaseConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultPelicanShowcaseConfig(), nil
	}
	var cfg PelicanShowcaseConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, fmt.Errorf("invalid pelican showcase config JSON")
	}
	return NormalizePelicanShowcaseConfig(cfg)
}

func (cfg PelicanShowcaseConfig) retentionCutoff(now time.Time) time.Time {
	if !cfg.AutoCleanup {
		return time.Time{}
	}
	return now.Add(-time.Duration(cfg.RetentionDays) * 24 * time.Hour)
}

type PelicanShowcaseRuntime struct {
	Enabled    bool
	APIEnabled bool
	Config     PelicanShowcaseConfig
}

var errPelicanShowcaseSettingsUnavailable = errors.New("pelican showcase settings unavailable")

func (s *SettingService) GetPelicanShowcaseRuntime(ctx context.Context) (PelicanShowcaseRuntime, error) {
	if s == nil || s.settingRepo == nil {
		return PelicanShowcaseRuntime{}, errPelicanShowcaseSettingsUnavailable
	}
	vals, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyPelicanShowcaseEnabled, SettingKeyPelicanShowcaseConfig, SettingKeyPelicanShowcaseAPIEnabled})
	if err != nil {
		return PelicanShowcaseRuntime{}, err
	}
	cfg, err := parsePelicanShowcaseConfig(vals[SettingKeyPelicanShowcaseConfig])
	if err != nil {
		return PelicanShowcaseRuntime{}, err
	}
	return PelicanShowcaseRuntime{
		Enabled: vals[SettingKeyPelicanShowcaseEnabled] == "true", APIEnabled: pelicanShowcaseAPIEnabled(vals), Config: cfg,
	}, nil
}

func pelicanShowcaseAPIEnabled(vals map[string]string) bool {
	raw, exists := vals[SettingKeyPelicanShowcaseAPIEnabled]
	return !exists || raw == "true"
}

func (s *SettingService) UpdatePelicanShowcaseSettings(ctx context.Context, enabled bool, cfg PelicanShowcaseConfig, apiEnabled *bool) (PelicanShowcaseRuntime, error) {
	if s == nil || s.settingRepo == nil {
		return PelicanShowcaseRuntime{}, errPelicanShowcaseSettingsUnavailable
	}
	normalized, err := NormalizePelicanShowcaseConfig(cfg)
	if err != nil {
		return PelicanShowcaseRuntime{}, infraerrors.BadRequest("INVALID_PELICAN_SHOWCASE", err.Error())
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return PelicanShowcaseRuntime{}, err
	}
	updates := map[string]string{
		SettingKeyPelicanShowcaseEnabled: strconv.FormatBool(enabled),
		SettingKeyPelicanShowcaseConfig:  string(raw),
	}
	var currentAPIEnabled bool
	if apiEnabled != nil {
		currentAPIEnabled = *apiEnabled
		updates[SettingKeyPelicanShowcaseAPIEnabled] = strconv.FormatBool(*apiEnabled)
	} else {
		vals, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyPelicanShowcaseAPIEnabled})
		if err != nil {
			return PelicanShowcaseRuntime{}, err
		}
		currentAPIEnabled = pelicanShowcaseAPIEnabled(vals)
	}
	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return PelicanShowcaseRuntime{}, err
	}
	return PelicanShowcaseRuntime{Enabled: enabled, APIEnabled: currentAPIEnabled, Config: normalized}, nil
}
