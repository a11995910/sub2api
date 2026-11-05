package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateDeepSeekToolSearchRequest(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"query":null}`, `{"query":"   "}`, `{"query":"q","count":0}`,
		`{"query":"q","count":51}`, `{"query":"q","count":1.5}`, `{"query":"q","count":null}`,
		`{"query":"q","need_content":null}`, `{"query":"q","need_content":"true"}`,
		`{"query":"q","localization":"not-an-ip"}`, `{"query":"q","localization":null}`,
		`{"query":"q","ori_question":""}`, `{"query":"` + strings.Repeat("中", 129) + `"}`,
	} {
		_, err := ValidateDeepSeekToolRequest(context.Background(), DeepSeekToolSearch, []byte(body))
		require.Error(t, err, body)
	}
	valid := `{"query":"搜索 site:example.com","count":2,"need_content":true,"ori_question":null,"localization":"8.8.8.8","api_key":"must-not-forward"}`
	body, err := ValidateDeepSeekToolRequest(context.Background(), DeepSeekToolSearch, []byte(valid))
	require.NoError(t, err)
	require.JSONEq(t, `{"query":"搜索 site:example.com","count":2,"need_content":true,"localization":"8.8.8.8"}`, string(body))
	body, err = ValidateDeepSeekToolRequest(context.Background(), DeepSeekToolSearch, []byte(`{"query":"`+strings.Repeat("中", 128)+`"}`))
	require.NoError(t, err)
	require.NotEmpty(t, body)
	_, err = ValidateDeepSeekToolRequest(context.Background(), DeepSeekToolSearch, []byte(strings.Repeat(" ", deepSeekToolMaxRequestBytes+1)))
	var toolErr *DeepSeekToolError
	require.ErrorAs(t, err, &toolErr)
	require.Equal(t, http.StatusRequestEntityTooLarge, toolErr.StatusCode)
}

func TestValidateDeepSeekFetchURLSecurity(t *testing.T) {
	t.Parallel()
	lookup := func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }
	for _, target := range []string{
		"https://localhost/", "https://a.localhost/", "http://127.0.0.1/", "http://127.0.0.1./",
		"https://ｌｏｃａｌｈｏｓｔ/", "https://１２７.０.０.１/",
		"http://10.0.0.1/", "http://172.16.0.1/", "http://192.168.1.1/", "http://169.254.169.254/",
		"http://0.0.0.0/", "http://100.64.0.1/", "http://192.0.2.1/", "http://198.18.0.1/",
		"http://224.0.0.1/", "http://240.0.0.1/", "http://[::1]/", "http://[::ffff:127.0.0.1]/",
		"http://[fe80::1]/", "http://[fc00::1]/", "http://[64:ff9b::7f00:1]/", "http://[2002:7f00:1::]/",
		"https://user:pass@example.com/", "https://example.com/#part", "file:///tmp/a", "ftp://example.com/a",
		"https://example.com:0/", "https://example.com:65536/", " https://example.com/", "https://example.com/ ",
		"https://example.com/" + strings.Repeat("a", 8192),
	} {
		require.Error(t, validateDeepSeekFetchURL(context.Background(), target, lookup), target)
	}
	for _, target := range []string{"https://example.com/a?x=1", "http://8.8.8.8/", "HTTPS://example.com/", "https://[2606:4700:4700::1111]/"} {
		require.NoError(t, validateDeepSeekFetchURL(context.Background(), target, lookup), target)
	}
	for _, ips := range [][]net.IP{nil, {net.ParseIP("8.8.8.8"), net.ParseIP("127.0.0.1")}} {
		err := validateDeepSeekFetchURL(context.Background(), "https://example.com/", func(context.Context, string, string) ([]net.IP, error) { return ips, nil })
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ValidateDeepSeekToolRequest(ctx, DeepSeekToolFetch, []byte(`{"url":"https://example.com/"}`))
	require.ErrorIs(t, err, context.Canceled)
}

type deepSeekToolUpstreamStub struct {
	HTTPUpstream
	do    func(*http.Request, string, int64, int) (*http.Response, error)
	calls int
}

func (s *deepSeekToolUpstreamStub) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	s.calls++
	return s.do(req, proxy, id, concurrency)
}

func deepSeekToolTestAccount() *Account {
	return &Account{ID: 73, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Concurrency: 4,
		Credentials: map[string]any{"api_key": "upstream-test-key", "api_protocol": APIProtocolAdaptive,
			"api_base_urls": map[string]any{APIProtocolChatCompletions: "https://relay.example/v1", APIProtocolAnthropic: "https://different.example"}}}
}

func TestExecuteDeepSeekToolUsesSelectedAccountAndStopsRedirects(t *testing.T) {
	t.Parallel()
	account := deepSeekToolTestAccount()
	account.Credentials[credKeyHeaderOverrideEnabled] = true
	account.Credentials[credKeyHeaderOverrides] = map[string]any{"accept": "text/event-stream", "x-relay-tag": "tool-test", "authorization": "Bearer forbidden-override"}
	proxyID := int64(9)
	account.ProxyID, account.Proxy = &proxyID, &Proxy{Protocol: "socks5", Host: "127.0.0.1", Port: 1080}
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
		require.Equal(t, "https://relay.example/v1/tools/fetch", req.URL.String())
		require.Equal(t, "Bearer upstream-test-key", req.Header.Get("Authorization"))
		require.Empty(t, req.Header.Get("Cookie"))
		require.Empty(t, req.Header.Get("X-Api-Key"))
		require.Equal(t, "application/json", req.Header.Get("Accept"))
		require.Equal(t, "tool-test", getHeaderRaw(req.Header, "x-relay-tag"))
		require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))
		deadline, ok := req.Context().Deadline()
		require.True(t, ok)
		require.LessOrEqual(t, time.Until(deadline), deepSeekToolTimeout)
		require.Equal(t, "socks5://127.0.0.1:1080", proxy)
		require.Equal(t, account.ID, id)
		require.Equal(t, account.Concurrency, concurrency)
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"url":"https://8.8.8.8/"}`, string(body))
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": []string{"fetch-123"}}, Body: io.NopCloser(strings.NewReader(`{"title":"网页标题","content":"实际内容"}`))}, nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/tools/fetch", nil)
	c.Request.Header.Set("Authorization", "Bearer customer-key")
	c.Request.Header.Set("X-Api-Key", "customer-key")
	c.Request.Header.Set("Cookie", "private-cookie")
	result, err := svc.ExecuteDeepSeekTool(context.Background(), c, account, DeepSeekToolFetch, []byte(`{"url":"https://8.8.8.8/","headers":{"Authorization":"Bearer injected"}}`))
	require.NoError(t, err)
	require.True(t, result.Successful)
	require.Equal(t, "fetch-123", result.RequestID)
	require.Equal(t, 1, upstream.calls)
}

func TestExecuteDeepSeekToolFailuresAreNotRetriedOrBillable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		transport error
		wantError bool
	}{
		{"重定向", 307, `{"secret":"upstream-test-key"}`, nil, true},
		{"限流", 429, `{"error":"upstream-test-key"}`, nil, true},
		{"凭据错误", 401, `{"error":"upstream-test-key"}`, nil, true},
		{"连接错误", 0, "", errors.New("secret upstream-test-key"), true},
		{"畸形JSON", 200, `not-json upstream-test-key`, nil, true},
		{"伪成功", 200, `{"title":"t","content":"c","error":{"message":"secret"}}`, nil, true},
		{"空正文", 200, `{"title":"t","content":"  "}`, nil, false},
		{"null正文", 200, `{"title":"t","content":null}`, nil, true},
		{"超长结果", 200, strings.Repeat("a", deepSeekToolMaxResponseBytes+1), nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &deepSeekToolUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				if tc.transport != nil {
					return nil, tc.transport
				}
				return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			result, err := svc.ExecuteDeepSeekTool(context.Background(), nil, deepSeekToolTestAccount(), DeepSeekToolFetch, []byte(`{"url":"https://8.8.8.8/"}`))
			if tc.wantError {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "upstream-test-key")
			} else {
				require.NoError(t, err)
			}
			if result != nil {
				require.False(t, result.Successful)
			}
			require.Equal(t, 1, upstream.calls)
		})
	}
}

func TestDeepSeekToolResponseSearchSuccessRequiresResults(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		body    string
		success bool
		valid   bool
	}{
		{`{"results":[]}`, false, true},
		{`{"results":[{"title":"","url":"https://example.com/"}]}`, true, true},
		{`{"results":null}`, false, false},
		{`{"results":[{}]}`, false, false},
		{`{"results":[{"title":"t","url":"file:///secret"}]}`, false, false},
		{`{"results":[{"title":"t","url":"https://user:password@example.com"}]}`, false, false},
	} {
		success, err := validateDeepSeekToolResponse(DeepSeekToolSearch, []byte(tc.body))
		require.Equal(t, tc.valid, err == nil, tc.body)
		require.Equal(t, tc.success, success, tc.body)
	}
}

func TestDeepSeekToolBaseNeverFallsBackToOfficialWithThirdPartyCredentials(t *testing.T) {
	t.Parallel()
	account := deepSeekToolTestAccount()
	delete(account.Credentials["api_base_urls"].(map[string]any), APIProtocolChatCompletions)
	_, err := deepSeekToolBaseURL(account)
	require.Error(t, err)
	account.Credentials["base_url"] = "https://legacy-relay.example/v1"
	base, err := deepSeekToolBaseURL(account)
	require.NoError(t, err)
	require.Equal(t, "https://legacy-relay.example/v1", base)
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	base, err = deepSeekToolBaseURL(account)
	require.NoError(t, err)
	require.Equal(t, "https://legacy-relay.example/v1", base)
}

func TestExecuteDeepSeekToolCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		cancel()
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	_, err := svc.ExecuteDeepSeekTool(ctx, nil, deepSeekToolTestAccount(), DeepSeekToolSearch, []byte(`{"query":"test"}`))
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, upstream.calls)
}
