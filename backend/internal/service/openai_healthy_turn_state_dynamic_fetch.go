package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyurl"
)

const healthyDynamicBatchMaxBytes = 64 << 10

// 只有固定生成的脱敏消息可以进入运行结果，未知底层错误不向客户端透传。
type healthyDynamicPublicError struct{ message string }

func (e *healthyDynamicPublicError) Error() string { return e.message }
func newHealthyDynamicPublicError(message string) error {
	return &healthyDynamicPublicError{message: message}
}

var healthyDynamicReservedNetworks = mustParseCIDRs([]string{
	"192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32",
})

func healthyDynamicPublicIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || isPrivateIP(ip) {
		return false
	}
	for _, network := range healthyDynamicReservedNetworks {
		if network.Contains(ip) {
			return false
		}
	}
	return true
}

func normalizeHealthyDynamicProxy(line, protocol string) (string, error) {
	invalid := newHealthyDynamicPublicError("提取结果包含无效或非公网的代理入口")
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
	if !healthyDynamicPublicIP(ip) {
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

func parseHealthyDynamicProxyBatch(body []byte, protocol string) ([]string, error) {
	if len(body) > healthyDynamicBatchMaxBytes {
		return nil, newHealthyDynamicPublicError("提取结果过大，请减少单批代理数量")
	}
	// 仅识别已确认的供应商完整错误格式；输出重新解析后的 IP，不透传原始正文。
	if address, matched := strings.CutSuffix(string(body), " not added to whitelist"); matched {
		if ip := net.ParseIP(address); ip != nil {
			return nil, newHealthyDynamicPublicError(fmt.Sprintf("服务器出口 IP %s 未加入供应商白名单，请在当前产品的 API 白名单中添加", ip.String()))
		}
	}
	var proxies []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimPrefix(string(body), "\ufeff"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		proxy, err := normalizeHealthyDynamicProxy(line, protocol)
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

func newHealthyDynamicFetchClient() *http.Client {
	transport := &http.Transport{
		Proxy: nil, DialContext: safeDialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second,
		DisableKeepAlives: true, MaxConnsPerHost: 1,
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

func fetchHealthyDynamicProxyBatch(ctx context.Context, input HealthyTurnStateDynamicConfigInput) ([]string, error) {
	client := newHealthyDynamicFetchClient()
	defer client.CloseIdleConnections()
	return fetchHealthyDynamicProxyBatchWithClient(ctx, input, client)
}

func fetchHealthyDynamicProxyBatchWithClient(ctx context.Context, input HealthyTurnStateDynamicConfigInput, client *http.Client) ([]string, error) {
	if validateHealthyDynamicConfig(input) != nil {
		return nil, newHealthyDynamicPublicError("动态代理提取设置无效，请重新保存")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.APIURL, nil)
	if err != nil {
		return nil, newHealthyDynamicPublicError("无法构造代理提取请求")
	}
	req.Header.Set("Accept", "text/plain")
	response, err := client.Do(req)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	if err != nil || response == nil {
		return nil, newHealthyDynamicPublicError("代理提取连接失败或超时，请检查接口与服务器 IP 白名单")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, newHealthyDynamicPublicError(fmt.Sprintf("代理提取接口返回 HTTP %d，请检查接口授权与服务器 IP 白名单", response.StatusCode))
	}
	if response.Body == nil {
		return nil, newHealthyDynamicPublicError("代理提取接口未返回内容")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, healthyDynamicBatchMaxBytes+1))
	if err != nil {
		return nil, newHealthyDynamicPublicError("读取代理提取结果失败")
	}
	return parseHealthyDynamicProxyBatch(body, input.Protocol)
}
