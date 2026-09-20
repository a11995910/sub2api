package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

type openAIHealthyWSFrame struct {
	kind    coderws.MessageType
	payload []byte
}

// 每轮在首个模型声明或有效输出前暂存少量帧，避免将错误模型的 response.created 发给客户端。
type openAIHealthyWSModelGate struct {
	mu        sync.Mutex
	observer  *openAIHealthyTurnStateObserver
	preview   *openAIHealthyTurnStateObserver
	request   []byte
	pending   []openAIHealthyWSFrame
	bytes     int
	ready     bool
	canReplay bool
	retry     func(context.Context, []byte, *openAIHealthyTurnStateAttempt) (http.Header, error)
}

// 观察与转发层相同的完整 JSON 文档，但保留原帧交由原有修复和异常处理逻辑转发。
func (o *openAIHealthyTurnStateObserver) observeWSFrame(payload []byte) {
	if o == nil || o.finished || o.failed {
		return
	}
	if gjson.ValidBytes(payload) {
		o.observe(payload, "")
		return
	}
	documents, repaired := splitOpenAIConcatenatedJSONDocuments(payload)
	if !repaired {
		// 非法帧不能继续等待模型声明，否则会吞掉原帧并阻塞下游异常处理。
		o.failed = true
		o.finish()
		return
	}
	terminalHasTail := false
	for i, document := range documents {
		if o.observeModel(document) {
			return
		}
		if i < len(documents)-1 && isUpstreamResponseModelTerminalEvent(gjson.GetBytes(document, "type").String()) {
			terminalHasTail = true
			break
		}
	}
	if terminalHasTail {
		// 终止事件后仍有文档时，下游会丢弃尾部并销毁连接；不能提前记为健康。
		o.failed = true
		o.finish()
		return
	}
	for _, document := range documents {
		o.observe(document, "")
	}
}

func (g *openAIHealthyWSModelGate) begin(payload []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if gjson.GetBytes(payload, "type").String() != "response.create" {
		g.canReplay = false
		return
	}
	// 并行轮次不可安全重放；保持现有流传输，同时停止替换。
	if g.request != nil && !g.observer.finished {
		g.canReplay = false
		return
	}
	attempt := g.observer.attempt
	if model := firstValidTrimmedGJSONString(payload, "model", "response.model"); model != "" {
		attempt.scope.model = model
	}
	if g.observer.finished {
		attempt.startedAt = time.Now()
		g.observer = newOpenAIHealthyTurnStateObserver(attempt, http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{g.observer.candidate.value}})
	}
	g.request = append([]byte(nil), payload...)
	g.preview = newOpenAIHealthyModelPreview(attempt.scope.model)
	g.pending, g.bytes, g.ready = nil, 0, false
	g.canReplay = gjson.GetBytes(payload, "previous_response_id").String() == "" && gjson.GetBytes(payload, "response.previous_response_id").String() == ""
}

func (g *openAIHealthyWSModelGate) read(ctx context.Context, read func() (coderws.MessageType, []byte, error)) (coderws.MessageType, []byte, error) {
	for {
		g.mu.Lock()
		if g.ready && len(g.pending) > 0 {
			frame := g.pending[0]
			g.pending = g.pending[1:]
			g.observer.observeWSFrame(frame.payload)
			mismatch := g.observer.modelMismatch
			g.mu.Unlock()
			if mismatch {
				return 0, nil, errOpenAIUpstreamModelMismatch
			}
			return frame.kind, frame.payload, nil
		}
		g.mu.Unlock()
		kind, payload, err := read()
		g.mu.Lock()
		if err != nil {
			g.observer.finish()
			g.mu.Unlock()
			return kind, payload, err
		}
		if g.preview == nil || g.ready {
			g.observer.observeWSFrame(payload)
			mismatch := g.observer.modelMismatch
			g.mu.Unlock()
			if mismatch {
				return 0, nil, errOpenAIUpstreamModelMismatch
			}
			return kind, payload, nil
		}
		g.preview.observeWSFrame(payload)
		if g.preview.modelMismatch {
			g.observer.modelMismatch, g.observer.failed = true, true
			g.observer.finish()
			if !g.canReplay || g.retry == nil {
				g.mu.Unlock()
				return 0, nil, errOpenAIUpstreamModelMismatch
			}
			headers, retryErr := g.retry(ctx, g.request, g.observer.attempt)
			if retryErr != nil {
				g.mu.Unlock()
				return 0, nil, retryErr
			}
			g.observer = newOpenAIHealthyTurnStateObserver(g.observer.attempt, headers)
			g.preview = newOpenAIHealthyModelPreview(g.observer.attempt.scope.model)
			g.pending, g.bytes, g.canReplay = nil, 0, false
			g.mu.Unlock()
			continue
		}
		g.pending = append(g.pending, openAIHealthyWSFrame{kind, payload})
		g.bytes += len(payload)
		g.ready = g.preview.modelPreviewDone() || g.bytes >= openAIHealthyTurnStateEventMaxBytes || len(g.pending) >= 256
		g.mu.Unlock()
	}
}

func (g *openAIHealthyWSModelGate) finish() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.observer.finish()
}

func (l *openAIWSConnLease) prepareHealthyModelRequest(value any) {
	if l.healthyModelGate == nil {
		return
	}
	payload, err := json.Marshal(value)
	if err == nil {
		l.healthyModelGate.begin(payload)
	}
}

func (s *OpenAIGatewayService) prepareHealthyWSLeaseGate(lease *openAIWSConnLease, req openAIWSAcquireRequest) {
	if lease.healthyTurnState == nil {
		return
	}
	gate := &openAIHealthyWSModelGate{observer: lease.healthyTurnState}
	lease.healthyModelGate = gate
	if req.ForcePreferredConn || req.SkipHealthyPreflight {
		return
	}
	gate.retry = func(ctx context.Context, payload []byte, attempt *openAIHealthyTurnStateAttempt) (http.Header, error) {
		attempt.rejectModelMismatch(lease.HandshakeHeaders(), req.Headers.Get(openAICodexTurnStateHeader))
		ok, err := attempt.claimModelMismatchRetry(ctx, lease.HandshakeHeaders(), req.Headers.Get(openAICodexTurnStateHeader))
		if err != nil {
			return nil, err
		}
		if !ok || !attempt.started(http.StatusSwitchingProtocols) {
			return nil, errOpenAIUpstreamModelMismatch
		}
		replacement := cloneOpenAIWSAcquireRequest(req)
		replacement.Headers.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
		replacement.PreferredConnID, replacement.ForceNewConn = "", true
		lease.MarkBroken()
		next, err := s.getOpenAIWSConnPool().Acquire(ctx, replacement)
		if err != nil {
			attempt.failed()
			return nil, err
		}
		lease.conn.release()
		lease.conn, lease.reused = next.conn, false
		next.released.Store(true)
		attempt.httpStatus = http.StatusSwitchingProtocols
		if err = lease.conn.writeJSON(json.RawMessage(payload), ctx); err != nil {
			attempt.failed()
			return nil, err
		}
		return lease.HandshakeHeaders(), nil
	}
}

func (s *OpenAIGatewayService) prepareHealthyWSFrameGate(conn *openAIHealthyTurnStateFrameConn, account *Account, wsURL string, headers http.Header, proxyURL string, dialer openAIWSClientDialer) {
	gate := &openAIHealthyWSModelGate{observer: conn.observer}
	conn.gate = gate
	gate.retry = func(ctx context.Context, payload []byte, attempt *openAIHealthyTurnStateAttempt) (http.Header, error) {
		attempt.rejectModelMismatch(nil, headers.Get(openAICodexTurnStateHeader))
		ok, err := attempt.claimModelMismatchRetry(ctx, nil, headers.Get(openAICodexTurnStateHeader))
		if err != nil {
			return nil, err
		}
		if !ok || !attempt.started(http.StatusSwitchingProtocols) {
			return nil, errOpenAIUpstreamModelMismatch
		}
		retryHeaders := headers.Clone()
		retryHeaders.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
		retryHeaders, err = s.refreshOpenAIAgentIdentityHeaders(ctx, account, retryHeaders)
		if err != nil {
			attempt.failed()
			return nil, err
		}
		dialCtx, cancel := context.WithTimeout(ctx, s.openAIWSDialTimeout())
		defer cancel()
		next, status, responseHeaders, err := dialer.Dial(dialCtx, wsURL, retryHeaders, proxyURL)
		attempt.httpStatus = status
		if err != nil || next == nil {
			if next != nil {
				_ = next.Close()
			}
			attempt.failed()
			if err == nil {
				err = errOpenAIUpstreamModelMismatch
			}
			return nil, err
		}
		nextFrame, ok := next.(openaiwsv2.FrameConn)
		if !ok {
			_ = next.Close()
			attempt.failed()
			return nil, errOpenAIUpstreamModelMismatch
		}
		conn.mu.Lock()
		_ = conn.FrameConn.Close()
		conn.FrameConn = nextFrame
		err = nextFrame.WriteFrame(ctx, coderws.MessageText, payload)
		conn.mu.Unlock()
		if err != nil {
			attempt.failed()
			return nil, err
		}
		return responseHeaders, nil
	}
}
