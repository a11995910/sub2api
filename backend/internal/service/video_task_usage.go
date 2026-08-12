package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type VideoTaskUsageContext struct {
	InboundEndpoint    string
	UpstreamEndpoint   string
	UserAgent          string
	IPAddress          string
	SessionID          string
	RequestPayloadHash string
	QuotaPlatform      string
	PricingAt          time.Time
	ChannelUsageFields
}

type deferredOpenAIUsageRecorder interface {
	RecordUsage(ctx context.Context, input *OpenAIRecordUsageInput) error
}

type VideoTaskUsageService struct {
	recorder      deferredOpenAIUsageRecorder
	apiKeyRepo    APIKeyRepository
	userRepo      UserRepository
	accountRepo   AccountRepository
	apiKeyService APIKeyQuotaUpdater
}

func NewVideoTaskUsageService(
	recorder deferredOpenAIUsageRecorder,
	apiKeyRepo APIKeyRepository,
	userRepo UserRepository,
	accountRepo AccountRepository,
	apiKeyService APIKeyQuotaUpdater,
) *VideoTaskUsageService {
	return &VideoTaskUsageService{
		recorder: recorder, apiKeyRepo: apiKeyRepo, userRepo: userRepo, accountRepo: accountRepo, apiKeyService: apiKeyService,
	}
}

func (s *VideoTaskUsageService) RecordDeferredVideoUsage(ctx context.Context, task *VideoTaskBilling) error {
	if s == nil || s.recorder == nil || s.apiKeyRepo == nil || s.userRepo == nil || s.accountRepo == nil || task == nil {
		return errors.New("video task usage service is unavailable")
	}
	apiKey, err := s.apiKeyRepo.GetByID(ctx, task.APIKeyID)
	if err != nil {
		return err
	}
	user, err := s.userRepo.GetByID(ctx, task.UserID)
	if err != nil {
		return err
	}
	account, err := s.accountRepo.GetByID(ctx, task.AccountID)
	if err != nil {
		return err
	}
	if apiKey == nil || user == nil || account == nil || apiKey.ID != task.APIKeyID || apiKey.UserID != task.UserID || user.ID != task.UserID || account.ID != task.AccountID {
		return fmt.Errorf("video task billing ownership mismatch")
	}
	if task.GroupID != nil && (apiKey.GroupID == nil || *apiKey.GroupID != *task.GroupID) {
		return fmt.Errorf("video task billing group mismatch")
	}
	apiKey.User = user

	var usageContext VideoTaskUsageContext
	if len(task.UsageContextJSON) > 0 {
		if err := json.Unmarshal(task.UsageContextJSON, &usageContext); err != nil {
			return fmt.Errorf("decode video task usage context: %w", err)
		}
	}
	return s.recorder.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID:            fmt.Sprintf("video_task:%d:capture", task.ID),
			ResponseID:           task.UpstreamTaskID,
			Model:                task.Model,
			BillingModel:         task.Model,
			UpstreamModel:        task.UpstreamModel,
			UpstreamEndpoint:     usageContext.UpstreamEndpoint,
			VideoCount:           1,
			VideoResolution:      task.Resolution,
			VideoDurationSeconds: task.DurationSeconds,
			VideoInputImageCount: task.ReferenceImageCount,
			VideoStatus:          VideoTaskStatusCompleted,
			VideoResponseJSON:    append(json.RawMessage(nil), task.ResponseJSON...),
		},
		APIKey:             apiKey,
		User:               user,
		Account:            account,
		InboundEndpoint:    usageContext.InboundEndpoint,
		UpstreamEndpoint:   usageContext.UpstreamEndpoint,
		UserAgent:          usageContext.UserAgent,
		IPAddress:          usageContext.IPAddress,
		SessionID:          usageContext.SessionID,
		RequestPayloadHash: usageContext.RequestPayloadHash,
		APIKeyService:      s.apiKeyService,
		QuotaPlatform:      usageContext.QuotaPlatform,
		PricingAt:          usageContext.PricingAt,
		BalanceAlreadyHeld: true,
		ChannelUsageFields: usageContext.ChannelUsageFields,
	})
}
