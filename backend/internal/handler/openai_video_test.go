//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type openAIVideoHandlerCacheStub struct {
	service.GatewayCache
	values map[string]int64
}

func (s *openAIVideoHandlerCacheStub) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	accountID, ok := s.values[sessionHash]
	if !ok {
		return 0, redis.Nil
	}
	return accountID, nil
}

func (s *openAIVideoHandlerCacheStub) SetSessionAccountID(_ context.Context, _ int64, sessionHash string, accountID int64, _ time.Duration) error {
	if s.values == nil {
		s.values = make(map[string]int64)
	}
	s.values[sessionHash] = accountID
	return nil
}

func (s *openAIVideoHandlerCacheStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *openAIVideoHandlerCacheStub) DeleteSessionAccountID(context.Context, int64, string) error {
	return nil
}

type openAIVideoHandlerAccountRepoStub struct {
	service.AccountRepository
	account *service.Account
}

func (s openAIVideoHandlerAccountRepoStub) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if s.account != nil && s.account.ID == id {
		return s.account, nil
	}
	return nil, redis.Nil
}

func TestOpenAIVideoLookupHidesOwnershipMismatchAsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(7)
	cache := &openAIVideoHandlerCacheStub{}
	gatewayService := service.NewOpenAIGatewayService(
		openAIVideoHandlerAccountRepoStub{}, nil, nil, nil, nil, nil, nil, cache, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	require.NoError(t, gatewayService.BindVideoTaskAccount(context.Background(), &groupID, "task-1", 10, 20, 30))
	h := &OpenAIGatewayHandler{gatewayService: gatewayService}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task-1", nil)
	c.Params = gin.Params{{Key: "task_id", Value: "task-1"}}
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 20, GroupID: &groupID})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 11})

	h.OpenAIVideoStatus(c)

	require.Equal(t, http.StatusNotFound, recorder.Code)
	require.Contains(t, recorder.Body.String(), "Video task not found")
}

func TestIsModelTestVideoRequestRequiresExactInternalMarker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{value: "video", want: true},
		{value: " video ", want: true},
		{value: "VIDEO", want: false},
		{value: "true", want: false},
		{value: "", want: false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		c.Request.Header.Set("X-Sub2API-Model-Test", tc.value)
		require.Equal(t, tc.want, isModelTestVideoRequest(c), tc.value)
	}
}

func TestShouldReserveOpenAIVideoBillingOnlyForOrdinaryBalanceRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(7)
	apiKey := &service.APIKey{ID: 20, GroupID: &groupID, Group: &service.Group{ID: groupID}}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "video-model"})

	require.True(t, shouldReserveOpenAIVideoBilling(c, apiKey, nil))

	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "video-model", RecordModelTestTask: true})
	require.False(t, shouldReserveOpenAIVideoBilling(c, apiKey, nil))

	apiKey.Group.SubscriptionType = service.SubscriptionTypeSubscription
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "video-model"})
	require.False(t, shouldReserveOpenAIVideoBilling(c, apiKey, &service.UserSubscription{}))
}

func TestValidateOpenAIVideoRequestForAccountRejectsUnifiedUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "video-model"})
	account := &service.Account{
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://ai.cangyuansuanli.cn/v1",
		},
	}
	h := &OpenAIGatewayHandler{}

	valid := h.validateOpenAIVideoRequestForAccount(c, account, []byte(`{"model":"video-model","prompt":"x","watermark":true}`), false)

	require.False(t, valid)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "invalid_request_error")
	require.Contains(t, recorder.Body.String(), `unsupported video field \"watermark\"`)
}

func TestValidateOpenAIVideoRequestForAccountKeepsLegacyExtensions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "video-model"})
	account := &service.Account{
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://video.example.com/v1",
		},
	}
	h := &OpenAIGatewayHandler{}

	valid := h.validateOpenAIVideoRequestForAccount(c, account, []byte(`{"model":"video-model","prompt":"x","watermark":true}`), false)

	require.True(t, valid)
	require.Zero(t, recorder.Body.Len())
}

func TestValidateOpenAIVideoRequestForAccountUpdatesBillingContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{
		Model: "public-video", Resolution: "", DurationSeconds: 30,
	})
	account := &service.Account{
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://ai.cangyuansuanli.cn/v1",
			"model_mapping": map[string]any{
				"public-video": "sd4-seedance-2.5-480p",
			},
		},
	}
	h := &OpenAIGatewayHandler{}

	valid := h.validateOpenAIVideoRequestForAccount(c, account, []byte(`{
		"model":"public-video","prompt":"x","duration":30,"resolution":"720p"
	}`), false)

	require.True(t, valid)
	meta, ok := service.OpenAIVideoContextFromGin(c)
	require.True(t, ok)
	require.Equal(t, "480p", meta.Resolution)
	require.Equal(t, 30, meta.DurationSeconds)
	require.Zero(t, recorder.Body.Len())
}

func TestValidateOpenAIVideoRequestForAccountKeepsLegacyDurationLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	service.SetOpenAIVideoContext(c, service.OpenAIVideoContext{Model: "video-model"})
	account := &service.Account{
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"base_url": "https://video.example.com/v1",
		},
	}
	h := &OpenAIGatewayHandler{}

	valid := h.validateOpenAIVideoRequestForAccount(c, account, []byte(`{
		"model":"video-model","prompt":"x","duration":16
	}`), false)

	require.False(t, valid)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "duration must not exceed 15 seconds")
}
