package service

import (
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// sanitizeOpenAIResponsesInputItemIDs 删除 Responses input 中与 item 类型契约冲突的 id。
//
// Codex 会回放历史输出；部分兼容上游曾给 message/function_call 返回 item_* id。
// OpenAI Responses 上游要求 message id 以 msg 开头、call-input id 以 fc 开头，
// 但这两类 id 都是可选字段。删除非法 id 比伪造前缀安全，并保留 call_id 配对、
// output item id、消息内容和其他历史上下文字段。
func sanitizeOpenAIResponsesInputItemIDs(body []byte) ([]byte, bool, error) {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body, false, nil
	}

	paths := make([]string, 0)
	for index, item := range input.Array() {
		typ := strings.TrimSpace(item.Get("type").String())
		id := item.Get("id")
		if id.Type != gjson.String || id.String() == "" {
			continue
		}

		invalidMessageID := typ == "message" && !strings.HasPrefix(id.String(), "msg")
		invalidCallInputID := isCodexToolCallInputType(typ) && !strings.HasPrefix(id.String(), "fc")
		if invalidMessageID || invalidCallInputID {
			paths = append(paths, fmt.Sprintf("input.%d.id", index))
		}
	}
	if len(paths) == 0 {
		return body, false, nil
	}

	normalized := body
	for _, path := range paths {
		var err error
		normalized, err = sjson.DeleteBytes(normalized, path)
		if err != nil {
			return body, false, fmt.Errorf("sanitize Responses input item id %s: %w", path, err)
		}
	}
	return normalized, true, nil
}
