package service

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

// doOpenAIUpstreamWithHealthyTurnState 不改变请求体或客户端请求头，只补试尚未返回响应的请求。
func (s *OpenAIGatewayService) doOpenAIUpstreamWithHealthyTurnState(req *http.Request, proxyURL string, account *Account) (*http.Response, error) {
	attempt := openAIHealthyTurnStateAttemptFromRequest(req)
	response, err := s.doOpenAIUpstreamOnce(req, proxyURL, account)
	if attempt == nil || err != nil || response == nil {
		return response, err
	}
	if req.GetBody != nil && (response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable) {
		retry, retryErr := attempt.claimRetry(req.Context(), response.StatusCode, response.Header, req.Header.Get(openAICodexTurnStateHeader))
		if retryErr != nil {
			if response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, retryErr
		}
		if retry {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				attempt.restore()
				return response, nil
			}
			if !attempt.started(response.StatusCode) {
				_ = body.Close()
				return response, nil
			}
			if response.Body != nil {
				_ = response.Body.Close()
			}
			retryReq := req.Clone(req.Context())
			retryReq.Body = body
			retryReq.Header.Set(openAICodexTurnStateHeader, attempt.borrowed.value)
			response, err = s.doOpenAIUpstreamOnce(retryReq, proxyURL, account)
			attempt.httpStatus = 0
			if response != nil {
				attempt.httpStatus = response.StatusCode
			}
			if err != nil || response == nil || response.StatusCode < 200 || response.StatusCode >= 300 {
				// 交给传输层后无法确认是否已发送，取消也不能当作健康成功归还。
				attempt.failed()
				return response, err
			}
		}
	}
	if response.Body != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		observer := newOpenAIHealthyTurnStateObserver(attempt, response.Header)
		if observer != nil {
			response.Body = newOpenAIHealthyTurnStateBody(response, observer)
		}
	} else if response.Body == nil {
		attempt.failed()
	}
	return response, err
}

// 仅观察原始响应字节，不预读、不缓冲整个响应、不修改 SSE 或 JSON 内容。
type openAIHealthyTurnStateBody struct {
	io.ReadCloser
	mu           sync.Mutex
	observer     *openAIHealthyTurnStateObserver
	sse          bool
	sniffSSE     bool
	formatPrefix []byte
	line, data   []byte
	event        string
	discard      bool
	lineOverflow bool
	closed       bool
}

const openAIHealthyTurnStateEventMaxBytes = 1 << 20

func newOpenAIHealthyTurnStateBody(response *http.Response, observer *openAIHealthyTurnStateObserver) *openAIHealthyTurnStateBody {
	return &openAIHealthyTurnStateBody{
		ReadCloser: response.Body,
		observer:   observer,
		sse:        isEventStreamResponse(response.Header),
		sniffSSE:   strings.TrimSpace(response.Header.Get("Content-Type")) == "",
	}
}

func (b *openAIHealthyTurnStateBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return n, err
	}
	b.observeBytes(p[:n])
	if err != nil {
		if err == io.EOF {
			if b.sse {
				b.consumeLine()
				b.dispatch()
			} else if !b.discard {
				b.observer.observeJSON(b.data)
			}
		}
		b.observer.finish()
	}
	return n, err
}

func (b *openAIHealthyTurnStateBody) observeBytes(chunk []byte) {
	// OAuth 上游可能省略 Content-Type。仅用已读到的短前缀识别格式，
	// 不额外读取上游，也不把 JSON 文本中的 data:/event: 当作 SSE。
	if b.sniffSSE {
		for i, value := range chunk {
			if len(b.formatPrefix) == 0 && (value == ' ' || value == '\t' || value == '\r' || value == '\n') {
				continue
			}
			b.formatPrefix = append(b.formatPrefix, value)
			prefix := b.formatPrefix
			if bodyHasSSEFraming(prefix) || prefix[0] == ':' || bytes.HasPrefix(prefix, []byte("id:")) || bytes.HasPrefix(prefix, []byte("retry:")) {
				b.sse = true
			} else if bytes.HasPrefix([]byte("data:"), prefix) || bytes.HasPrefix([]byte("event:"), prefix) || bytes.HasPrefix([]byte("id:"), prefix) || bytes.HasPrefix([]byte("retry:"), prefix) {
				continue
			}
			b.sniffSSE = false
			b.observeBytes(b.formatPrefix)
			b.formatPrefix = nil
			b.observeBytes(chunk[i+1:])
			return
		}
		return
	}
	if b.sse {
		for _, value := range chunk {
			if value == '\n' {
				b.consumeLine()
				continue
			}
			if len(b.line) >= openAIHealthyTurnStateEventMaxBytes {
				b.lineOverflow = true
			}
			if !b.lineOverflow {
				b.line = append(b.line, value)
			}
		}
	} else if !b.discard {
		if len(b.data)+len(chunk) > openAIHealthyTurnStateEventMaxBytes {
			b.discard = true
			b.data = nil
		} else {
			b.data = append(b.data, chunk...)
		}
	}
}

func (b *openAIHealthyTurnStateBody) consumeLine() {
	line := bytes.TrimSuffix(b.line, []byte{'\r'})
	if b.lineOverflow {
		b.discard = true
		b.data = nil
	} else if len(line) == 0 {
		if !b.discard {
			b.dispatch()
		}
		b.discard = false
		b.data = b.data[:0]
		b.event = ""
	} else if bytes.HasPrefix(line, []byte("data:")) && !b.discard {
		data := bytes.TrimPrefix(line[5:], []byte{' '})
		if len(b.data)+len(data)+1 > openAIHealthyTurnStateEventMaxBytes {
			b.discard = true
			b.data = nil
		} else {
			if len(b.data) > 0 {
				b.data = append(b.data, '\n')
			}
			b.data = append(b.data, data...)
		}
	} else if bytes.HasPrefix(line, []byte("event:")) && !b.discard {
		b.event = strings.TrimSpace(string(line[6:]))
	}
	b.line = b.line[:0]
	b.lineOverflow = false
}

func (b *openAIHealthyTurnStateBody) dispatch() {
	if !b.discard && len(b.data) > 0 {
		b.observer.observe(b.data, b.event)
	}
	b.data = b.data[:0]
	b.event = ""
}

func (b *openAIHealthyTurnStateBody) Close() error {
	// 先关闭底层，允许另一个 goroutine 中阻塞的 Read 及时退出。
	err := b.ReadCloser.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		b.observer.finish()
	}
	return err
}

type openAIHealthyTurnStateObserver struct {
	attempt                             *openAIHealthyTurnStateAttempt
	candidate                           openAIHealthyTurnStateEntry
	healthy, terminal, failed, finished bool
}

func newOpenAIHealthyTurnStateObserver(attempt *openAIHealthyTurnStateAttempt, headers http.Header) *openAIHealthyTurnStateObserver {
	if attempt == nil {
		return nil
	}
	value := extractOpenAICodexTurnState(headers)
	if len(value) > openAIHealthyTurnStateMaxBytes {
		value = ""
	}
	if (!attempt.record || value == "") && attempt.borrowed.value == "" {
		return nil
	}
	return &openAIHealthyTurnStateObserver{attempt: attempt, candidate: openAIHealthyTurnStateEntry{value: value, expiresAt: time.Now().Add(openAIHealthyTurnStateTTL)}}
}

func (o *openAIHealthyTurnStateObserver) observe(payload []byte, event string) {
	if o == nil || o.finished || o.failed {
		return
	}
	if string(bytes.TrimSpace(payload)) == "[DONE]" {
		o.terminal = true
		return
	}
	if !gjson.ValidBytes(payload) {
		return
	}
	if event == "" {
		event = gjson.GetBytes(payload, "type").String()
	}
	status := gjson.GetBytes(payload, "response.status").String()
	if event == "error" || event == "response.failed" || event == "response.incomplete" || status == "failed" || status == "incomplete" || gjson.GetBytes(payload, "response.error").IsObject() {
		o.failed = true
		o.finish()
		return
	}
	if !o.healthy && openAIStreamDataStartsVisibleOutput(string(payload), event) {
		o.healthy = true
		// 等完整响应成功后再持久化，首字不代表调用完成。
	}
	if event == "response.completed" || event == "response.done" {
		o.terminal = true
		o.finish()
	}
}

func (o *openAIHealthyTurnStateObserver) observeJSON(payload []byte) {
	if o == nil || !gjson.ValidBytes(payload) {
		return
	}
	if gjson.GetBytes(payload, "error").IsObject() || gjson.GetBytes(payload, "status").String() != "completed" {
		return
	}
	for _, item := range gjson.GetBytes(payload, "output").Array() {
		if openAIStreamItemHasVisibleOutput(item) {
			o.healthy = true
			break
		}
	}
	o.terminal = true
}

func (o *openAIHealthyTurnStateObserver) finish() {
	if o == nil || o.finished {
		return
	}
	o.finished = true
	if o.healthy && o.terminal && !o.failed {
		o.attempt.completed(true)
		if o.attempt.record {
			o.attempt.cache.store(o.attempt.scope, o.candidate)
		}
		return
	}
	// 首字后的错误、异常 EOF、超时和空成功响应也会淘汰已尝试的记录。
	o.attempt.failed()
	if o.attempt.record && o.healthy {
		o.attempt.cache.reject(o.attempt.scope, o.candidate)
	}
}
