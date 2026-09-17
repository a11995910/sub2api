package service

import "strings"

// canonicalizeOpenAIRequestIntegrity 仅归一化已知等价表示，不运行转发流水线。
// 身份元数据不参与摘要，工具正文、调用关联和推理内容仍保留用于观察。
func canonicalizeOpenAIRequestIntegrity(body map[string]any, account *Account) {
	if account == nil || !account.UsesOpenAICodexProtocol() {
		canonicalizeOpenAIIntegrityMessages(body)
		return
	}
	// Codex 订阅端点不接受这两个参数；API Key 路径继续检查它们。
	delete(body, "max_output_tokens")
	delete(body, "max_completion_tokens")
	if model, ok := body["model"].(string); ok {
		body["model"] = strings.TrimSpace(model)
	}
	if reasoning, ok := body["reasoning"].(map[string]any); ok {
		if reasoning["effort"] == "minimal" {
			reasoning["effort"] = "none"
		}
		model, _ := body["model"].(string)
		mode, _ := reasoning["mode"].(string)
		effort, _ := reasoning["effort"].(string)
		if !isOpenAIGPT6AstraModel(model) && strings.EqualFold(strings.TrimSpace(mode), "pro") && strings.TrimSpace(effort) == "" {
			reasoning["effort"] = "max"
			delete(reasoning, "mode")
		}
	}
	if text, ok := body["input"].(string); ok {
		body["input"] = []any{}
		if strings.TrimSpace(text) != "" {
			body["input"] = []any{map[string]any{"type": "message", "role": "user", "content": text}}
		}
	}
	omitSystem := true
	if text, ok := body["text"].(map[string]any); ok {
		if format, ok := text["format"].(map[string]any); ok && format["type"] == "json_object" {
			omitSystem = false
		}
	}
	extractSystemMessagesFromInput(body, omitSystem)
	if instructions, ok := body["instructions"].(string); body["instructions"] == nil || (ok && strings.TrimSpace(instructions) == "") {
		delete(body, "instructions")
	}
	_, hasTools := body["tools"]
	if functions, ok := body["functions"].([]any); ok && !hasTools {
		tools := make([]any, 0, len(functions))
		for _, function := range functions {
			tools = append(tools, map[string]any{"type": "function", "function": function})
		}
		body["tools"] = tools
		delete(body, "functions")
	}
	_, hasChoice := body["tool_choice"]
	if choice, exists := body["function_call"]; exists && !hasChoice {
		switch c := choice.(type) {
		case string:
			body["tool_choice"] = c
			delete(body, "function_call")
		case map[string]any:
			if name, ok := c["name"].(string); ok && name != "" {
				body["tool_choice"] = map[string]any{"type": "function", "name": name}
				delete(body, "function_call")
			}
		}
	}
	if tools, ok := body["tools"].([]any); ok {
		for _, raw := range tools {
			tool, ok := raw.(map[string]any)
			if !ok || tool["type"] != "function" {
				continue
			}
			if function, ok := tool["function"].(map[string]any); ok {
				for key, value := range function {
					if _, exists := tool[key]; !exists {
						tool[key] = value
					}
				}
				delete(tool, "function")
			}
		}
	}
	if choice, ok := body["tool_choice"].(map[string]any); ok && choice["type"] == "function" {
		if function, ok := choice["function"].(map[string]any); ok {
			if _, exists := choice["name"]; !exists {
				choice["name"] = function["name"]
			}
			delete(choice, "function")
		}
	}
	_, _, _ = aliasOpenAIOAuthReservedToolNames(body)
	input, ok := body["input"].([]any)
	if !ok {
		return
	}
	referenceIDs := codexItemReferenceIDMappings(input, false)
	itemIDs := codexInputItemIDs(input)
	callIDs := codexInputCallIDs(input)
	for i, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Codex 兼容层会移除输入项的内部消息元数据，不影响正文与工具参数。
		delete(item, "internal_chat_message_metadata_passthrough")
		if item["role"] == "tool" && strings.TrimSpace(firstNonEmptyString(item["call_id"], item["tool_call_id"], item["id"])) != "" {
			if _, lossless := extractLosslessTextFromContent(item["content"]); lossless {
				if result, changed := normalizeCodexToolRoleMessages([]any{item}); changed {
					item = result[0].(map[string]any)
					input[i] = item
				}
			}
		}
		typ, _ := item["type"].(string)
		if typ == "" && (item["role"] == "user" || item["role"] == "assistant" || item["role"] == "developer") {
			typ = "message"
			item["type"] = typ
		}
		switch typ {
		case "message":
			delete(item, "id")
			if text, ok := item["content"].(string); ok {
				item["content"] = []any{map[string]any{"type": "input_text", "text": text}}
			}
			if parts, ok := item["content"].([]any); ok {
				for _, rawPart := range parts {
					if part, ok := rawPart.(map[string]any); ok && (part["type"] == "text" || part["type"] == "output_text") {
						part["type"] = "input_text"
					}
				}
			}
		case "reasoning":
			delete(item, "id")
			delete(item, "call_id")
			if item["summary"] == nil {
				item["summary"] = []any{}
			}
		case "compaction_summary":
			delete(item, "id")
		case "item_reference":
			id, _ := item["id"].(string)
			id = strings.TrimSpace(id)
			if _, existing := itemIDs[id]; !existing && strings.HasPrefix(id, "call_") {
				if mapped, exists := referenceIDs[id]; exists {
					item["id"] = mapped
				} else if _, sameTurnCall := callIDs[id]; !sameTurnCall {
					item["id"] = normalizeCodexCallID(id)
				}
			}
		}
		if isCodexToolCallItemType(typ) {
			id := firstNonEmptyString(item["call_id"], item["id"])
			if id != "" {
				item["call_id"] = normalizeCodexCallIDForItemType(typ, id)
			}
			delete(item, "id")
		}
	}
}

// Responses 的字符串输入与单条 user 消息等价；仅统一文本载体，不丢弃内容。
func canonicalizeOpenAIIntegrityMessages(body map[string]any) {
	if text, ok := body["input"].(string); ok {
		body["input"] = []any{map[string]any{"type": "message", "role": "user", "content": text}}
	}
	input, _ := body["input"].([]any)
	for _, raw := range input {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ, _ := item["type"].(string)
		if typ == "" && (item["role"] == "user" || item["role"] == "assistant" || item["role"] == "system" || item["role"] == "developer") {
			item["type"] = "message"
			typ = "message"
		}
		if typ != "message" {
			continue
		}
		if text, ok := item["content"].(string); ok {
			item["content"] = []any{map[string]any{"type": "input_text", "text": text}}
		}
		parts, _ := item["content"].([]any)
		for _, rawPart := range parts {
			if part, ok := rawPart.(map[string]any); ok && (part["type"] == "text" || part["type"] == "output_text") {
				part["type"] = "input_text"
			}
		}
	}
}
