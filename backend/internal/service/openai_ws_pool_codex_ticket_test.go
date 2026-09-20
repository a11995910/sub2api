package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func newCodexTicketPoolTest(t *testing.T) (*openAIWSConnPool, *openAIWSCountingDialer) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(cfg)
	dialer := &openAIWSCountingDialer{}
	pool.setClientDialerForTest(dialer)
	t.Cleanup(pool.Close)
	return pool, dialer
}

func TestOpenAIWSConnPool_CodexTicketRefreshAndModeSwitchReplaceHandshake(t *testing.T) {
	pool, dialer := newCodexTicketPoolTest(t)
	account := ticketTestAccount(931)
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateOff
	req := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/v1/responses", Headers: make(http.Header)}
	lease, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	oldID := lease.ConnID()
	lease.Release()

	// 开启票模式后的第一发必须真正携票建立新握手。
	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateCodexTicket
	req.Headers.Set(openAICodexTurnStateHeader, "ticket-first")
	lease, err = pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.False(t, lease.Reused())
	require.NotEqual(t, oldID, lease.ConnID())
	oldID = lease.ConnID()
	lease.Release()

	// 同票允许复用，刷新后的新票须重新握手，不能仅修改未发出的 Header 对象。
	lease, err = pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.True(t, lease.Reused())
	require.Equal(t, oldID, lease.ConnID())
	lease.Release()
	req.Headers.Set(openAICodexTurnStateHeader, "ticket-refreshed")
	lease, err = pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.False(t, lease.Reused())
	require.NotEqual(t, oldID, lease.ConnID())
	oldID = lease.ConnID()
	lease.Release()

	account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateOff
	lease, err = pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.False(t, lease.Reused())
	require.NotEqual(t, oldID, lease.ConnID())
	lease.Release()
	require.Equal(t, 4, dialer.DialCount())
}

func TestOpenAIWSConnPool_CodexTicketContinuationPreservesOriginalConnection(t *testing.T) {
	for _, forcePreferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "续链标记", true: "严格原连接"}[forcePreferred], func(t *testing.T) {
			pool, dialer := newCodexTicketPoolTest(t)
			account := ticketTestAccount(932)
			req := openAIWSAcquireRequest{Account: account, WSURL: "wss://example.com/v1/responses", Headers: make(http.Header)}
			req.Headers.Set(openAICodexTurnStateHeader, "ticket-before-refresh")
			first, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			firstID := first.ConnID()
			first.Release()

			req.PreferredConnID = firstID
			req.ForcePreferredConn = forcePreferred
			req.SkipHealthyPreflight = !forcePreferred
			req.Headers.Set(openAICodexTurnStateHeader, "ticket-after-refresh")
			account.Extra[OpenAITurnStateModeKey] = OpenAITurnStateOff
			continued, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			require.True(t, continued.Reused())
			require.Equal(t, firstID, continued.ConnID())
			continued.Release()
			require.Equal(t, 1, dialer.DialCount())

			// 续链复用不改变真实握手身份，后续独立请求仍应使用新模式和新票。
			req.ForcePreferredConn = false
			req.SkipHealthyPreflight = false
			independent, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			require.False(t, independent.Reused())
			require.NotEqual(t, firstID, independent.ConnID())
			independent.Release()
			require.Equal(t, 2, dialer.DialCount())
		})
	}
}
