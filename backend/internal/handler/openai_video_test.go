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
		openAIVideoHandlerAccountRepoStub{}, nil, nil, nil, nil, nil, cache, nil,
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
