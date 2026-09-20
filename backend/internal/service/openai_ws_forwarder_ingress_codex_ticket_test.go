package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexTicketIngressReconnectDialer struct {
	queue   openAIWSQueueDialer
	mu      sync.Mutex
	headers []http.Header
}

func (d *codexTicketIngressReconnectDialer) Dial(ctx context.Context, url string, headers http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	d.headers = append(d.headers, headers.Clone())
	d.mu.Unlock()
	conn, status, _, err := d.queue.Dial(ctx, url, headers, proxy)
	responseHeaders := http.Header{}
	responseHeaders.Set(openAICodexTurnStateHeader, "首轮握手返回的状态头")
	return conn, status, responseHeaders, err
}

func TestOpenAIWSIngressCodexTicketReconnectUsesCurrentModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousPingIdle := openAIWSIngressPreflightPingIdle
	openAIWSIngressPreflightPingIdle = 0
	defer func() { openAIWSIngressPreflightPingIdle = previousPingIdle }()

	cfg := newOpenAIWSExecutionScopeTestConfig()
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra", "gpt-5.6-sol"},
	}
	firstConn := &openAIWSPreflightFailConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-6-astra","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	secondConn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"id":"resp_second","model":"gpt-5.6-sol","usage":{"input_tokens":1,"output_tokens":1}}}`),
	}}
	dialer := &codexTicketIngressReconnectDialer{queue: openAIWSQueueDialer{conns: []openAIWSClientConn{firstConn, secondConn}}}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(dialer)
	defer pool.Close()
	svc := &OpenAIGatewayService{
		cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool,
	}
	account := ticketTestAccount(943)
	account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
	account.Extra["responses_websockets_v2_enabled"] = true
	account.Credentials["model_mapping"] = map[string]any{"first-alias": "gpt-6-astra", "second-alias": "gpt-5.6-sol"}
	firstTicket := fakeCodexTicketState(292)
	secondTicket := openAICodexTicketStatePrefix + strings.Repeat("C", 292-len(openAICodexTicketStatePrefix))
	for model, state := range map[string]string{"gpt-6-astra": firstTicket, "gpt-5.6-sol": secondTicket} {
		svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{
			Model: model, State: state, Length: len(state), ExpiresAt: time.Now().Add(time.Hour),
		})
	}

	serverErrCh := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			serverErrCh <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()
		ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ginCtx.Request = r
		readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		_, firstMessage, readErr := conn.Read(readCtx)
		cancel()
		if readErr != nil {
			serverErrCh <- readErr
			return
		}
		serverErrCh <- svc.ProxyResponsesWebSocketFromClient(r.Context(), ginCtx, conn, account, "test-token", firstMessage, nil)
	}))
	defer server.Close()
	client := dialOpenAIWSExecutionScopeClient(t, server.URL, "门票真实重连")
	defer func() { _ = client.CloseNow() }()
	for _, turn := range []struct{ model, responseID string }{{"first-alias", "resp_first"}, {"second-alias", "resp_second"}} {
		// 不带缓存键及续链 ID，确保真正重连依赖当前模型，而不是缓存键触发的头部重建。
		writeOpenAIWSExecutionScopeRequest(t, client, `{"type":"response.create","model":"`+turn.model+`","stream":true,"input":[{"role":"user","content":"`+turn.responseID+`"}]}`)
		readCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		for {
			_, payload, err := client.Read(readCtx)
			require.NoError(t, err)
			if gjson.GetBytes(payload, "type").String() == "response.completed" {
				require.Equal(t, turn.responseID, gjson.GetBytes(payload, "response.id").String())
				break
			}
		}
		cancel()
	}
	require.NoError(t, client.Close(coderws.StatusNormalClosure, "完成"))
	select {
	case err := <-serverErrCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("等待门票重连测试结束超时")
	}
	dialer.mu.Lock()
	headers := append([]http.Header(nil), dialer.headers...)
	dialer.mu.Unlock()
	require.Len(t, headers, 2)
	require.Equal(t, firstTicket, headers[0].Get(openAICodexTurnStateHeader))
	require.Equal(t, secondTicket, headers[1].Get(openAICodexTurnStateHeader), "重连必须使用当前实际模型的门票")
	require.Equal(t, 1, firstConn.WriteCount(), "原连接应只处理首轮")
	secondConn.mu.Lock()
	writes := append([]map[string]any(nil), secondConn.writes...)
	secondConn.mu.Unlock()
	require.Len(t, writes, 1)
	require.Equal(t, "gpt-5.6-sol", writes[0]["model"])
}
