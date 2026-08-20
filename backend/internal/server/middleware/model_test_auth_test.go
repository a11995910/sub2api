//go:build unit

package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type modelTestAuthUserRepo struct {
	service.UserRepository
	users map[int64]*service.User
}

func (r *modelTestAuthUserRepo) GetByID(_ context.Context, id int64) (*service.User, error) {
	user, ok := r.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	copy := *user
	return &copy, nil
}

func (r *modelTestAuthUserRepo) GetUserAvatar(_ context.Context, _ int64) (*service.UserAvatar, error) {
	return nil, nil
}

func newModelTestAuthEnv(t *testing.T, keyUser, tokenUser *service.User) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	cfg.JWT.Secret = "model-test-jwt-secret-32bytes-long"
	cfg.JWT.AccessTokenExpireMinutes = 60
	userRepo := &modelTestAuthUserRepo{users: map[int64]*service.User{tokenUser.ID: tokenUser}}
	authService := service.NewAuthService(nil, userRepo, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	userService := service.NewUserService(userRepo, nil, nil, nil)
	token, err := authService.GenerateToken(context.Background(), tokenUser)
	require.NoError(t, err)

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(ContextKeyAPIKey), &service.APIKey{ID: 9, UserID: keyUser.ID, User: keyUser})
		c.Next()
	})
	router.Use(gin.HandlerFunc(NewModelTestAuthMiddleware(authService, userService, nil, nil)))
	router.POST("/v1/chat/completions", func(c *gin.Context) {
		mode, ok := service.TrustedModelTestMode(c)
		c.JSON(http.StatusOK, gin.H{"trusted": ok, "mode": mode})
	})
	return router, token
}

func TestModelTestAuthMiddlewareValidatesSessionAndAPIKeyOwnership(t *testing.T) {
	user := &service.User{ID: 7, Email: "user@example.com", Role: "user", Status: service.StatusActive, TokenVersion: 3}
	router, token := newModelTestAuthEnv(t, user, user)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(ModelTestRequestHeader, service.ModelTestModeText)
	req.Header.Set(ModelTestAuthorizationHeader, "Bearer "+token)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"trusted":true,"mode":"text"}`, recorder.Body.String())
}

func TestModelTestAuthMiddlewareRejectsRawMarkerWithoutSession(t *testing.T) {
	user := &service.User{ID: 7, Status: service.StatusActive}
	router, _ := newModelTestAuthEnv(t, user, user)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(ModelTestRequestHeader, service.ModelTestModeText)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
	require.Contains(t, recorder.Body.String(), "MODEL_TEST_AUTH_REQUIRED")
}

func TestModelTestAuthMiddlewareRejectsAnotherUsersAPIKey(t *testing.T) {
	keyUser := &service.User{ID: 7, Status: service.StatusActive, TokenVersion: 1}
	tokenUser := &service.User{ID: 8, Status: service.StatusActive, TokenVersion: 1}
	router, token := newModelTestAuthEnv(t, keyUser, tokenUser)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(ModelTestRequestHeader, service.ModelTestModeText)
	req.Header.Set(ModelTestAuthorizationHeader, "Bearer "+token)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusForbidden, recorder.Code)
	require.Contains(t, recorder.Body.String(), "MODEL_TEST_API_KEY_FORBIDDEN")
}

func TestModelTestAuthMiddlewareDoesNotAffectOrdinaryGatewayRequests(t *testing.T) {
	user := &service.User{ID: 7, Status: service.StatusActive}
	router, _ := newModelTestAuthEnv(t, user, user)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"trusted":false,"mode":""}`, recorder.Body.String())
}

func TestModelTestAuthMiddlewareRejectsModeOnAnotherEndpoint(t *testing.T) {
	user := &service.User{ID: 7, Status: service.StatusActive}
	router, token := newModelTestAuthEnv(t, user, user)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	req.Header.Set(ModelTestRequestHeader, service.ModelTestModeVideo)
	req.Header.Set(ModelTestAuthorizationHeader, "Bearer "+token)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, req)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "MODEL_TEST_ENDPOINT_MISMATCH")
}
