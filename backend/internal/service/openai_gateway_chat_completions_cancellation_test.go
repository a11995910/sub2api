//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contextBoundBlockingReadCloser struct {
	data       []byte
	offset     int
	ctx        context.Context
	forceClose chan struct{}
	closeOnce  sync.Once
}

func newContextBoundBlockingReadCloser(data []byte) *contextBoundBlockingReadCloser {
	return &contextBoundBlockingReadCloser{
		data:       data,
		forceClose: make(chan struct{}),
	}
}

func (r *contextBoundBlockingReadCloser) Read(p []byte) (int, error) {
	if r.offset < len(r.data) {
		n := copy(p, r.data[r.offset:])
		r.offset += n
		return n, nil
	}
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case <-r.forceClose:
		return 0, io.EOF
	}
}

func (r *contextBoundBlockingReadCloser) Close() error {
	select {
	case <-r.ctx.Done():
	case <-r.forceClose:
	}
	return nil
}

func (r *contextBoundBlockingReadCloser) forceUnblock() {
	r.closeOnce.Do(func() { close(r.forceClose) })
}

type contextBoundHTTPUpstream struct {
	body *contextBoundBlockingReadCloser
}

func (u *contextBoundHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.body.ctx = req.Context()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       u.body,
	}, nil
}

func (u *contextBoundHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func TestForwardAsChatCompletions_CancelsUpstreamBeforeClosingBody(t *testing.T) {
	testForwardChatCompletionsCancellation(t, "gpt-5.1")
}

func TestForwardAsChatCompletions_ModelMismatchCancelsBeforeClosingBody(t *testing.T) {
	testForwardChatCompletionsCancellation(t, "gpt-5.4")
}

func testForwardChatCompletionsCancellation(t *testing.T, responseModel string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	body := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}],"stream":true}`)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(body)))
	c.Request.Header.Set("Content-Type", "application/json")

	upstreamBody := []byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"object\":\"response\",\"model\":\"gpt-5.4\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"status\":\"completed\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":17,\"output_tokens\":8,\"total_tokens\":25}}}\n\n")
	// 实际上游模型为映射后的 gpt-5.1；另一个用例专门验证模型不一致的关闭路径。
	upstreamBody = []byte(strings.ReplaceAll(string(upstreamBody), "gpt-5.4", responseModel))
	stream := newContextBoundBlockingReadCloser(upstreamBody)
	t.Cleanup(stream.forceUnblock)

	cfg := rawChatCompletionsTestConfig()
	cfg.Gateway.StreamKeepaliveInterval = 1
	svc := &OpenAIGatewayService{
		cfg:          cfg,
		httpUpstream: &contextBoundHTTPUpstream{body: stream},
	}

	type forwardResult struct {
		result *OpenAIForwardResult
		err    error
	}
	resultCh := make(chan forwardResult, 1)
	go func() {
		result, err := svc.ForwardAsChatCompletions(context.Background(), c, rawChatCompletionsTestAccount(), body, "", "gpt-5.1")
		resultCh <- forwardResult{result: result, err: err}
	}()

	select {
	case got := <-resultCh:
		// 已移除健康头模型拦截；保留模型审计，并确保普通转发仍释放上游读取。
		require.NoError(t, got.err)
		require.NotNil(t, got.result)
		require.Equal(t, responseModel, got.result.UpstreamResponseModel)
		require.Equal(t, 17, got.result.Usage.InputTokens)
		require.ErrorIs(t, stream.ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("ForwardAsChatCompletions did not cancel upstream before closing the body")
	}
}

func TestCodexTicketProbeCancelsBeforeClosingCompletedStream(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "gpt-5.6-luna"} {
		t.Run(model, func(t *testing.T) {
			body := newContextBoundBlockingReadCloser([]byte(codexTicketSuccessSSE(model)))
			t.Cleanup(body.forceUnblock)
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				body.ctx = req.Context()
				response := codexTicketResponse()
				response.Body = body
				return response, nil
			}})
			result := make(chan error, 1)
			go func() {
				_, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "test-token", "gpt-6-astra", "", 5*time.Second)
				result <- err
			}()
			select {
			case err := <-result:
				if model == "gpt-6-astra" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, errCodexTicketModelMismatch)
				}
				require.ErrorIs(t, body.ctx.Err(), context.Canceled)
			case <-time.After(time.Second):
				t.Fatal("模型验证结束后必须先取消请求再关闭流，不能等待上游断开")
			}
		})
	}
}
