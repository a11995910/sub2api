//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type healthyDynamicRoundTripper func(*http.Request) (*http.Response, error)

func (f healthyDynamicRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestHealthyDynamicProxyParserRejectsUnsafeEntries(t *testing.T) {
	for _, line := range []string{
		"", "127.0.0.1:80", "10.0.0.1:80", "169.254.169.254:80", "100.64.1.2:80", "0.0.0.0:80", "224.0.0.1:80", "240.1.1.1:80", "192.0.2.1:80", "[::1]:80", "[::ffff:127.0.0.1]:80", "[fc00::1]:80", "[2001:db8::1]:80", "proxy.example:80", "8.8.8.8", "8.8.8.8:0", "8.8.8.8:65536", "http://8.8.8.8:80/path", "http://8.8.8.8:80?token=private", "http://8.8.8.8:80#private", "ftp://8.8.8.8:80", "http://private-user:private-secret@127.0.0.1:80", "{\"ip\":\"8.8.8.8\"}",
	} {
		_, err := normalizeHealthyDynamicProxy(line, "http")
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-secret")
		require.NotContains(t, err.Error(), "private-user")
	}
	batch, err := parseHealthyDynamicProxyBatch([]byte("8.8.8.8:8080\r\nhttp://8.8.8.8:8080/\nhttps://user:secret@8.8.8.8:8080\nsocks5://8.8.4.4:1080\n[2001:4860:4860::8888]:1080\n"), "http")
	require.NoError(t, err)
	require.Equal(t, []string{"http://8.8.8.8:8080", "https://user:secret@8.8.8.8:8080", "socks5h://8.8.4.4:1080", "http://[2001:4860:4860::8888]:1080"}, batch, "完整入口去重，协议和认证不同的入口互不覆盖")
	_, err = parseHealthyDynamicProxyBatch([]byte(strings.Repeat(" ", healthyDynamicBatchMaxBytes+1)), "http")
	require.Error(t, err)
}

func TestHealthyDynamicProxyParserWhitelistFailureRequiresExactFormat(t *testing.T) {
	for _, tc := range []struct{ body, address string }{
		{"8.8.8.8 not added to whitelist", "8.8.8.8"},
		{"2001:4860:4860:0:0:0:0:8888 not added to whitelist", "2001:4860:4860::8888"},
	} {
		batch, err := parseHealthyDynamicProxyBatch([]byte(tc.body), "http")
		require.Nil(t, batch)
		var publicError *healthyDynamicPublicError
		require.ErrorAs(t, err, &publicError)
		require.Equal(t, "服务器出口 IP "+tc.address+" 未加入供应商白名单，请在当前产品的 API 白名单中添加", publicError.message)
	}
	for _, body := range []string{
		"private-token not added to whitelist",
		"8.8.8.8 private-token not added to whitelist",
		"8.8.8.8 not added to whitelist private-token",
		"private-token\n8.8.8.8 not added to whitelist",
		"<script>private-token</script> not added to whitelist",
		"8.8.8.8  not added to whitelist",
		"8.8.8.8 not added to whitelist\n",
	} {
		batch, err := parseHealthyDynamicProxyBatch([]byte(body), "http")
		require.Nil(t, batch)
		require.EqualError(t, err, "提取结果包含无效或非公网的代理入口")
		require.NotContains(t, err.Error(), "private-token")
		require.NotContains(t, err.Error(), "8.8.8.8")
	}
	batch, err := parseHealthyDynamicProxyBatch([]byte("8.8.8.8:8080\n8.8.4.4:8080\n"), "http")
	require.NoError(t, err)
	require.Equal(t, []string{"http://8.8.8.8:8080", "http://8.8.4.4:8080"}, batch)
}

func TestHealthyDynamicFetchLimitsRedirectsAndRedactsErrors(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9")
	client := newHealthyDynamicFetchClient()
	transport := client.Transport.(*http.Transport)
	require.Nil(t, transport.Proxy, "提取接口禁止使用环境代理")
	require.Equal(t, 20*time.Second, client.Timeout)
	_, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:9")
	require.Error(t, err, "真实拨号必须拒绝私网地址")
	input := HealthyTurnStateDynamicConfigInput{APIURL: "https://supplier.example/secret-path?token=private-token", Protocol: "http", TargetCount: 1, MaxAttempts: 1}
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		networkErr bool
		want       string
	}{
		{"正常", 200, "8.8.8.8:8080\n", false, ""},
		{"重定向", 302, "provider private-token", false, "HTTP 302"},
		{"鉴权失败", 403, "provider private-token", false, "HTTP 403"},
		{"白名单未添加", 200, "8.8.8.8 not added to whitelist", false, "服务器出口 IP 8.8.8.8 未加入供应商白名单"},
		{"过大", 200, strings.Repeat(" ", healthyDynamicBatchMaxBytes+1), false, "过大"},
		{"格式错误", 200, "{\"secret\":\"private-token\"}", false, "无效"},
		{"网络错误", 0, "", true, "连接失败"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client.Transport = healthyDynamicRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "text/plain, application/json", req.Header.Get("Accept"))
				deadline, ok := req.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 20*time.Second)
				if tc.networkErr {
					return nil, errors.New("transport leak https://supplier.example/secret-path?token=private-token")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{"https://127.0.0.1/private-token"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})
			batch, err := fetchHealthyDynamicProxyBatchWithClient(context.Background(), input, client)
			if tc.want == "" {
				require.NoError(t, err)
				require.Equal(t, []string{"http://8.8.8.8:8080"}, batch)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.want)
				require.NotContains(t, err.Error(), "private-token")
				require.NotContains(t, err.Error(), "secret-path")
			}
			require.Equal(t, 1, calls, "不得跟随重定向或自动重试供应商提取")
		})
	}
}

func TestHealthyDynamicFetchCancellation(t *testing.T) {
	client := newHealthyDynamicFetchClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client.Transport = healthyDynamicRoundTripper(func(req *http.Request) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	_, err := fetchHealthyDynamicProxyBatchWithClient(ctx, HealthyTurnStateDynamicConfigInput{APIURL: "https://supplier.example", Protocol: "http", TargetCount: 1, MaxAttempts: 1}, client)
	require.Error(t, err)
}

func TestHealthyDynamicJSONFetchPreservesHTTPURLAndNormalizesEntries(t *testing.T) {
	input := HealthyTurnStateDynamicConfigInput{
		APIURL:   "http://supplier.example:8089/gen?zone=custom&ptype=1&region=US&count=1&proto=http&stype=json&sessType=rotating",
		Protocol: "socks5h", TargetCount: 1, MaxAttempts: 10,
	}
	client := newHealthyDynamicFetchClient()
	client.Transport = healthyDynamicRoundTripper(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, input.APIURL, req.URL.String(), "提取数量与轮换参数必须原样传递")
		// 以正文识别格式，供应商未声明 Content-Type 时也能读取。
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("\ufeff " + `{"code":200,"success":"success","msg":"","error":"","data":[{"ip":"8.8.8.8","port":8080},{"ip":"8.8.8.8","port":8080},{"ip":"8.8.8.8","port":8081},{"ip":"2001:4860:4860::8888","port":1080}]}`)), Request: req}, nil
	})
	batch, err := fetchHealthyDynamicProxyBatchWithClient(context.Background(), input, client)
	require.NoError(t, err)
	require.Equal(t, []string{"socks5h://8.8.8.8:8080", "socks5h://8.8.8.8:8081", "socks5h://[2001:4860:4860::8888]:1080"}, batch)
}

func TestHealthyDynamicJSONRejectsInvalidAndRedactsSupplierErrors(t *testing.T) {
	for _, body := range []string{
		`{"code":200,"success":"success","data":[{"ip":"8.8.8.8","port":8080},{"ip":"127.0.0.1","port":80}]}`,
		`{"code":200,"success":"success","data":[{"ip":"169.254.169.254","port":80}]}`,
		`{"code":200,"success":"success","data":[{"ip":"proxy.example","port":80}]}`,
		`{"code":200,"success":"success","data":[{"ip":"user:private-token@8.8.8.8","port":80}]}`,
		`{"code":200,"success":"success","data":[{"ip":"8.8.8.8","port":0}]}`,
		`{"code":200,"success":"success","data":[{"ip":"8.8.8.8","port":65536}]}`,
		`{"code":200,"success":"success","data":[{"ip":"8.8.8.8","port":"private-token"}]}`,
		`{"code":200,"success":"success","data":[null]}`,
		`{"code":200,"success":"success","data":null}`,
		`{"code":200,"success":"success"}`,
		`{"code":200,"success":"success","data":[]}{"secret":"private-token"}`,
		`{"code":400,"success":"","msg":"private-token","error":"private-token","data":[]}`,
		`{"code":200,"success":"","data":[{"ip":"8.8.8.8","port":80}]}`,
		`{"data":[{"ip":"8.8.8.8","port":80}]}`,
	} {
		batch, err := parseHealthyDynamicProxyBatch([]byte(body), "http")
		require.Error(t, err)
		require.Nil(t, batch, "任一条目无效时拒绝整批")
		require.NotContains(t, err.Error(), "private-token")
	}
	batch, err := parseHealthyDynamicProxyBatch([]byte(`{"code":400,"success":"","msg":"IP Whitelist Check Failed","error":"IP Whitelist Check Failed","data":[]}`), "http")
	require.Nil(t, batch)
	require.ErrorContains(t, err, "未加入供应商白名单")
	batch, err = parseHealthyDynamicProxyBatch([]byte(`{"code":200,"success":"success","data":[]}`), "http")
	require.NoError(t, err)
	require.Empty(t, batch, "成功空批次交由已有的无新入口机制处理")
}
