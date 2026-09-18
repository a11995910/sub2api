package service

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// OpenAIHealthyTurnStateProbeResult 只返回记录结果，不把不透明状态头或代理凭据发给浏览器。
type OpenAIHealthyTurnStateProbeResult struct {
	Status     string     `json:"status"`
	Model      string     `json:"model"`
	Transport  string     `json:"transport"`
	HTTPStatus int        `json:"http_status,omitempty"`
	Message    string     `json:"message,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
}

// ProbeOpenAIHealthyTurnState 每次调用只发送一次 hi；显式手动采集不依赖自动记录开关。
// 所有出口覆盖仅作用于账号副本，且复用正常转发的认证、身份、插件和缓存实例。
func (s *AccountTestService) ProbeOpenAIHealthyTurnState(ctx context.Context, account *Account, model, transport string) (*OpenAIHealthyTurnStateProbeResult, error) {
	if account == nil || account.ID <= 0 || account.Platform != PlatformOpenAI {
		return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_ACCOUNT", "仅支持 OpenAI 账号")
	}
	if s.openaiGatewayService == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_TURN_STATE_UNAVAILABLE", "状态头测试服务暂不可用")
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "gpt-6-astra"
	}
	if !account.IsModelSupported(model) || isOpenAIImageModel(model) {
		return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请选择账号支持的文本模型")
	}
	if transport == "" {
		transport = "http"
	}
	if transport != "http" && transport != "websocket" {
		return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_TRANSPORT", "测试方式仅支持 HTTP 或 WebSocket")
	}
	result := &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: model, Transport: transport}
	defer func() {
		if repo := s.openaiGatewayService.openaiHealthyTurnStates.repo; repo != nil {
			storeCtx, cancel := healthyTurnStateStoreContext()
			defer cancel()
			proxyID := int64(0)
			if account.ProxyID != nil {
				proxyID = *account.ProxyID
			}
			probe := HealthyTurnStateProbeLog{Model: result.Model, Transport: transport, ProxyID: proxyID, Status: result.Status, HTTPStatus: result.HTTPStatus}
			if err := repo.RecordProbe(storeCtx, account.ID, probe); err != nil {
				healthyTurnStateStoreError("probe", account.ID, err)
				result.Message = strings.TrimSpace(result.Message + " 测试结果历史保存失败，请稍后刷新统计核实")
			}
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	copyAccount := *account
	copyAccount.Extra = maps.Clone(account.Extra)
	if copyAccount.Extra == nil {
		copyAccount.Extra = make(map[string]any)
	}
	copyAccount.Extra[openAIHealthyTurnStateRecordKey] = true
	copyAccount.Extra[openAIHealthyTurnStateReplaceKey] = false
	account = &copyAccount
	gateway := s.openaiGatewayService
	token, _, err := gateway.GetAccessToken(ctx, account)
	if err != nil {
		result.Message = "无法取得账号认证凭据，请检查账号状态"
		return result, nil
	}
	model = account.GetMappedModel(model)
	if account.UsesOpenAICodexProtocol() {
		model = normalizeOpenAIModelForUpstream(account, model)
	}
	result.Model = model
	// 独立上下文避免将管理端的 Cookie、JWT 或客户端头透传给模型服务。
	probeContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	probeContext.Request, _ = http.NewRequestWithContext(ctx, http.MethodPost, "/v1/responses", nil)
	probeContext.Request.Header.Set("User-Agent", CodexCanonicalUserAgent())
	session := uuid.NewString()
	probeContext.Request.Header.Set("session_id", session)
	payload := createOpenAITestPayload(model, account.UsesOpenAICodexProtocol())
	body, _ := json.Marshal(payload)
	body, _, err = applyCodexAccountAndFingerprintIdentityRaw(probeContext, account, body)
	if err != nil {
		result.Message = "无法构造测试请求"
		return result, nil
	}
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	var observer *openAIHealthyTurnStateObserver
	if transport == "websocket" {
		observer = gateway.probeHealthyTurnStateWS(ctx, probeContext, account, token, session, proxyURL, body, result)
	} else {
		observer = gateway.probeHealthyTurnStateHTTP(ctx, probeContext, account, token, session, proxyURL, body, result)
	}
	if observer == nil {
		return result, nil
	}
	if observer.modelMismatch {
		result.Status = "unhealthy"
		result.Message = "上游响应模型与请求模型不一致，未记录状态头"
		return result, nil
	}
	if ctx.Err() != nil || !observer.healthy || !observer.terminal || observer.failed {
		result.Status = "unhealthy"
		return result, nil
	}
	if observer.firstOutputTooLate {
		result.Status = "unhealthy"
		result.Message = "首字超过 5 秒，未记录状态头"
		return result, nil
	}
	if observer.candidate.value == "" {
		result.Status = "no_header"
		return result, nil
	}
	// 手动测试直到完整健康响应后才提交，测试中途不会产生可供其他请求领取的记录。
	cache, scope := observer.attempt.cache, observer.attempt.scope
	if cache.store(scope, observer.candidate) {
		result.Status, result.ExpiresAt = "recorded", &observer.candidate.expiresAt
		return result, nil
	}
	entry, exists := cache.get(scope)
	if exists && entry.value == observer.candidate.value {
		result.Status, result.ExpiresAt = "already_recorded", &entry.expiresAt
	} else {
		result.Status = "not_recorded"
	}
	return result, nil
}

func newOpenAIHealthyTurnStateProbeObserver(attempt *openAIHealthyTurnStateAttempt, headers http.Header) *openAIHealthyTurnStateObserver {
	attempt.record, attempt.replace = false, false
	value := extractOpenAICodexTurnState(headers)
	if len(value) > openAIHealthyTurnStateMaxBytes || strings.ContainsAny(value, "\r\n") {
		value = ""
	}
	return &openAIHealthyTurnStateObserver{attempt: attempt, candidate: openAIHealthyTurnStateEntry{value: value, expiresAt: time.Now().Add(openAIHealthyTurnStateTTL)}}
}

func (s *OpenAIGatewayService) probeHealthyTurnStateHTTP(ctx context.Context, c *gin.Context, account *Account, token, session, proxyURL string, body []byte, result *OpenAIHealthyTurnStateProbeResult) *openAIHealthyTurnStateObserver {
	req, err := s.buildUpstreamRequest(ctx, c, account, body, token, true, session, true)
	if err != nil {
		result.Message = "无法构造上游请求，请检查账号配置"
		return nil
	}
	req.Header.Del(openAICodexTurnStateHeader)
	if attempt := openAIHealthyTurnStateAttemptFromRequest(req); attempt != nil {
		attempt.markStarted()
	}
	// 直接发送一次，任何错误均不触发状态替换、重试或账号切换。
	resp, err := s.doOpenAIUpstreamOnce(req, proxyURL, account)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil || resp == nil {
		result.Message = "连接失败或测试超时，请检查代理和账号"
		return nil
	}
	result.HTTPStatus = resp.StatusCode
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil {
		return nil
	}
	observer := newOpenAIHealthyTurnStateProbeObserver(openAIHealthyTurnStateAttemptFromRequest(req), resp.Header)
	observedBody := newOpenAIHealthyTurnStateBody(resp, observer)
	// hi 测试限制响应总量，异常长流不消耗无界内存或流量。
	const maxProbeBytes = 8 << 20
	n, readErr := io.Copy(io.Discard, io.LimitReader(observedBody, maxProbeBytes+1))
	if readErr != nil || n > maxProbeBytes {
		observer.failed = true
	}
	observer.finish()
	return observer
}

func (s *OpenAIGatewayService) probeHealthyTurnStateWS(ctx context.Context, c *gin.Context, account *Account, token, session, proxyURL string, body []byte, result *OpenAIHealthyTurnStateProbeResult) *openAIHealthyTurnStateObserver {
	wsURL, err := s.buildOpenAIResponsesWSURL(account)
	if err != nil {
		result.Message = "无法构造 WebSocket 地址，请检查账号配置"
		return nil
	}
	decision := OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}
	headers, _, err := s.buildOpenAIWSHeaders(ctx, c, account, token, decision, true, "", "", session, result.Model, "")
	if err == nil {
		headers, err = s.refreshOpenAIAgentIdentityHeaders(ctx, account, headers)
	}
	if err != nil {
		result.Message = "无法构造 WebSocket 认证请求"
		return nil
	}
	headers.Del(openAICodexTurnStateHeader)
	attempt := s.newOpenAIHealthyTurnStateAttempt(c, account, result.Model, "ws:"+wsURL, proxyURL, headers)
	if attempt != nil {
		attempt.markStarted()
	}
	conn, status, responseHeaders, err := s.getOpenAIWSPassthroughDialer().Dial(ctx, wsURL, headers, proxyURL)
	if conn != nil {
		defer conn.Close()
	}
	result.HTTPStatus = status
	if err != nil || conn == nil || status != http.StatusSwitchingProtocols {
		result.Message = "WebSocket 握手失败或超时"
		return nil
	}
	observer := newOpenAIHealthyTurnStateProbeObserver(attempt, responseHeaders)
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil || conn.WriteJSON(ctx, s.buildOpenAIWSCreatePayload(payload, account)) != nil {
		observer.failed = true
		return observer
	}
	total := 0
	for !observer.finished {
		data, readErr := conn.ReadMessage(ctx)
		total += len(data)
		if readErr != nil || total > 8<<20 {
			observer.failed = true
			break
		}
		observer.observe(data, "")
		if observer.terminal {
			break
		}
	}
	observer.finish()
	return observer
}
