package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

const codexTicketProxyBatchMaxBytes = 64 << 10

// 只有固定生成的脱敏消息可以进入运行结果，未知底层错误不向客户端透传。
type codexTicketProxyPublicError struct{ message string }

func (e *codexTicketProxyPublicError) Error() string { return e.message }
func newCodexTicketProxyPublicError(message string) error {
	return &codexTicketProxyPublicError{message: message}
}

var codexTicketProxyReservedNetworks = mustParseCIDRs([]string{
	"192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32",
})

func codexTicketProxyPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || isPrivateIP(ip) {
		return false
	}
	for _, network := range codexTicketProxyReservedNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func normalizeCodexTicketProxy(line, protocol string) (string, error) {
	invalid := newCodexTicketProxyPublicError("提取结果包含无效或非公网的代理入口")
	line = strings.TrimSpace(line)
	if line == "" || len(line) > 4096 || strings.ContainsAny(line, "\r\n\t ") {
		return "", invalid
	}
	if !strings.Contains(line, "://") {
		line = protocol + "://" + line
	}
	_, parsed, err := proxyurl.Parse(line)
	if err != nil || parsed == nil {
		return "", invalid
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return "", invalid
	}
	ip := net.ParseIP(parsed.Hostname())
	if !codexTicketProxyPublicIP(ip) {
		return "", invalid
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", invalid
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = net.JoinHostPort(ip.String(), strconv.Itoa(port))
	parsed.Path, parsed.RawPath = "", ""
	return parsed.String(), nil
}

func parseCodexTicketProxyBatch(body []byte, protocol string) ([]string, error) {
	if len(body) > codexTicketProxyBatchMaxBytes {
		return nil, newCodexTicketProxyPublicError("提取结果过大，请减少单批代理数量")
	}
	// 仅识别已确认的供应商完整错误格式；输出重新解析后的 IP，不透传原始正文。
	if address, matched := strings.CutSuffix(string(body), " not added to whitelist"); matched {
		if ip := net.ParseIP(address); ip != nil {
			return nil, newCodexTicketProxyPublicError(fmt.Sprintf("服务器出口 IP %s 未加入供应商白名单，请在当前产品的 API 白名单中添加", ip.String()))
		}
	}
	lines := strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\n")
	trimmed := strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff"))
	if strings.HasPrefix(trimmed, "{") {
		var err error
		lines, err = parseCodexTicketProxyJSON([]byte(trimmed))
		if err != nil {
			return nil, err
		}
	}
	var proxies []string
	seen := make(map[string]bool)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		proxy, err := normalizeCodexTicketProxy(line, protocol)
		if err != nil {
			return nil, err
		}
		if !seen[proxy] {
			proxies = append(proxies, proxy)
			seen[proxy] = true
		}
	}
	return proxies, nil
}

// 兼容已核实的 BestGo JSON 格式；供应商消息与原始解析错误均不得直接回显。
func parseCodexTicketProxyJSON(body []byte) ([]string, error) {
	var batch struct {
		Code    int    `json:"code"`
		Success string `json:"success"`
		Message string `json:"msg"`
		Error   string `json:"error"`
		Data    []struct {
			IP   string `json:"ip"`
			Port int    `json:"port"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, newCodexTicketProxyPublicError("代理提取 JSON 格式无效")
	}
	if batch.Code != 200 || batch.Success != "success" {
		if batch.Message == "IP Whitelist Check Failed" || batch.Error == "IP Whitelist Check Failed" {
			return nil, newCodexTicketProxyPublicError("服务器出口 IP 未加入供应商白名单，请在当前产品的 API 白名单中添加")
		}
		return nil, newCodexTicketProxyPublicError("代理提取 JSON 返回无效或未成功，请检查供应商接口配置")
	}
	if batch.Data == nil {
		return nil, newCodexTicketProxyPublicError("代理提取 JSON 缺少有效的 data 列表")
	}
	lines := make([]string, 0, len(batch.Data))
	for _, entry := range batch.Data {
		// 拼接前验证字段，拒绝将 ip 中夹带的协议或认证内容解释为代理地址。
		ip := net.ParseIP(entry.IP)
		if !codexTicketProxyPublicIP(ip) || entry.Port < 1 || entry.Port > 65535 {
			return nil, newCodexTicketProxyPublicError("提取结果包含无效或非公网的代理入口")
		}
		lines = append(lines, net.JoinHostPort(ip.String(), strconv.Itoa(entry.Port)))
	}
	return lines, nil
}

func newCodexTicketProxyFetchClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil, DialContext: safeDialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second,
		DisableKeepAlives: true, MaxConnsPerHost: 1,
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func fetchCodexTicketProxyBatch(ctx context.Context, input codexTicketProxyFetchInput) ([]string, error) {
	client := newCodexTicketProxyFetchClient()
	defer client.CloseIdleConnections()
	return fetchCodexTicketProxyBatchWithClient(ctx, input, client)
}

func fetchCodexTicketProxyBatchWithClient(ctx context.Context, input codexTicketProxyFetchInput, client *http.Client) ([]string, error) {
	if validateCodexTicketProxyConfig(input) != nil {
		return nil, newCodexTicketProxyPublicError("动态代理提取设置无效，请重新保存")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.APIURL, nil)
	if err != nil {
		return nil, newCodexTicketProxyPublicError("无法构造代理提取请求")
	}
	req.Header.Set("Accept", "text/plain, application/json")
	response, err := client.Do(req)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil {
		return nil, newCodexTicketProxyPublicError("代理提取连接失败或超时，请检查接口与服务器 IP 白名单")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newCodexTicketProxyPublicError(fmt.Sprintf("代理提取接口返回 HTTP %d，请检查接口授权与服务器 IP 白名单", response.StatusCode))
	}
	if response.Body == nil {
		return nil, newCodexTicketProxyPublicError("代理提取接口未返回内容")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, codexTicketProxyBatchMaxBytes+1))
	if err != nil {
		return nil, newCodexTicketProxyPublicError("读取代理提取结果失败")
	}
	return parseCodexTicketProxyBatch(body, input.Protocol)
}

// 门票代理提取仅需要接口地址与默认代理协议。
type codexTicketProxyFetchInput struct {
	APIURL   string
	Protocol string
}

func validateCodexTicketProxyAPIURL(raw string) error {
	if len(raw) == 0 || len(raw) > 4096 || strings.ContainsAny(raw, "\r\n") {
		return newCodexTicketProxyPublicError("代理提取接口或协议无效")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || isBlockedHostname(u.Hostname()) {
		return newCodexTicketProxyPublicError("代理提取接口或协议无效")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return newCodexTicketProxyPublicError("代理提取接口或协议无效")
		}
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !codexTicketProxyPublicIP(ip) {
		return newCodexTicketProxyPublicError("代理提取接口或协议无效")
	}
	return nil
}

func validateCodexTicketProxyConfig(input codexTicketProxyFetchInput) error {
	if input.Protocol != "http" && input.Protocol != "https" && input.Protocol != "socks5h" {
		return newCodexTicketProxyPublicError("代理提取接口或协议无效")
	}
	return validateCodexTicketProxyAPIURL(input.APIURL)
}
