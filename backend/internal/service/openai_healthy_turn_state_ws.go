package service

import (
	"context"
	"errors"
	"net/http"
	"sync"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func (s *OpenAIGatewayService) acquireOpenAIWSWithHealthyTurnState(ctx context.Context, c *gin.Context, req openAIWSAcquireRequest, model string) (*openAIWSConnLease, error) {
	attempt := s.newOpenAIHealthyTurnStateAttempt(c, req.Account, model, "ws:"+req.WSURL, req.ProxyURL, req.Headers)
	lease, err := s.getOpenAIWSConnPool().Acquire(ctx, req)
	var dialErr *openAIWSDialError
	// 严格续链依赖原连接，不能为了替换请求头破坏 previous_response_id 的归属。
	if attempt != nil && !req.ForcePreferredConn && errors.As(err, &dialErr) {
		retry, retryErr := attempt.claimRetry(ctx, dialErr.StatusCode, dialErr.ResponseHeaders, req.Headers.Get(openAICodexTurnStateHeader))
		if retryErr != nil {
			return nil, retryErr
		}
		if retry {
			req.Headers = req.Headers.Clone()
			req.Headers.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
			req.PreferredConnID = ""
			req.ForceNewConn = true
			lease, err = s.getOpenAIWSConnPool().Acquire(ctx, req)
			if err != nil {
				attempt.failed()
			}
		}
	}
	if err == nil && lease != nil {
		lease.healthyTurnState = newOpenAIHealthyTurnStateObserver(attempt, lease.HandshakeHeaders())
	}
	return lease, err
}

func (s *OpenAIGatewayService) dialOpenAIWSWithHealthyTurnState(ctx context.Context, c *gin.Context, account *Account, model, wsURL string, headers http.Header, proxyURL string, dialer openAIWSClientDialer) (openAIWSClientConn, int, http.Header, *openAIHealthyTurnStateObserver, error) {
	attempt := s.newOpenAIHealthyTurnStateAttempt(c, account, model, "ws:"+wsURL, proxyURL, headers)
	conn, status, responseHeaders, err := dialer.Dial(ctx, wsURL, headers, proxyURL)
	if err != nil && attempt != nil {
		retry, retryErr := attempt.claimRetry(ctx, status, responseHeaders, headers.Get(openAICodexTurnStateHeader))
		if retryErr != nil {
			return nil, status, responseHeaders, nil, retryErr
		}
		if retry {
			if conn != nil {
				_ = conn.Close()
			}
			headers = headers.Clone()
			headers.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
			conn, status, responseHeaders, err = dialer.Dial(ctx, wsURL, headers, proxyURL)
			if err != nil || conn == nil {
				attempt.failed()
			}
		}
	}
	var observer *openAIHealthyTurnStateObserver
	if err == nil && conn != nil {
		observer = newOpenAIHealthyTurnStateObserver(attempt, responseHeaders)
	}
	return conn, status, responseHeaders, observer, err
}

func (l *openAIWSConnLease) observeHealthyTurnState(payload []byte, err error) {
	l.healthyTurnStateMu.Lock()
	defer l.healthyTurnStateMu.Unlock()
	if l.healthyTurnState == nil {
		return
	}
	if len(payload) > 0 {
		l.healthyTurnState.observe(payload, "")
	}
	if err != nil {
		l.healthyTurnState.finish()
	}
}

type openAIHealthyTurnStateFrameConn struct {
	openaiwsv2.FrameConn
	mu       sync.Mutex
	observer *openAIHealthyTurnStateObserver
}

func (c *openAIHealthyTurnStateFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	kind, payload, err := c.FrameConn.ReadFrame(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.observer != nil {
		if kind == coderws.MessageText || kind == coderws.MessageBinary {
			c.observer.observe(payload, "")
		}
		if err != nil {
			c.observer.finish()
		}
	}
	return kind, payload, err
}

func (c *openAIHealthyTurnStateFrameConn) Close() error {
	err := c.FrameConn.Close()
	c.finishObservation()
	return err
}

func (c *openAIHealthyTurnStateFrameConn) finishObservation() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observer.finish()
}
