package repository

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestHTTPUpstreamTemporaryProxyDoesNotReplaceSharedClients(t *testing.T) {
	var directCalls, proxyCalls, closedConnections atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		directCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	proxy := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		_, _ = io.WriteString(w, "经代理返回")
	}))
	proxy.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			closedConnections.Add(1)
		}
	}
	proxy.Start()
	t.Cleanup(proxy.Close)

	// 账号级隔离下最容易让探测代理替换正式账号的已有连接池。
	cfg := &config.Config{}
	cfg.Gateway.ConnectionPoolIsolation = config.ConnectionPoolIsolationAccount
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	shared, err := upstream.getOrCreateClient("", 41, 1)
	require.NoError(t, err)
	t.Cleanup(shared.client.CloseIdleConnections)

	const attempts = 12
	for i := 0; i < attempts; i++ {
		ctx, cancel := context.WithTimeout(service.WithHealthyTurnStateTemporaryProxy(t.Context()), 2*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
		require.NoError(t, err)
		proxyURL := strings.Replace(proxy.URL, "http://", fmt.Sprintf("http://session-%d@", i), 1)
		resp, err := upstream.Do(req, proxyURL, 41, 1)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, "经代理返回", string(body))
		require.NoError(t, resp.Body.Close())
		cancel()
	}
	require.Equal(t, int64(attempts), proxyCalls.Load())
	require.Zero(t, directCalls.Load(), "临时代理不能静默回退直连")
	require.Len(t, upstream.clients, 1)
	for _, entry := range upstream.clients {
		require.Same(t, shared, entry, "动态探测不能替换正式连接池")
	}
	require.Eventually(t, func() bool { return closedConnections.Load() == attempts }, time.Second, 5*time.Millisecond, "探测结束应及时释放代理连接")
}

func TestHTTPUpstreamTemporaryProxyFailureDoesNotRecordHTTP2Fallback(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		// 制造能触发现有 H2 兼容性识别的真实网络错误。
		_, _ = rw.WriteString("HTTP/1.1 goaway\r\n\r\n")
		_ = rw.Flush()
	}))
	t.Cleanup(proxy.Close)
	cfg := &config.Config{}
	cfg.Gateway.OpenAIHTTP2.Enabled = true
	cfg.Gateway.OpenAIHTTP2.AllowProxyFallbackToHTTP1 = true
	upstream := NewHTTPUpstream(cfg).(*httpUpstreamService)
	ctx := service.WithHealthyTurnStateTemporaryProxy(service.WithHTTPUpstreamProfile(t.Context(), service.HTTPUpstreamProfileOpenAI))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://upstream.invalid/responses", nil)
	require.NoError(t, err)
	resp, err := upstream.Do(req, proxy.URL, 42, 1)
	require.Error(t, err)
	require.Nil(t, resp)
	require.True(t, isOpenAIHTTP2CompatibilityError(err), "错误必须覆盖 H2 回退记录分支")
	require.Empty(t, upstream.clients)
	upstream.openAIHTTP2Fallbacks.Range(func(_, _ any) bool {
		t.Error("临时代理不应产生长期 H2 回退状态")
		return true
	})
}

func TestHTTPUpstreamTemporaryProxyPreservesTLSFingerprintRouting(t *testing.T) {
	var connectCalls atomic.Int64
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			connectCalls.Add(1)
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(proxy.Close)
	upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
	req, err := http.NewRequestWithContext(service.WithHealthyTurnStateTemporaryProxy(t.Context()), http.MethodGet, "https://upstream.invalid/responses", nil)
	require.NoError(t, err)
	resp, err := upstream.DoWithTLS(req, proxy.URL, 43, 1, &tlsfingerprint.Profile{Name: "临时采集测试"})
	require.ErrorContains(t, err, "proxy CONNECT failed", "必须复用指纹 CONNECT 拨号器")
	require.Nil(t, resp)
	require.Equal(t, int64(1), connectCalls.Load())
	require.Empty(t, upstream.clients)
}

func TestHTTPUpstreamTemporaryProxyRejectsMissingOrInvalidProxy(t *testing.T) {
	var calls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	for _, proxyURL := range []string{"", "://invalid", "ftp://127.0.0.1:1234"} {
		req, err := http.NewRequestWithContext(service.WithHealthyTurnStateTemporaryProxy(t.Context()), http.MethodGet, target.URL, nil)
		require.NoError(t, err)
		resp, err := NewHTTPUpstream(nil).Do(req, proxyURL, 44, 1)
		require.Error(t, err)
		require.Nil(t, resp)
	}
	require.Zero(t, calls.Load())
}
