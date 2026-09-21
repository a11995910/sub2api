package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketProbeRequiresMatchingModelAndCompletedResponse(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"匹配的完整响应", codexTicketSuccessSSE("gpt-6-astra"), nil},
		{"终态声明降级模型", codexTicketSuccessSSE("gpt-5.6-luna"), errCodexTicketModelMismatch},
		{"先匹配后降级", "data: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n" + codexTicketSuccessSSE("gpt-5.6-luna"), errCodexTicketModelMismatch},
		{"缺少模型", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n", errCodexTicketUnverified},
		{"只有响应头", "", errCodexTicketUnverified},
		{"只有结束标记", "data: [DONE]\n\n", errCodexTicketUnverified},
		{"模型一致但失败", "data: {\"type\":\"response.failed\",\"response\":{\"model\":\"gpt-6-astra\",\"error\":{\"code\":\"upstream_error\"}}}\n\n", errCodexTicketUnverified},
		{"中途断流", "data: {\"type\":\"response.created\",\"response\":{\"model\":\"gpt-6-astra\"}}\n\n", errCodexTicketUnverified},
		{"多行JSON", "{\n\"model\":\"gpt-6-astra\",\n\"status\":\"completed\"\n}", nil},
		{"多行SSE与命名事件", "event: response.completed\r\ndata: {\"response\":{\r\ndata: \"model\":\"gpt-6-astra\",\"status\":\"completed\"}}\r\n\r\n", nil},
		{"不从输出文字识别模型", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"gpt-5.6-luna\"}\n\n" + codexTicketSuccessSSE("gpt-6-astra"), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, reader := range []io.Reader{strings.NewReader(tc.body), iotest.OneByteReader(strings.NewReader(tc.body))} {
				err := validateCodexTicketProbeResponse(reader, "gpt-6-astra")
				if tc.want == nil {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, tc.want)
				}
			}
		})
	}
	require.ErrorIs(t, validateCodexTicketProbeResponse(strings.NewReader("data: "+strings.Repeat("x", codexTicketResponseMaxBytes+1)), "gpt-6-astra"), errCodexTicketUnverified)
}

func TestCodexTicketProbeRejectsMismatchBeforePersistAndTriesNextProxy(t *testing.T) {
	for _, length := range []int{292, 332} {
		t.Run(strconv.Itoa(length), func(t *testing.T) {
			bad, good := codexTicketResponse(), codexTicketResponse()
			bad.Header.Set(openAICodexTurnStateHeader, fakeCodexTicketState(length))
			good.Header.Set(openAICodexTurnStateHeader, fakeCodexTicketState(length))
			bad.Body = io.NopCloser(strings.NewReader(codexTicketSuccessSSE("gpt-5.6-luna")))
			upstream := &httpUpstreamRecorder{responses: []*http.Response{bad, good}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://192.0.2.1:8080"}, upstream)
			account := ticketTestAccount(41)
			account.Status = StatusActive
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			status := svc.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0]
			require.False(t, status.Ready)
			require.Equal(t, "model_mismatch", status.LastResult)
			svc.probeOpenAICodexTicketWithProxies(context.Background(), account, "gpt-6-astra", []string{"http://192.0.2.2:8080"}, svc.openAICodexTicketHarvestSource(context.Background()))
			ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
			require.NotNil(t, ticket)
			require.Equal(t, length, ticket.Length)
			require.Equal(t, "http://192.0.2.2:8080", ticket.ProxyURL)
		})
	}
}

func TestCodexTicketHTTPModelMismatchRetiresBindingAndPersistsReason(t *testing.T) {
	for _, payload := range []string{
		codexTicketSuccessSSE("gpt-5.6-luna"),
		"event: response.completed\ndata: {\"response\":{\ndata: \"model\":\"gpt-5.6-luna\",\"status\":\"completed\"}}\n\n",
		"{\n\"model\":\"gpt-5.6-luna\",\n\"status\":\"completed\"\n}",
	} {
		t.Run(strconv.Itoa(len(payload)), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(iotest.OneByteReader(strings.NewReader(payload)))}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
			account := ticketTestAccount(41)
			ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
			svc.storeOpenAICodexTicket(context.Background(), account, ticket)
			repo := &codexTicketRefreshRepo{}
			svc.accountRepo = repo
			req, _ := http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
			require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, ticket.Model))
			resp, err := svc.doOpenAIUpstream(req, "", account)
			require.NoError(t, err)
			got, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, payload, string(got), "不回放或修改已转发的业务响应")
			status := svc.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0]
			require.False(t, status.Ready)
			require.True(t, status.Blocked)
			require.Equal(t, "model_mismatch", status.InvalidReason)
			encoded, err := json.Marshal(repo.updates)
			require.NoError(t, err)
			var persisted map[string]any
			require.NoError(t, json.Unmarshal(encoded, &persisted))
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = persisted[openAICodexTicketExtraKey(ticket.Model)]
			restarted := ticketTestService(t, svc.openAICodexTicketConfig(), nil)
			require.True(t, restarted.openAICodexTicketBlocksAccount(account, ticket.Model))
			require.Equal(t, "model_mismatch", restarted.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0].InvalidReason)
			_, err = svc.doOpenAIUpstream(req, "", account)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
		})
	}
}

func TestCodexTicketMismatchDoesNotRetireOtherModelOrNewGeneration(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	account := ticketTestAccount(41)
	old := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
	other := boundTicket("gpt-5.6-sol", "http://192.0.2.2:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, old)
	svc.storeOpenAICodexTicket(context.Background(), account, other)
	old = svc.lookupOpenAICodexTicket(account, old.Model)
	other = svc.lookupOpenAICodexTicket(account, other.Model)
	observe := svc.observeOpenAICodexTicketBinding(account, old)
	newer := boundTicket(old.Model, "http://192.0.2.3:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, newer)
	newer = svc.lookupOpenAICodexTicket(account, newer.Model)
	observe(context.Background(), 0, nil, []byte(`{"type":"response.completed","response":{"model":"gpt-5.6-luna"}}`))
	require.True(t, svc.openAICodexTicketBindingUsable(account, newer))
	require.True(t, svc.openAICodexTicketBindingUsable(account, other))
	for _, payload := range []string{`{"type":"response.output_text.delta","delta":"gpt-5.6-luna"}`, `{"model":"gpt-6-astra"}`, `{"output":[{"model":"gpt-5.6-luna"}]}`} {
		svc.observeOpenAICodexTicketBinding(account, newer)(context.Background(), 0, nil, []byte(payload))
		require.True(t, svc.openAICodexTicketBindingUsable(account, newer))
	}
}

func TestCodexTicketWebSocketModelMismatchRetiresBinding(t *testing.T) {
	for _, rawFrames := range []bool{false, true} {
		t.Run(strconv.FormatBool(rawFrames), func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
			account := ticketTestAccount(41)
			ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
			svc.storeOpenAICodexTicket(context.Background(), account, ticket)
			conn := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"model":"gpt-5.6-luna"}}`)}}
			if rawFrames {
				frame := &openAICodexTicketFrameConn{FrameConn: conn, observe: svc.observeOpenAICodexTicketBinding(account, ticket)}
				_, _, err := frame.ReadFrame(context.Background())
				require.NoError(t, err)
			} else {
				pool, _ := newCodexTicketPoolTest(t)
				pool.setClientDialerForTest(&openAIWSQueueDialer{conns: []openAIWSClientConn{conn}})
				svc.openaiWSPool = pool
				lease, err := svc.acquireOpenAIWSWithCodexTicket(context.Background(), openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: make(http.Header)}, ticket.Model)
				require.NoError(t, err)
				defer lease.Release()
				_, err = lease.ReadMessage(time.Second)
				require.NoError(t, err)
				require.False(t, svc.openAICodexTicketLeaseMatches(account, ticket.Model, lease))
			}
			require.True(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
			require.Equal(t, "model_mismatch", svc.lookupOpenAICodexTicket(account, ticket.Model).InvalidReason)
		})
	}
}

func TestCodexTicketModelAuditCatchesLargeResponseAndResetsForNextRequest(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(strconv.FormatBool(passthrough), func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
			account := ticketTestAccount(41)
			ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
			svc.storeOpenAICodexTicket(context.Background(), account, ticket)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			body := []byte(`{"model":"gpt-6-astra"}`)
			var req *http.Request
			var err error
			if passthrough {
				req, err = svc.buildUpstreamRequestOpenAIPassthrough(context.Background(), c, account, body, "test-token")
			} else {
				req, err = svc.buildUpstreamRequest(context.Background(), c, account, body, "test-token", true, "", true)
			}
			require.NoError(t, err)
			require.NotNil(t, req)
			payload := []byte(`{"model":"gpt-5.6-luna","status":"completed","output_text":"` + strings.Repeat("x", codexTicketResponseMaxBytes+1) + `"}`)
			observer := upstreamResponseModelObserverFromContext(c)
			require.NotNil(t, observer)
			observer.ObserveOpenAI(payload, "")
			require.True(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
			fresh := boundTicket(ticket.Model, "http://192.0.2.2:8080")
			svc.storeOpenAICodexTicket(context.Background(), account, fresh)
			// 后续未绑定门票的请求不能沿用上次的失效回调。
			unbound, _ := http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
			svc.observeOpenAICodexTicketRequestModel(c, unbound, account)
			require.Nil(t, observer.codexTicketModelObserved)
			observer.ObserveOpenAI(payload, "")
			require.False(t, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
		})
	}
}
