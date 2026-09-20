package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 管理接口只返回脱敏地址；提取凭据仅存在于加密设置或当前运行内存。
type HealthyTurnStateDynamicConfig struct {
	ModelsInherited          bool     `json:"models_inherited,omitempty"`
	ModelsInheritanceMessage string   `json:"models_inheritance_message,omitempty"`
	SharedProxyConflict      bool     `json:"shared_proxy_conflict,omitempty"`
	Configured               bool     `json:"configured"`
	APIURLMasked             string   `json:"api_url_masked"`
	Protocol                 string   `json:"protocol"`
	TargetCount              int      `json:"target_count"`
	MaxAttempts              int      `json:"max_attempts"`
	Models                   []string `json:"models"`
	Transport                string   `json:"transport"`
}

type HealthyTurnStateDynamicConfigInput struct {
	// false 表示沿用或仅调整账号限额，不把继承后的模型交集改写成全局默认。
	UpdateDefaultModels *bool `json:"update_default_models,omitempty"`
	// false 表示仅保存账号设置，避免旧弹窗覆盖其他账号刚更新的共享协议。
	UpdateSharedProxy *bool    `json:"update_shared_proxy,omitempty"`
	APIURL            string   `json:"api_url"`
	Protocol          string   `json:"protocol"`
	TargetCount       int      `json:"target_count"`
	MaxAttempts       int      `json:"max_attempts"`
	Models            []string `json:"models"`
	Transport         string   `json:"transport"`
}

type HealthyTurnStateDynamicStartInput struct {
	Model     string `json:"model"`
	Transport string `json:"transport"`
}

func healthyDynamicConfigKey(accountID int64) string {
	return fmt.Sprintf("healthy_turn_state_dynamic_config_%d", accountID)
}

func (s *AccountTestService) SetHealthyTurnStateDynamicStorage(repo SettingRepository, encryptor SecretEncryptor) {
	if s != nil {
		s.healthyTurnStateDynamic = &healthyTurnStateDynamicService{settings: repo, cipher: encryptor, runs: make(map[string]*healthyTurnStateDynamicSession)}
	}
}

func (s *AccountTestService) healthyDynamicService() (*healthyTurnStateDynamicService, error) {
	if s == nil || s.healthyTurnStateDynamic == nil || s.healthyTurnStateDynamic.settings == nil || s.healthyTurnStateDynamic.cipher == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_UNAVAILABLE", "动态代理采集服务暂不可用")
	}
	return s.healthyTurnStateDynamic, nil
}

func healthyDynamicConfigError() error {
	return infraerrors.BadRequest("INVALID_HEALTHY_DYNAMIC_CONFIG", "请填写有效的 HTTPS 提取接口、代理协议和采集限额")
}

func validateHealthyDynamicAPIURL(raw string) error {
	if len(raw) == 0 || len(raw) > 4096 || strings.ContainsAny(raw, "\r\n") {
		return healthyDynamicConfigError()
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || isBlockedHostname(u.Hostname()) {
		return healthyDynamicConfigError()
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return healthyDynamicConfigError()
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !healthyDynamicPublicIP(ip) {
		return healthyDynamicConfigError()
	}
	return nil
}

func validateHealthyDynamicConfig(input HealthyTurnStateDynamicConfigInput) error {
	if input.Protocol != "http" && input.Protocol != "https" && input.Protocol != "socks5h" {
		return healthyDynamicConfigError()
	}
	if input.TargetCount < 1 || input.TargetCount > 100 || input.MaxAttempts < input.TargetCount || input.MaxAttempts > 1000 {
		return healthyDynamicConfigError()
	}
	return validateHealthyDynamicAPIURL(input.APIURL)
}

func healthyDynamicConfigView(input HealthyTurnStateDynamicConfigInput) *HealthyTurnStateDynamicConfig {
	view := &HealthyTurnStateDynamicConfig{Configured: input.APIURL != "", Protocol: input.Protocol, TargetCount: input.TargetCount, MaxAttempts: input.MaxAttempts}
	view.Models, view.Transport = append([]string{}, input.Models...), input.Transport
	if input.APIURL != "" {
		// 路径也可能携带供应商密钥，仅显示供应商主机和固定脱敏后缀。
		if u, err := url.Parse(input.APIURL); err == nil {
			view.APIURLMasked = "https://" + u.Host + "/••••"
		}
	}
	return view
}

const healthyDynamicSharedProxyKey = "healthy_turn_state_dynamic_shared_proxy"
const healthyDynamicLastModelsKey = "healthy_turn_state_dynamic_last_models"

// 提取入口与代理协议全局共用；账号设置继续保存完整加密副本，兼容旧版本回滚。
type healthyDynamicSharedProxy struct {
	APIURL   string `json:"api_url"`
	Protocol string `json:"protocol"`
}

func healthyDynamicConfigReadError() error {
	return infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_READ_FAILED", "读取动态代理设置失败，请稍后重试")
}

func healthyDynamicConfigInvalidError() error {
	return infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_INVALID", "动态代理设置不可用，请重新保存设置")
}

func healthyDynamicLegacyConflictError() error {
	return infraerrors.Conflict("HEALTHY_DYNAMIC_SHARED_PROXY_CONFLICT", "历史账号使用了不同的动态 IP 接口或协议，请填写一次统一的提取接口并选择代理协议")
}

// 调用方必须持有 configMu。只在共享配置不存在时扫描一次旧设置。
func (d *healthyTurnStateDynamicService) loadSharedProxyLocked(ctx context.Context, migrate bool) (healthyDynamicSharedProxy, error) {
	var shared healthyDynamicSharedProxy
	value, err := d.settings.GetValue(ctx, healthyDynamicSharedProxyKey)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return shared, healthyDynamicConfigReadError()
	}
	if value != "" {
		plain, err := d.cipher.Decrypt(value)
		if err != nil || json.Unmarshal([]byte(plain), &shared) != nil ||
			validateHealthyDynamicAPIURL(shared.APIURL) != nil ||
			(shared.Protocol != "http" && shared.Protocol != "https" && shared.Protocol != "socks5h") {
			if !migrate {
				// 显式输入地址可修复损坏的共享密文，存储读取错误仍须正常报错。
				return healthyDynamicSharedProxy{}, nil
			}
			return healthyDynamicSharedProxy{}, healthyDynamicConfigInvalidError()
		}
		return shared, nil
	}
	if !migrate {
		return shared, nil
	}
	if d.legacyProxyChecked {
		if d.legacyProxyConflict {
			return shared, healthyDynamicLegacyConflictError()
		}
		return shared, nil
	}
	values, err := d.settings.GetAll(ctx)
	if err != nil {
		return shared, healthyDynamicConfigReadError()
	}
	for key, value := range values {
		id, ok := strings.CutPrefix(key, "healthy_turn_state_dynamic_config_")
		accountID, parseErr := strconv.ParseInt(id, 10, 64)
		if !ok || parseErr != nil || accountID <= 0 || value == "" {
			continue
		}
		var legacy HealthyTurnStateDynamicConfigInput
		plain, decryptErr := d.cipher.Decrypt(value)
		if decryptErr != nil || json.Unmarshal([]byte(plain), &legacy) != nil || validateHealthyDynamicConfig(legacy) != nil {
			continue
		}
		candidate := healthyDynamicSharedProxy{APIURL: legacy.APIURL, Protocol: legacy.Protocol}
		if shared.APIURL != "" && shared != candidate {
			d.legacyProxyChecked, d.legacyProxyConflict = true, true
			return healthyDynamicSharedProxy{}, healthyDynamicLegacyConflictError()
		}
		shared = candidate
	}
	if shared.APIURL != "" {
		encrypted, err := d.encryptHealthyDynamicConfig(shared)
		if err != nil {
			return healthyDynamicSharedProxy{}, err
		}
		if err := d.settings.Set(ctx, healthyDynamicSharedProxyKey, encrypted); err != nil {
			return healthyDynamicSharedProxy{}, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_SAVE_FAILED", "保存共享动态代理设置失败，请稍后重试")
		}
	}
	d.legacyProxyChecked = true
	return shared, nil
}

func (d *healthyTurnStateDynamicService) encryptHealthyDynamicConfig(input any) (string, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return "", healthyDynamicConfigError()
	}
	encrypted, err := d.cipher.Encrypt(string(encoded))
	if err != nil {
		return "", infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_ENCRYPT_FAILED", "动态代理设置加密失败")
	}
	return encrypted, nil
}

func (d *healthyTurnStateDynamicService) loadConfig(ctx context.Context, accountID int64) (HealthyTurnStateDynamicConfigInput, error) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	return d.loadConfigLocked(ctx, accountID)
}

func (d *healthyTurnStateDynamicService) loadConfigLocked(ctx context.Context, accountID int64) (HealthyTurnStateDynamicConfigInput, error) {
	input := HealthyTurnStateDynamicConfigInput{Protocol: "http", TargetCount: 10, MaxAttempts: 100, Models: []string{}, Transport: "http"}
	if accountID <= 0 {
		return input, healthyDynamicConfigError()
	}
	value, err := d.settings.GetValue(ctx, healthyDynamicConfigKey(accountID))
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return input, healthyDynamicConfigReadError()
	}
	if value != "" {
		plain, err := d.cipher.Decrypt(value)
		if err != nil || json.Unmarshal([]byte(plain), &input) != nil || validateHealthyDynamicConfig(input) != nil {
			return HealthyTurnStateDynamicConfigInput{}, healthyDynamicConfigInvalidError()
		}
	}
	shared, err := d.loadSharedProxyLocked(ctx, true)
	if err != nil {
		// 保留账号选项供管理接口展示冲突恢复表单；调用方必须先处理错误。
		return input, err
	}
	input.APIURL = shared.APIURL
	if shared.Protocol != "" {
		input.Protocol = shared.Protocol
	}
	return input, nil
}

func (s *AccountTestService) loadHealthyDynamicLastModels(ctx context.Context) ([]string, error) {
	d := s.healthyTurnStateDynamic
	d.configMu.Lock()
	defer d.configMu.Unlock()
	return s.loadHealthyDynamicLastModelsLocked(ctx)
}

// 只访问持久化仓储，不在 configMu 内请求上游。首次升级按保存时间恢复默认选择。
func (s *AccountTestService) loadHealthyDynamicLastModelsLocked(ctx context.Context) ([]string, error) {
	d := s.healthyTurnStateDynamic
	value, err := d.settings.GetValue(ctx, healthyDynamicLastModelsKey)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, healthyDynamicConfigReadError()
	}
	if value != "" {
		var models []string
		plain, err := d.cipher.Decrypt(value)
		if err != nil || json.Unmarshal([]byte(plain), &models) != nil || !validHealthyDynamicDefaultModels(models) {
			return nil, healthyDynamicConfigInvalidError()
		}
		return models, nil
	}
	if s.accountRepo == nil {
		return []string{}, nil
	}
	values, err := d.settings.GetAll(ctx)
	if err != nil {
		return nil, healthyDynamicConfigReadError()
	}
	models := []string{}
	var latest time.Time
	var latestID int64
	for key, value := range values {
		id, ok := strings.CutPrefix(key, "healthy_turn_state_dynamic_config_")
		accountID, parseErr := strconv.ParseInt(id, 10, 64)
		if !ok || parseErr != nil || accountID <= 0 || value == "" {
			continue
		}
		var candidate HealthyTurnStateDynamicConfigInput
		plain, err := d.cipher.Decrypt(value)
		if err != nil || json.Unmarshal([]byte(plain), &candidate) != nil || validateHealthyDynamicConfig(candidate) != nil ||
			len(candidate.Models) == 0 || !validHealthyDynamicDefaultModels(candidate.Models) {
			continue
		}
		// ListByPlatform 只返回启用账号；暂停账号的上次选择仍有意义，逐个核实存在性。
		account, err := s.accountRepo.GetByID(ctx, accountID)
		if errors.Is(err, ErrAccountNotFound) || (err == nil && (account == nil || !account.IsOpenAIOAuthLike())) {
			continue
		}
		if err != nil {
			return nil, healthyDynamicConfigReadError()
		}
		setting, err := d.settings.Get(ctx, key)
		if errors.Is(err, ErrSettingNotFound) || (err == nil && setting == nil) {
			continue
		}
		if err != nil {
			return nil, healthyDynamicConfigReadError()
		}
		if setting.UpdatedAt.After(latest) || (setting.UpdatedAt.Equal(latest) && account.ID > latestID) {
			models, latest, latestID = candidate.Models, setting.UpdatedAt, account.ID
		}
	}
	encrypted, err := d.encryptHealthyDynamicConfig(models)
	if err != nil {
		return nil, err
	}
	if err := d.settings.Set(ctx, healthyDynamicLastModelsKey, encrypted); err != nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_SAVE_FAILED", "保存默认模型选择失败，请稍后重试")
	}
	return models, nil
}

func validHealthyDynamicDefaultModels(models []string) bool {
	if len(models) > 100 {
		return false
	}
	for _, model := range models {
		if model == "" || model != strings.TrimSpace(model) || len(model) > 200 ||
			strings.ContainsAny(model, "\r\n*") || isHealthyTurnStateNonTextModel(model) {
			return false
		}
	}
	return true
}

func (s *AccountTestService) healthyDynamicInheritedModels(ctx context.Context, account *Account, defaults []string) ([]string, error) {
	models := []string{}
	if len(defaults) == 0 {
		return models, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_DEFAULT_MODELS_MISSING", "尚无上次模型选择，请先在任一 OAuth 账号勾选模型并保存")
	}
	if account == nil || !account.IsOpenAIOAuthLike() {
		return models, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_ACCOUNT", "仅支持 OpenAI OAuth 账号")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	available, err := s.FetchOpenAIHealthyTurnStateModels(ctx, account)
	if err != nil {
		return models, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_MODELS_UNAVAILABLE", "无法核实上游支持模型，暂未继承上次选择，将稍后重试")
	}
	for _, model := range defaults {
		if slices.ContainsFunc(available, func(item OpenAIHealthyTurnStateModel) bool { return item.ID == model }) && !slices.Contains(models, model) {
			models = append(models, model)
		}
	}
	if len(models) == 0 {
		return models, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_DEFAULT_MODELS_UNSUPPORTED", "本账号不支持上次选择的模型，请勾选本账号支持的模型并保存")
	}
	return models, nil
}

// 后台首次启用才固定继承选择；并发显式保存始终优先，继承不能反写全局默认。
func (s *AccountTestService) inheritHealthyDynamicConfig(ctx context.Context, account *Account) (HealthyTurnStateDynamicConfigInput, error) {
	d := s.healthyTurnStateDynamic
	config, err := d.loadConfig(ctx, account.ID)
	if err != nil || len(config.Models) != 0 {
		return config, err
	}
	defaults, err := s.loadHealthyDynamicLastModels(ctx)
	if err != nil {
		return config, err
	}
	models, err := s.healthyDynamicInheritedModels(ctx, account, defaults)
	if err != nil {
		return config, err
	}
	d.configMu.Lock()
	defer d.configMu.Unlock()
	current, err := d.loadConfigLocked(ctx, account.ID)
	if err != nil || len(current.Models) != 0 {
		return current, err
	}
	latest, err := s.loadHealthyDynamicLastModelsLocked(ctx)
	if err != nil {
		return current, err
	}
	if !slices.Equal(defaults, latest) {
		return current, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_DEFAULT_MODELS_CHANGED", "默认模型选择已更新，将重新核实后继承")
	}
	current.Models = models
	if err := validateHealthyDynamicConfig(current); err != nil {
		return current, err
	}
	encrypted, err := d.encryptHealthyDynamicConfig(current)
	if err != nil {
		return current, err
	}
	if err := d.settings.Set(ctx, healthyDynamicConfigKey(account.ID), encrypted); err != nil {
		return current, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_SAVE_FAILED", "保存继承模型选择失败，将稍后重试")
	}
	return current, nil
}

func (s *AccountTestService) GetHealthyTurnStateDynamicConfig(ctx context.Context, accountID int64) (*HealthyTurnStateDynamicConfig, error) {
	d, err := s.healthyDynamicService()
	if err != nil {
		return nil, err
	}
	input, err := d.loadConfig(ctx, accountID)
	if infraerrors.Reason(err) == "HEALTHY_DYNAMIC_SHARED_PROXY_CONFLICT" {
		input.APIURL = ""
		view := healthyDynamicConfigView(input)
		view.SharedProxyConflict = true
		return view, nil
	}
	if err != nil {
		return nil, err
	}
	view := healthyDynamicConfigView(input)
	if len(input.Models) != 0 || s.accountRepo == nil {
		return view, nil
	}
	models, err := s.loadHealthyDynamicLastModels(ctx)
	if err != nil {
		return nil, err
	}
	view.ModelsInherited = len(models) > 0
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	view.Models, err = s.healthyDynamicInheritedModels(ctx, account, models)
	if err != nil {
		view.ModelsInheritanceMessage = infraerrors.Message(err)
	}
	return view, nil
}

func (s *AccountTestService) SaveHealthyTurnStateDynamicConfig(ctx context.Context, accountID int64, input HealthyTurnStateDynamicConfigInput) (*HealthyTurnStateDynamicConfig, error) {
	d, err := s.healthyDynamicService()
	if err != nil {
		return nil, err
	}
	if accountID <= 0 {
		return nil, healthyDynamicConfigError()
	}
	input.APIURL = strings.TrimSpace(input.APIURL)
	if input.Transport == "" {
		input.Transport = "http"
	}
	if input.Transport != "http" && input.Transport != "websocket" {
		return nil, healthyDynamicConfigError()
	}
	if len(input.Models) == 0 || len(input.Models) > 100 {
		return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请至少勾选一个账号支持的文本模型")
	}
	models := make([]string, 0, len(input.Models))
	for _, model := range input.Models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 200 || isHealthyTurnStateNonTextModel(model) || strings.ContainsAny(model, "\r\n*") {
			return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请选择账号支持的文本模型")
		}
		if !slices.Contains(models, model) {
			models = append(models, model)
		}
	}
	input.Models = models
	if s.accountRepo != nil {
		account, err := s.accountRepo.GetByID(ctx, accountID)
		if err != nil || account == nil || !account.IsOpenAIOAuthLike() {
			return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_ACCOUNT", "仅支持 OpenAI OAuth 账号")
		}
		available, err := s.FetchOpenAIHealthyTurnStateModels(ctx, account)
		if err != nil {
			return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_MODELS_UNAVAILABLE", "无法核实上游支持的模型，请刷新模型列表后重试")
		}
		for _, model := range input.Models {
			if !slices.ContainsFunc(available, func(item OpenAIHealthyTurnStateModel) bool { return item.ID == model }) {
				return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "所选模型已不在账号上游支持列表中，请刷新模型列表")
			}
		}
	}
	d.configMu.Lock()
	defer d.configMu.Unlock()
	// 显式输入新地址时跳过历史冲突迁移，允许管理员直接修复配置。
	previous, err := d.loadSharedProxyLocked(ctx, input.APIURL == "")
	if err != nil {
		return nil, err
	}
	updateShared := input.APIURL != "" || input.UpdateSharedProxy == nil || *input.UpdateSharedProxy
	if input.APIURL == "" {
		input.APIURL = previous.APIURL
	}
	if !updateShared || input.Protocol == "" {
		input.Protocol = previous.Protocol
	}
	if err := validateHealthyDynamicConfig(input); err != nil {
		return nil, err
	}
	shared := healthyDynamicSharedProxy{APIURL: input.APIURL, Protocol: input.Protocol}
	updateDefaults := input.UpdateDefaultModels == nil || *input.UpdateDefaultModels
	if !updateDefaults {
		// 首次升级先从原有保存时间恢复默认，避免本次仅改数量的写入成为迁移来源。
		if _, err := s.loadHealthyDynamicLastModelsLocked(ctx); err != nil {
			return nil, err
		}
	}
	// 该字段只表达本次写入意图，不属于持久配置。
	input.UpdateSharedProxy = nil
	input.UpdateDefaultModels = nil
	encrypted, err := d.encryptHealthyDynamicConfig(input)
	if err != nil {
		return nil, err
	}
	sharedEncrypted, err := d.encryptHealthyDynamicConfig(shared)
	if err != nil {
		return nil, err
	}
	values := map[string]string{
		healthyDynamicConfigKey(accountID): encrypted,
		healthyDynamicSharedProxyKey:       sharedEncrypted,
	}
	if updateDefaults {
		modelsEncrypted, err := d.encryptHealthyDynamicConfig(input.Models)
		if err != nil {
			return nil, err
		}
		values[healthyDynamicLastModelsKey] = modelsEncrypted
	}
	if err := d.settings.SetMultiple(ctx, values); err != nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_SAVE_FAILED", "保存动态代理设置失败，请稍后重试")
	}
	d.legacyProxyChecked, d.legacyProxyConflict = true, false
	d.mu.Lock()
	sharedChanged := shared != previous
	if sharedChanged {
		clear(d.maintenanceRetry)
		d.sharedConfigRevision++
	} else {
		delete(d.maintenanceRetry, accountID)
	}
	if d.accountConfigRevision == nil {
		d.accountConfigRevision = make(map[int64]uint64)
	}
	d.accountConfigRevision[accountID]++
	for _, run := range d.runs {
		run.mu.Lock()
		sharedMismatch := run.config.APIURL != input.APIURL || run.config.Protocol != input.Protocol
		accountMismatch := run.accountID == accountID && !sameHealthyDynamicConfig(run.config, input)
		if sharedMismatch || accountMismatch {
			run.finishLocked("stopped", "采集配置已更新，使用新配置继续维护")
		}
		run.mu.Unlock()
	}
	d.mu.Unlock()
	return healthyDynamicConfigView(input), nil
}

type healthyTurnStateTemporaryProxyKey struct{}

// WithHealthyTurnStateTemporaryProxy 标记一次性采集传输，不写入正式客户端连接池。
func WithHealthyTurnStateTemporaryProxy(ctx context.Context) context.Context {
	return context.WithValue(ctx, healthyTurnStateTemporaryProxyKey{}, true)
}

func IsHealthyTurnStateTemporaryProxy(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	temporary, _ := ctx.Value(healthyTurnStateTemporaryProxyKey{}).(bool)
	return temporary
}
