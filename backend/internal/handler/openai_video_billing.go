package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func (h *OpenAIGatewayHandler) reserveOpenAIVideoTask(
	c *gin.Context,
	apiKey *service.APIKey,
	account *service.Account,
	subscription *service.UserSubscription,
	mapping service.ChannelMappingResult,
	model string,
	pricingAt time.Time,
	body []byte,
) (*service.VideoTaskBilling, error) {
	if h == nil || h.videoTaskBilling == nil || !shouldReserveOpenAIVideoBilling(c, apiKey, subscription) {
		return nil, nil
	}
	meta, ok := service.OpenAIVideoContextFromGin(c)
	if !ok || account == nil || apiKey == nil || apiKey.User == nil {
		return nil, errors.New("video task billing context is incomplete")
	}
	requestID, _ := c.Request.Context().Value(ctxkey.RequestID).(string)
	if requestID = strings.TrimSpace(requestID); requestID == "" {
		requestID = uuid.NewString()
	}
	upstreamModel := strings.TrimSpace(mapping.MappedModel)
	if upstreamModel == "" {
		upstreamModel = account.GetMappedModel(model)
	}
	usageContext, err := json.Marshal(service.VideoTaskUsageContext{
		InboundEndpoint:    GetInboundEndpoint(c),
		UpstreamEndpoint:   GetUpstreamEndpoint(c, account.Platform),
		UserAgent:          c.GetHeader("User-Agent"),
		IPAddress:          ip.GetClientIP(c),
		SessionID:          service.ExtractClientSessionID(c),
		RequestPayloadHash: service.HashUsageRequestPayload(body),
		QuotaPlatform:      service.QuotaPlatform(c.Request.Context(), apiKey),
		PricingAt:          pricingAt,
		ChannelUsageFields: clientRequestedUsageFields(c, mapping, model, upstreamModel),
	})
	if err != nil {
		return nil, err
	}
	task, err := h.videoTaskBilling.Reserve(c.Request.Context(), service.VideoTaskReserveInput{
		RequestID:           requestID,
		Platform:            account.Platform,
		UserID:              apiKey.User.ID,
		APIKeyID:            apiKey.ID,
		GroupID:             apiKey.GroupID,
		AccountID:           account.ID,
		APIKey:              apiKey,
		Model:               model,
		UpstreamModel:       upstreamModel,
		Resolution:          meta.Resolution,
		DurationSeconds:     meta.DurationSeconds,
		ReferenceImageCount: meta.ReferenceImageCount,
		UsageContextJSON:    usageContext,
	})
	if err != nil {
		return nil, err
	}
	meta.AccountID = account.ID
	meta.BillingTaskID = task.ID
	service.SetOpenAIVideoContext(c, meta)
	return task, nil
}

func (h *OpenAIGatewayHandler) observeOpenAIVideoCreated(c *gin.Context, task *service.VideoTaskBilling, result *service.OpenAIForwardResult) error {
	if task == nil || h == nil || h.videoTaskBilling == nil {
		return nil
	}
	if result == nil || strings.TrimSpace(result.ResponseID) == "" {
		return h.markOpenAIVideoSubmissionUnknown(c, task, errors.New("video upstream response did not include task_id"))
	}
	ctx, cancel := detachedVideoBillingContext(c)
	defer cancel()
	_, err := h.videoTaskBilling.ObserveCreated(ctx, task, strings.TrimSpace(result.ResponseID), result)
	return err
}

func (h *OpenAIGatewayHandler) observeOpenAIVideoSubmissionError(c *gin.Context, task *service.VideoTaskBilling, cause error) error {
	if task == nil || h == nil || h.videoTaskBilling == nil {
		return nil
	}
	if service.IsVideoTaskSubmissionUncertain(cause) {
		return h.markOpenAIVideoSubmissionUnknown(c, task, cause)
	}
	ctx, cancel := detachedVideoBillingContext(c)
	defer cancel()
	return h.videoTaskBilling.ApplyOutcome(ctx, task, service.VideoTaskOutcome{
		Status: service.VideoTaskStatusFailed, ErrorMessage: cause.Error(),
	})
}

func (h *OpenAIGatewayHandler) markOpenAIVideoSubmissionUnknown(c *gin.Context, task *service.VideoTaskBilling, cause error) error {
	ctx, cancel := detachedVideoBillingContext(c)
	defer cancel()
	return h.videoTaskBilling.MarkSubmissionUnknown(ctx, task, cause)
}

func detachedVideoBillingContext(c *gin.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if c != nil && c.Request != nil {
		base = context.WithoutCancel(c.Request.Context())
	}
	return context.WithTimeout(base, 10*time.Second)
}
