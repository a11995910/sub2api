package service

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestCoderOpenAIWSClientDialerTemporaryProxySuccessLeavesSharedCache(t *testing.T) {
	var proxyCalls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		_ = conn.Write(r.Context(), coderws.MessageText, []byte(`{"status":"经代理返回"}`))
		_, _, _ = conn.Read(r.Context())
	}))
	t.Cleanup(proxy.Close)
	dialer := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
	shared, err := dialer.proxyHTTPClient("http://127.0.0.1:1")
	require.NoError(t, err)
	t.Cleanup(shared.CloseIdleConnections)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(WithHealthyTurnStateTemporaryProxy(t.Context()), 2*time.Second)
		conn, _, _, err := dialer.Dial(ctx, "ws://upstream.invalid/responses", nil, proxy.URL)
		require.NoError(t, err)
		body, err := conn.ReadMessage(ctx)
		require.NoError(t, err)
		require.Contains(t, string(body), "经代理返回", "关闭临时 HTTP 空闲连接不得中断升级后的 WS 连接")
		require.NoError(t, conn.Close())
		cancel()
	}
	require.Equal(t, int64(3), proxyCalls.Load())
	require.Len(t, dialer.proxyClients, 1)
	require.Same(t, shared, dialer.proxyClients["http://127.0.0.1:1"].client)
	require.Equal(t, int64(1), dialer.proxyMisses.Load(), "临时代理不应计入共享缓存")
}

func TestCoderOpenAIWSClientDialerTemporaryProxyFailureClosesConnections(t *testing.T) {
	var closedConnections atomic.Int64
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("代理拒绝握手"))
	}))
	proxy.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closedConnections.Add(1)
		}
	}
	proxy.Start()
	t.Cleanup(proxy.Close)
	dialer := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
	ctx, cancel := context.WithTimeout(WithHealthyTurnStateTemporaryProxy(t.Context()), 2*time.Second)
	defer cancel()
	conn, status, _, err := dialer.Dial(ctx, "ws://upstream.invalid/responses", nil, proxy.URL)
	require.Error(t, err)
	require.Nil(t, conn)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Empty(t, dialer.proxyClients)
	require.Eventually(t, func() bool { return closedConnections.Load() == 1 }, time.Second, 5*time.Millisecond)
}

func TestCoderOpenAIWSClientDialerTemporaryProxyNeverFallsBackToDirect(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(target.Close)
	dialer := newDefaultOpenAIWSClientDialer().(*coderOpenAIWSClientDialer)
	for _, proxyURL := range []string{"", "://invalid", "ftp://127.0.0.1:1234"} {
		conn, _, _, err := dialer.Dial(WithHealthyTurnStateTemporaryProxy(t.Context()), "ws"+strings.TrimPrefix(target.URL, "http"), nil, proxyURL)
		require.Error(t, err)
		require.Nil(t, conn)
	}
	require.Zero(t, calls.Load())
	require.Empty(t, dialer.proxyClients)
}
