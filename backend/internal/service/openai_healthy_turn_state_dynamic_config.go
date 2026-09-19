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

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 管理接口只返回脱敏地址；提取凭据仅存在于加密设置或当前运行内存。
type HealthyTurnStateDynamicConfig struct {
	Configured   bool     `json:"configured"`
	APIURLMasked string   `json:"api_url_masked"`
	Protocol     string   `json:"protocol"`
	TargetCount  int      `json:"target_count"`
	MaxAttempts  int      `json:"max_attempts"`
	Models       []string `json:"models"`
	Transport    string   `json:"transport"`
}

type HealthyTurnStateDynamicConfigInput struct {
	APIURL      string   `json:"api_url"`
	Protocol    string   `json:"protocol"`
	TargetCount int      `json:"target_count"`
	MaxAttempts int      `json:"max_attempts"`
	Models      []string `json:"models"`
	Transport   string   `json:"transport"`
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

func (d *healthyTurnStateDynamicService) loadConfig(ctx context.Context, accountID int64) (HealthyTurnStateDynamicConfigInput, error) {
	input := HealthyTurnStateDynamicConfigInput{Protocol: "http", TargetCount: 3, MaxAttempts: 100, Models: []string{}, Transport: "http"}
	if accountID <= 0 {
		return input, healthyDynamicConfigError()
	}
	value, err := d.settings.GetValue(ctx, healthyDynamicConfigKey(accountID))
	if errors.Is(err, ErrSettingNotFound) || (err == nil && value == "") {
		return input, nil
	}
	if err != nil {
		return input, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_READ_FAILED", "读取动态代理设置失败，请稍后重试")
	}
	plain, err := d.cipher.Decrypt(value)
	if err != nil || json.Unmarshal([]byte(plain), &input) != nil || validateHealthyDynamicConfig(input) != nil {
		return HealthyTurnStateDynamicConfigInput{}, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_INVALID", "动态代理设置不可用，请重新保存设置")
	}
	return input, nil
}

func (s *AccountTestService) GetHealthyTurnStateDynamicConfig(ctx context.Context, accountID int64) (*HealthyTurnStateDynamicConfig, error) {
	d, err := s.healthyDynamicService()
	if err != nil {
		return nil, err
	}
	input, err := d.loadConfig(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return healthyDynamicConfigView(input), nil
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
		if model == "" || len(model) > 200 || isOpenAIImageModel(model) || strings.ContainsAny(model, "\r\n*") {
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
	if input.APIURL == "" {
		previous, err := d.loadConfig(ctx, accountID)
		if err != nil {
			return nil, err
		}
		input.APIURL = previous.APIURL
	}
	if err := validateHealthyDynamicConfig(input); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, healthyDynamicConfigError()
	}
	encrypted, err := d.cipher.Encrypt(string(encoded))
	if err != nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_ENCRYPT_FAILED", "动态代理设置加密失败")
	}
	if err := d.settings.Set(ctx, healthyDynamicConfigKey(accountID), encrypted); err != nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CONFIG_SAVE_FAILED", "保存动态代理设置失败，请稍后重试")
	}
	d.mu.Lock()
	delete(d.maintenanceRetry, accountID)
	for _, run := range d.runs {
		if run.accountID == accountID {
			run.mu.Lock()
			if !sameHealthyDynamicConfig(run.config, input) {
				run.finishLocked("stopped", "采集配置已更新，使用新配置继续维护")
			}
			run.mu.Unlock()
		}
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
