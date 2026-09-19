package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type healthyTurnStateModelsAdminService struct {
	*stubAdminService
	account service.Account
}

func (s *healthyTurnStateModelsAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if id != s.account.ID {
		return nil, errors.New("账号不存在")
	}
	return &s.account, nil
}

func TestAccountHealthyTurnStateModelsValidation(t *testing.T) {
	for _, tc := range []struct {
		name, id, platform string
		want               int
	}{
		{"非法账号", "0", "openai", http.StatusBadRequest},
		{"账号不存在", "99", "openai", http.StatusNotFound},
		{"其他平台", "7", "grok", http.StatusBadRequest},
		{"服务未就绪", "7", "openai", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			svc := &healthyTurnStateModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 7, Platform: tc.platform}}
			handler := &AccountHandler{adminService: svc}
			router := gin.New()
			router.GET("/accounts/:id/healthy-turn-state/models", handler.GetHealthyTurnStateModels)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/accounts/"+tc.id+"/healthy-turn-state/models", nil))
			require.Equal(t, tc.want, recorder.Code, recorder.Body.String())
		})
	}
}
