package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// 只复用纯协议转换器，不进入 Codex 的重试、限流或错误健康处理链路。
type excelBPSCompatOutput struct {
	format    string
	model     string
	chat      *apicompat.ResponsesEventToChatState
	anthropic *apicompat.ResponsesEventToAnthropicState
}

func newExcelBPSCompatOutput(format, model string) *excelBPSCompatOutput {
	o := &excelBPSCompatOutput{format: format, model: model, chat: apicompat.NewResponsesEventToChatState(), anthropic: apicompat.NewResponsesEventToAnthropicState()}
	o.chat.Model = model
	o.chat.IncludeUsage = true
	o.anthropic.Model = model
	return o
}
func (o *excelBPSCompatOutput) event(payload []byte) (string, error) {
	if failure := basispoints.ParseUpstreamFailure(payload); failure != nil {
		if o.format == "anthropic" {
			raw, _ := json.Marshal(map[string]any{"type": "error", "error": failure.Details()})
			return "event: error\ndata: " + string(raw) + "\n\n", nil
		}
		return buildChatStreamErrorSSE(failure.Code, failure.Message) + "data: [DONE]\n\n", nil
	}
	var event apicompat.ResponsesStreamEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return "", err
	}
	var out strings.Builder
	if o.format == "anthropic" {
		for _, e := range apicompat.ResponsesEventToAnthropicEvents(&event, o.anthropic) {
			wire, err := apicompat.ResponsesAnthropicEventToSSE(e)
			if err != nil {
				return "", err
			}
			out.WriteString(wire)
		}
	} else {
		// 部分 BPS 响应仅在终态携带文本，补出尚未发送的文本后再发结束块。
		if event.Type == "response.completed" && !o.chat.SawText {
			for _, item := range gjson.GetBytes(payload, "response.output").Array() {
				if item.Get("type").String() != "message" {
					continue
				}
				for _, part := range item.Get("content").Array() {
					if part.Get("type").String() != "output_text" {
						continue
					}
					delta := apicompat.ResponsesStreamEvent{Type: "response.output_text.delta", Delta: part.Get("text").String()}
					for _, chunk := range apicompat.ResponsesEventToChatChunks(&delta, o.chat) {
						wire, err := apicompat.ChatChunkToSSE(chunk)
						if err != nil {
							return "", err
						}
						out.WriteString(wire)
					}
				}
			}
		}
		for _, chunk := range apicompat.ResponsesEventToChatChunks(&event, o.chat) {
			wire, err := apicompat.ChatChunkToSSE(chunk)
			if err != nil {
				return "", err
			}
			out.WriteString(wire)
		}
		if event.Type == "response.completed" || event.Type == "response.incomplete" {
			out.WriteString("data: [DONE]\n\n")
		}
	}
	return out.String(), nil
}
func (o *excelBPSCompatOutput) buffered(raw []byte) ([]byte, error) {
	var response apicompat.ResponsesResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil, fmt.Errorf("Excel/BPS 响应格式无效")
	}
	if o.format == "anthropic" {
		return json.Marshal(apicompat.ResponsesToAnthropic(&response, o.model))
	}
	return json.Marshal(apicompat.ResponsesToChatCompletions(&response, o.model))
}

// 失败也沿用调用方协议，避免已开始的流混入另一种协议事件。
func writeExcelBPSOutputError(c *gin.Context, format string, stream bool, status int, code, message string) {
	detail := gin.H{"type": "server_error", "code": code, "message": message}
	if !stream {
		if format == "anthropic" {
			c.JSON(status, gin.H{"type": "error", "error": detail})
		} else {
			c.JSON(status, gin.H{"error": detail})
		}
		return
	}
	var wire string
	switch format {
	case "anthropic":
		raw, _ := json.Marshal(gin.H{"type": "error", "error": detail})
		wire = "event: error\ndata: " + string(raw) + "\n\n"
	case "chat":
		wire = buildChatStreamErrorSSE(code, message) + "data: [DONE]\n\n"
	default:
		raw, _ := json.Marshal(gin.H{"type": "response.failed", "response": gin.H{"status": "failed", "error": detail}})
		wire = "event: response.failed\ndata: " + string(raw) + "\n\n"
	}
	_, _ = c.Writer.WriteString(wire)
	c.Writer.Flush()
}
