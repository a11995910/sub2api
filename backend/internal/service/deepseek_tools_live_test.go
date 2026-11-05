//go:build deepseek_live

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 真实验收仅接受进程环境里的凭据，默认跳过，不读客户请求，不连接本站数据库。
type deepSeekLiveTransport struct {
	HTTPUpstream
	client      *http.Client
	modelCalls  int
	fetchCalls  int
	searchCalls int
	statuses    []int
}

func (s *deepSeekLiveTransport) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	if proxy != "" {
		return nil, fmt.Errorf("真实验收未配置代理传输")
	}
	switch req.URL.Path {
	case "/v1/messages":
		s.modelCalls++
	case "/v1/tools/fetch":
		s.fetchCalls++
	case "/v1/tools/search":
		s.searchCalls++
	}
	response, err := s.client.Do(req)
	if response != nil {
		s.statuses = append(s.statuses, response.StatusCode)
	}
	return response, err
}

func deepSeekLiveSetup(t *testing.T) (*OpenAIGatewayService, *Account, *deepSeekLiveTransport) {
	t.Helper()
	if os.Getenv("SUB2_DEEPSEEK_LIVE") != "1" {
		t.Skip("未启用真实上游验收")
	}
	raw := os.Getenv("SUB2_DEEPSEEK_LIVE_CREDENTIALS")
	if raw == "" {
		t.Skip("没有运行时上游凭据")
	}
	var credentials map[string]any
	if json.Unmarshal([]byte(raw), &credentials) != nil {
		t.Fatal("运行时凭据格式无效")
	}
	if key, _ := credentials["api_key"].(string); strings.TrimSpace(key) == "" {
		t.Fatal("运行时上游 Key 缺失")
	}
	var extra map[string]any
	if rawExtra := os.Getenv("SUB2_DEEPSEEK_LIVE_EXTRA"); rawExtra != "" {
		if json.Unmarshal([]byte(rawExtra), &extra) != nil {
			t.Fatal("运行时账号扩展配置格式无效")
		}
	}
	account := &Account{ID: 11279, Platform: PlatformDeepseek, Type: AccountTypeAPIKey, Concurrency: 1, Credentials: credentials, Extra: extra}
	base, err := deepSeekToolBaseURL(account)
	if err != nil {
		t.Fatal("运行时工具地址无效")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		t.Fatal("真实验收仅支持 HTTPS 上游")
	}
	t.Logf("upstream_host=%s model=deepseek-v4.1-flash", parsed.Hostname())
	transport := &deepSeekLiveTransport{client: &http.Client{Timeout: 120 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	t.Cleanup(transport.client.CloseIdleConnections)
	return &OpenAIGatewayService{httpUpstream: transport}, account, transport
}

// 失败诊断只保留错误消息，遮蔽凭据及 URL 用户信息、查询串和片段，不输出响应正文。
func deepSeekLiveDiagnostic(account *Account, recorder *httptest.ResponseRecorder, err error) string {
	message := ""
	if recorder != nil {
		message = gjson.GetBytes(recorder.Body.Bytes(), "error.message").String()
	}
	if message == "" && err != nil {
		message = err.Error()
	}
	key := account.GetOpenAIProtocolAPIKey()
	if key != "" {
		message = strings.ReplaceAll(message, key, "[REDACTED]")
		message = strings.ReplaceAll(message, url.QueryEscape(key), "[REDACTED]")
	}
	pattern := regexp.MustCompile(`https?://[^\s"'<>]+`)
	message = pattern.ReplaceAllStringFunc(message, func(raw string) string {
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil {
			return "[URL]"
		}
		parsed.User, parsed.RawQuery, parsed.Fragment, parsed.ForceQuery = nil, "", "", false
		return parsed.String()
	})
	runes := []rune(message)
	if len(runes) > 512 {
		message = string(runes[:512])
	}
	return message
}

func TestDeepSeekLiveFetchRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream_%t", stream), func(t *testing.T) {
			svc, account, transport := deepSeekLiveSetup(t)
			// 不主动设置 thinking，真实覆盖强制工具选择与供应商默认思考模式的兼容处理。
			body := []byte(fmt.Sprintf(`{"model":"deepseek-v4.1-flash","stream":%t,"max_tokens":512,"tools":[{"type":"web_fetch_20250910","name":"web_fetch","max_uses":1}],"tool_choice":{"type":"tool","name":"web_fetch"},"messages":[{"role":"user","content":"先调用 web_fetch 获取 https://www.deepseek.com/，再只用一句话报告这次实际抓取的页面标题。不要再次抓取，不要凭记忆回答。"}]}`, stream))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
			// 本地执行器不接入本站计费；显式零值只用于通过本地价格准入检查。
			zero := 0.0
			c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformDeepseek, SearchPricePer1k: &zero}})
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			result, err := svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
			if err != nil {
				t.Fatalf("真实 Fetch 闭环失败：错误类型=%T；下游状态=%d 模型调用=%d 抓取调用=%d upstream_statuses=%v diagnostic=%s", err, recorder.Code, transport.modelCalls, transport.fetchCalls, transport.statuses, deepSeekLiveDiagnostic(account, recorder, err))
			}
			require.NotNil(t, result)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Equal(t, 1, transport.fetchCalls)
			require.GreaterOrEqual(t, transport.modelCalls, 2)
			require.Equal(t, 1, result.DeepSeekToolCalls)
			require.GreaterOrEqual(t, len(result.DeepSeekModelUsages), 2)
			require.Positive(t, result.Usage.InputTokens)
			require.Positive(t, result.Usage.OutputTokens)
			inputTotal, outputTotal := 0, 0
			for _, round := range result.DeepSeekModelUsages {
				require.Positive(t, round.InputTokens)
				require.Positive(t, round.OutputTokens)
				inputTotal += round.InputTokens
				outputTotal += round.OutputTokens
			}
			require.Equal(t, inputTotal, result.Usage.InputTokens)
			require.Equal(t, outputTotal, result.Usage.OutputTokens)
			payload := recorder.Body.Bytes()
			// 只打印布尔值，不在失败断言中输出完整正文。
			hasCall, hasResult, hasText := false, false, false
			if stream {
				for _, line := range strings.Split(string(payload), "\n") {
					if !strings.HasPrefix(line, "data: ") {
						continue
					}
					frame := gjson.Parse(strings.TrimPrefix(line, "data: "))
					kind := frame.Get("content_block.type").String()
					hasCall = hasCall || kind == "server_tool_use"
					hasResult = hasResult || kind == "web_fetch_tool_result" && frame.Get("content_block.content.type").String() == "web_fetch_result"
					hasText = hasText || frame.Get("delta.type").String() == "text_delta" && frame.Get("delta.text").String() != ""
				}
				require.Equal(t, 1, strings.Count(string(payload), "event: message_stop"))
			} else {
				for _, block := range gjson.GetBytes(payload, "content").Array() {
					hasCall = hasCall || block.Get("type").String() == "server_tool_use"
					hasResult = hasResult || block.Get("type").String() == "web_fetch_tool_result" && block.Get("content.type").String() == "web_fetch_result"
					hasText = hasText || block.Get("type").String() == "text" && block.Get("text").String() != ""
				}
			}
			require.True(t, hasCall, "缺少服务端工具调用")
			require.True(t, hasResult, "缺少真实抓取结果")
			require.True(t, hasText, "缺少工具回传后的文本")
			t.Logf("status=%d stream=%t model_calls=%d successful_fetch=%d usage_rounds=%d input_tokens=%d output_tokens=%d", recorder.Code, stream, transport.modelCalls, result.DeepSeekToolCalls, len(result.DeepSeekModelUsages), result.Usage.InputTokens, result.Usage.OutputTokens)
		})
	}
}

func TestDeepSeekLiveIndependentSearch(t *testing.T) {
	svc, account, transport := deepSeekLiveSetup(t)
	result, err := svc.ExecuteDeepSeekTool(context.Background(), nil, account, DeepSeekToolSearch, []byte(`{"query":"DeepSeek official API context caching","count":1}`))
	if err != nil {
		t.Fatalf("真实 Search 失败：错误类型=%T upstream_statuses=%v diagnostic=%s", err, transport.statuses, deepSeekLiveDiagnostic(account, nil, err))
	}
	require.NotNil(t, result)
	require.True(t, result.Successful)
	require.Equal(t, 1, transport.searchCalls)
	t.Logf("status=%d successful_search=1 results=%d", result.StatusCode, len(gjson.GetBytes(result.Body, "results").Array()))
}
