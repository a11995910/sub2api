//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type videoTokenUsageRepo struct {
	service.UsageLogRepository
	log *service.UsageLog
}

func (r *videoTokenUsageRepo) Create(_ context.Context, log *service.UsageLog) (bool, error) {
	r.log = log
	return true, nil
}

type videoTokenBillingRepo struct {
	service.UsageBillingRepository
	commands []*service.UsageBillingCommand
}

func (r *videoTokenBillingRepo) Apply(_ context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	r.commands = append(r.commands, cmd)
	return &service.UsageBillingApplyResult{Applied: true}, nil
}

func TestOpenAIVideoNonReservedTokenBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, original, mapped, upstream, pricedModel, source string
		modelTest, noReservationService, staleSnapshot        bool
	}{
		{name: "原模型按token", original: "legacy-video", upstream: "legacy-video", pricedModel: "legacy-video"},
		{name: "渠道映射按token", original: "public-video", mapped: "channel-video", upstream: "provider-video", pricedModel: "channel-video", source: service.BillingModelSourceChannelMapped},
		{name: "显式按请求模型计费", original: "public-video", mapped: "channel-video", upstream: "provider-video", pricedModel: "public-video", source: service.BillingModelSourceRequested},
		{name: "保留Grok候选规则", original: "public-video", mapped: "grok-imagine-video", upstream: "provider-video", pricedModel: "grok-imagine-video"},
		{name: "模型测试按token", original: "legacy-video", upstream: "legacy-video", pricedModel: "legacy-video", modelTest: true},
		{name: "未装配预留服务", original: "legacy-video", upstream: "legacy-video", pricedModel: "legacy-video", noReservationService: true},
		{name: "切换账号清除旧报价", original: "legacy-video", upstream: "legacy-video", pricedModel: "legacy-video", staleSnapshot: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Default.RateMultiplier = 1
			usageRepo, billingRepo := &videoTokenUsageRepo{}, &videoTokenBillingRepo{}
			billing := service.NewBillingService(cfg, nil)
			gateway := service.NewOpenAIGatewayService(
				nil, nil, usageRepo, billingRepo, nil, nil, nil, nil, cfg,
				nil, nil, billing, nil, &service.BillingCacheService{}, nil,
				&service.DeferredService{}, nil, nil, service.NewModelPricingResolver(nil, billing), nil, nil, nil, nil,
			)
			groupID, inputPrice, outputPrice := int64(10), 0.001, 0.002
			key := &service.APIKey{ID: 11, User: &service.User{ID: 7}, GroupID: &groupID, Group: &service.Group{
				ID: groupID, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1,
				ModelPricing:     []service.ChannelModelPricing{{Models: []string{tt.pricedModel}, BillingMode: service.BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}},
				VideoModelPrices: map[string]map[string]float64{tt.original: {"720p": 9}},
			}}
			forwardModel := tt.original
			if tt.mapped != "" {
				forwardModel = tt.mapped
			}
			account := &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"model_mapping": map[string]any{forwardModel: tt.upstream}}}
			mapping := service.ChannelMappingResult{Mapped: tt.mapped != "", MappedModel: tt.mapped, BillingModelSource: tt.source}
			subscription := &service.UserSubscription{ID: 3}
			if tt.modelTest || tt.noReservationService {
				subscription = nil
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			meta := service.OpenAIVideoContext{Model: tt.original, Resolution: "720p", DurationSeconds: 5, RecordModelTestTask: tt.modelTest}
			if tt.staleSnapshot {
				meta.CostSnapshot = &service.CostBreakdown{TotalCost: 45, ActualCost: 45}
				meta.AccountStatsSnapshot = &service.VideoAccountStatsSnapshot{Cost: &inputPrice, RateMultiplier: 8}
			}
			service.SetOpenAIVideoContext(c, meta)
			h := &OpenAIGatewayHandler{gatewayService: gateway}
			if !tt.noReservationService {
				// 误入预留分支会调用空仓库，测试应直接失败。
				h.videoTaskBilling = service.NewVideoTaskBillingService(nil, gateway, nil)
			}
			task, err := h.reserveOpenAIVideoTask(c, key, account, subscription, mapping, tt.original, time.Now(), nil)
			require.NoError(t, err)
			require.Nil(t, task)
			meta, _ = service.OpenAIVideoContextFromGin(c)
			require.Nil(t, meta.CostSnapshot)
			require.Nil(t, meta.AccountStatsSnapshot)
			result := &service.OpenAIForwardResult{RequestID: "token-video", Model: forwardModel, BillingModel: forwardModel, UpstreamModel: tt.upstream,
				VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 5, Usage: service.OpenAIUsage{InputTokens: 100, OutputTokens: 200}}
			err = gateway.RecordUsage(context.Background(), &service.OpenAIRecordUsageInput{
				Result: result, APIKey: key, User: key.User, Account: account, Subscription: subscription,
				ChannelUsageFields: clientRequestedUsageFields(c, mapping, tt.original, tt.upstream),
				PrecalculatedCost:  meta.CostSnapshot, VideoAccountStatsSnapshot: meta.AccountStatsSnapshot,
			})
			require.NoError(t, err)
			require.Len(t, billingRepo.commands, 1)
			cmd := billingRepo.commands[0]
			if subscription != nil {
				require.InDelta(t, 0.5, cmd.SubscriptionCost, 1e-9)
				require.Zero(t, cmd.BalanceCost)
			} else {
				require.InDelta(t, 0.5, cmd.BalanceCost, 1e-9)
				require.Zero(t, cmd.SubscriptionCost)
			}
			require.NotNil(t, usageRepo.log)
			require.Equal(t, string(service.BillingModeToken), *usageRepo.log.BillingMode)
			require.InDelta(t, 0.5, usageRepo.log.ActualCost, 1e-9)
			require.Equal(t, 1, usageRepo.log.VideoCount)
			require.Equal(t, tt.original, usageRepo.log.RequestedModel)
		})
	}
}
