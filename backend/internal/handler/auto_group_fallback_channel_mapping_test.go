package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type autoFallbackHandlerGroupRepo struct {
	service.GroupRepository
	groups map[int64]*service.Group
}

func (r *autoFallbackHandlerGroupRepo) GetByID(_ context.Context, id int64) (*service.Group, error) {
	return r.GetByIDLite(context.Background(), id)
}

func (r *autoFallbackHandlerGroupRepo) GetByIDLite(_ context.Context, id int64) (*service.Group, error) {
	group, ok := r.groups[id]
	if !ok {
		return nil, service.ErrGroupNotFound
	}
	return group, nil
}

type autoFallbackHandlerAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

type autoFallbackHandlerUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *autoFallbackHandlerUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	if r.user != nil && r.user.ID == id {
		return r.user, nil
	}
	return nil, service.ErrUserNotFound
}

func (r *autoFallbackHandlerAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			return &r.accounts[i], nil
		}
	}
	return nil, service.ErrAccountNotFound
}

func (r *autoFallbackHandlerAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]service.Account, error) {
	accounts := make([]service.Account, 0)
	for _, account := range r.accounts {
		if account.Platform == platform && autoFallbackHandlerAccountInGroup(account, groupID) {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (r *autoFallbackHandlerAccountRepo) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, _ bool) ([]service.Account, error) {
	if groupID == nil {
		return nil, nil
	}
	accounts := make([]service.Account, 0)
	for _, account := range r.accounts {
		if !autoFallbackHandlerAccountInGroup(account, *groupID) {
			continue
		}
		for _, platform := range platforms {
			if account.Platform == platform {
				accounts = append(accounts, account)
				break
			}
		}
	}
	return accounts, nil
}

func autoFallbackHandlerAccountInGroup(account service.Account, groupID int64) bool {
	for _, relation := range account.AccountGroups {
		if relation.GroupID == groupID {
			return true
		}
	}
	return false
}

type autoFallbackHandlerChannelRepo struct {
	service.ChannelRepository
	channels       []service.Channel
	groupPlatforms map[int64]string
}

func (r *autoFallbackHandlerChannelRepo) ListAll(_ context.Context) ([]service.Channel, error) {
	return r.channels, nil
}

func (r *autoFallbackHandlerChannelRepo) GetGroupPlatforms(_ context.Context, groupIDs []int64) (map[int64]string, error) {
	platforms := make(map[int64]string, len(groupIDs))
	for _, groupID := range groupIDs {
		if platform := strings.TrimSpace(r.groupPlatforms[groupID]); platform != "" {
			platforms[groupID] = platform
		}
	}
	return platforms, nil
}

type autoFallbackHandlerUpstream struct {
	service.HTTPUpstream
	mu   sync.Mutex
	body []byte
}

func (u *autoFallbackHandlerUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.mu.Lock()
	u.body = append([]byte(nil), body...)
	u.mu.Unlock()
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body: io.NopCloser(strings.NewReader(`{"id":"resp_fallback_mapping","object":"response","model":"pro-upstream-model","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}, nil
}

func (u *autoFallbackHandlerUpstream) requestBody() []byte {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]byte(nil), u.body...)
}

func TestOpenAIResponsesAutoGroupFallbackUsesTargetChannelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	plusID := int64(7200)
	proID := int64(7300)
	plus := &service.Group{
		ID:                  plusID,
		Name:                "plus",
		Platform:            service.PlatformOpenAI,
		Status:              service.StatusActive,
		SubscriptionType:    service.SubscriptionTypeStandard,
		RateMultiplier:      0.12,
		AutoFallbackGroupID: &proID,
		Hydrated:            true,
	}
	pro := &service.Group{
		ID:               proID,
		Name:             "pro",
		Platform:         service.PlatformOpenAI,
		Status:           service.StatusActive,
		SubscriptionType: service.SubscriptionTypeStandard,
		RateMultiplier:   0.18,
		Hydrated:         true,
	}
	groupRepo := &autoFallbackHandlerGroupRepo{groups: map[int64]*service.Group{plusID: plus, proID: pro}}
	cooldownUntil := time.Now().Add(time.Hour)
	accountRepo := &autoFallbackHandlerAccountRepo{accounts: []service.Account{
		{
			ID:               7201,
			Name:             "plus-unavailable",
			Platform:         service.PlatformOpenAI,
			Type:             service.AccountTypeAPIKey,
			Status:           service.StatusActive,
			Schedulable:      true,
			Concurrency:      1,
			RateLimitResetAt: &cooldownUntil,
			AccountGroups:    []service.AccountGroup{{GroupID: plusID}},
			Credentials:      map[string]any{"api_key": "sk-plus", "base_url": "https://plus.example.test"},
			Extra:            map[string]any{"openai_passthrough": true},
		},
		{
			ID:            7301,
			Name:          "pro-available",
			Platform:      service.PlatformOpenAI,
			Type:          service.AccountTypeAPIKey,
			Status:        service.StatusActive,
			Schedulable:   true,
			Concurrency:   1,
			AccountGroups: []service.AccountGroup{{GroupID: proID}},
			Credentials:   map[string]any{"api_key": "sk-pro", "base_url": "https://pro.example.test"},
			Extra:         map[string]any{"openai_passthrough": true},
		},
	}}
	channelService := service.NewChannelService(&autoFallbackHandlerChannelRepo{
		channels: []service.Channel{
			{
				ID:           7202,
				Name:         "plus-channel",
				Status:       service.StatusActive,
				GroupIDs:     []int64{plusID},
				ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"gpt-5.6-sol": "plus-upstream-model"}},
			},
			{
				ID:           7302,
				Name:         "pro-channel",
				Status:       service.StatusActive,
				GroupIDs:     []int64{proID},
				ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"gpt-5.6-sol": "pro-upstream-model"}},
			},
		},
		groupPlatforms: map[int64]string{plusID: service.PlatformOpenAI, proID: service.PlatformOpenAI},
	}, groupRepo, nil, nil)

	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	upstream := &autoFallbackHandlerUpstream{}
	user := &service.User{ID: 9002, Status: service.StatusActive, Balance: 10}
	userRepo := &autoFallbackHandlerUserRepo{user: user}
	billingCacheService := service.NewBillingCacheService(nil, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingCacheService.Stop)
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		groupRepo,
		nil,
		nil,
		userRepo,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		service.NewBillingService(cfg, nil),
		nil,
		billingCacheService,
		upstream,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		channelService,
		nil,
		nil,
		nil,
	)
	concurrencyService := service.NewConcurrencyService(&concurrencyCacheMock{
		acquireUserSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
		acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) {
			return true, nil
		},
	})
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		concurrencyService,
		billingCacheService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	apiKey := &service.APIKey{
		ID:                       9001,
		GroupID:                  &plusID,
		Group:                    plus,
		User:                     user,
		AutoGroupFallbackEnabled: true,
	}
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/responses", bytes.NewBufferString(`{"model":"gpt-5.6-sol","input":"hello","stream":false}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(service.WithAutoGroupFallbackState(req.Context(), apiKey))
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: apiKey.User.ID, Concurrency: 0})

	handler.Responses(c)

	upstreamBody := upstream.requestBody()
	require.Equal(t, "pro-upstream-model", gjson.GetBytes(upstreamBody, "model").String(), "status=%d response=%s upstream=%s", recorder.Code, recorder.Body.String(), string(upstreamBody))
	require.NotEqual(t, "plus-upstream-model", gjson.GetBytes(upstreamBody, "model").String())
	require.Equal(t, proID, *apiKey.GroupID)
	require.Same(t, pro, apiKey.Group)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
}
