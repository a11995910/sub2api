package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketLiveStatusTracksIndependentRetry(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"},
	}, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		close(started)
		select {
		case <-release:
			return nil, errors.New("proxyconnect http://secret:password@proxy.example:8080: connection refused")
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}})
	account := ticketTestAccount(41)
	account.Status = StatusActive
	svc.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	svc.StartOpenAICodexTicketHarvester()
	t.Cleanup(svc.StopOpenAICodexTicketHarvester)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("未开始采集")
	}
	status := svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0]
	require.Equal(t, "collecting", status.HarvestStatus)
	require.NotNil(t, status.LastAttemptAt)
	require.Equal(t, 1, status.AttemptIndex)
	require.Equal(t, 1, status.AttemptTotal)
	require.Nil(t, status.NextAttemptAt, "进行中的目标没有确定下次重试时刻")
	require.Equal(t, 6, status.RetryIntervalSeconds)
	require.Equal(t, 25, status.AttemptTimeoutSeconds)
	finishedAfter := time.Now()
	close(release)
	require.Eventually(t, func() bool {
		return svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0].NextAttemptAt != nil
	}, time.Second, time.Millisecond, "该目标结束后应安排独立重试")
	status = svc.OpenAICodexTicketStatuses(ctx, account, time.Now())[0]
	require.Equal(t, "waiting", status.HarvestStatus)
	require.Equal(t, "proxy_error", status.LastResult)
	require.True(t, status.NextAttemptAt.After(finishedAfter.Add(6*time.Second)), "该目标从失败结束起等6秒")
	require.WithinDuration(t, time.Now().Add(6*time.Second), *status.NextAttemptAt, time.Second)
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	for _, secret := range []string{"password", "secret", "proxy.example"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestCodexTicketLiveStatusHonorsSwitchesAndRefreshWindow(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}, FailClosed: true,
	}, &httpUpstreamRecorder{})
	account := ticketTestAccount(41)
	account.Status = StatusActive
	now := time.Now()
	svc.storeOpenAICodexTicket(context.Background(), account, &openAICodexTicket{ProxyURL: "http://192.0.2.10:8080",
		Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292, ExpiresAt: now.Add(time.Hour),
	})
	status := svc.OpenAICodexTicketStatuses(context.Background(), account, now)[0]
	require.True(t, status.Ready, "未持久化的内存门票也应立即显示")
	require.Equal(t, "ready", status.HarvestStatus)
	require.Equal(t, now.Add(time.Hour), *status.RefreshDueAt)
	require.Nil(t, status.NextAttemptAt)
	require.False(t, status.Blocked)
	svc.openaiCodexTicketHarvest.beginAttempt(account.ID, status.Model, 1, 2)
	account.Status = "disabled"
	status = svc.OpenAICodexTicketStatuses(context.Background(), account, now)[0]
	require.Equal(t, "paused", status.HarvestStatus)
	require.Equal(t, "account_inactive", status.PauseReason)
	require.Nil(t, status.NextAttemptAt)
	account.Status = StatusActive
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = ""
	require.Equal(t, "proxy_not_configured", svc.OpenAICodexTicketStatuses(context.Background(), account, now)[0].PauseReason)
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
	require.Empty(t, svc.OpenAICodexTicketStatuses(context.Background(), account, now))
	svc.cfg.Gateway.OpenAICodexTicket.Enabled = true
	account.Extra["openai_turn_state_mode"] = "off"
	require.Empty(t, svc.OpenAICodexTicketStatuses(context.Background(), account, now))
}

func TestCodexTicketProbeSafeResults(t *testing.T) {
	for _, tt := range []struct {
		status int
		result string
	}{
		{http.StatusOK, "invalid_format"}, {http.StatusUnauthorized, "auth_failed"},
		{http.StatusProxyAuthRequired, "proxy_error"}, {http.StatusForbidden, "upstream_error"}, {http.StatusTooManyRequests, "upstream_error"},
	} {
		t.Run(tt.result+http.StatusText(tt.status), func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://proxy.example:8080", Models: []string{"gpt-6-astra"}}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				response := codexTicketResponse()
				response.StatusCode = tt.status
				response.Header.Set(openAICodexTurnStateHeader, "格式不符的秘密响应")
				return response, nil
			}})
			account := ticketTestAccount(41)
			account.Status = StatusActive
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			status := svc.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0]
			require.Equal(t, tt.result, status.LastResult)
			require.Equal(t, tt.status, status.LastHTTPStatus)
		})
	}
	require.Equal(t, "canceled", classifyOpenAICodexTicketProbeError(context.Canceled))
	require.Equal(t, "request_timeout", classifyOpenAICodexTicketProbeError(context.DeadlineExceeded))
}

func TestCodexTicketBatchDropsResultsAfterSourceChanges(t *testing.T) {
	cfg := config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://old.example:8080", Models: []string{"gpt-6-astra"}}
	svc := ticketTestService(t, cfg, nil)
	var calls int
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://new.example:8080"
		return codexTicketResponse(), nil
	}}
	account := ticketTestAccount(41)
	account.Status = StatusActive
	source := svc.openAICodexTicketHarvestSource(context.Background())
	svc.probeOpenAICodexTicketWithProxies(context.Background(), account, "gpt-6-astra", []string{source.ProxyURL, "http://another.example:8080"}, source)
	require.Equal(t, 1, calls)
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	require.Equal(t, "canceled", svc.OpenAICodexTicketStatuses(context.Background(), account, time.Now())[0].LastResult)
}
