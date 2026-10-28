package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// 仅为未发送的休息切号保留当前轮上下文。历史不完整、跨连接响应 ID、
// 不完整终态或超过容量上限时拒绝自动重放，由客户端携带完整历史重连。
type accountRestWSReplay struct {
	mu                sync.Mutex
	payload           []byte
	input             []json.RawMessage
	complete          bool
	responseID        string
	bindingResponseID string
	model             string
	collector         openAIWSToolCallReplayCollector
	disabled          bool
}

const accountRestWSReplayMaxBytes = 8 << 20

func (r *accountRestWSReplay) begin(payload []byte, model string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	previous := strings.TrimSpace(gjson.GetBytes(payload, "previous_response_id").String())
	r.bindingResponseID = previous
	chainKnown := previous == "" || (r.complete && previous == r.responseID && r.responseID != "")
	items, exists, err := buildOpenAIWSReplayInputSequence(r.input, r.complete, payload, previous != "")
	r.payload = append([]byte(nil), payload...)
	r.input, r.complete = items, chainKnown && exists && err == nil && !r.disabled
	r.model = model
	r.responseID = ""
	r.collector = openAIWSToolCallReplayCollector{}
	r.boundLocked()
}

func (r *accountRestWSReplay) boundLocked() {
	size := len(r.payload)
	for _, item := range r.input {
		size += len(item)
	}
	for _, item := range r.collector.allItems {
		size += len(item)
	}
	if size > accountRestWSReplayMaxBytes {
		r.input, r.payload, r.complete = nil, nil, false
		r.collector = openAIWSToolCallReplayCollector{}
		r.disabled = true
	}
}

func (r *accountRestWSReplay) observe(payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.disabled {
		return
	}
	event := gjson.GetBytes(payload, "type").String()
	r.collector.AddEvent(event, payload)
	if openAIWSPassthroughIsTerminalOutput(payload) {
		// 仅成功终态的完整 output 可作为迁移历史；流中只有 delta 时不能猜测完整答案。
		success := (event == "response.completed" || event == "response.done") && gjson.GetBytes(payload, "response.status").String() != "incomplete" && gjson.GetBytes(payload, "response.status").String() != "failed"
		r.complete = r.complete && success && gjson.GetBytes(payload, "response.output").IsArray()
		r.input = combineOpenAIWSReplayItems(r.input, r.collector.AllItems())
		r.responseID = gjson.GetBytes(payload, "response.id").String()
		r.collector = openAIWSToolCallReplayCollector{}
	}
	r.boundLocked()
}

func (r *accountRestWSReplay) invalidate() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled, r.complete = true, false
	r.input, r.payload = nil, nil
	r.collector = openAIWSToolCallReplayCollector{}
}

func (r *accountRestWSReplay) wrap(err error) error {
	var payload []byte
	if IsAccountRestError(err) {
		r.mu.Lock()
		candidate, safe, buildErr := buildOpenAIWSCurrentTurnRetryPayload(r.payload, r.input, r.complete, r.model)
		if buildErr == nil && safe {
			payload = candidate
		}
		r.mu.Unlock()
	}
	return newOpenAIWSCurrentTurnFailoverError(err, payload)
}

func (r *accountRestWSReplay) clearBinding(ctx context.Context, s *OpenAIGatewayService, c *gin.Context, err error) {
	if !IsAccountRestError(err) {
		return
	}
	r.mu.Lock()
	payload := append([]byte(nil), r.payload...)
	responseID := r.bindingResponseID
	r.mu.Unlock()
	if c != nil && c.Request != nil {
		c.Request.Header.Del(openAIWSTurnStateHeader)
	}
	groupID := getOpenAIGroupIDFromContext(c)
	s.ClearRestingAccountBinding(ctx, &groupID, s.GenerateSessionHash(c, payload), responseID, err)
	if scope, _ := resolveOpenAIWSExecutionScope(c, payload, getAPIKeyIDFromContext(c)); scope != "" {
		s.ClearRestingAccountBinding(ctx, &groupID, scope, "", err)
	}
}
