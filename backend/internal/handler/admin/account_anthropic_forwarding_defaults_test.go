package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCreateAnthropicOAuthAccountAppliesForwardingSafeDefaults(t *testing.T) {
	router, adminSvc := setupAccountAdminRouterForTest()

	body, _ := json.Marshal(map[string]any{
		"name":        "anthropic-oauth",
		"platform":    service.PlatformAnthropic,
		"type":        service.AccountTypeOAuth,
		"credentials": map[string]any{"access_token": "token"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.createdAccounts, 1)
	extra := adminSvc.createdAccounts[0].Extra
	require.Equal(t, defaultAnthropicForwardingBaseRPM, service.ParseExtraInt(extra["base_rpm"]))
	require.Equal(t, defaultAnthropicForwardingMaxSessions, service.ParseExtraInt(extra["max_sessions"]))
	require.Equal(t, defaultAnthropicForwardingSessionIdleTimeoutMinutes, service.ParseExtraInt(extra["session_idle_timeout_minutes"]))
}

func TestCreateAnthropicAPIKeyAccountDoesNotApplyOAuthForwardingDefaults(t *testing.T) {
	router, adminSvc := setupAccountAdminRouterForTest()

	body, _ := json.Marshal(map[string]any{
		"name":        "anthropic-apikey",
		"platform":    service.PlatformAnthropic,
		"type":        service.AccountTypeAPIKey,
		"credentials": map[string]any{"api_key": "sk-ant"},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Nil(t, adminSvc.createdAccounts[0].Extra)
}

func TestApplyAnthropicForwardingSafeDefaultsKeepsExplicitValues(t *testing.T) {
	extra := map[string]any{
		"base_rpm":                     2,
		"max_sessions":                 1,
		"session_idle_timeout_minutes": 9,
	}

	applyAnthropicForwardingSafeDefaults(service.PlatformAnthropic, service.AccountTypeSetupToken, &extra)

	require.Equal(t, 2, service.ParseExtraInt(extra["base_rpm"]))
	require.Equal(t, 1, service.ParseExtraInt(extra["max_sessions"]))
	require.Equal(t, 9, service.ParseExtraInt(extra["session_idle_timeout_minutes"]))
}

func setupAccountAdminRouterForTest() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)
	return router, adminSvc
}
