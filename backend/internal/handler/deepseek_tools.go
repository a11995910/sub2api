package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// DeepSeekSearch 接入供应商独立搜索端点，不伪造一次模型请求。
func (h *OpenAIGatewayHandler) DeepSeekSearch(c *gin.Context) { h.deepSeekTool(c, "search") }

// DeepSeekFetch 接入供应商独立网页抓取端点。
func (h *OpenAIGatewayHandler) DeepSeekFetch(c *gin.Context) { h.deepSeekTool(c, "fetch") }

func (h *OpenAIGatewayHandler) deepSeekTool(c *gin.Context, kind string) {
	started := false
	defer h.recoverResponsesPanic(c, &started)
	setOpenAIClientTransportHTTP(c)
	start := time.Now()
	apiKey, ok := middleware2.GetAPIKeyFromContext(c)
	if !ok || apiKey == nil || apiKey.Group == nil {
		h.errorResponse(c, http.StatusUnauthorized, "authentication_error", "Invalid API key")
		return
	}
	if apiKey.Group.Platform != service.PlatformDeepseek {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		h.errorResponse(c, http.StatusNotFound, "not_found_error", "This endpoint requires a DeepSeek group")
		return
	}
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		h.errorResponse(c, http.StatusInternalServerError, "api_error", "User context not found")
		return
	}
	reqLog := requestLogger(c, "handler.deepseek.tools", zap.Int64("user_id", subject.UserID), zap.Int64("api_key_id", apiKey.ID))
	if !h.ensureResponsesDependencies(c, reqLog) {
		return
	}
	if err := service.ValidateDeepSeekToolPricing(apiKey.Group); err != nil {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalFeatureGate)
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	body, err := pkghttputil.ReadRequestBodyWithPrealloc(c.Request)
	if err != nil {
		if maxErr, ok := extractMaxBytesError(err); ok {
			h.errorResponse(c, http.StatusRequestEntityTooLarge, "invalid_request_error", buildBodyTooLargeMessage(maxErr.Limit))
		} else {
			h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		}
		return
	}
	body, err = service.ValidateDeepSeekToolRequest(c.Request.Context(), kind, body)
	if err != nil {
		h.errorResponse(c, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	model := "deepseek-tools-" + kind
	endpoint := "/v1/tools/" + kind
	setOpsRequestContext(c, model, false)
	setOpsEndpointContext(c, "", int16(service.RequestTypeSync))
	if decision := h.checkSecurityAudit(c, reqLog, apiKey, subject, "deepseek_tools", model, body); decision != nil && !decision.AllowNextStage {
		h.openAISecurityAuditError(c, decision)
		return
	}
	subscription, _ := middleware2.GetSubscriptionFromContext(c)
	userRelease, acquired := h.acquireResponsesUserSlot(c, subject.UserID, subject.Concurrency, false, &started, reqLog)
	if !acquired {
		return
	}
	if userRelease != nil {
		defer userRelease()
	}
	if err := h.billingCacheService.CheckBillingEligibility(c.Request.Context(), apiKey.User, apiKey, apiKey.Group, subscription, service.QuotaPlatform(c.Request.Context(), apiKey)); err != nil {
		h.deepSeekToolBillingError(c, err)
		return
	}
	done, err := reserveInflightBalance(c, h.billingCacheService, h.gatewayService, apiKey, subscription,
		service.InflightEstimateRequest{Kind: service.InflightEstimateDeepSeekTool, DeepSeekToolCalls: 1})
	if err != nil {
		h.deepSeekToolBillingError(c, err)
		return
	}
	defer done()
	service.SetOpsLatencyMs(c, service.OpsAuthLatencyMsKey, time.Since(start).Milliseconds())
	routingStart := time.Now()
	// 工具没有 model，使用平台+能力筛选；不能传伪造的模型名参与账号模型白名单。
	selection, _, err := h.gatewayService.SelectAccountWithSchedulerForCapability(c.Request.Context(), apiKey.GroupID,
		"", "", "", nil, service.OpenAIUpstreamTransportHTTPSSE, service.OpenAIEndpointCapabilityDeepSeekTools,
		false, false, false, service.PlatformDeepseek)
	if err != nil || selection == nil || selection.Account == nil {
		markOpsRoutingCapacityLimitedIfNoAvailable(c, err)
		h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available DeepSeek tool accounts")
		return
	}
	release, slotResult := h.acquireResponsesAccountSlot(c, apiKey.GroupID, "", selection, false, &started, reqLog)
	if slotResult != openAISlotAcquireOK {
		if slotResult == openAISlotAcquireProfitVetoed {
			h.errorResponse(c, http.StatusServiceUnavailable, "api_error", "No available DeepSeek tool accounts")
		}
		return
	}
	if release != nil {
		defer release()
	}
	account := selection.Account
	setOpsSelectedAccount(c, account.ID, account.Platform)
	service.SetActualOpenAIUpstreamEndpoint(c, endpoint)
	service.SetOpsLatencyMs(c, service.OpsRoutingLatencyMsKey, time.Since(routingStart).Milliseconds())
	forwardStart := time.Now()
	toolResult, err := h.gatewayService.ExecuteDeepSeekTool(c.Request.Context(), c, account, kind, body)
	service.SetOpsLatencyMs(c, service.OpsResponseLatencyMsKey, time.Since(forwardStart).Milliseconds())
	if err != nil {
		// 工具可能已经由供应商执行，传输失败后不自动重试，避免重复收费。
		h.gatewayService.ReportOpenAIAccountScheduleResult(account, "", false, nil, err)
		if !failoverClientGone(c) {
			var toolErr *service.DeepSeekToolError
			if errors.As(err, &toolErr) {
				h.errorResponse(c, toolErr.StatusCode, toolErr.Code, toolErr.Message)
			} else {
				h.errorResponse(c, http.StatusBadGateway, "upstream_error", "DeepSeek tool request failed")
			}
		}
		return
	}
	if toolResult == nil {
		h.errorResponse(c, http.StatusBadGateway, "upstream_error", "DeepSeek tool returned no response")
		return
	}
	h.gatewayService.ReportOpenAIAccountScheduleResult(account, "", true, nil)
	if toolResult.Successful {
		result := &service.OpenAIForwardResult{
			RequestID: toolResult.RequestID, UpstreamHeaders: toolResult.UpstreamHeaders,
			Model: model, UpstreamEndpoint: endpoint, Duration: time.Since(start),
			DeepSeekToolCalls: 1, DeepSeekToolOnly: true,
		}
		h.recordDeepSeekToolUsage(c, apiKey, account, subscription, body, result, endpoint)
	}
	c.Data(http.StatusOK, "application/json", toolResult.Body)
}

func (h *OpenAIGatewayHandler) deepSeekToolBillingError(c *gin.Context, err error) {
	status, code, message, retryAfter := billingErrorDetails(err)
	if retryAfter > 0 {
		c.Header("Retry-After", strconv.Itoa(retryAfter))
	}
	h.errorResponse(c, status, code, message)
}

func (h *OpenAIGatewayHandler) recordDeepSeekToolUsage(c *gin.Context, key *service.APIKey, account *service.Account,
	subscription *service.UserSubscription, body []byte, result *service.OpenAIForwardResult, endpoint string) {
	input := &service.OpenAIRecordUsageInput{
		Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription,
		InboundEndpoint: endpoint, UpstreamEndpoint: endpoint, UserAgent: c.GetHeader("User-Agent"),
		IPAddress: ip.GetClientIP(c), SessionID: service.ExtractClientSessionID(c),
		RequestPayloadHash: service.HashUsageRequestPayload(body), APIKeyService: h.apiKeyService,
		QuotaPlatform: service.QuotaPlatform(c.Request.Context(), key), PricingAt: time.Now(),
	}
	// 在写出结果之前提交不可丢弃的用量任务；客户端断开也结算已成功的上游调用。
	h.submitMandatoryUsageRecordTask(c.Request.Context(), func(ctx context.Context) {
		if err := h.gatewayService.RecordUsage(ctx, input); err != nil {
			logger.L().Error("deepseek_tool.record_usage_failed", zap.Int64("api_key_id", key.ID), zap.Int64("account_id", account.ID), zap.Error(err))
		}
	})
}
