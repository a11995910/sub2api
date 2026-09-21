package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketExtractRoundTripper func(*http.Request) (*http.Response, error)

func (f codexTicketExtractRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestCodexTicketHarvestExtractValidationAndMask(t *testing.T) {
	valid := OpenAICodexTicketHarvestSource{Mode: "extract", ExtractURL: "https://supplier.example/private-path?key=private-token", ExtractProtocol: "http"}
	require.NoError(t, ValidateOpenAICodexTicketHarvestSource(valid))
	require.Equal(t, "https://supplier.example/••••", MaskOpenAICodexTicketHarvestExtractURL(valid.ExtractURL))
	httpSource := valid
	httpSource.ExtractURL = "http://supplier.example:8089/gen?key=private-token"
	require.NoError(t, ValidateOpenAICodexTicketHarvestSource(httpSource))
	require.Equal(t, "http://supplier.example:8089/••••", MaskOpenAICodexTicketHarvestExtractURL(httpSource.ExtractURL))
	for _, raw := range []string{"ftp://supplier.example/private-token", "http://localhost/private-token", "http://127.0.0.1/private-token", "https://localhost/private-token", "https://127.0.0.1/private-token", "https://169.254.169.254/private-token", "https://10.0.0.1/private-token", "https://user:private-token@supplier.example/", "https://supplier.example/#private-token"} {
		source := valid
		source.ExtractURL = raw
		err := ValidateOpenAICodexTicketHarvestSource(source)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-token")
		require.Empty(t, MaskOpenAICodexTicketHarvestExtractURL(raw))
	}
	for _, protocol := range []string{"http", "https", "socks5h"} {
		source := valid
		source.ExtractProtocol = protocol
		require.NoError(t, ValidateOpenAICodexTicketHarvestSource(source))
	}
	for _, source := range []OpenAICodexTicketHarvestSource{
		{Mode: "unknown"},
		{Mode: "extract", ExtractProtocol: "http"},
		{Mode: "extract", ExtractURL: valid.ExtractURL, ExtractProtocol: "ftp"},
	} {
		require.Error(t, ValidateOpenAICodexTicketHarvestSource(source))
	}
	require.NoError(t, ValidateOpenAICodexTicketHarvestSource(OpenAICodexTicketHarvestSource{}), "未配置固定代理仍可保存其他设置")
}

func TestCodexTicketHarvestExtractBatchUsesSafeParserAndSingleFetch(t *testing.T) {
	source := OpenAICodexTicketHarvestSource{Mode: "extract", ExtractURL: "http://supplier.example:8089/gen?key=private-token&count=10&proto=http&stype=json&sessType=rotating", ExtractProtocol: "socks5h"}
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		networkErr bool
		want       []string
	}{
		{name: "多行去重", status: 200, body: "8.8.8.8:8080\r\n8.8.4.4:1080\n8.8.8.8:8080\n", want: []string{"socks5h://8.8.8.8:8080", "socks5h://8.8.4.4:1080"}},
		{name: "JSON列表", status: 200, body: `{"code":200,"success":"success","data":[{"ip":"8.8.8.8","port":8080}]}`, want: []string{"socks5h://8.8.8.8:8080"}},
		{name: "保留完整认证代理", status: 200, body: "http://user:private-password@8.8.8.8:8080\n", want: []string{"http://user:private-password@8.8.8.8:8080"}},
		{name: "私网拒绝整批", status: 200, body: "8.8.8.8:8080\n127.0.0.1:80"},
		{name: "空批", status: 200, body: "\r\n"},
		{name: "重定向", status: 302, body: "private-token"},
		{name: "授权失败", status: 403, body: "private-token"},
		{name: "过大响应", status: 200, body: strings.Repeat(" ", healthyDynamicBatchMaxBytes+1)},
		{name: "未知格式", status: 200, body: `{"key":"private-token"}`},
		{name: "网络错误", networkErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client := newHealthyDynamicFetchClient()
			client.Transport = codexTicketExtractRoundTripper(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, source.ExtractURL, req.URL.String())
				require.Equal(t, "text/plain, application/json", req.Header.Get("Accept"))
				deadline, ok := req.Context().Deadline()
				require.True(t, ok)
				require.LessOrEqual(t, time.Until(deadline), 20*time.Second)
				if tc.networkErr {
					return nil, errors.New("private-path private-token private-password")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": {"https://127.0.0.1/private-token"}}, Body: io.NopCloser(strings.NewReader(tc.body)), Request: req}, nil
			})
			proxies, err := fetchOpenAICodexTicketHarvestProxyBatch(context.Background(), source, client)
			if tc.want != nil {
				require.NoError(t, err)
				require.Equal(t, tc.want, proxies)
			} else {
				require.Error(t, err)
				require.Nil(t, proxies)
				for _, secret := range []string{"private-path", "private-token", "private-password"} {
					require.NotContains(t, err.Error(), secret)
				}
			}
			require.Equal(t, 1, calls, "每轮仅提取一次，错误或重定向不自动重试")
		})
	}
}

func TestCodexTicketHarvestSourceDefaultsConfiguredAndProxyCompatibility(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{HarvestProxyURL: "http://user:private-password@proxy.example:8080"}, nil)
	proxies, mode, err := svc.resolveOpenAICodexTicketHarvestProxies(context.Background())
	require.NoError(t, err)
	require.Equal(t, "proxy", mode)
	require.Equal(t, []string{"http://user:private-password@proxy.example:8080"}, proxies)
	require.True(t, svc.openAICodexTicketHarvestSourceConfigured(context.Background()))
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = ""
	require.False(t, svc.openAICodexTicketHarvestSourceConfigured(context.Background()))
	_, _, err = svc.resolveOpenAICodexTicketHarvestProxies(context.Background())
	require.ErrorIs(t, err, ErrOpenAICodexTicketHarvestSourceUnconfigured)
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyMode = "extract"
	svc.cfg.Gateway.OpenAICodexTicket.HarvestExtractURL = "https://supplier.example/secret"
	require.True(t, svc.openAICodexTicketHarvestSourceConfigured(context.Background()), "状态检查只读配置，不能访问提取接口")
}
