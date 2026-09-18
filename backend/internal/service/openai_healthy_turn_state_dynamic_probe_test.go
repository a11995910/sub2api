//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type healthyDynamicProbeStore struct {
	healthyStateStoreStub
	logs []HealthyTurnStateProbeLog
}

func (s *healthyDynamicProbeStore) RecordProbe(_ context.Context, _ int64, log HealthyTurnStateProbeLog) error {
	s.logs = append(s.logs, log)
	return nil
}

type healthyDynamicProbeWSDialer struct {
	proxy     string
	temporary bool
}

func (d *healthyDynamicProbeWSDialer) Dial(ctx context.Context, _ string, _ http.Header, proxy string) (openAIWSClientConn, int, http.Header, error) {
	d.proxy = proxy
	d.temporary = IsHealthyTurnStateTemporaryProxy(ctx)
	return &healthyTurnStateProbeWSConn{events: [][]byte{[]byte(healthyTurnStateDelta), []byte(healthyTurnStateDone)}}, 0, healthyTurnStateResponse(200, "临时WS状态头", "").Header, nil
}

func TestHealthyDynamicProbeUsesExplicitProxyAndTemporaryLog(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			store := &healthyDynamicProbeStore{}
			upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(200, "临时状态头", healthyTurnStateSSE())}}
			dialer := &healthyDynamicProbeWSDialer{}
			gateway := &OpenAIGatewayService{httpUpstream: upstream, openaiWSPassthroughDialer: dialer, openaiHealthyTurnStates: openAIHealthyTurnStateCache{repo: store}}
			svc := &AccountTestService{openaiGatewayService: gateway}
			account := healthyTurnStateProbeAccount()
			account.Proxy = &Proxy{ID: 3, Protocol: "http", Host: "127.0.0.1", Port: 8888}
			account.ProxyID = &account.Proxy.ID
			const proxyURL = "socks5h://probe-user:probe-secret@8.8.8.8:1080"
			result, err := svc.ProbeOpenAIHealthyTurnStateWithProxy(context.Background(), account, "gpt-5.4", transport, proxyURL)
			require.NoError(t, err)
			require.Equal(t, "recorded", result.Status)
			if transport == "http" {
				require.Equal(t, []string{proxyURL}, upstream.proxies)
				require.True(t, IsHealthyTurnStateTemporaryProxy(upstream.requests[0].Context()))
			} else {
				require.Equal(t, proxyURL, dialer.proxy)
				require.True(t, dialer.temporary)
				require.Equal(t, 101, result.HTTPStatus, "真实拨号器的成功status0必须识别为握手成功")
			}
			require.EqualValues(t, 3, *account.ProxyID, "临时出口不修改保存账号")
			require.Len(t, store.logs, 1)
			require.True(t, store.logs[0].TemporaryProxy)
			require.Zero(t, store.logs[0].ProxyID)
			require.Len(t, store.scopes, 1)
			require.Equal(t, account.ID, store.scopes[0].AccountID)
			require.Zero(t, store.scopes[0].ProxyID, "临时采集不能伪造或沿用固定代理ID")
			encoded, _ := json.Marshal(result)
			require.NotContains(t, string(encoded), "probe-secret")
			require.NotContains(t, string(encoded), "8.8.8.8")
		})
	}
}

func TestHealthyDynamicProbeInvalidProxyNeverFallsBackToDirect(t *testing.T) {
	upstream := &healthyTurnStateUpstream{}
	svc := &AccountTestService{openaiGatewayService: &OpenAIGatewayService{httpUpstream: upstream}}
	for _, proxyURL := range []string{"", "http://127.0.0.1:8080", "http://proxy.example:8080", "http://8.8.8.8:0"} {
		_, err := svc.ProbeOpenAIHealthyTurnStateWithProxy(context.Background(), healthyTurnStateProbeAccount(), "gpt-5.4", "http", proxyURL)
		require.Error(t, err)
	}
	require.Empty(t, upstream.requests)
}
