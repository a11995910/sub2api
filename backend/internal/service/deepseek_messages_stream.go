package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
)

// 多个上游模型轮合成为一个 Messages 响应；内容 delta 到达即刷出，不缓存最终文本伪装流式。
type deepSeekFetchWriter struct {
	c                               *gin.Context
	stream                          bool
	id, model                       string
	start                           time.Time
	result                          *OpenAIForwardResult
	started, disconnected, finished bool
	content                         []any
	nextIndex                       int
	searchCalls, searchAttempts     int
}

func (w *deepSeekFetchWriter) emit(kind string, event any) error {
	if !w.stream {
		return nil
	}
	if err := flushSSEJSON(w.c.Writer, kind, event); err != nil {
		w.disconnected = true
		return errors.New("客户端流已断开")
	}
	return nil
}

func (w *deepSeekFetchWriter) begin() error {
	if w.started {
		return nil
	}
	w.started = true
	if !w.stream {
		return nil
	}
	setSSEHeaders(w.c)
	return w.emit("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": w.id, "type": "message", "role": "assistant", "model": w.model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]int{"input_tokens": 0, "output_tokens": 0}}})
}

func (w *deepSeekFetchWriter) clientBlock(block map[string]any) map[string]any {
	output := cloneDeepSeekBlock(block)
	if output["type"] == "tool_use" && output["name"] == "web_fetch" {
		output["type"] = "server_tool_use"
		output["caller"] = map[string]any{"type": "direct"}
	}
	return output
}

func (w *deepSeekFetchWriter) observeBlock(block map[string]any) {
	if block["type"] == "server_tool_use" && block["name"] == "web_search" {
		w.searchAttempts++
	}
	if block["type"] == "web_search_tool_result" {
		if results, ok := block["content"].([]any); ok && len(results) > 0 {
			w.searchCalls++
		}
	}
}

func (w *deepSeekFetchWriter) block(block map[string]any) error {
	if err := w.begin(); err != nil {
		return err
	}
	output := w.clientBlock(block)
	w.content = append(w.content, output)
	w.observeBlock(block)
	index := w.nextIndex
	w.nextIndex++
	if err := w.emit("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": output}); err != nil {
		return err
	}
	return w.emit("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
}

func (w *deepSeekFetchWriter) finish(reason string, sequence any, usage *ClaudeUsage, fetches int) error {
	if reason == "" {
		return errors.New("上游缺少 stop_reason")
	}
	if err := w.begin(); err != nil {
		return err
	}
	counts := map[string]any{"input_tokens": usage.InputTokens, "output_tokens": usage.OutputTokens,
		"cache_read_input_tokens": usage.CacheReadInputTokens, "cache_creation_input_tokens": usage.CacheCreationInputTokens,
		"server_tool_use": map[string]int{"web_fetch_requests": fetches, "web_search_requests": w.searchCalls}}
	if w.stream {
		if err := w.emit("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": sequence}, "usage": counts}); err != nil {
			return err
		}
		if err := w.emit("message_stop", map[string]any{"type": "message_stop"}); err != nil {
			return err
		}
	} else {
		w.c.JSON(http.StatusOK, map[string]any{"id": w.id, "type": "message", "role": "assistant", "model": w.model, "content": w.content, "stop_reason": reason, "stop_sequence": sequence, "usage": counts})
	}
	w.finished = true
	return nil
}

func (w *deepSeekFetchWriter) fail(message string) {
	if w.finished || w.disconnected || w.c.Request.Context().Err() != nil {
		return
	}
	if w.stream && w.started {
		_ = w.emit("error", map[string]any{"type": "error", "error": map[string]string{"type": "api_error", "message": message}})
	} else {
		writeAnthropicError(w.c, http.StatusBadGateway, "api_error", message)
	}
}

func readDeepSeekFetchRound(ctx context.Context, resp *http.Response, writer *deepSeekFetchWriter, cfg *config.Config, observer *upstreamResponseModelObserver) (map[string]any, ClaudeUsage, error) {
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return readDeepSeekFetchStream(ctx, resp, writer, cfg, observer)
	}
	var usage ClaudeUsage
	data, err := io.ReadAll(io.LimitReader(resp.Body, deepSeekFetchMaxConversationBytes+1))
	if err != nil {
		return nil, usage, errors.New("无法读取模型响应")
	}
	if len(data) > deepSeekFetchMaxConversationBytes {
		return nil, usage, errors.New("模型响应超过大小限制")
	}
	var message map[string]any
	if json.Unmarshal(data, &message) != nil || message["type"] != "message" {
		return nil, usage, errors.New("模型响应不是有效 Messages 对象")
	}
	if parsed := parseClaudeUsageFromResponseBody(data); parsed != nil {
		usage = *parsed
	}
	if observer != nil {
		observer.ObserveAnthropic(data)
	}
	content, ok := message["content"].([]any)
	if !ok {
		return nil, usage, errors.New("模型响应缺少 content")
	}
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok {
			return nil, usage, errors.New("模型内容块格式无效")
		}
		if err := writer.block(block); err != nil {
			return message, usage, err
		}
	}
	return message, usage, nil
}

type deepSeekStreamBlock struct {
	value  map[string]any
	input  strings.Builder
	fields map[string]*strings.Builder
	index  int
	closed bool
}

func readDeepSeekFetchStream(ctx context.Context, resp *http.Response, writer *deepSeekFetchWriter, cfg *config.Config, observer *upstreamResponseModelObserver) (map[string]any, ClaudeUsage, error) {
	message := map[string]any{"content": []any{}}
	usage := ClaudeUsage{}
	var blocks []*deepSeekStreamBlock
	started, terminal := false, false
	process := func(raw []byte) error {
		if len(raw) == 0 {
			return nil
		}
		if observer != nil {
			observer.ObserveAnthropic(raw)
		}
		var event map[string]any
		if json.Unmarshal(raw, &event) != nil {
			return errors.New("模型 SSE 事件不是有效 JSON")
		}
		parseSSEUsagePassthrough(string(raw), &usage)
		kind, _ := event["type"].(string)
		switch kind {
		case "message_start":
			if started {
				return errors.New("模型重复开始消息")
			}
			started = true
			if msg, ok := event["message"].(map[string]any); ok {
				if initial, ok := msg["usage"].(map[string]any); ok {
					if output, ok := initial["output_tokens"].(float64); ok {
						usage.OutputTokens = int(output)
					}
				}
			}
			return writer.begin()
		case "content_block_start":
			index, ok := event["index"].(float64)
			if !started || !ok || index != float64(len(blocks)) {
				return errors.New("模型内容块顺序无效")
			}
			if len(blocks) > 0 && !blocks[len(blocks)-1].closed {
				return errors.New("模型内容块交错，无法安全还原")
			}
			value, ok := event["content_block"].(map[string]any)
			if !ok {
				return errors.New("模型内容块格式无效")
			}
			block := &deepSeekStreamBlock{value: value, index: writer.nextIndex}
			writer.nextIndex++
			blocks = append(blocks, block)
			event["index"] = block.index
			event["content_block"] = writer.clientBlock(value)
			writer.observeBlock(value)
			return writer.emit(kind, event)
		case "content_block_delta", "content_block_stop":
			index, ok := event["index"].(float64)
			if !ok || index < 0 || int(index) >= len(blocks) || index != float64(int(index)) {
				return errors.New("模型内容块索引无效")
			}
			block := blocks[int(index)]
			if block.closed {
				return errors.New("模型内容块已结束")
			}
			event["index"] = block.index
			if kind == "content_block_stop" {
				block.closed = true
				for key, buffer := range block.fields {
					block.value[key] = buffer.String()
				}
				if block.input.Len() > 0 {
					var input map[string]any
					if json.Unmarshal([]byte(block.input.String()), &input) != nil {
						return errors.New("模型工具参数不是有效 JSON")
					}
					block.value["input"] = input
				}
				writer.content = append(writer.content, writer.clientBlock(block.value))
			} else {
				delta, ok := event["delta"].(map[string]any)
				if !ok {
					return errors.New("模型内容增量格式无效")
				}
				field := ""
				switch delta["type"] {
				case "input_json_delta":
					value, _ := delta["partial_json"].(string)
					block.input.WriteString(value)
				case "text_delta":
					field = "text"
				case "thinking_delta":
					field = "thinking"
				case "signature_delta":
					field = "signature"
				case "citations_delta":
					citations, _ := block.value["citations"].([]any)
					block.value["citations"] = append(citations, delta["citation"])
				default:
					return errors.New("不支持的模型内容增量")
				}
				if field != "" {
					value, _ := delta[field].(string)
					if block.fields == nil {
						block.fields = make(map[string]*strings.Builder)
					}
					buffer := block.fields[field]
					if buffer == nil {
						buffer = &strings.Builder{}
						previous, _ := block.value[field].(string)
						buffer.WriteString(previous)
						block.fields[field] = buffer
					}
					buffer.WriteString(value)
					if writer.result.FirstTokenMs == nil && value != "" && field != "signature" {
						ms := int(time.Since(writer.start).Milliseconds())
						writer.result.FirstTokenMs = &ms
					}
				}
			}
			return writer.emit(kind, event)
		case "message_delta":
			delta, _ := event["delta"].(map[string]any)
			if reason, ok := delta["stop_reason"].(string); ok {
				message["stop_reason"] = reason
			}
			message["stop_sequence"] = delta["stop_sequence"]
		case "message_stop":
			if !started || message["stop_reason"] == nil {
				return errors.New("模型流缺少结束原因")
			}
			content := make([]any, 0, len(blocks))
			for _, block := range blocks {
				if !block.closed {
					return errors.New("模型流有未完成内容块")
				}
				content = append(content, block.value)
			}
			message["content"] = content
			terminal = true
		case "ping":
			return writer.emit("ping", map[string]string{"type": "ping"})
		case "error":
			return errors.New("上游模型流返回错误")
		default:
			return errors.New("不支持的模型流事件")
		}
		return nil
	}
	// 读协程只传完整 SSE 事件；退出时 done + Body.Close 共同释放阻塞读。
	type eventRead struct {
		data []byte
		err  error
	}
	events := make(chan eventRead, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(events)
		send := func(event eventRead) bool {
			select {
			case events <- event:
				return true
			case <-done:
				return false
			}
		}
		scanner := bufio.NewScanner(resp.Body)
		maxLine := defaultMaxLineSize
		if cfg != nil && cfg.Gateway.MaxLineSize > 0 {
			maxLine = cfg.Gateway.MaxLineSize
		}
		scanner.Buffer(make([]byte, 4096), maxLine)
		var data strings.Builder
		total := 0
		for scanner.Scan() {
			line := scanner.Text()
			total += len(line)
			if total > deepSeekFetchMaxConversationBytes {
				send(eventRead{err: errors.New("模型流超过大小限制")})
				return
			}
			if line == "" {
				if data.Len() > 0 {
					if !send(eventRead{data: []byte(strings.TrimSuffix(data.String(), "\n"))}) {
						return
					}
					data.Reset()
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
				data.WriteByte('\n')
			}
		}
		if data.Len() > 0 {
			if !send(eventRead{data: []byte(strings.TrimSuffix(data.String(), "\n"))}) {
				return
			}
		}
		if err := scanner.Err(); err != nil {
			send(eventRead{err: errors.New("模型流读取中断")})
		}
	}()
	defer resp.Body.Close()
	idle := 60 * time.Second
	if cfg != nil && cfg.Gateway.StreamDataIntervalTimeout > 0 {
		idle = time.Duration(cfg.Gateway.StreamDataIntervalTimeout) * time.Second
	}
	timer := time.NewTimer(idle)
	defer timer.Stop()
	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return message, usage, ctx.Err()
		case <-timer.C:
			return message, usage, errors.New("模型流等待超时")
		case <-ping.C:
			if writer.started {
				if err := writer.emit("ping", map[string]string{"type": "ping"}); err != nil {
					return message, usage, err
				}
			}
		case event, ok := <-events:
			if !ok {
				return message, usage, errors.New("模型流缺少 message_stop")
			}
			if event.err != nil {
				return message, usage, event.err
			}
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(idle)
			if err := process(event.data); err != nil {
				return message, usage, err
			}
			if terminal {
				return message, usage, nil
			}
		}
	}
}
