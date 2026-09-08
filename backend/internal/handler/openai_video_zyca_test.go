//go:build unit

package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type zycaHandlerUpstream struct {
	service.HTTPUpstream
	body  string
	err   error
	calls int
}

func (u *zycaHandlerUpstream) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	if u.err != nil {
		return nil, u.err
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}

type zycaHandlerBillingRepo struct {
	service.VideoTaskBillingRepository
	task     *service.VideoTaskBilling
	released bool
	unknown  bool
}

func (r *zycaHandlerBillingRepo) ReserveAndCreate(_ context.Context, task *service.VideoTaskBilling) error {
	task.ID = 1
	r.task = task
	return nil
}

type zycaHandlerCostEstimator struct{ input service.VideoTaskReserveInput }

func (e *zycaHandlerCostEstimator) EstimateVideoCost(context.Context, *service.APIKey, string, string, int) (*service.CostBreakdown, error) {
	return &service.CostBreakdown{TotalCost: 2, ActualCost: 2}, nil
}

func (e *zycaHandlerCostEstimator) EstimateVideoAccountStats(_ context.Context, input service.VideoTaskReserveInput, _ float64) (*service.VideoAccountStatsSnapshot, error) {
	e.input = input
	cost := 1.3
	return &service.VideoAccountStatsSnapshot{Cost: &cost, RateMultiplier: 1}, nil
}

func TestZYCAReservationUsesFinalAccountMappedModelForCost(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "public-video", Resolution: "4K", DurationSeconds: 5})
	account := zycaHandlerAccount()
	account.Credentials["model_mapping"] = map[string]any{"channel-video": "minimax-h3"}
	repo, estimator := &zycaHandlerBillingRepo{}, &zycaHandlerCostEstimator{}
	h := &OpenAIGatewayHandler{videoTaskBilling: service.NewVideoTaskBillingService(repo, estimator, nil)}
	task, err := h.reserveOpenAIVideoTask(c, &service.APIKey{ID: 11, User: &service.User{ID: 7}}, account, nil,
		service.ChannelMappingResult{Mapped: true, MappedModel: "channel-video"}, "public-video", time.Now(), nil)
	require.NoError(t, err)
	require.Equal(t, "public-video", task.Model)
	require.Equal(t, "minimax-h3", task.UpstreamModel)
	require.Equal(t, "minimax-h3", estimator.input.UpstreamModel)
	require.Same(t, account, estimator.input.Account)
}

func (r *zycaHandlerBillingRepo) UpdatePoll(_ context.Context, _ int64, outcome service.VideoTaskOutcome, _ time.Time) (*service.VideoTaskBilling, error) {
	r.task.TaskStatus = outcome.Status
	return r.task, nil
}

func (r *zycaHandlerBillingRepo) Release(context.Context, int64, string) error {
	r.released = true
	r.task.BillingStatus = service.VideoTaskBillingReleased
	return nil
}

func (r *zycaHandlerBillingRepo) MarkSubmissionUnknown(context.Context, int64, string) error {
	r.unknown = true
	return nil
}

func zycaHandlerGateway(upstream service.HTTPUpstream, channels ...*service.ChannelService) *service.OpenAIGatewayService {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	var channelService *service.ChannelService
	if len(channels) > 0 {
		channelService = channels[0]
	}
	billing := service.NewBillingService(cfg, nil)
	return service.NewOpenAIGatewayService(
		nil, nil, nil, nil, nil, nil, nil, nil, cfg,
		nil, nil, billing, nil, nil, upstream,
		nil, nil, nil, service.NewModelPricingResolver(channelService, billing), channelService, nil, nil, nil,
	)
}

func TestZYCANonReservedVideoPricingSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name          string
		modelTest     bool
		missingTier   bool
		staleModel    bool
		customerPrice float64
	}{
		{name: "订阅固定原始模型售价", customerPrice: 0.5},
		{name: "模型测试固定原始模型售价", modelTest: true, customerPrice: 0.5},
		{name: "订阅保留显式零价"},
		{name: "重试保留入口模型售价", staleModel: true, customerPrice: 0.5},
		{name: "订阅成本缺档拒绝提交", missingTier: true, customerPrice: 0.5},
		{name: "模型测试成本缺档拒绝提交", modelTest: true, missingTier: true, customerPrice: 0.5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			groupID, upstreamPrice, accountRate := int64(10), 0.12, 0.5
			tier := "1080p"
			if tt.missingTier {
				tier = "768p"
			}
			channelService := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
				channels: []service.Channel{{ID: 1, Status: service.StatusActive, GroupIDs: []int64{groupID},
					AccountStatsPricingRules: []service.AccountStatsPricingRule{{AccountIDs: []int64{1}, Pricing: []service.ChannelModelPricing{{
						Models: []string{"minimax-h3"}, BillingMode: service.BillingModeVideo,
						Intervals: []service.PricingInterval{{TierLabel: tier, PerRequestPrice: &upstreamPrice}},
					}}}},
				}},
				groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
			}, nil, nil, nil, nil)
			gateway := zycaHandlerGateway(nil, channelService)
			account := zycaHandlerAccount()
			account.RateMultiplier = &accountRate
			account.Credentials["model_mapping"] = map[string]any{"channel-video": "minimax-h3"}
			key := &service.APIKey{ID: 11, User: &service.User{ID: 7}, GroupID: &groupID, Group: &service.Group{
				ID: groupID, SubscriptionType: service.SubscriptionTypeSubscription, RateMultiplier: 1,
				VideoModelPrices: map[string]map[string]float64{"public-video": {"1080p": tt.customerPrice}},
				// 映射模型的 token 价不能绕过 ZYCA 的每秒售价和成本校验。
				ModelPricing: []service.ChannelModelPricing{{Models: []string{"channel-video"}, BillingMode: service.BillingModeToken,
					InputPrice: &upstreamPrice, OutputPrice: &upstreamPrice}},
			}}
			var subscription *service.UserSubscription
			if !tt.modelTest {
				subscription = &service.UserSubscription{ID: 3}
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			contextModel := "public-video"
			if tt.staleModel {
				contextModel = "channel-video"
			}
			service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{
				Model: contextModel, Resolution: "1080p", DurationSeconds: 5, RecordModelTestTask: tt.modelTest,
			})
			repo := &zycaHandlerBillingRepo{}
			h := &OpenAIGatewayHandler{gatewayService: gateway, videoTaskBilling: service.NewVideoTaskBillingService(repo, gateway, nil)}
			task, err := h.reserveOpenAIVideoTask(c, key, account, subscription, service.ChannelMappingResult{
				Mapped: true, MappedModel: "channel-video", BillingModelSource: service.BillingModelSourceChannelMapped,
			}, "public-video", time.Now(), nil)
			require.Nil(t, task)
			require.Nil(t, repo.task, "非预留路径不冻结余额")
			meta, _ := service.OpenAIVideoContextFromGin(c)
			if tt.missingTier {
				require.ErrorContains(t, err, "每秒成本")
				require.Nil(t, meta.CostSnapshot)
				return
			}
			require.NoError(t, err)
			key.Group.VideoModelPrices["public-video"]["1080p"] = 9
			accountRate = 8
			require.NotNil(t, meta.CostSnapshot)
			require.InDelta(t, tt.customerPrice*5, meta.CostSnapshot.TotalCost, 1e-9)
			require.InDelta(t, tt.customerPrice*5, meta.CostSnapshot.ActualCost, 1e-9)
			require.NotNil(t, meta.AccountStatsSnapshot)
			require.InDelta(t, 0.6, *meta.AccountStatsSnapshot.Cost, 1e-9)
			require.Equal(t, 0.5, meta.AccountStatsSnapshot.RateMultiplier)
		})
	}
}

func zycaHandlerAccount() *service.Account {
	return &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey,
		Credentials: map[string]any{"base_url": "https://api.zyca.top", "api_key": "test-key", "video_request_profile": "zyca",
			"model_mapping": map[string]any{"public-video": "minimax-h3"}}}
}

func TestZYCASubmissionRejectionReleasesReservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, body string
		err        error
		released   bool
	}{
		{name: "明确拒绝", body: `{"success":false,"message":"参数校验失败"}`, released: true},
		{name: "缺少成功标志", body: `{"message":"参数校验失败"}`},
		{name: "空成功标志", body: `{"success":null,"message":"参数校验失败"}`},
		{name: "错误成功标志类型", body: `{"success":"false","message":"参数校验失败"}`},
		{name: "拒绝但带任务 ID", body: `{"success":false,"data":{"id":"gen-1"}}`},
		{name: "无效响应", body: `not-json`},
		{name: "超时", err: context.DeadlineExceeded},
		{name: "断连", err: io.ErrUnexpectedEOF},
	} {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &zycaHandlerUpstream{body: tt.body, err: tt.err}
			gateway := zycaHandlerGateway(upstream)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			body := []byte(`{"model":"public-video","prompt":"视频","duration":4,"resolution":"1080p"}`)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(string(body)))
			_, err := gateway.ForwardOpenAIVideoCreate(context.Background(), c, zycaHandlerAccount(), body, "")
			require.Error(t, err)
			require.Equal(t, !tt.released, service.IsVideoTaskSubmissionUncertain(err))
			require.Equal(t, 1, upstream.calls)
			task := &service.VideoTaskBilling{ID: 1, UserID: 1, EstimatedCost: 1, TaskStatus: service.VideoTaskStatusSubmitting, BillingStatus: service.VideoTaskBillingReserved}
			repo := &zycaHandlerBillingRepo{task: task}
			h := &OpenAIGatewayHandler{videoTaskBilling: service.NewVideoTaskBillingService(repo, nil, nil)}
			require.NoError(t, h.observeOpenAIVideoSubmissionError(c, task, err))
			require.Equal(t, tt.released, repo.released)
			require.Equal(t, !tt.released, repo.unknown)
			if tt.released {
				require.Contains(t, recorder.Body.String(), "参数校验失败")
			}
		})
	}
}

func TestZYCAPriceValidationUsesOriginalModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name, forwardedModel, pricedModel, resolution string
		price                                         float64
		modelTest, subscription, valid                bool
	}{
		{name: "渠道模型别名", forwardedModel: "minimax-h3", pricedModel: "public-video", resolution: "4K", price: 0.2, valid: true},
		{name: "账号模型别名", forwardedModel: "public-video", pricedModel: "public-video", resolution: "4K", price: 0.2, valid: true},
		{name: "不能借用映射模型价格", forwardedModel: "minimax-h3", pricedModel: "minimax-h3", resolution: "4K", price: 0.2},
		{name: "常规档位缺价", forwardedModel: "minimax-h3", resolution: "1080p"},
		{name: "模型测试缺价", forwardedModel: "minimax-h3", resolution: "1080p", modelTest: true},
		{name: "订阅缺价", forwardedModel: "minimax-h3", resolution: "1080p", subscription: true},
		{name: "显式零价", forwardedModel: "minimax-h3", pricedModel: "public-video", resolution: "1080p", valid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gateway := zycaHandlerGateway(nil)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
			group := &service.Group{VideoModelPrices: map[string]map[string]float64{}}
			if tt.pricedModel != "" {
				group.VideoModelPrices[tt.pricedModel] = map[string]float64{tt.resolution: tt.price}
			}
			if tt.subscription {
				group.SubscriptionType = service.SubscriptionTypeSubscription
			}
			key := &service.APIKey{Group: group}
			c.Set(string(middleware2.ContextKeyAPIKey), key)
			service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "public-video", RecordModelTestTask: tt.modelTest})
			h := &OpenAIGatewayHandler{gatewayService: gateway}
			body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"视频","duration":4,"resolution":%q,"reference_image_urls":["https://cdn.example/ref.png"]}`, tt.forwardedModel, tt.resolution))
			require.Equal(t, tt.valid, h.validateOpenAIVideoRequestForAccount(c, zycaHandlerAccount(), body, false))
			if tt.valid {
				meta, _ := service.OpenAIVideoContextFromGin(c)
				require.Equal(t, "public-video", meta.Model)
				cost, err := gateway.EstimateVideoCost(context.Background(), key, meta.Model, meta.Resolution, meta.DurationSeconds)
				require.NoError(t, err)
				require.InDelta(t, tt.price*4, cost.TotalCost, 1e-9)
			} else {
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				require.Contains(t, recorder.Body.String(), "每秒价格")
			}
		})
	}
}
