//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountHandlerListIDsUsesLightweightService(t *testing.T) {
	svc := newStubAdminService()
	svc.accounts = []service.Account{
		{ID: 101, Name: "codex-1", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth},
		{ID: 202, Name: "codex-2", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth},
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(svc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.GET("/api/v1/admin/accounts/ids", handler.ListIDs)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/ids?page=1&page_size=1000&platform=openai&type=oauth&sort_by=id&sort_order=asc", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data struct {
			Items    []int64 `json:"items"`
			Total    int64   `json:"total"`
			Page     int     `json:"page"`
			PageSize int     `json:"page_size"`
			Pages    int     `json:"pages"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, []int64{101, 202}, resp.Data.Items)
	require.Equal(t, int64(2), resp.Data.Total)
	require.Equal(t, 1, resp.Data.Page)
	require.Equal(t, 1000, resp.Data.PageSize)
	require.Equal(t, 1, resp.Data.Pages)

	require.Equal(t, 1, svc.lastListAccountIDs.calls)
	require.Equal(t, 0, svc.lastListAccounts.calls)
	require.Equal(t, service.PlatformOpenAI, svc.lastListAccountIDs.platform)
	require.Equal(t, service.AccountTypeOAuth, svc.lastListAccountIDs.accountType)
	require.Equal(t, "id", svc.lastListAccountIDs.sortBy)
	require.Equal(t, "asc", svc.lastListAccountIDs.sortOrder)
}
