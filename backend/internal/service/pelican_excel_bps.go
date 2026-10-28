package service

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// 直接复用 BPS 转发，避免通用网关在自动授权等待期间选择原生协议。
func (s *AccountTestService) testIntelligenceExcelBPS(c *gin.Context, account *Account, model string) error {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Accel-Buffering", "no")
	s.sendEvent(c, TestEvent{Type: "test_start", Model: model})
	if !account.IsExcelBPSEnabled() {
		return s.sendErrorAndEnd(c, "Excel/BPS 未开启或账号类型不支持，未切换原生 Codex")
	}
	if !account.isExcelBPSEnabledForUpstreamModel(model) {
		return s.sendErrorAndEnd(c, "测试模型不在账号的 Excel/BPS 模型范围内，未切换原生 Codex")
	}
	gateway := s.openaiGatewayService
	if gateway == nil {
		return s.sendErrorAndEnd(c, "Excel/BPS 转发服务不可用")
	}
	ctx := c.Request.Context()
	cfg, err := gateway.settingService.GetExcelBPSConfig(ctx)
	if err != nil {
		return s.sendErrorAndEnd(c, "Excel/BPS 配置读取失败")
	}
	if !cfg.Enabled {
		return s.sendErrorAndEnd(c, "Excel/BPS 全局开关已关闭，未切换原生 Codex")
	}
	options, _ := pelicanTestOptionsFromContext(ctx)
	body, err := json.Marshal(createPelicanOpenAIPayload(model, true, options.prompt, options.reasoningEffort))
	if err != nil {
		return s.sendErrorAndEnd(c, "Excel/BPS 测试请求构造失败")
	}
	// 测试不因 403 自动关闭协议，避免后续测试悄悄改走原生。
	testAccount := *account
	testAccount.Extra = maps.Clone(account.Extra)
	testAccount.Extra["openai_excel_bps_auto_disable_on_403"] = false
	s.sendEvent(c, TestEvent{Type: "status", Text: "正在通过 Excel/BPS 测试，失败不会回退原生 Codex"})

	output := c.Copy()
	output.Writer = c.Writer
	usage := startPelicanTestStream(c, "openai")
	completed := false
	failure := ""
	writer := &excelBPSEventWriter{
		ResponseWriter: c.Writer, headers: make(http.Header), status: http.StatusOK,
		send: func(payload []byte) error {
			usage.read(string(payload))
			switch gjson.GetBytes(payload, "type").String() {
			case "response.output_text.delta":
				s.sendEvent(output, TestEvent{Type: "content", Text: gjson.GetBytes(payload, "delta").String()})
			case "response.completed":
				if candyTestRequested(c) {
					var event map[string]any
					if json.Unmarshal(payload, &event) != nil {
						failure = "糖果测试流包含无法解析的事件"
					} else if message := candyResponseCompletionError(event); message != "" {
						failure = message
					}
				}
				completed = true
			case "response.refusal.delta", "response.refusal.done":
				if candyTestRequested(c) {
					failure = "模型拒绝回答糖果题"
				}
			case "response.incomplete":
				failure = "Excel/BPS 响应未完成"
			case "error", "response.failed", "response.cancelled":
				// 此处仅接收 BPS 适配器已脱敏的输出，不透传上游原始正文。
				failure = gjson.GetBytes(payload, "response.error.message").String()
				if failure == "" {
					failure = gjson.GetBytes(payload, "error.message").String()
				}
				if failure == "" {
					failure = "Excel/BPS 响应未完成"
				}
			}
			return ctx.Err()
		},
	}
	func() {
		previous := c.Writer
		c.Writer = writer
		defer func() { c.Writer = previous }()
		// model 已完成账号映射，显式传入防止重复映射。
		_, err = gateway.forwardExcelBPS(ctx, c, &testAccount, body, time.Now(), "responses", model)
	}()
	if writer.status >= 400 {
		failure = fmt.Sprintf("Excel/BPS HTTP %d：%s", writer.status, gjson.GetBytes(writer.pending, "error.message").String())
	}
	if failure != "" {
		return s.sendErrorAndEnd(c, failure)
	}
	if err != nil || !completed {
		return s.sendErrorAndEnd(c, "Excel/BPS 调用失败或响应中断，未切换原生 Codex")
	}
	s.sendEvent(c, TestEvent{Type: "test_complete", Success: true})
	return nil
}
