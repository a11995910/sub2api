package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func deepSeekFetchTestBody(stream bool, tools string) []byte {
	return []byte(fmt.Sprintf(`{"model":"deepseek-flash","max_tokens":512,"stream":%t,"tools":%s,"messages":[{"role":"user","content":"获取 https://8.8.8.8/ 的标题并回答"}]}`, stream, tools))
}

func deepSeekFetchTestContext(body []byte, recorder http.ResponseWriter) *gin.Context {
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	price := 1.25
	c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformDeepseek, SearchPricePer1k: &price}})
	beginUpstreamResponseModelObservation(c)
	return c
}

func deepSeekFetchTestJSON(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"ds-message-test"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func deepSeekFetchTestMessage(content, stop, usage string) string {
	return `{"id":"msg_test","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":` + content + `,"stop_reason":"` + stop + `","stop_sequence":null,"usage":` + usage + `}`
}

const deepSeekFetchTestTool = `[{"type":"web_fetch_20250910","name":"web_fetch","max_uses":2}]`
const deepSeekFetchTestCall = `{"type":"tool_use","id":"toolu_fetch1","name":"web_fetch","input":{"url":"https://8.8.8.8/"}}`

func TestDeepSeekFetchBufferedAccumulatesEveryRoundAndMapsModel(t *testing.T) {
	body := deepSeekFetchTestBody(false, deepSeekFetchTestTool)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	modelCalls, fetchCalls := 0, 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		if req.URL.Path == "/v1/tools/fetch" {
			fetchCalls++
			return deepSeekFetchTestJSON(200, `{"title":"实际标题","content":"真实抓取正文"}`), nil
		}
		modelCalls++
		require.Equal(t, "/v1/messages", req.URL.Path)
		require.Equal(t, "deepseek-v4.1-flash", gjson.GetBytes(payload, "model").String())
		require.False(t, gjson.GetBytes(payload, "tools.0.type").Exists(), "普通函数不能残留服务端工具类型")
		if modelCalls == 1 {
			return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`]`, "tool_use", `{"input_tokens":11,"output_tokens":5,"cache_read_input_tokens":3,"cache_creation_input_tokens":2}`)), nil
		}
		require.Equal(t, 2, modelCalls)
		require.Equal(t, "tool_use", gjson.GetBytes(payload, "messages.1.content.0.type").String())
		require.Equal(t, "tool_result", gjson.GetBytes(payload, "messages.2.content.0.type").String())
		require.Contains(t, gjson.GetBytes(payload, "messages.2.content.0.content").String(), "真实抓取正文")
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"实际标题"}]`, "end_turn", `{"input_tokens":19,"output_tokens":7,"cache_read_input_tokens":4,"cache_creation_input_tokens":1}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "deepseek-v4.1-flash")
	require.NoError(t, err)
	require.Equal(t, 2, modelCalls)
	require.Equal(t, 1, fetchCalls)
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.Equal(t, 40, result.Usage.InputTokens)
	require.Equal(t, 12, result.Usage.OutputTokens)
	require.Equal(t, 7, result.Usage.CacheReadInputTokens)
	require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
	require.Equal(t, "deepseek-flash", result.Model)
	require.Equal(t, "deepseek-v4.1-flash", result.BillingModel)
	response := recorder.Body.Bytes()
	require.Equal(t, "deepseek-flash", gjson.GetBytes(response, "model").String())
	require.Equal(t, "server_tool_use", gjson.GetBytes(response, "content.0.type").String())
	require.Equal(t, "direct", gjson.GetBytes(response, "content.0.caller.type").String())
	require.Equal(t, "web_fetch_tool_result", gjson.GetBytes(response, "content.1.type").String())
	require.Equal(t, "真实抓取正文", gjson.GetBytes(response, "content.1.content.content.source.data").String())
	_, err = time.Parse(time.RFC3339, gjson.GetBytes(response, "content.1.content.retrieved_at").String())
	require.NoError(t, err)
	require.Equal(t, "end_turn", gjson.GetBytes(response, "stop_reason").String())
	require.Equal(t, int64(30), gjson.GetBytes(response, "usage.input_tokens").Int())
	require.Equal(t, int64(12), gjson.GetBytes(response, "usage.output_tokens").Int())
}

func TestDeepSeekFetchMaxUsesAppliesAcrossParallelCallsAndRounds(t *testing.T) {
	body := deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","max_uses":1}]`)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	modelCalls, fetchCalls := 0, 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			fetchCalls++
			return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
		}
		modelCalls++
		if modelCalls == 1 {
			return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`,{"type":"tool_use","id":"toolu_fetch2","name":"web_fetch","input":{"url":"https://8.8.8.8/second"}}]`, "tool_use", `{"input_tokens":10,"output_tokens":4}`)), nil
		}
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		if modelCalls == 2 {
			require.Contains(t, string(payload), "max_uses_exceeded")
			return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"tool_use","id":"toolu_fetch3","name":"web_fetch","input":{"url":"https://8.8.8.8/third"}}]`, "tool_use", `{"input_tokens":20,"output_tokens":6}`)), nil
		}
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"已达到抓取上限"}]`, "end_turn", `{"input_tokens":30,"output_tokens":8}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 1, fetchCalls, "一个请求最多真实抓取一次，不能每轮重置")
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.Equal(t, 3, modelCalls)
	require.Equal(t, 60, result.Usage.InputTokens)
	require.Equal(t, 2, strings.Count(recorder.Body.String(), "max_uses_exceeded"))
}

func TestDeepSeekFetchMixedClientToolWaitsForClient(t *testing.T) {
	body := deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch"},{"name":"read_file","input_schema":{"type":"object","properties":{}}}]`)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	modelCalls := 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
		}
		modelCalls++
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`,{"type":"tool_use","id":"toolu_client","name":"read_file","input":{}}]`, "tool_use", `{"input_tokens":10,"output_tokens":8}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 1, modelCalls, "客户工具结果尚未到达时不得继续请求模型")
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.Equal(t, "tool_use", gjson.Get(recorder.Body.String(), "stop_reason").String())
	require.Equal(t, "tool_use", gjson.Get(recorder.Body.String(), "content.1.type").String())
	require.Equal(t, "read_file", gjson.Get(recorder.Body.String(), "content.1.name").String())
}

func TestDeepSeekFetchPreservesNativeSearchBlocks(t *testing.T) {
	body := deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch"},{"type":"web_search_20250305","name":"web_search","max_uses":1}]`)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, "web_search_20250305", gjson.GetBytes(payload, "tools.1.type").String())
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"server_tool_use","id":"srvtoolu_search","name":"web_search","input":{"query":"DeepSeek"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_search","content":[{"type":"web_search_result","url":"https://8.8.8.8/","title":"来源","encrypted_content":"opaque"}]},{"type":"text","text":"找到了来源"}]`, "end_turn", `{"input_tokens":20,"output_tokens":9,"server_tool_use":{"web_search_requests":1}}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 0, result.DeepSeekToolCalls, "原生搜索不能误记为独立Fetch费用")
	require.Equal(t, "server_tool_use", gjson.Get(recorder.Body.String(), "content.0.type").String())
	require.Equal(t, "web_search_tool_result", gjson.Get(recorder.Body.String(), "content.1.type").String())
	require.Equal(t, "opaque", gjson.Get(recorder.Body.String(), "content.1.content.0.encrypted_content").String())
}

func TestDeepSeekFetchHistoryRestoresWithoutCurrentToolDeclaration(t *testing.T) {
	body := []byte(`{"model":"deepseek-v4.1-flash","max_tokens":100,"messages":[{"role":"user","content":"抓网页"},{"role":"assistant","content":[{"type":"server_tool_use","id":"srvtoolu_fetch","name":"web_fetch","input":{"url":"https://8.8.8.8/"},"caller":{"type":"direct"}},{"type":"web_fetch_tool_result","tool_use_id":"srvtoolu_fetch","content":{"type":"web_fetch_result","url":"https://8.8.8.8/","retrieved_at":"2026-11-05T00:00:00Z","content":{"type":"document","title":"先前标题","source":{"type":"text","media_type":"text/plain","data":"先前正文"}}}},{"type":"text","text":"已获取"}]},{"role":"user","content":"再解释一下"}]}`)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		messages := gjson.GetBytes(payload, "messages").Array()
		require.Equal(t, "tool_use", messages[1].Get("content.0.type").String())
		require.Equal(t, "user", messages[2].Get("role").String())
		require.Equal(t, "tool_result", messages[2].Get("content.0.type").String())
		require.Contains(t, messages[2].Get("content.0.content").String(), "先前正文")
		for _, message := range messages {
			for _, block := range message.Get("content").Array() {
				require.NotEqual(t, "server_tool_use", block.Get("type").String())
				require.NotEqual(t, "web_fetch_tool_result", block.Get("type").String())
			}
		}
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"解释"}]`, "end_turn", `{"input_tokens":20,"output_tokens":5}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	_, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 1, upstream.calls)
}

func TestDeepSeekFetchLaterFailurePreservesBillableUsageWithoutFailover(t *testing.T) {
	body := deepSeekFetchTestBody(false, deepSeekFetchTestTool)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	modelCalls := 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
		}
		modelCalls++
		if modelCalls == 1 {
			return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`]`, "tool_use", `{"input_tokens":31,"output_tokens":6}`)), nil
		}
		return deepSeekFetchTestJSON(503, `{"error":{"message":"secret upstream-test-key"}}`), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.Error(t, err)
	var failover *UpstreamFailoverError
	require.False(t, errors.As(err, &failover), "工具执行后错误不得触发整请求自动重放")
	require.NotNil(t, result)
	require.Equal(t, 31, result.Usage.InputTokens)
	require.Equal(t, 6, result.Usage.OutputTokens)
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.Equal(t, 2, modelCalls)
	require.NotContains(t, err.Error(), "upstream-test-key")
	require.NotContains(t, recorder.Body.String(), "upstream-test-key")
}

type deepSeekFetchFlushRecorder struct {
	*httptest.ResponseRecorder
	seen chan struct{}
	once sync.Once
}

func (r *deepSeekFetchFlushRecorder) Flush() {
	r.ResponseRecorder.Flush()
	if strings.Contains(r.Body.String(), "立即显示") {
		r.once.Do(func() { close(r.seen) })
	}
}

func deepSeekFetchSSE(event string, value any) string {
	b, _ := json.Marshal(value)
	return "event: " + event + "\ndata: " + string(b) + "\n\n"
}

func TestDeepSeekFetchStreamFlushesBeforeUpstreamFinishes(t *testing.T) {
	body := deepSeekFetchTestBody(true, deepSeekFetchTestTool)
	recorder := &deepSeekFetchFlushRecorder{ResponseRecorder: httptest.NewRecorder(), seen: make(chan struct{})}
	c := deepSeekFetchTestContext(body, recorder)
	reader, writer := io.Pipe()
	producerDone := make(chan error, 1)
	go func() {
		defer writer.Close()
		prefix := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_first","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"usage":{"input_tokens":15,"output_tokens":1}}}` + "\n\n" +
			"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
			"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"立即显示"}}` + "\n\n"
		if _, err := io.WriteString(writer, prefix); err != nil {
			producerDone <- err
			return
		}
		select {
		case <-recorder.seen:
		case <-time.After(3 * time.Second):
			err := errors.New("流式内容在上游结束前没有刷出")
			_ = writer.CloseWithError(err)
			producerDone <- err
			return
		}
		suffix := "event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n" +
			"event: content_block_start\ndata: " + `{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_fetch1","name":"web_fetch","input":{}}}` + "\n\n" +
			deepSeekFetchSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"url":"https://8.8.`}}) +
			deepSeekFetchSSE("content_block_delta", map[string]any{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": `8.8/"}`}}) +
			"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":1}` + "\n\n" +
			"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":8}}` + "\n\n" +
			"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
		_, err := io.WriteString(writer, suffix)
		producerDone <- err
	}()
	modelCalls := 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
		}
		modelCalls++
		if modelCalls == 1 {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: reader}, nil
		}
		final := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_second","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"usage":{"input_tokens":25,"output_tokens":1}}}` + "\n\n" +
			"event: content_block_start\ndata: " + `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
			"event: content_block_delta\ndata: " + `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"最终回答"}}` + "\n\n" +
			"event: content_block_stop\ndata: " + `{"type":"content_block_stop","index":0}` + "\n\n" +
			"event: message_delta\ndata: " + `{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":12}}` + "\n\n" +
			"event: message_stop\ndata: " + `{"type":"message_stop"}` + "\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(final))}, nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.NoError(t, <-producerDone)
	require.Equal(t, 40, result.Usage.InputTokens)
	require.Equal(t, 20, result.Usage.OutputTokens)
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.NotNil(t, result.FirstTokenMs)
	response := recorder.Body.String()
	require.Equal(t, 1, strings.Count(response, "event: message_start\n"))
	require.Equal(t, 1, strings.Count(response, "event: message_stop\n"))
	require.Contains(t, response, "最终回答")
	require.Contains(t, response, "web_fetch_tool_result")
	indexes := make([]int64, 0)
	for _, line := range strings.Split(response, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		event := gjson.Parse(strings.TrimPrefix(line, "data: "))
		if event.Get("type").String() == "content_block_start" {
			indexes = append(indexes, event.Get("index").Int())
		}
	}
	require.Equal(t, []int64{0, 1, 2, 3}, indexes, "跨轮内容块索引应保持递增")
}

func TestDeepSeekFetchIncompleteStreamPreservesObservedUsage(t *testing.T) {
	for _, tail := range []string{"", "event: content_block_delta\ndata: {broken\n\n"} {
		t.Run(fmt.Sprintf("tail_%d", len(tail)), func(t *testing.T) {
			body := deepSeekFetchTestBody(true, deepSeekFetchTestTool)
			recorder := httptest.NewRecorder()
			c := deepSeekFetchTestContext(body, recorder)
			upstream := &deepSeekToolUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
				sse := "event: message_start\ndata: " + `{"type":"message_start","message":{"id":"msg_partial","type":"message","role":"assistant","model":"deepseek-v4.1-flash","content":[],"usage":{"input_tokens":17,"output_tokens":1}}}` + "\n\n" + tail
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(sse))}, nil
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
			require.Error(t, err)
			require.NotNil(t, result)
			require.Equal(t, 17, result.Usage.InputTokens)
			var failover *UpstreamFailoverError
			require.False(t, errors.As(err, &failover))
			require.NotContains(t, recorder.Body.String(), "event: message_stop\n", "未完成流不能伪装成功结束")
		})
	}
}

func TestDeepSeekFetchCancellationStopsBeforeAnotherModelRound(t *testing.T) {
	body := deepSeekFetchTestBody(false, deepSeekFetchTestTool)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	modelCalls, fetchCalls := 0, 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			fetchCalls++
			cancel()
			return nil, context.Canceled
		}
		modelCalls++
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`]`, "tool_use", `{"input_tokens":23,"output_tokens":4}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(ctx, c, deepSeekToolTestAccount(), body, "")
	require.Error(t, err)
	require.NotNil(t, result)
	require.True(t, result.ClientDisconnect)
	require.Equal(t, 1, modelCalls)
	require.Equal(t, 1, fetchCalls)
	require.Zero(t, result.DeepSeekToolCalls)
	require.Equal(t, 23, result.Usage.InputTokens, "取消不能抹掉此前完成的模型用量")
}

func TestDeepSeekFetchHistoryMergesClientAndServerResults(t *testing.T) {
	var request map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"messages":[{"role":"assistant","content":[{"type":"server_tool_use","name":"web_fetch","id":"fetch1","input":{"url":"https://8.8.8.8/"}},{"type":"tool_use","name":"read_file","id":"client1","input":{}},{"type":"web_fetch_tool_result","tool_use_id":"fetch1","content":{"type":"web_fetch_tool_result_error","error_code":"unavailable"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"client1","content":"文件内容"},{"type":"text","text":"继续"}]}]}`), &request))
	require.NoError(t, restoreDeepSeekFetchHistory(request))
	encoded, err := json.Marshal(request)
	require.NoError(t, err)
	messages := gjson.GetBytes(encoded, "messages").Array()
	require.Len(t, messages, 2)
	require.Equal(t, "assistant", messages[0].Get("role").String())
	require.Equal(t, "user", messages[1].Get("role").String())
	require.Equal(t, "tool_use", messages[0].Get("content.0.type").String())
	require.Equal(t, "fetch1", messages[1].Get("content.0.tool_use_id").String())
	require.True(t, messages[1].Get("content.0.is_error").Bool())
	require.Equal(t, "client1", messages[1].Get("content.1.tool_use_id").String())
	require.Equal(t, "继续", messages[1].Get("content.2.text").String())
}

func TestDeepSeekFetchSharedOutputBudget(t *testing.T) {
	for _, tc := range []struct {
		name          string
		firstOutput   int
		thinking      bool
		wantCalls     int
		wantStop      string
		wantRemaining int64
	}{
		{"续轮只能使用剩余输出预算", 4, false, 2, "end_turn", 6},
		{"预算耗尽不再调用模型", 10, false, 1, "max_tokens", 0},
		{"思考预算不足暂停而不降级", 3, true, 1, "pause_turn", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(strings.Replace(string(deepSeekFetchTestBody(false, deepSeekFetchTestTool)), `"max_tokens":512`, `"max_tokens":10`, 1))
			if tc.thinking {
				body = []byte(strings.Replace(string(body), `"max_tokens":10`, `"max_tokens":10,"thinking":{"type":"enabled","budget_tokens":8}`, 1))
			}
			recorder := httptest.NewRecorder()
			c := deepSeekFetchTestContext(body, recorder)
			modelCalls := 0
			upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				if req.URL.Path == "/v1/tools/fetch" {
					return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
				}
				modelCalls++
				if modelCalls == 1 {
					return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`]`, "tool_use", fmt.Sprintf(`{"input_tokens":17,"output_tokens":%d}`, tc.firstOutput))), nil
				}
				payload, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				require.Equal(t, tc.wantRemaining, gjson.GetBytes(payload, "max_tokens").Int())
				return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"答案"}]`, "end_turn", `{"input_tokens":23,"output_tokens":2}`)), nil
			}}
			svc := &OpenAIGatewayService{httpUpstream: upstream}
			result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
			require.NoError(t, err)
			require.Equal(t, tc.wantCalls, modelCalls)
			require.Equal(t, tc.wantStop, gjson.Get(recorder.Body.String(), "stop_reason").String())
			require.Len(t, result.DeepSeekModelUsages, tc.wantCalls, "每轮单独保留计价依据")
		})
	}
}

func TestDeepSeekFetchNativeSearchLimitSurvivesContinuation(t *testing.T) {
	body := deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch"},{"type":"web_search_20250305","name":"web_search","max_uses":1}]`)
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	modelCalls := 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
		}
		modelCalls++
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		if modelCalls == 1 {
			require.Equal(t, int64(1), gjson.GetBytes(payload, "tools.1.max_uses").Int())
			content := `[{"type":"server_tool_use","id":"srvtoolu_search","name":"web_search","input":{"query":"DeepSeek"}},{"type":"web_search_tool_result","tool_use_id":"srvtoolu_search","content":[{"type":"web_search_result","url":"https://8.8.8.8/","title":"来源","encrypted_content":"opaque"}]},` + deepSeekFetchTestCall + `]`
			return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(content, "tool_use", `{"input_tokens":20,"output_tokens":8}`)), nil
		}
		require.Equal(t, int64(0), gjson.GetBytes(payload, "tools.1.max_uses").Int(), "原生搜索总额度不能在Fetch续轮重置")
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"根据抓取来源回答"}]`, "end_turn", `{"input_tokens":30,"output_tokens":4}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 2, modelCalls)
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.Equal(t, int64(1), gjson.Get(recorder.Body.String(), "usage.server_tool_use.web_search_requests").Int())
}

func TestDeepSeekFetchNullableOptionsAndCitationBoundary(t *testing.T) {
	for _, kind := range []string{"web_fetch_20250910", "web_fetch_20250305", "web_fetch"} {
		body := deepSeekFetchTestBody(false, fmt.Sprintf(`[{"type":%q,"name":"web_fetch","max_uses":null,"max_content_tokens":null,"citations":null,"allowed_domains":null,"blocked_domains":null}]`, kind))
		_, options, err := parseDeepSeekFetchRequest(body)
		require.NoError(t, err)
		require.Equal(t, deepSeekFetchMaxUses, options.maxUses)
		require.Equal(t, deepSeekFetchMaxContentBytes, options.maxContentBytes)
	}
	for _, option := range []string{`"citations":{"enabled":true}`, `"url_sources":{"user_input":{"type":"all"}}`, `"max_uses":11`, `"max_content_tokens":-1`} {
		body := deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch",`+option+`}]`)
		_, _, err := parseDeepSeekFetchRequest(body)
		require.Error(t, err, "未实现的能力或越界值必须显式拒绝：%s", option)
	}
	_, options, err := parseDeepSeekFetchRequest(deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","citations":{"enabled":false}}]`))
	require.NoError(t, err)
	require.False(t, options.citations)
}

func TestDeepSeekFetchDomainRestrictionsNormalizeIDNA(t *testing.T) {
	_, blocked, err := parseDeepSeekFetchRequest(deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","blocked_domains":["bücher.de"]}]`))
	require.NoError(t, err)
	require.False(t, blocked.permits("https://xn--bcher-kva.de/"), "Punycode不能绕过Unicode域名黑名单")
	require.False(t, blocked.permits("https://sub.xn--bcher-kva.de/"))
	require.True(t, blocked.permits("https://other.de/"))
	_, allowed, err := parseDeepSeekFetchRequest(deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","allowed_domains":["xn--bcher-kva.de"]}]`))
	require.NoError(t, err)
	require.True(t, allowed.permits("https://bücher.de/"))
	require.False(t, allowed.permits("https://xn--bcher-kva.de.attacker.example/"))
	_, widthBlocked, err := parseDeepSeekFetchRequest(deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","blocked_domains":["ｅｘａｍｐｌｅ.com"]}]`))
	require.NoError(t, err)
	require.False(t, widthBlocked.permits("https://example.com/"))
	_, widthAllowed, err := parseDeepSeekFetchRequest(deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","allowed_domains":["good.com"]}]`))
	require.NoError(t, err)
	require.True(t, widthAllowed.permits("https://ｇｏｏｄ.com/"))
	require.True(t, widthAllowed.permits("https://sub.good.com/"))
	require.False(t, widthAllowed.permits("https://evilgood.com/"))
	_, unrestricted, err := parseDeepSeekFetchRequest(deepSeekFetchTestBody(false, `[{"type":"web_fetch_20250910","name":"web_fetch","allowed_domains":[],"blocked_domains":[]}]`))
	require.NoError(t, err)
	require.True(t, unrestricted.permits("https://public.example/"))
}

func deepSeekFetchTestWithFields(t *testing.T, body []byte, fields map[string]any) []byte {
	t.Helper()
	var request map[string]any
	require.NoError(t, json.Unmarshal(body, &request))
	for key, value := range fields {
		request[key] = value
	}
	encoded, err := json.Marshal(request)
	require.NoError(t, err)
	return encoded
}

func TestDeepSeekFetchForcedToolDisablesOnlyImplicitThinking(t *testing.T) {
	for _, choiceType := range []string{"tool", "any"} {
		for _, thinkingType := range []string{"omitted", "null", "disabled", "off"} {
			t.Run(choiceType+"_"+thinkingType, func(t *testing.T) {
				choice := map[string]any{"type": choiceType}
				if choiceType == "tool" {
					choice["name"] = "web_fetch"
				}
				fields := map[string]any{"tool_choice": choice}
				if thinkingType == "null" {
					fields["thinking"] = nil
				} else if thinkingType != "omitted" {
					fields["thinking"] = map[string]any{"type": thinkingType}
				}
				body := deepSeekFetchTestWithFields(t, deepSeekFetchTestBody(false, deepSeekFetchTestTool), fields)
				recorder := httptest.NewRecorder()
				c := deepSeekFetchTestContext(body, recorder)
				modelCalls := 0
				upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					if req.URL.Path == "/v1/tools/fetch" {
						return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
					}
					modelCalls++
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					wantThinking := thinkingType
					if wantThinking == "omitted" || wantThinking == "null" {
						wantThinking = "disabled"
					}
					require.Equal(t, wantThinking, gjson.GetBytes(payload, "thinking.type").String())
					if modelCalls == 1 {
						require.Equal(t, choiceType, gjson.GetBytes(payload, "tool_choice.type").String())
						return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[`+deepSeekFetchTestCall+`]`, "tool_use", `{"input_tokens":20,"output_tokens":5}`)), nil
					}
					require.Equal(t, "auto", gjson.GetBytes(payload, "tool_choice.type").String(), "只强制首轮工具，不阻碍最终回答")
					return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"抓取完成"}]`, "end_turn", `{"input_tokens":30,"output_tokens":5}`)), nil
				}}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
				require.NoError(t, err)
				require.Equal(t, 2, modelCalls)
				require.Equal(t, 1, result.DeepSeekToolCalls)
			})
		}
	}
}

func TestDeepSeekFetchForcedToolRejectsExplicitThinkingBeforeUpstream(t *testing.T) {
	for _, choiceType := range []string{"tool", "any"} {
		for _, thinkingType := range []string{"enabled", "adaptive"} {
			t.Run(choiceType+"_"+thinkingType, func(t *testing.T) {
				choice := map[string]any{"type": choiceType}
				if choiceType == "tool" {
					choice["name"] = "web_fetch"
				}
				body := deepSeekFetchTestWithFields(t, deepSeekFetchTestBody(false, deepSeekFetchTestTool), map[string]any{
					"tool_choice": choice, "thinking": map[string]any{"type": thinkingType},
				})
				recorder := httptest.NewRecorder()
				c := deepSeekFetchTestContext(body, recorder)
				upstream := &deepSeekToolUpstreamStub{do: func(*http.Request, string, int64, int) (*http.Response, error) {
					t.Error("不兼容参数必须在消耗上游请求前拒绝")
					return nil, errors.New("不应调用上游")
				}}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
				require.Error(t, err)
				require.Nil(t, result)
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Zero(t, upstream.calls)
			})
		}
	}
}

func TestDeepSeekFetchAutoThinkingAndSignatureSurviveToolContinuation(t *testing.T) {
	body := deepSeekFetchTestWithFields(t, deepSeekFetchTestBody(false, deepSeekFetchTestTool), map[string]any{
		"tool_choice": map[string]any{"type": "auto"}, "thinking": map[string]any{"type": "enabled", "budget_tokens": 64},
	})
	recorder := httptest.NewRecorder()
	c := deepSeekFetchTestContext(body, recorder)
	modelCalls := 0
	upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		if req.URL.Path == "/v1/tools/fetch" {
			return deepSeekFetchTestJSON(200, `{"title":"t","content":"c"}`), nil
		}
		modelCalls++
		payload, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, "auto", gjson.GetBytes(payload, "tool_choice.type").String())
		require.Equal(t, "enabled", gjson.GetBytes(payload, "thinking.type").String())
		require.Equal(t, int64(64), gjson.GetBytes(payload, "thinking.budget_tokens").Int())
		if modelCalls == 1 {
			content := `[{"type":"thinking","thinking":"先抓取真实来源再回答","signature":"opaque-signature-do-not-change"},` + deepSeekFetchTestCall + `]`
			return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(content, "tool_use", `{"input_tokens":20,"output_tokens":10}`)), nil
		}
		require.Equal(t, "thinking", gjson.GetBytes(payload, "messages.1.content.0.type").String())
		require.Equal(t, "先抓取真实来源再回答", gjson.GetBytes(payload, "messages.1.content.0.thinking").String())
		require.Equal(t, "opaque-signature-do-not-change", gjson.GetBytes(payload, "messages.1.content.0.signature").String())
		return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"根据正文回答"}]`, "end_turn", `{"input_tokens":30,"output_tokens":8}`)), nil
	}}
	svc := &OpenAIGatewayService{httpUpstream: upstream}
	result, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
	require.NoError(t, err)
	require.Equal(t, 2, modelCalls)
	require.Equal(t, 1, result.DeepSeekToolCalls)
	require.Equal(t, "opaque-signature-do-not-change", gjson.Get(recorder.Body.String(), "content.0.signature").String())
}

func TestDeepSeekFetchUnforcedChoiceDoesNotInjectThinking(t *testing.T) {
	for _, choiceType := range []string{"auto", "none"} {
		for _, thinkingType := range []string{"omitted", "null", "enabled", "adaptive"} {
			t.Run(choiceType+"_"+thinkingType, func(t *testing.T) {
				fields := map[string]any{"tool_choice": map[string]any{"type": choiceType}}
				if thinkingType == "null" {
					fields["thinking"] = nil
				} else if thinkingType != "omitted" {
					fields["thinking"] = map[string]any{"type": thinkingType}
				}
				body := deepSeekFetchTestWithFields(t, deepSeekFetchTestBody(false, deepSeekFetchTestTool), fields)
				recorder := httptest.NewRecorder()
				c := deepSeekFetchTestContext(body, recorder)
				upstream := &deepSeekToolUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
					payload, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.Equal(t, gjson.GetBytes(body, "thinking").Raw, gjson.GetBytes(payload, "thinking").Raw)
					require.Equal(t, choiceType, gjson.GetBytes(payload, "tool_choice.type").String())
					return deepSeekFetchTestJSON(200, deepSeekFetchTestMessage(`[{"type":"text","text":"无需抓取"}]`, "end_turn", `{"input_tokens":20,"output_tokens":5}`)), nil
				}}
				svc := &OpenAIGatewayService{httpUpstream: upstream}
				_, err := svc.forwardDeepSeekFetch(context.Background(), c, deepSeekToolTestAccount(), body, "")
				require.NoError(t, err)
				require.Equal(t, 1, upstream.calls)
			})
		}
	}
}
