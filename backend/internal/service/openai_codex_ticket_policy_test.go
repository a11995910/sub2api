package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func ticketPolicySettings(values map[string]string) *SettingService {
	return NewSettingService(&codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: values}}, &config.Config{})
}

func TestCodexTicketPolicyHTTPIndependentSwitches(t *testing.T) {
	for _, rejectMismatch := range []bool{false, true} {
		for _, useProxy := range []bool{false, true} {
			t.Run(strconv.FormatBool(rejectMismatch)+"/"+strconv.FormatBool(useProxy), func(t *testing.T) {
				payload := codexTicketSuccessSSE("gpt-5.6-luna")
				upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(payload))}}
				svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, upstream)
				svc.settingService = ticketPolicySettings(map[string]string{
					SettingKeyOpenAICodexTicketModelMismatchInvalidation: strconv.FormatBool(rejectMismatch),
					SettingKeyOpenAICodexTicketUseHarvestProxy:           strconv.FormatBool(useProxy),
				})
				account := ticketTestAccount(41)
				ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
				svc.storeOpenAICodexTicket(context.Background(), account, ticket)
				req, _ := http.NewRequest(http.MethodPost, "https://example.com/responses", nil)
				require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, ticket.Model))
				resp, err := svc.doOpenAIUpstream(req, "http://192.0.2.99:8080", account)
				require.NoError(t, err)
				got, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				require.NoError(t, resp.Body.Close())
				require.Equal(t, payload, string(got))
				wantProxy := "http://192.0.2.99:8080"
				if useProxy {
					wantProxy = ticket.ProxyURL
				}
				require.Equal(t, wantProxy, upstream.lastProxyURL)
				require.Equal(t, ticket.State, upstream.requests[0].Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, rejectMismatch, svc.lookupOpenAICodexTicket(account, ticket.Model).Invalidated)
				// 关闭模型检查不关闭无效门票与网络失败处理。
				svc.observeOpenAICodexTicketBinding(account, ticket)(context.Background(), 403, nil, nil)
				require.True(t, svc.lookupOpenAICodexTicket(account, ticket.Model).Invalidated)
			})
		}
	}
}

func TestCodexTicketPolicyProbeKeepsCompletionValidation(t *testing.T) {
	for _, body := range []string{codexTicketSuccessSSE("gpt-5.6-luna"), "", `{"model":"gpt-5.6-luna","status":"failed"}`} {
		upstream := &httpUpstreamRecorder{resp: codexTicketResponse()}
		upstream.resp.Body = io.NopCloser(strings.NewReader(body))
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
		svc.settingService = ticketPolicySettings(map[string]string{SettingKeyOpenAICodexTicketModelMismatchInvalidation: "false"})
		_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "测试凭据", "gpt-6-astra", "http://192.0.2.1:8080", time.Second)
		if body == codexTicketSuccessSSE("gpt-5.6-luna") {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, errCodexTicketUnverified)
		}
	}
}

func TestCodexTicketPolicyAuditAndRawFramesKeepTicketWhenDisabled(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	svc.settingService = ticketPolicySettings(map[string]string{SettingKeyOpenAICodexTicketModelMismatchInvalidation: "false"})
	account := ticketTestAccount(41)
	ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
	svc.storeOpenAICodexTicket(context.Background(), account, ticket)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	require.NoError(t, svc.bindOpenAICodexTicketRequest(req, account, ticket.Model))
	svc.observeOpenAICodexTicketRequestModel(c, req, account)
	payload := []byte(`{"type":"response.completed","response":{"model":"gpt-5.6-luna"}}`)
	upstreamResponseModelObserverFromContext(c).ObserveOpenAI(payload, "")
	require.False(t, svc.lookupOpenAICodexTicket(account, ticket.Model).Invalidated)
	frame := &openAICodexTicketFrameConn{
		FrameConn: &openAIWSCaptureConn{events: [][]byte{payload}},
		observe:   svc.observeOpenAICodexTicketBinding(account, ticket),
	}
	_, _, err := frame.ReadFrame(context.Background())
	require.NoError(t, err)
	require.False(t, svc.lookupOpenAICodexTicket(account, ticket.Model).Invalidated)
}

func TestCodexTicketPolicyWSProxyChangeRequiresNewConnection(t *testing.T) {
	ctx := context.Background()
	values := map[string]string{SettingKeyOpenAICodexTicketUseHarvestProxy: "true", SettingKeyOpenAICodexTicketModelMismatchInvalidation: "false"}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	svc.settingService = ticketPolicySettings(values)
	account := ticketTestAccount(41)
	ticket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080")
	svc.storeOpenAICodexTicket(ctx, account, ticket)
	pool, _ := newCodexTicketPoolTest(t)
	conn := &openAIWSCaptureConn{events: [][]byte{[]byte(`{"type":"response.completed","response":{"model":"gpt-5.6-luna"}}`)}}
	pool.setClientDialerForTest(&openAIWSQueueDialer{conns: []openAIWSClientConn{conn, &openAIWSCaptureConn{}}})
	svc.openaiWSPool = pool
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/responses", Headers: make(http.Header)}
	lease, err := svc.acquireOpenAIWSWithCodexTicket(ctx, req, ticket.Model)
	require.NoError(t, err)
	_, err = lease.ReadMessage(time.Second)
	require.NoError(t, err)
	require.True(t, svc.openAICodexTicketLeaseMatches(account, ticket.Model, lease))
	require.False(t, svc.lookupOpenAICodexTicket(account, ticket.Model).Invalidated)
	values[SettingKeyOpenAICodexTicketUseHarvestProxy] = "false"
	svc.settingService.InvalidateOpenAICodexTicketPolicyCache()
	require.False(t, svc.openAICodexTicketLeaseMatches(account, ticket.Model, lease))
	oldID := lease.ConnID()
	lease.Release()
	next, err := svc.acquireOpenAIWSWithCodexTicket(ctx, req, ticket.Model)
	require.NoError(t, err)
	defer next.Release()
	require.NotEqual(t, oldID, next.ConnID())
	require.Empty(t, next.conn.codexTicketProxyURL, "无账号代理时恢复直连")
	require.True(t, svc.openAICodexTicketLeaseMatches(account, ticket.Model, next))
}
