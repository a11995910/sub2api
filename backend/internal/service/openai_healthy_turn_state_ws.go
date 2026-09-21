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
	req.Headers = req.Headers.Clone()
	if req.Headers == nil {
		req.Headers = make(http.Header)
	}
	// 账号模式开启时，新握手不沿用上一模型或失效门票遗留的状态头。
	if isOpenAICodexTicketAccount(req.Account) && s.openAICodexTicketEnabledContext(ctx) {
		req.Headers.Del(openAICodexTurnStateHeader)
	}
	ticket, ticketErr := s.bindOpenAICodexTicket(ctx, req.Account, model, req.Headers)
	if ticketErr != nil {
		return nil, ticketErr
	}
	if ticket != nil {
		req.ProxyURL = ticket.ProxyURL
		req.codexTicket = ticket
		req.codexTicketUsable = func() bool { return s.openAICodexTicketBindingUsable(req.Account, ticket) }
		req.observeCodexTicket = s.observeOpenAICodexTicketBinding(req.Account, ticket)
	}

	attempt := s.newOpenAIHealthyTurnStateAttempt(c, req.Account, model, "ws:"+req.WSURL, req.ProxyURL, req.Headers)
	if attempt != nil {
		attempt.markStarted()
	}
	// 依赖原连接的续链不改握手头，也不为首发策略强制重连。
	if attempt != nil && !req.ForcePreferredConn && !req.SkipHealthyPreflight {
		req.Headers = req.Headers.Clone()
		if req.Headers == nil {
			req.Headers = make(http.Header)
		}
		used, preflightErr := attempt.claimPreflight(ctx, req.Headers)
		if preflightErr != nil {
			return nil, preflightErr
		}
		if used {
			req.PreferredConnID, req.ForceNewConn = "", true
		}
	}
	lease, err := s.getOpenAIWSConnPool().Acquire(ctx, req)
	if attempt != nil && attempt.sent {
		attempt.httpStatus = http.StatusSwitchingProtocols
		if err != nil || lease == nil {
			attempt.httpStatus = 0
			var failedDial *openAIWSDialError
			if errors.As(err, &failedDial) {
				attempt.httpStatus = failedDial.StatusCode
			}
			attempt.failed()
		}
	}
	var dialErr *openAIWSDialError
	// 严格续链依赖原连接，不能为了替换请求头破坏 previous_response_id 的归属。
	if attempt != nil && !req.ForcePreferredConn && !req.SkipHealthyPreflight && errors.As(err, &dialErr) {
		retry, retryErr := attempt.claimRetry(ctx, dialErr.StatusCode, dialErr.ResponseHeaders, req.Headers.Get(openAICodexTurnStateHeader))
		if retryErr != nil {
			return nil, retryErr
		}
		if retry {
			if !attempt.started(dialErr.StatusCode) {
				return lease, err
			}
			req.Headers = req.Headers.Clone()
			req.Headers.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
			req.PreferredConnID = ""
			req.ForceNewConn = true
			lease, err = s.getOpenAIWSConnPool().Acquire(ctx, req)
			attempt.httpStatus = http.StatusSwitchingProtocols
			if err != nil {
				attempt.httpStatus = 0
				var retryDialErr *openAIWSDialError
				if errors.As(err, &retryDialErr) {
					attempt.httpStatus = retryDialErr.StatusCode
				}
				attempt.failed()
			}
		}
	}
	if err == nil && lease != nil {
		lease.healthyTurnState = newOpenAIHealthyTurnStateObserver(attempt, lease.HandshakeHeaders())
		s.prepareHealthyWSLeaseGate(lease, req)
	}
	return lease, err
}

func (s *OpenAIGatewayService) dialOpenAIWSWithHealthyTurnState(ctx context.Context, c *gin.Context, account *Account, model, wsURL string, headers http.Header, proxyURL string, dialer openAIWSClientDialer) (openAIWSClientConn, int, http.Header, *openAIHealthyTurnStateObserver, error) {
	attempt := s.newOpenAIHealthyTurnStateAttempt(c, account, model, "ws:"+wsURL, proxyURL, headers)
	if attempt != nil {
		attempt.markStarted()
	}
	headers = headers.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	if _, err := attempt.claimPreflight(ctx, headers); err != nil {
		return nil, 0, nil, nil, err
	}
	conn, status, responseHeaders, err := dialer.Dial(ctx, wsURL, headers, proxyURL)
	if attempt != nil && attempt.sent {
		attempt.httpStatus = status
		if err != nil || conn == nil {
			attempt.failed()
		}
	}
	if err != nil && attempt != nil {
		retry, retryErr := attempt.claimRetry(ctx, status, responseHeaders, headers.Get(openAICodexTurnStateHeader))
		if retryErr != nil {
			return nil, status, responseHeaders, nil, retryErr
		}
		if retry {
			if !attempt.started(status) {
				return conn, status, responseHeaders, nil, err
			}
			if conn != nil {
				_ = conn.Close()
			}
			headers = headers.Clone()
			headers.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
			conn, status, responseHeaders, err = dialer.Dial(ctx, wsURL, headers, proxyURL)
			attempt.httpStatus = status
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
	gate     *openAIHealthyWSModelGate
}

func (c *openAIHealthyTurnStateFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	if c.gate != nil {
		return c.gate.read(ctx, func() (coderws.MessageType, []byte, error) {
			c.mu.Lock()
			conn := c.FrameConn
			c.mu.Unlock()
			return conn.ReadFrame(ctx)
		})
	}
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

func (c *openAIHealthyTurnStateFrameConn) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if c.gate != nil {
		c.gate.begin(payload)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.FrameConn.WriteFrame(ctx, kind, payload)
}

func (c *openAIHealthyTurnStateFrameConn) Close() error {
	c.mu.Lock()
	err := c.FrameConn.Close()
	c.mu.Unlock()
	c.finishObservation()
	return err
}

func (c *openAIHealthyTurnStateFrameConn) finishObservation() {
	if c.gate != nil {
		c.gate.finish()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.observer.finish()
}
