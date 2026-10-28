package service

import (
	"encoding/json"
	"strings"

	"github.com/tidwall/gjson"
)

// 仅明确的密文无效拒绝允许恢复；其他 400、鉴权、配额或传输错误均不重放请求。
func isExcelBPSInvalidEncryptedContent(raw []byte) bool {
	if !gjson.ValidBytes(raw) {
		return false
	}
	if code := extractUpstreamErrorCode(raw); code != "" {
		return code == "invalid_encrypted_content"
	}
	// 上游未提供错误码时，只接受固定诊断描述，避免把回显内容中的加密字样误判为此错误。
	message := strings.ToLower(strings.TrimSpace(extractUpstreamErrorMessage(raw)))
	return strings.HasPrefix(message, "the encrypted content ") &&
		strings.Contains(message, "could not be verified") &&
		strings.Contains(message, "could not be decrypted or parsed")
}

// 仅移除不透明推理，保留消息、工具、结果、附件及路由元数据后同路由重试一次。压缩项和加密消息可能是唯一上下文，不能为通过请求而删除。
func prepareExcelBPSInvalidEncryptedRetry(body, rejection []byte) ([]byte, bool) {
	if !isExcelBPSInvalidEncryptedContent(rejection) {
		return body, false
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(body, &request) != nil {
		return body, false
	}
	var input []json.RawMessage
	if json.Unmarshal(request["input"], &input) != nil {
		return body, false
	}
	kept := make([]json.RawMessage, 0, len(input))
	removed := false
	hasHistory := false
	for _, item := range input {
		typeName := gjson.GetBytes(item, "type").String()
		encrypted := gjson.GetBytes(item, "encrypted_content")
		if typeName == "reasoning" && encrypted.Type == gjson.String && encrypted.String() != "" {
			removed = true
			continue
		}
		if encrypted.Exists() || len(gjson.GetBytes(item, "encrypted_function_args").Array()) > 0 {
			return body, false
		}
		for _, field := range []string{"content", "output"} {
			for _, part := range gjson.GetBytes(item, field).Array() {
				if part.Get("type").String() == "encrypted_content" || part.Get("encrypted_content").Exists() {
					return body, false
				}
			}
		}
		// 只有注入指令和压缩触发项不足以恢复用户任务。
		role := gjson.GetBytes(item, "role").String()
		if role == "user" || role == "assistant" || typeName == "agent_message" ||
			typeName == "function_call" || typeName == "function_call_output" {
			hasHistory = true
		}
		kept = append(kept, item)
	}
	if !removed || !hasHistory {
		return body, false
	}
	var err error
	request["input"], err = json.Marshal(kept)
	if err != nil {
		return body, false
	}
	retry, err := json.Marshal(request)
	if err != nil {
		return body, false
	}
	return retry, true
}
