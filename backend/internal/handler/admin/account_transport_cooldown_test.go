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

func TestTransportCooldownBoundariesRejectBeforeMutation(t *testing.T) {
	const invalid = `"extra":{"openai_transport_error_cooldown_seconds":0}`
	tests := []struct {
		name  string
		path  string
		body  string
		mount func(*gin.Engine, *AccountHandler)
	}{
		{"新建", "/accounts", `{"name":"test","platform":"openai","type":"apikey","credentials":{"api_key":"test"},` + invalid + `}`,
			func(r *gin.Engine, h *AccountHandler) { r.POST("/accounts", h.Create) }},
		{"批量新建", "/accounts/batch", `{"accounts":[{"name":"test","platform":"openai","type":"apikey","credentials":{"api_key":"test"},` + invalid + `}]}`,
			func(r *gin.Engine, h *AccountHandler) { r.POST("/accounts/batch", h.BatchCreate) }},
		{"OAuth重新授权", "/accounts/1/apply-oauth-credentials", `{"type":"oauth","credentials":{"access_token":"test"},` + invalid + `}`,
			func(r *gin.Engine, h *AccountHandler) {
				r.POST("/accounts/:id/apply-oauth-credentials", h.ApplyOAuthCredentials)
			}},
		{"Codex导入", "/accounts/import-codex-session", `{"content":"test",` + invalid + `}`,
			func(r *gin.Engine, h *AccountHandler) { r.POST("/accounts/import-codex-session", h.ImportCodexSession) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			stub := newStubAdminService()
			stub.getAccountResult = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
			handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			tt.mount(router, handler)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, tt.path, bytes.NewBufferString(tt.body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			requireTransportCooldownBadRequest(t, recorder)
			require.Zero(t, stub.updateAccountCalls)
			require.Zero(t, stub.updateAccountExtraCalls)
		})
	}
}

func TestTransportCooldownPATRejectsBeforeTokenValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewOpenAIOAuthHandler(nil, newStubAdminService(), nil, nil)
	router := gin.New()
	router.POST("/openai/create-from-codex-pat", handler.CreateAccountFromCodexPAT)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/openai/create-from-codex-pat", bytes.NewBufferString(
		`{"access_token":"test","extra":{"openai_transport_error_cooldown_seconds":1.5}}`,
	))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	requireTransportCooldownBadRequest(t, recorder)
}

func requireTransportCooldownBadRequest(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	var body struct {
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "INVALID_OPENAI_TRANSPORT_ERROR_COOLDOWN", body.Reason)
}
