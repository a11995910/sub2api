package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/idna"
)

const (
	DeepSeekToolSearch           = "search"
	DeepSeekToolFetch            = "fetch"
	deepSeekToolMaxRequestBytes  = 64 << 10
	deepSeekToolMaxResponseBytes = 2 << 20
	deepSeekToolTimeout          = 30 * time.Second
)

// DeepSeekToolResult 仅在成功返回实际结果时计一次工具费用；空搜索不计费。
type DeepSeekToolResult struct {
	Body            []byte
	Successful      bool
	RequestID       string
	UpstreamHeaders http.Header
	StatusCode      int
}

// DeepSeekToolError 不包含上游原文、请求网址或凭据，可安全反馈给调用方。
type DeepSeekToolError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *DeepSeekToolError) Error() string { return e.Message }

func deepSeekToolError(status int, code, message string) error {
	return &DeepSeekToolError{StatusCode: status, Code: code, Message: message}
}

func deepSeekToolInvalid(message string) error {
	return deepSeekToolError(http.StatusBadRequest, "invalid_request_error", message)
}

// ValidateDeepSeekToolRequest 校验并只保留官方工具参数，未知字段按官方语义忽略。
// URL 的实际抓取由上游完成，本地校验不替代上游对重定向及 DNS 重绑定的防护。
func ValidateDeepSeekToolRequest(ctx context.Context, kind string, body []byte) ([]byte, error) {
	return validateDeepSeekToolRequest(ctx, kind, body, net.DefaultResolver.LookupIP)
}

type deepSeekLookupIP func(context.Context, string, string) ([]net.IP, error)

func validateDeepSeekToolRequest(ctx context.Context, kind string, body []byte, lookup deepSeekLookupIP) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(body) > deepSeekToolMaxRequestBytes {
		return nil, deepSeekToolError(http.StatusRequestEntityTooLarge, "request_too_large", "工具请求超过大小限制")
	}
	var fields map[string]json.RawMessage
	if !utf8.Valid(body) || json.Unmarshal(body, &fields) != nil || fields == nil {
		return nil, deepSeekToolInvalid("工具请求必须是有效 JSON 对象")
	}
	result := make(map[string]any)
	switch kind {
	case DeepSeekToolFetch:
		var target string
		if json.Unmarshal(fields["url"], &target) != nil || target == "" {
			return nil, deepSeekToolInvalid("url 必须是非空字符串")
		}
		if err := validateDeepSeekFetchURL(ctx, target, lookup); err != nil {
			return nil, err
		}
		result["url"] = target
	case DeepSeekToolSearch:
		var query string
		if json.Unmarshal(fields["query"], &query) != nil || strings.TrimSpace(query) == "" || utf8.RuneCountInString(query) > 128 {
			return nil, deepSeekToolInvalid("query 必须包含 1 至 128 个字符，且不能全为空白")
		}
		result["query"] = query
		if raw, ok := fields["count"]; ok {
			var count int
			if json.Unmarshal(raw, &count) != nil || count < 1 || count > 50 {
				return nil, deepSeekToolInvalid("count 必须是 1 至 50 的整数")
			}
			result["count"] = count
		}
		if raw, ok := fields["ori_question"]; ok && string(raw) != "null" {
			var question string
			if json.Unmarshal(raw, &question) != nil || utf8.RuneCountInString(question) < 1 || utf8.RuneCountInString(question) > 128 {
				return nil, deepSeekToolInvalid("ori_question 必须包含 1 至 128 个字符")
			}
			result["ori_question"] = question
		}
		if raw, ok := fields["need_content"]; ok {
			var needed bool
			if string(raw) == "null" || json.Unmarshal(raw, &needed) != nil {
				return nil, deepSeekToolInvalid("need_content 必须是布尔值")
			}
			result["need_content"] = needed
		}
		if raw, ok := fields["localization"]; ok {
			var location string
			if string(raw) == "null" || json.Unmarshal(raw, &location) != nil || (location != "" && net.ParseIP(location) == nil) {
				return nil, deepSeekToolInvalid("localization 必须是 IP 地址或空字符串")
			}
			result["localization"] = location
		}
	default:
		return nil, deepSeekToolInvalid("不支持的 DeepSeek 工具")
	}
	return json.Marshal(result)
}

func validateDeepSeekFetchURL(ctx context.Context, raw string, lookup deepSeekLookupIP) error {
	if utf8.RuneCountInString(raw) > 8192 || raw != strings.TrimSpace(raw) {
		return deepSeekToolInvalid("url 长度不能超过 8192 个字符，且不能包含首尾空白")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return deepSeekToolInvalid("url 必须是无凭据、无片段的公开 HTTP 或 HTTPS 地址")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return deepSeekToolInvalid("url 端口无效")
		}
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if net.ParseIP(host) == nil {
		host, err = idna.Lookup.ToASCII(host)
		if err != nil {
			return deepSeekToolInvalid("url 主机不允许访问")
		}
	}
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return deepSeekToolInvalid("url 主机不允许访问")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !deepSeekPublicIP(ip) {
			return deepSeekToolInvalid("url 不能指向内网或保留地址")
		}
		return nil
	}
	dnsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ips, err := lookup(dnsCtx, "ip", host)
	if err != nil || len(ips) == 0 {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return deepSeekToolInvalid("url 主机无法解析")
	}
	for _, ip := range ips {
		if !deepSeekPublicIP(ip) {
			return deepSeekToolInvalid("url 不能解析到内网或保留地址")
		}
	}
	return nil
}

// 私网之外还排除共享地址、文档地址、基准测试地址、组播及 IPv6 转换地址。
var deepSeekBlockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("2002::/16"),
}

func deepSeekPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, blocked := range deepSeekBlockedNetworks {
		if blocked.Contains(addr) {
			return false
		}
	}
	return true
}

// ExecuteDeepSeekTool 只请求账号配置的工具上游，绝不携带本站客户 Key 请求目标网页。
// 不重试：读超时并不能证明上游未执行，自动重试可能重复收费。
func (s *OpenAIGatewayService) ExecuteDeepSeekTool(ctx context.Context, c *gin.Context, account *Account, kind string, body []byte) (*DeepSeekToolResult, error) {
	if account == nil || account.Platform != PlatformDeepseek || account.Type != AccountTypeAPIKey {
		return nil, deepSeekToolError(http.StatusBadRequest, "unsupported_account", "工具仅支持 DeepSeek API Key 账号")
	}
	toolCtx, cancel := context.WithTimeout(ctx, deepSeekToolTimeout)
	defer cancel()
	normalized, err := ValidateDeepSeekToolRequest(toolCtx, kind, body)
	if err != nil {
		return nil, err
	}
	base, err := deepSeekToolBaseURL(account)
	if err != nil {
		return nil, err
	}
	base, err = s.validateUpstreamBaseURL(base)
	if err != nil {
		return nil, deepSeekToolError(http.StatusBadGateway, "upstream_configuration_error", "DeepSeek 工具上游地址配置无效")
	}
	parsedBase, err := url.Parse(base)
	if err != nil || parsedBase.User != nil || parsedBase.RawQuery != "" || parsedBase.Fragment != "" {
		return nil, deepSeekToolError(http.StatusBadGateway, "upstream_configuration_error", "DeepSeek 工具上游地址不能包含凭据、查询参数或片段")
	}
	apiKey := strings.TrimSpace(account.GetOpenAIProtocolAPIKey())
	if apiKey == "" {
		return nil, deepSeekToolError(http.StatusBadGateway, "upstream_configuration_error", "DeepSeek 工具上游凭据未配置")
	}
	endpoint := "/v1/tools/" + kind
	request, err := http.NewRequestWithContext(WithHTTPUpstreamRedirectsDisabled(toolCtx), http.MethodPost, buildOpenAIEndpointURL(base, endpoint), bytes.NewReader(normalized))
	if err != nil {
		return nil, deepSeekToolError(http.StatusBadGateway, "upstream_configuration_error", "无法构造 DeepSeek 工具请求")
	}
	account.ApplyHeaderOverrides(request.Header)
	// 出站认证取自已调度账号，不接受客户或覆写中的其他认证方式。
	deleteHeaderAllForms(request.Header, "cookie")
	deleteHeaderAllForms(request.Header, "x-api-key")
	deleteHeaderAllForms(request.Header, "x-goog-api-key")
	deleteHeaderAllForms(request.Header, "authorization")
	deleteHeaderAllForms(request.Header, "accept")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	proxyURL := ""
	if account.ProxyID != nil {
		if account.Proxy == nil {
			return nil, deepSeekToolError(http.StatusBadGateway, "upstream_configuration_error", "账号代理配置未加载")
		}
		proxyURL = account.Proxy.URL()
	}
	SetActualOpenAIUpstreamEndpoint(c, endpoint)
	resp, err := s.doOpenAIUpstream(request, proxyURL, account)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		if errors.Is(toolCtx.Err(), context.Canceled) {
			return nil, context.Canceled
		}
		if errors.Is(toolCtx.Err(), context.DeadlineExceeded) {
			return nil, deepSeekToolError(http.StatusGatewayTimeout, "tool_timeout", "DeepSeek 工具请求超时")
		}
		return nil, deepSeekToolError(http.StatusBadGateway, "upstream_error", "DeepSeek 工具上游连接失败")
	}
	if resp == nil || resp.Body == nil {
		return nil, deepSeekToolError(http.StatusBadGateway, "invalid_upstream_response", "DeepSeek 工具上游未返回响应")
	}
	defer resp.Body.Close()
	result := &DeepSeekToolResult{StatusCode: resp.StatusCode, RequestID: resp.Header.Get("x-request-id"), UpstreamHeaders: resp.Header.Clone()}
	if resp.StatusCode != http.StatusOK {
		status := http.StatusBadGateway
		code := "upstream_error"
		if resp.StatusCode == http.StatusTooManyRequests {
			status, code = http.StatusTooManyRequests, "rate_limit_error"
		}
		if resp.StatusCode == http.StatusNotFound {
			status, code = http.StatusNotFound, "url_not_accessible"
		}
		if resp.StatusCode == http.StatusForbidden {
			status, code = http.StatusForbidden, "tool_access_denied"
		}
		if resp.StatusCode == http.StatusGatewayTimeout || resp.StatusCode == http.StatusRequestTimeout {
			status, code = http.StatusGatewayTimeout, "tool_timeout"
		}
		return result, deepSeekToolError(status, code, fmt.Sprintf("DeepSeek 工具上游返回 HTTP %d", resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, deepSeekToolMaxResponseBytes+1))
	if err != nil {
		if errors.Is(toolCtx.Err(), context.Canceled) {
			return result, context.Canceled
		}
		if errors.Is(toolCtx.Err(), context.DeadlineExceeded) {
			return result, deepSeekToolError(http.StatusGatewayTimeout, "tool_timeout", "DeepSeek 工具响应读取超时")
		}
		return result, deepSeekToolError(http.StatusBadGateway, "upstream_error", "DeepSeek 工具响应读取失败")
	}
	if len(data) > deepSeekToolMaxResponseBytes {
		return result, deepSeekToolError(http.StatusBadGateway, "tool_response_too_large", "DeepSeek 工具响应超过大小限制")
	}
	result.Successful, err = validateDeepSeekToolResponse(kind, data)
	if err != nil {
		return result, err
	}
	result.Body = data
	return result, nil
}

func deepSeekToolBaseURL(account *Account) (string, error) {
	if account.IsAdaptiveAPIProtocol() {
		// 若其他协议已配置第三方地址，不能把它的 Key 静默发往官方默认域名。
		if urls, ok := account.Credentials["api_base_urls"].(map[string]any); ok {
			if base, ok := urls[APIProtocolChatCompletions].(string); ok && strings.TrimSpace(base) != "" {
				return strings.TrimSpace(base), nil
			}
			if base := strings.TrimSpace(account.GetCredential("base_url")); base != "" {
				return base, nil
			}
			for _, value := range urls {
				if value, ok := value.(string); ok && strings.TrimSpace(value) != "" {
					return "", deepSeekToolError(http.StatusBadGateway, "upstream_configuration_error", "请为 DeepSeek 工具配置 Chat Completions 上游地址")
				}
			}
		}
		return account.GetCNProtocolBaseURL(APIProtocolChatCompletions), nil
	}
	if base := strings.TrimSpace(account.GetCredential("base_url")); base != "" {
		return base, nil
	}
	return account.GetCNProtocolBaseURL(APIProtocolChatCompletions), nil
}

func validateDeepSeekToolResponse(kind string, body []byte) (bool, error) {
	bad := func() (bool, error) {
		return false, deepSeekToolError(http.StatusBadGateway, "invalid_upstream_response", "DeepSeek 工具返回无效结果")
	}
	var fields map[string]json.RawMessage
	if !utf8.Valid(body) || json.Unmarshal(body, &fields) != nil || fields == nil {
		return bad()
	}
	if raw, ok := fields["error"]; ok && string(raw) != "null" {
		return bad()
	}
	switch kind {
	case DeepSeekToolFetch:
		var title, content string
		if string(fields["title"]) == "null" || string(fields["content"]) == "null" || json.Unmarshal(fields["title"], &title) != nil || json.Unmarshal(fields["content"], &content) != nil {
			return bad()
		}
		return strings.TrimSpace(content) != "", nil
	case DeepSeekToolSearch:
		var results []map[string]json.RawMessage
		if json.Unmarshal(fields["results"], &results) != nil || results == nil || len(results) > 50 {
			return bad()
		}
		for _, item := range results {
			var title, target string
			if string(item["title"]) == "null" || json.Unmarshal(item["title"], &title) != nil || json.Unmarshal(item["url"], &target) != nil {
				return bad()
			}
			u, err := url.Parse(target)
			if err != nil || u.Hostname() == "" || u.User != nil || (!strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https")) {
				return bad()
			}
		}
		return len(results) > 0, nil
	default:
		return bad()
	}
}
