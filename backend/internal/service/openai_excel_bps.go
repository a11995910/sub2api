package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var excelBPSReplay basispoints.ReplayCache
var excelBPSCatalog basispoints.CatalogCache

func (s *OpenAIGatewayService) excelBPSImageRelay(ctx context.Context) (*basispoints.ImageRelay, error) {
	settings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		return nil, err
	}
	return s.excelBPSImageRelayForSettings(settings)
}

func (s *OpenAIGatewayService) excelBPSImageRelayForSettings(settings ExcelBPSImageRelaySettings) (*basispoints.ImageRelay, error) {
	if !settings.Enabled || settings.Mode == ExcelBPSImageModeNative {
		if s != nil {
			s.excelBPSImagesMu.Lock()
			if s.excelBPSImages != nil {
				_ = s.excelBPSImages.Close()
				s.excelBPSImages = nil
			}
			s.excelBPSImagesMu.Unlock()
		}
		return nil, nil
	}
	var err error
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	if s.excelBPSImages == nil {
		dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
		if dataDir == "" {
			dataDir = "./data"
		}
		s.excelBPSImages, err = basispoints.NewImageRelay(settings.BaseURL, filepath.Join(dataDir, "bps-images"))

	}
	if err == nil {
		err = s.excelBPSImages.Configure(settings.BaseURL, settings.Limits)
	}
	return s.excelBPSImages, err
}

func (s *OpenAIGatewayService) CloseExcelBPSImages() error {
	if s == nil {
		return nil
	}
	s.excelBPSImagesMu.Lock()
	defer s.excelBPSImagesMu.Unlock()
	return s.excelBPSImages.Close()
}

// 通过不可猜测的临时地址供上游读取图片。
func (s *OpenAIGatewayService) ServeExcelBPSImage(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	s.excelBPSImagesMu.Lock()
	relay := s.excelBPSImages
	s.excelBPSImagesMu.Unlock()
	if !relay.HasLiveImage(c.Request.URL.Path) {
		http.NotFound(c.Writer, c.Request)
		return
	}
	settings, err := s.settingService.GetExcelBPSImageRelaySettings(c.Request.Context())
	if err != nil || !settings.Enabled || settings.Mode != ExcelBPSImageModeRelay {
		http.NotFound(c.Writer, c.Request)
		return
	}
	relay.ServeHTTP(c.Writer, c.Request)
}

func excelBPSAccountID(account *Account, accessToken string) string {
	if accountID := strings.TrimSpace(account.GetChatGPTAccountID()); accountID != "" {
		return accountID
	}
	claims, err := openai.DecodeIDToken(accessToken)
	if err != nil || claims.OpenAIAuth == nil {
		return ""
	}
	return strings.TrimSpace(claims.OpenAIAuth.ChatGPTAccountID)
}

func newExcelBPSRequest(ctx context.Context, body []byte, token, accountID string) (*http.Request, error) {
	return newExcelBPSRequestTo(ctx, basispoints.ResponsesURL, "text/event-stream", body, token, accountID)
}

func newExcelBPSRequestTo(ctx context.Context, targetURL, accept string, body []byte, token, accountID string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = http.Header{
		"Authorization": {"Bearer " + token}, "Chatgpt-Account-Id": {accountID}, "X-Openai-Account-Id": {accountID},
		"X-Basispoints-Auth-Mode": {"chatgpt"}, "Content-Type": {"application/json"}, "Accept": {accept},
		"Origin": {"https://bps.openai.com"}, "User-Agent": {"Mozilla/5.0"},
		"X-Openai-Internal-Basispoints-Client-Product":       {"basispoints-excel-plugin"},
		"X-Openai-Internal-Basispoints-Client-Agent-Profile": {"excel"},
	}
	return req, nil
}

// BPS 请求不注入 Codex 门票、Cookie 或插件头，仅使用选中账号的 Excel 凭据及工作区 ID。
func (s *OpenAIGatewayService) forwardExcelBPS(ctx context.Context, c *gin.Context, account *Account, body []byte, start time.Time, formats ...string) (forwardResult *OpenAIForwardResult, forwardErr error) {
	var compactUsage OpenAIUsage
	var compactID string
	originalImagePolicyModel := gjson.GetBytes(body, "model").String()
	var compactEffort string
	defer func() {
		defer func() {
			if forwardResult != nil && account.IsExcelBPSCacheCreationAsInputEnabled() {
				forwardResult.Usage.CacheCreationInputTokens = 0
			}
		}()
		if compactID == "" || !openAIUsageHasTokens(&compactUsage) {
			return
		}
		if forwardResult == nil {
			forwardResult = &OpenAIForwardResult{Model: originalImagePolicyModel, ReasoningEffort: &compactEffort, UpstreamModel: gjson.GetBytes(body, "model").String(), UpstreamEndpoint: "/basispoints/api/responses", RequestID: compactID, Stream: gjson.GetBytes(body, "stream").Bool(), Duration: time.Since(start)}
		}
		addImagePolicyUsage(&forwardResult.Usage, compactUsage)
	}()
	fail := func(status int, code, message string, param ...string) (*OpenAIForwardResult, error) {
		MarkResponseCommitted(c)
		if len(formats) > 0 && formats[0] == "anthropic" {
			writeAnthropicError(c, status, "invalid_request_error", message)
		} else {
			c.JSON(status, gin.H{"error": gin.H{"type": "invalid_request_error", "code": code, "message": message}})
		}

		return nil, fmt.Errorf("Excel/BPS: %s", code)
	}
	originalModel := gjson.GetBytes(body, "model").String()
	model := account.GetMappedModel(originalModel)
	if len(formats) > 1 {
		model = formats[1]
	}
	if s.isExcelBPSCoolingDown(account) {
		return fail(429, "basispoints_rate_limited", "Excel/BPS 正在限流冷却，请稍后重试")
	}
	if s.isExcelBPSModelCoolingDown(account, originalModel) {
		return fail(403, "basispoints_model_access_changed", "此账号暂时无权访问该 BPS 模型")
	}
	var policyErr error
	body, _, policyErr = ApplyOpenAIReasoningEffortPolicyFromContext(ctx, body)
	if policyErr != nil {
		return fail(403, "basispoints_reasoning_policy", policyErr.Error())
	}
	body, policyErr = s.applyOpenAIFastPolicyToBody(ctx, account, model, body)
	if policyErr != nil {
		return fail(403, "basispoints_fast_policy", policyErr.Error())
	}
	stream := gjson.GetBytes(body, "stream").Bool()
	clientCanceled := func() (*OpenAIForwardResult, error) {
		MarkResponseCommitted(c)

		// 响应头之前没有响应或已计量用量，不创建用量记录。
		return nil, context.Canceled
	}
	imageSettings, err := s.settingService.GetExcelBPSImageRelaySettings(ctx)
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}
		return fail(503, "basispoints_image_settings_unavailable", "Excel BPS image settings are unavailable")
	}
	// 在请求体改写及转换前预留内存预算，超额时直接返回容量错误。
	releaseImageAdmission, err := s.admitExcelBPSImageRequest(c, imageSettings, len(body))
	if err != nil {
		return fail(503, "basispoints_image_capacity", err.Error())
	}
	defer releaseImageAdmission()
	body, err = sjson.SetBytes(body, "model", model)
	if err != nil {
		return fail(400, "basispoints_request_invalid", "Invalid model request")
	}
	identity, transient := resolveExcelBPSIdentity(c, body, getAPIKeyIDFromContext(c), c.GetBool(excelBPSManagedWSContextKey))
	if identity != "" {
		body, err = sjson.SetBytes(body, "prompt_cache_key", identity)
		if err != nil {
			return nil, err
		}
	}
	if isOpenAIResponsesCompactPath(c) {
		var request map[string]any
		if err = json.Unmarshal(body, &request); err != nil {
			return fail(400, "basispoints_request_invalid", "Invalid compact request")
		}
		var input []any
		switch v := request["input"].(type) {
		case []any:
			input = v
		case string:
			input = []any{map[string]any{"role": "user", "content": v}}
		default:
			return fail(400, "basispoints_request_invalid", "Compact requires input")
		}
		request["input"] = append(input, map[string]any{"type": "compaction_trigger"})
		request["tool_choice"] = "none"
		body, err = json.Marshal(request)
		if err != nil {
			return nil, err
		}
	}
	scope := fmt.Sprintf("account:%d/key:%d/thread:%s", account.ID, getAPIKeyIDFromContext(c), identity)
	if transient {
		scope = "transient:" + scope
	}
	if err = basispoints.ValidateNewAgentMessage(body); err != nil {
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if account.IsExcelBPSIgnoreEncryptedContentEnabled() {
		body, err = basispoints.StripEncryptedContent(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	imagePolicy, err := s.prepareExcelImagePolicy(ctx, c, body, imageSettings, scope+"/model:"+model, identity != "" && !transient)
	if err != nil {
		var policyErr *excelImagePolicyError
		if errors.As(err, &policyErr) {
			return fail(policyErr.Status, policyErr.Code, policyErr.Message)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	if len(imagePolicy.reconciledBody) > 0 {
		maxImageMiB, maxTotalMiB := imageSettings.Limits.MaxImageMiB, imageSettings.Limits.MaxTotalMiB
		if imageSettings.Mode == ExcelBPSImageModeNative {
			maxImageMiB, maxTotalMiB = 20, 32
		}
		if err = basispoints.ValidateImageBudget(body, maxImageMiB, maxTotalMiB); err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
		body = imagePolicy.reconciledBody
	}
	var compactOutput []any
	var compactUsageWire map[string]any
	var images *basispoints.NativeImages
	if imageSettings.Enabled && imageSettings.Mode == ExcelBPSImageModeNative {
		images, err = basispoints.PrepareNativeImagesWithLimit(body, imagePolicy.maxImages)
		if err == nil {
			body, err = images.Body()
		}
	} else {
		var relay *basispoints.ImageRelay
		relay, err = s.excelBPSImageRelayForSettings(imageSettings)
		if err != nil {
			return fail(503, "basispoints_image_relay_unavailable", "Excel BPS image relay is unavailable")
		}
		body, err = relay.RewriteWithImageLimit(body, scope, imagePolicy.maxImages)
	}
	if err != nil {
		if errors.Is(err, basispoints.ErrImageRelayFull) {
			return fail(503, "basispoints_image_relay_full", err.Error())
		}
		if errors.Is(err, basispoints.ErrImageRelayStorage) {
			return fail(503, "basispoints_image_relay_unavailable", err.Error())
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	// 上传前先完整校验请求；全部本地检查通过后才将有效占位符替换为真实附件 ID。
	var replay *basispoints.ReplayCache
	var catalog *basispoints.CatalogCache
	if identity != "" {
		replay, catalog = &excelBPSReplay, &excelBPSCatalog

	}
	var upstreamBody []byte
	var bridge *basispoints.Bridge
	if images != nil {
		upstreamBody, bridge, err = images.PrepareWithCatalog(scope, replay, catalog)
	} else {
		upstreamBody, bridge, err = basispoints.PrepareWithCatalog(body, scope, replay, catalog)
	}
	if err != nil {
		if errors.Is(err, basispoints.ErrStateUnavailable) {
			return fail(503, "basispoints_state_unavailable", basispoints.ErrStateUnavailable.Error())
		}
		var contentErr *basispoints.ContentValidationError
		if errors.As(err, &contentErr) {
			return fail(400, "basispoints_request_invalid", err.Error(), contentErr.Path)
		}
		return fail(400, "basispoints_request_invalid", err.Error())
	}
	token, err := s.getExcelBPSAccessToken(ctx, account)
	if err != nil {
		return fail(502, "basispoints_auth_unavailable", "Excel/BPS OAuth 凭据不可用")
	}
	ctx = s.withExcelBPSCredentialSnapshot(ctx, account, token)
	accountID := excelBPSAccountID(account, token)
	if accountID == "" {
		return fail(400, "basispoints_account_id_missing", "Excel BPS requires chatgpt_account_id")
	}
	attachmentProxy := ""
	if account.Proxy != nil {
		attachmentProxy = account.Proxy.URL()
	}

	if images != nil && images.HasImages() {
		attachmentScope := ""
		if identity != "" {
			attachmentScope = scope + "\x00" + accountID + "\x00" + token
		}
		body, err = images.Upload(ctx, &s.excelBPSAttachments, attachmentScope, func(uploadCtx context.Context, img basispoints.InlineAttachment) (string, error) {
			SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/attachments")
			return s.uploadExcelBPSAttachment(uploadCtx, account, token, accountID, attachmentProxy, img)
		})
		if err != nil {
			if isExcelBPSClientCancellation(c, err) {
				return clientCanceled()
			}
			status, code := http.StatusBadGateway, "basispoints_attachment_error"
			var uploadError *excelBPSAttachmentError
			if errors.As(err, &uploadError) {
				status = uploadError.status
			}
			if errors.Is(err, basispoints.ErrAttachmentBusy) {
				status = http.StatusServiceUnavailable
			}
			if status == http.StatusTooManyRequests && uploadError != nil {
				s.coolDownExcelBPS(ctx, account, uploadError.retryAfter)

				return fail(429, "basispoints_rate_limited", "Excel/BPS 请求限流，请稍后重试")
			}

			if status == http.StatusUnauthorized {
				return fail(status, code, "Excel BPS attachment authentication failed; request was not replayed")
			}
			return fail(status, code, "Excel BPS attachment upload failed; request was not replayed and account scheduling was not changed")
		}
		upstreamBody, bridge, err = bridge.Reprepare(body)
		if err != nil {
			return fail(400, "basispoints_request_invalid", err.Error())
		}
	}
	requestCtx := WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAI))

	if imagePolicy.split > 0 {
		logger.LegacyPrintf("service.openai_excel_bps", "image policy auto compact: account_id=%d history_images=%d new_images=%d limit=%d", account.ID, basispoints.CountInlineImages(imagePolicy.history.Input[:imagePolicy.split]), basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]), imageSettings.Limits.MaxImages)
		uploaded, inspectErr := basispoints.InspectImageHistory(body)
		if inspectErr != nil {
			return fail(400, "basispoints_request_invalid", inspectErr.Error())
		}
		compactBody, buildErr := uploaded.WithInput(uploaded.Input[:imagePolicy.split], true)
		if buildErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare image compaction")
		}
		compactWire, _, buildErr := bridge.Reprepare(compactBody)
		if buildErr != nil {
			return fail(400, "basispoints_request_invalid", buildErr.Error())
		}
		SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
		cr, compactErr := s.doExcelBPSRequest(requestCtx, account, compactWire, token, accountID)

		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction connection failed; generation was not started")
		}
		compactID = cr.Header.Get("x-request-id")
		if compactID == "" {
			compactID = "image-compaction"
		}
		if cr.StatusCode < 200 || cr.StatusCode >= 300 {
			_, _ = io.Copy(io.Discard, io.LimitReader(cr.Body, 512<<10))
			_ = cr.Body.Close()
			if cr.StatusCode == 403 {
				s.disableExcelBPSOn403(ctx, account)
			}
			return fail(cr.StatusCode, "basispoints_image_compaction_failed", "Image history compaction was rejected; generation was not started")
		}
		s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, cr.Header)
		compactEffort = bridge.Effort
		compactResponse, compactErr := basispoints.ReadImageCompaction(cr.Body, func(payload []byte) { s.parseSSEUsageBytes(payload, &compactUsage) })
		_ = cr.Body.Close()
		if compactResponse != nil {
			encoded, _ := json.Marshal(map[string]any{"type": "response.completed", "response": compactResponse})
			s.parseSSEUsageBytes(encoded, &compactUsage)
			compactUsageWire, _ = compactResponse["usage"].(map[string]any)
		}
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", "Image history compaction did not complete; generation was not started")
		}
		window, _, compactErr := basispoints.CompactWindow(compactResponse)
		if compactErr != nil {
			return fail(502, "basispoints_image_compaction_failed", compactErr.Error())
		}
		next := append(append([]any{}, window...), uploaded.Input[imagePolicy.split:]...)
		// 只统计保留的内嵌工具图片，已上传的附件占位符不重复计数。
		if basispoints.CountInlineImages(window)+basispoints.CountInlineImages(imagePolicy.history.Input[imagePolicy.split:]) > imageSettings.Limits.MaxImages {
			return fail(400, "basispoints_image_compaction_insufficient", "Compacted history still exceeds the image limit; reduce new images or compact manually")
		}
		body, compactErr = uploaded.WithInput(next, false)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", "Could not prepare compacted continuation")
		}
		upstreamBody, bridge, compactErr = bridge.Reprepare(body)
		if compactErr != nil {
			return fail(400, "basispoints_request_invalid", compactErr.Error())
		}
		if compactErr = imagePolicy.checkpoint(ctx, window); compactErr != nil {
			return fail(503, "basispoints_image_checkpoint_unavailable", "Could not save compacted history safely; retry or run compact manually")
		}
		compactOutput = window

	}
	SetActualOpenAIUpstreamEndpoint(c, "/basispoints/api/responses")
	SetOpsUpstreamModel(c, model)
	sent := time.Now()
	proxyURL := attachmentProxy
	resp, err := s.doExcelBPSRequest(requestCtx, account, upstreamBody, token, accountID)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
	if err != nil {
		if isExcelBPSClientCancellation(c, err) {
			return clientCanceled()
		}

		return fail(502, "basispoints_transport_error", "Excel BPS connection failed; request was not replayed after sending")
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()

	if resp.StatusCode == http.StatusBadRequest {
		// 重试前关闭响应以释放上游连接槽；继续使用当前账号代理。
		const maxRejectionBytes = 512 << 10
		raw, readErr := io.ReadAll(io.LimitReader(resp.Body, maxRejectionBytes+1))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		if retryBody, retry := prepareExcelBPSInvalidEncryptedRetry(upstreamBody, raw); retry && readErr == nil && len(raw) <= maxRejectionBytes && ctx.Err() == nil {
			retryReq, retryErr := newExcelBPSRequest(requestCtx, retryBody, token, accountID)
			if retryErr != nil {
				return fail(502, "basispoints_transport_error", "Excel BPS recovery request could not be prepared")
			}
			appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
				Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
				ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
				UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
				UpstreamURL: basispoints.ResponsesURL, Kind: "invalid_encrypted_content_retry",
				Message: "Excel BPS rejected encrypted reasoning; retrying once without opaque reasoning on the same route",
			})
			logger.LegacyPrintf("service.openai_excel_bps", "retrying invalid encrypted reasoning once: account_id=%d", account.ID)
			c.Set("excel_bps_upstream_attempt", c.GetInt("excel_bps_upstream_attempt")+1)
			// 发送后不重新获取代理或进入通用传输重试。
			resp, err = s.httpUpstream.Do(retryReq, proxyURL, account.ID, account.Concurrency)
			if err == nil && resp != nil && resp.Body != nil {
				resp.Body = basispoints.WithProgressTimeout(requestCtx, resp.Body, 2*time.Minute)
			}

			SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(sent).Milliseconds())
			if err != nil {
				if isExcelBPSClientCancellation(c, err) {
					return clientCanceled()
				}

				return fail(502, "basispoints_transport_error", "Excel BPS recovery connection failed; request was not replayed again")
			}

			upstreamBody = retryBody
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {

		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
		// BPS 限流仅影响此协议；诊断仅记录状态码，不保存可能回显凭据的错误正文。
		upstreamMessage := fmt.Sprintf("Excel BPS returned HTTP %d", resp.StatusCode)
		upstreamDetail := ""

		kind := "http_error"
		setOpsUpstreamError(c, resp.StatusCode, upstreamMessage, upstreamDetail)
		appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
			Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
			ProxyID: opsUpstreamProxyID(account), ProxyName: opsUpstreamProxyName(account),
			UpstreamStatusCode: resp.StatusCode, UpstreamRequestID: resp.Header.Get("x-request-id"),
			UpstreamURL: basispoints.ResponsesURL, Kind: kind,
			Message: upstreamMessage, Detail: upstreamDetail, UpstreamResponseBody: upstreamDetail,
		})
		if resp.StatusCode == http.StatusTooManyRequests {
			s.coolDownExcelBPS(ctx, account, resp.Header.Get("Retry-After"))
			return fail(429, "basispoints_rate_limited", "Excel/BPS 请求限流，请稍后重试")
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return fail(resp.StatusCode, "basispoints_upstream_error", "Excel BPS authentication failed; request was not replayed")
		}
		code := gjson.GetBytes(raw, "error.code").String()
		if isExcelBPSModelPermissionError(resp.StatusCode, code) {
			s.coolDownExcelBPSModel(account, model)
			return fail(resp.StatusCode, code, "This model is not available on the account's Excel BPS endpoint")
		}
		message := "Excel BPS rejected this request; account scheduling was not changed"
		errorCode := "basispoints_upstream_error"
		if resp.StatusCode == http.StatusBadRequest && isExcelBPSInvalidEncryptedContent(raw) {
			errorCode = "invalid_encrypted_content"
			message = "Excel BPS could not verify encrypted conversation state; resend the original plaintext history or start a new conversation"
		}
		if resp.StatusCode == http.StatusForbidden {
			// 仅关闭独立协议开关；账号其他路由不变。
			disabled := s.disableExcelBPSOn403(ctx, account)
			if disabled {
				message = "Excel/BPS 返回 403，已自动关闭该账号的 BPS 协议"
			}
		}
		return fail(resp.StatusCode, errorCode, message)
	}
	// 若上游返回配额头，及时更新共享配额快照，即使客户端随后断开或流异常。
	s.UpdateCodexUsageSnapshotFromHeaders(ctx, account.ID, resp.Header)
	converted := bridge.StreamWithRepairs(requestCtx, resp.Body, func(repairCtx context.Context, failed map[string]any, validation error) (map[string]any, error) {

		correctedBody, err := basispoints.BuildToolRepairRequest(upstreamBody, failed, validation)
		if err != nil {
			return nil, err
		}
		repairReq, err := newExcelBPSRequest(repairCtx, correctedBody, token, accountID)
		if err != nil {
			return nil, err
		}
		repairResp, err := s.httpUpstream.Do(repairReq, proxyURL, account.ID, account.Concurrency)
		if err == nil && repairResp != nil && repairResp.Body != nil {
			repairResp.Body = basispoints.WithProgressTimeout(repairCtx, repairResp.Body, 2*time.Minute)
		}

		if err != nil {
			if repairCtx.Err() != nil {
				return nil, repairCtx.Err()
			}
			return nil, fmt.Errorf("excel BPS correction connection failed")
		}
		defer func() { _ = repairResp.Body.Close() }()
		stop := context.AfterFunc(repairCtx, func() { _ = repairResp.Body.Close() })
		defer stop()

		if repairResp.StatusCode < 200 || repairResp.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repairResp.Body, 512<<10))
			if repairResp.StatusCode == http.StatusTooManyRequests {
				// 请求已被接受，仅冷却路由，不重放生成。
				s.coolDownExcelBPS(repairCtx, account, repairResp.Header.Get("Retry-After"))
			}
			if isExcelBPSModelPermissionError(repairResp.StatusCode, gjson.GetBytes(raw, "error.code").String()) {
				s.coolDownExcelBPSModel(account, model)
			}
			if repairResp.StatusCode == http.StatusForbidden && !isExcelBPSModelPermissionError(http.StatusForbidden, gjson.GetBytes(raw, "error.code").String()) {
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS correction returned HTTP %d", repairResp.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repairResp.Header)
		upstreamBody = correctedBody
		return basispoints.ReadToolRepairResponse(repairResp.Body)
	}, func(repairCtx context.Context) (io.ReadCloser, error) {
		repairBody, err := basispoints.RepairRequest(upstreamBody)
		if err != nil {
			return nil, err
		}
		retry, err := newExcelBPSRequest(repairCtx, repairBody, token, accountID)
		if err != nil {
			return nil, err
		}

		repaired, err := s.httpUpstream.Do(retry, proxyURL, account.ID, account.Concurrency)
		if err == nil && repaired != nil && repaired.Body != nil {
			repaired.Body = basispoints.WithProgressTimeout(repairCtx, repaired.Body, 2*time.Minute)
		}

		if err != nil {
			return nil, fmt.Errorf("excel BPS tool correction transport failed")
		}

		if repaired.StatusCode < 200 || repaired.StatusCode >= 300 {
			raw, _ := io.ReadAll(io.LimitReader(repaired.Body, 512<<10))
			_ = repaired.Body.Close()
			if repaired.StatusCode == http.StatusTooManyRequests {
				s.coolDownExcelBPS(repairCtx, account, repaired.Header.Get("Retry-After"))
			}
			if isExcelBPSModelPermissionError(repaired.StatusCode, gjson.GetBytes(raw, "error.code").String()) {
				s.coolDownExcelBPSModel(account, model)
			}
			if repaired.StatusCode == http.StatusForbidden && !isExcelBPSModelPermissionError(http.StatusForbidden, gjson.GetBytes(raw, "error.code").String()) {
				s.disableExcelBPSOn403(repairCtx, account)
			}
			return nil, fmt.Errorf("excel BPS tool correction returned HTTP %d", repaired.StatusCode)
		}
		s.UpdateCodexUsageSnapshotFromHeaders(repairCtx, account.ID, repaired.Header)
		return repaired.Body, nil
	})
	if len(compactOutput) > 0 {
		converted = basispoints.WithCompactedWindow(requestCtx, converted, compactOutput, compactUsageWire)
	}
	defer func() { _ = converted.Close() }()
	var compat *excelBPSCompatOutput
	if len(formats) > 0 && formats[0] != "responses" {
		compat = newExcelBPSCompatOutput(formats[0], originalModel)
	}
	// 保留客户端原始推理强度用于展示，使用 BPS 归一化后的强度计费。
	requestedEffort := coalesceRequestedReasoningEffort(RequestedReasoningEffortFromContext(ctx), &bridge.RequestedEffort)
	result := &OpenAIForwardResult{Model: originalModel, UpstreamModel: model, UpstreamEndpoint: "/basispoints/api/responses", Stream: stream, ReasoningEffort: &bridge.Effort, RequestedReasoningEffort: requestedEffort, RequestID: resp.Header.Get("x-request-id")}
	defer func() {
		if cacheCreationAsInput := account.IsExcelBPSCacheCreationAsInputEnabled(); cacheCreationAsInput {
			result.Usage.CacheCreationInputTokens = 0
		}
	}()
	if stream {
		c.Header("Content-Type", "text/event-stream")
		c.Header("Cache-Control", "no-cache")
		c.Header("X-Accel-Buffering", "no")
	}
	scanner := bufio.NewScanner(converted)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	stopRead := context.AfterFunc(ctx, func() { _ = converted.Close(); _ = resp.Body.Close() })
	defer stopRead()
	var completed []byte
	var upstreamFailure *basispoints.UpstreamFailure
	terminal := ""
	cacheCreationAsInput := account.IsExcelBPSCacheCreationAsInputEnabled()
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := []byte(strings.TrimPrefix(line, "data: "))
			kind := gjson.GetBytes(payload, "type").String()
			s.parseSSEUsageBytes(payload, &result.Usage)
			if len(compactOutput) > 0 && (kind == "response.completed" || kind == "response.failed" || kind == "response.cancelled" || kind == "response.incomplete") {
				subtractImagePolicyUsage(&result.Usage, compactUsage)
			}
			if cacheCreationAsInput {
				payload, err = excelBPSDownstreamUsage(payload)
				if err != nil {
					return fail(http.StatusBadGateway, "basispoints_usage_invalid", "Excel BPS usage could not be normalized")
				}
				line = "data: " + string(payload)
			}
			if result.FirstTokenMs == nil && (kind == "response.output_text.delta" || kind == "response.output_item.added") {
				ms := int(time.Since(start).Milliseconds())
				result.FirstTokenMs = &ms
			}
			switch kind {
			case "response.completed", "response.failed", "response.cancelled", "response.incomplete", "error":

				terminal = kind
				completed = []byte(gjson.GetBytes(payload, "response").Raw)
				result.ResponseID = gjson.GetBytes(payload, "response.id").String()
				result.UpstreamResponseModel = gjson.GetBytes(payload, "response.model").String()
				upstreamFailure = basispoints.ParseUpstreamFailure(payload)
				if upstreamFailure != nil {
					// 分别记录实际 HTTP 状态与事件错误状态，不重放已被接受的生成。
					setOpsUpstreamError(c, resp.StatusCode, upstreamFailure.Message, "")
					MarkOpsStreamErrorValue(c, OpsStreamError{
						ErrType: upstreamFailure.Type, Code: upstreamFailure.Code, Message: upstreamFailure.Message,
						IntendedStatus: upstreamFailure.Status, CountTowardsSLA: true, NonStream: !stream,
					})
					if upstreamFailure.Status == http.StatusTooManyRequests {
						s.coolDownExcelBPS(ctx, account, resp.Header.Get("Retry-After"))
					}
				}
			}
		}
		if stream && compat != nil {
			if strings.HasPrefix(line, "data: ") {
				wire, convertErr := compat.event([]byte(strings.TrimPrefix(line, "data: ")))
				if convertErr != nil {
					return result, convertErr
				}
				if _, writeErr := c.Writer.WriteString(wire); writeErr != nil {
					result.ClientDisconnect = true
					return result, writeErr
				}
				c.Writer.Flush()
			}
			continue
		}
		if stream {
			if _, err = c.Writer.WriteString(line + "\n"); err != nil {
				result.ClientDisconnect = true

				result.Duration = time.Since(start)
				return result, err
			}
			if line == "" {
				c.Writer.Flush()
			}
		}
	}
	result.Duration = time.Since(start)
	result.UpstreamTerminalEvent = terminal
	if err = scanner.Err(); err != nil || terminal == "" {
		if ctx.Err() != nil {

			result.ClientDisconnect = true
			return result, ctx.Err()
		}
		// 断流不重放；客户端取消不惩罚账号。

		MarkOpsStreamError(c, "basispoints_stream_incomplete", "Excel BPS stream ended before completion", http.StatusBadGateway)
		MarkResponseCommitted(c)
		format := "responses"
		if len(formats) > 0 {
			format = formats[0]
		}
		writeExcelBPSOutputError(c, format, stream, 502, "basispoints_stream_incomplete", "Excel/BPS 上游流在完成前中断")
		return result, fmt.Errorf("excel BPS stream incomplete")
	}
	if terminal != "response.completed" {
		MarkResponseCommitted(c)
	}
	if !stream {
		if upstreamFailure != nil {
			format := "responses"
			if len(formats) > 0 {
				format = formats[0]
			}
			writeExcelBPSOutputError(c, format, false, upstreamFailure.Status, upstreamFailure.Code, upstreamFailure.Message)
		} else if terminal != "response.completed" {
			format := "responses"
			if len(formats) > 0 {
				format = formats[0]
			}
			writeExcelBPSOutputError(c, format, false, 502, "basispoints_protocol_error", "Excel/BPS 未完成响应")
		} else {
			if compat != nil {
				completed, err = compat.buffered(completed)
				if err != nil {
					return result, err
				}
			}
			c.Data(200, "application/json", completed)
		}
	}
	if terminal != "response.completed" {
		return result, fmt.Errorf("excel BPS terminal: %s", terminal)
	}
	imagePolicy.finish(ctx, imagePolicy.compact || len(compactOutput) > 0 || (imagePolicy.history != nil && imagePolicy.history.Count < imageSettings.Limits.MaxImages-imageSettings.WarningRemaining))
	s.bindHTTPResponseAccount(ctx, c, account, result.ResponseID)
	return result, nil
}

// 显式会话标识保持粘性；匿名请求只在本次内部重试中复用标识，不跨调用方共享。
func resolveExcelBPSIdentity(c *gin.Context, body []byte, apiKeyID int64, managed bool) (string, bool) {
	if identity, _ := resolveOpenAIWSExecutionScope(c, body, apiKeyID); identity != "" {
		return identity, false
	}
	if !managed {
		return "", false
	}
	const key = "excel_bps_transient_identity"
	if identity := c.GetString(key); identity != "" {
		return identity, true
	}
	identity := uuid.NewString()
	c.Set(key, identity)
	return identity, true
}
