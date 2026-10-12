package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const excelBPSManagedWSContextKey = "excel_bps_managed_ws"

// 将协议适配器输出的 SSE 事件逐条转为既有 WebSocket 连接中的 JSON 帧。
// 不经过 httptest 缓冲整段响应，也不重复升级连接。
type excelBPSWSWriter struct {
	gin.ResponseWriter
	headers   http.Header
	pending   []byte
	status    int
	size      int
	send      func([]byte) error
	collector openAIWSToolCallReplayCollector
}

func (w *excelBPSWSWriter) Header() http.Header               { return w.headers }
func (w *excelBPSWSWriter) WriteHeader(status int)            { w.status = status }
func (w *excelBPSWSWriter) WriteHeaderNow()                   {}
func (w *excelBPSWSWriter) Status() int                       { return w.status }
func (w *excelBPSWSWriter) Size() int                         { return w.size }
func (w *excelBPSWSWriter) Written() bool                     { return w.size > 0 }
func (w *excelBPSWSWriter) Flush()                            {}
func (w *excelBPSWSWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *excelBPSWSWriter) Write(p []byte) (int, error) {
	if len(w.pending)+len(p) > 16<<20 {
		return 0, fmt.Errorf("BPS 事件超过大小限制")
	}
	w.pending = append(w.pending, p...)
	w.size += len(p)
	for {
		i := bytes.IndexByte(w.pending, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSpace(w.pending[:i])
		w.pending = w.pending[i+1:]
		if bytes.HasPrefix(line, []byte("data:")) {
			payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
			if !gjson.ValidBytes(payload) {
				continue
			}
			w.collector.AddEvent(gjson.GetBytes(payload, "type").String(), payload)
			if err := w.send(append([]byte(nil), payload...)); err != nil {
				return 0, err
			}
		}
	}
	return len(p), nil
}
func (s *OpenAIGatewayService) proxyExcelBPSWSTurn(ctx context.Context, c *gin.Context, a *Account, payload []byte, original string, send func([]byte) error) (*OpenAIForwardResult, error) {
	body := append([]byte(nil), payload...)
	var err error
	for _, field := range []string{"type", "generate", "previous_response_id"} {
		body, err = sjson.DeleteBytes(body, field)
		if err != nil {
			return nil, err
		}
	}
	body, err = sjson.SetBytes(body, "stream", true)
	if err != nil {
		return nil, err
	}
	writer := &excelBPSWSWriter{ResponseWriter: c.Writer, headers: make(http.Header), status: 200, send: send}
	previous := c.Writer
	previousManaged := c.GetBool(excelBPSManagedWSContextKey)
	c.Set(excelBPSManagedWSContextKey, true)
	c.Writer = writer
	defer func() { c.Writer = previous; c.Set(excelBPSManagedWSContextKey, previousManaged) }()
	upstream := gjson.GetBytes(body, "model").String()
	body, err = sjson.SetBytes(body, "model", original)
	if err != nil {
		return nil, err
	}
	result, err := s.forwardExcelBPS(ctx, c, a, body, time.Now(), "responses", upstream)
	if writer.status >= 400 {
		_ = send(buildOpenAIWSHTTPBridgeErrorEvent(writer.status, gjson.GetBytes(writer.pending, "error.message").String()))
	}
	if result != nil {
		result.Model = original
		result.OpenAIWSMode = true
		result.wsReplayInput = writer.collector.Items()
		result.wsReplayInputExists = len(result.wsReplayInput) > 0
		result.wsAccountFailoverReplayInput = writer.collector.AllItems()
	}
	return result, err
}

// 首帧选择 BPS 时延迟取令牌，避免先刷新无关的 Codex grant。每轮由自己的协议获取凭据。
func (s *OpenAIGatewayService) GetWebSocketRequestCredential(ctx context.Context, c *gin.Context, a *Account, model string) (string, string, error) {
	enabled, err := s.excelBPSRouteEnabled(ctx, a, model)
	if err != nil {
		return "", "", err
	}
	if enabled {
		return "", "excel-bps", nil
	}
	return s.GetRequestCredential(ctx, c, a)
}
