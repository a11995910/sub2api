package service

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	OpenAICodexTicketHarvestProxyMode   = "proxy"
	OpenAICodexTicketHarvestExtractMode = "extract"
)

var ErrOpenAICodexTicketHarvestSourceUnconfigured = errors.New("292 门票采集来源尚未配置")

// OpenAICodexTicketHarvestSource 仅供服务内部使用，包含供应商凭据，不得直接序列化给客户端。
type OpenAICodexTicketHarvestSource struct {
	Mode            string
	ProxyURL        string
	ExtractURL      string
	ExtractProtocol string
}

func normalizeOpenAICodexTicketHarvestSource(source OpenAICodexTicketHarvestSource) OpenAICodexTicketHarvestSource {
	source.Mode = strings.TrimSpace(source.Mode)
	if source.Mode == "" {
		source.Mode = OpenAICodexTicketHarvestProxyMode
	}
	source.ProxyURL = strings.TrimSpace(source.ProxyURL)
	source.ExtractURL = strings.TrimSpace(source.ExtractURL)
	source.ExtractProtocol = strings.TrimSpace(source.ExtractProtocol)
	if source.ExtractProtocol == "" {
		source.ExtractProtocol = "http"
	}
	return source
}

func ValidateOpenAICodexTicketHarvestSource(source OpenAICodexTicketHarvestSource) error {
	source = normalizeOpenAICodexTicketHarvestSource(source)
	invalid := infraerrors.BadRequest("INVALID_CODEX_TICKET_HARVEST_SOURCE", "请选择固定代理或批量提取，并填写有效的 HTTPS 提取接口及代理协议")
	if source.Mode != OpenAICodexTicketHarvestProxyMode && source.Mode != OpenAICodexTicketHarvestExtractMode {
		return invalid
	}
	if source.ExtractProtocol != "http" && source.ExtractProtocol != "https" && source.ExtractProtocol != "socks5h" {
		return invalid
	}
	if source.ExtractURL != "" && validateHealthyDynamicAPIURL(source.ExtractURL) != nil {
		return invalid
	}
	if source.Mode == OpenAICodexTicketHarvestExtractMode && source.ExtractURL == "" {
		return invalid
	}
	return ValidateOpenAICodexTicketHarvestProxyURL(source.ProxyURL)
}

// MaskOpenAICodexTicketHarvestExtractURL 连同路径一起脱敏，供应商密钥可能位于 URL 任意部分。
func MaskOpenAICodexTicketHarvestExtractURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || validateHealthyDynamicAPIURL(raw) != nil {
		return ""
	}
	parsed, _ := url.Parse(raw)
	return "https://" + parsed.Host + "/••••"
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestSource(ctx context.Context) OpenAICodexTicketHarvestSource {
	cfg := s.openAICodexTicketConfig()
	source := normalizeOpenAICodexTicketHarvestSource(OpenAICodexTicketHarvestSource{
		Mode: cfg.HarvestProxyMode, ProxyURL: cfg.HarvestProxyURL,
		ExtractURL: cfg.HarvestExtractURL, ExtractProtocol: cfg.HarvestExtractProtocol,
	})
	if s != nil && s.settingService != nil {
		source = s.settingService.GetOpenAICodexTicketHarvestSource(ctx, source)
	}
	return source
}

// 只读配置，不请求提取接口，供管理状态判断配置是否就绪。
func (s *OpenAIGatewayService) openAICodexTicketHarvestSourceConfigured(ctx context.Context) bool {
	source := s.openAICodexTicketHarvestSource(ctx)
	if ValidateOpenAICodexTicketHarvestSource(source) != nil {
		return false
	}
	return source.Mode == OpenAICodexTicketHarvestExtractMode || source.ProxyURL != ""
}

// 每轮只调用一次；批量结果供该轮全部账号与模型复用，不自动重试付费提取。
func (s *OpenAIGatewayService) resolveOpenAICodexTicketHarvestProxies(ctx context.Context) ([]string, string, error) {
	source := s.openAICodexTicketHarvestSource(ctx)
	if source.Mode == OpenAICodexTicketHarvestExtractMode {
		proxies, err := fetchOpenAICodexTicketHarvestProxyBatch(ctx, source, nil)
		return proxies, source.Mode, err
	}
	if ValidateOpenAICodexTicketHarvestSource(source) != nil || source.ProxyURL == "" {
		return nil, source.Mode, ErrOpenAICodexTicketHarvestSourceUnconfigured
	}
	return []string{source.ProxyURL}, source.Mode, nil
}

// 复用健康头提取的公网校验、DNS 安全拨号、禁止重定向、20 秒超时与 64KiB 上限。
// client 仅供测试注入；生产始终使用独立安全客户端，禁止环境代理。
func fetchOpenAICodexTicketHarvestProxyBatch(ctx context.Context, source OpenAICodexTicketHarvestSource, client *http.Client) ([]string, error) {
	source = normalizeOpenAICodexTicketHarvestSource(source)
	if source.Mode != OpenAICodexTicketHarvestExtractMode || source.ExtractURL == "" {
		return nil, ErrOpenAICodexTicketHarvestSourceUnconfigured
	}
	if err := ValidateOpenAICodexTicketHarvestSource(source); err != nil {
		return nil, err
	}
	input := HealthyTurnStateDynamicConfigInput{APIURL: source.ExtractURL, Protocol: source.ExtractProtocol, TargetCount: 1, MaxAttempts: 1}
	var proxies []string
	var err error
	if client == nil {
		proxies, err = fetchHealthyDynamicProxyBatch(ctx, input)
	} else {
		proxies, err = fetchHealthyDynamicProxyBatchWithClient(ctx, input, client)
	}
	if err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, newHealthyDynamicPublicError("代理提取接口未返回可用入口")
	}
	return proxies, nil
}
