package service

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/tidwall/gjson"
)

var (
	errCodexTicketModelMismatch = errors.New("门票响应模型不一致")
	errCodexTicketUnverified    = errors.New("门票响应未完整成功或缺少模型声明")
)

const codexTicketResponseMaxBytes = 1 << 20

// 仅读取协议中的模型声明，不扫描回答文本、工具参数或嵌套的用户内容。
func codexTicketResponseModel(payload []byte) string {
	if !gjson.ValidBytes(payload) {
		return ""
	}
	event := gjson.GetBytes(payload, "type").String()
	if event != "" && !strings.HasPrefix(event, "response.") {
		return ""
	}
	return firstValidTrimmedGJSONString(payload, "response.model", "model")
}

func codexTicketResponseModelMismatch(model string, payload []byte) bool {
	mismatch := upstreamModelMismatch(model, codexTicketResponseModel(payload))
	return mismatch != nil && *mismatch
}

// 有界观察 JSON 和 SSE（含分块、多行 data），不改动转发字节，也不保存正文。
// 每个事件至多缓存 1 MiB；采集超过限制时拒绝入库。
type codexTicketResponseParser struct {
	mode               byte
	line, data         []byte
	event              string
	overflow, exceeded bool
	observe            func([]byte, string)
}

func (p *codexTicketResponseParser) feed(chunk []byte) {
	if p.mode == 0 {
		chunk = bytes.TrimLeft(chunk, " \t\r\n")
		if len(chunk) == 0 {
			return
		}
		p.mode = 's'
		if chunk[0] == '{' {
			p.mode = 'j'
		}
	}
	if p.mode == 'j' {
		if len(p.data)+len(chunk) > codexTicketResponseMaxBytes {
			p.overflow, p.exceeded, p.data = true, true, nil
		} else if !p.overflow {
			p.data = append(p.data, chunk...)
			if gjson.ValidBytes(p.data) {
				p.observe(p.data, "")
				p.data = nil
				p.mode = 'x'
			}
		}
		return
	}
	if p.mode != 's' {
		return
	}
	for len(chunk) > 0 {
		end := bytes.IndexByte(chunk, '\n')
		part := chunk
		if end >= 0 {
			part = chunk[:end]
		}
		if len(p.line)+len(p.data)+len(part) > codexTicketResponseMaxBytes {
			p.overflow, p.exceeded, p.line, p.data = true, true, nil, nil
		} else if !p.overflow {
			p.line = append(p.line, part...)
		}
		if end < 0 {
			return
		}
		p.consumeLine()
		chunk = chunk[end+1:]
	}
}

func (p *codexTicketResponseParser) consumeLine() {
	line := bytes.TrimSuffix(p.line, []byte{'\r'})
	if len(line) == 0 {
		p.data, p.event, p.overflow = nil, "", false
	} else if !p.overflow && bytes.HasPrefix(line, []byte("event:")) {
		p.event = strings.TrimSpace(string(line[6:]))
	} else if !p.overflow && bytes.HasPrefix(line, []byte("data:")) {
		if len(p.data) > 0 {
			p.data = append(p.data, '\n')
		}
		p.data = append(p.data, bytes.TrimPrefix(line[5:], []byte{' '})...)
		// 完整单行事件及时观察，不等待上游关闭连接或额外空行。
		if gjson.ValidBytes(p.data) {
			p.observe(p.data, p.event)
		}
	}
	p.line = p.line[:0]
}

func (p *codexTicketResponseParser) finish() {
	if p.mode == 's' && len(p.line) > 0 {
		p.consumeLine()
	}
}

// 探测必须读到成功终态与一致的模型声明，单独的 HTTP 200 或 [DONE] 不算成功。
func validateCodexTicketProbeResponse(body io.Reader, model string) error {
	return validateCodexTicketProbeResponseWithPolicy(body, model, true)
}

func validateCodexTicketProbeResponseWithPolicy(body io.Reader, model string, rejectMismatch bool) error {
	if body == nil {
		return errCodexTicketUnverified
	}
	var result error
	matched, completed := false, false
	parser := codexTicketResponseParser{observe: func(payload []byte, event string) {
		if result != nil {
			return
		}
		if declared := codexTicketResponseModel(payload); declared != "" {
			if rejectMismatch && !upstreamModelsMatchForAudit(model, declared) {
				result = errCodexTicketModelMismatch
				return
			}
			matched = true
		}
		if kind := gjson.GetBytes(payload, "type").String(); kind != "" {
			event = kind
		}
		status := firstValidTrimmedGJSONString(payload, "response.status", "status")
		if gjson.GetBytes(payload, "error").IsObject() || gjson.GetBytes(payload, "response.error").IsObject() || event == "error" || event == "response.failed" || event == "response.incomplete" || event == "response.cancelled" || event == "response.canceled" || status == "failed" || status == "incomplete" || status == "cancelled" || status == "canceled" {
			result = errCodexTicketUnverified
			return
		}
		if event == "response.completed" || event == "response.done" || (event == "" && status == "completed") {
			completed = true
		}
	}}
	buffer := make([]byte, 8192)
	for {
		n, err := body.Read(buffer)
		parser.feed(buffer[:n])
		if err != nil {
			parser.finish()
		}
		if result != nil {
			return result
		}
		if parser.exceeded {
			return errCodexTicketUnverified
		}
		if completed {
			if !matched {
				return errCodexTicketUnverified
			}
			return nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return errCodexTicketUnverified
			}
			return err
		}
	}
}
