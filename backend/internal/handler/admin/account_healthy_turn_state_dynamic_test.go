package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHealthyTurnStateDynamicAccountBoundary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []struct{ method, suffix string }{
		{http.MethodGet, "/config"},
		{http.MethodPut, "/config"},
		{http.MethodPost, "/runs"},
		{http.MethodPost, "/runs/test/step"},
		{http.MethodPost, "/runs/test/stop"},
	} {
		for _, account := range []struct{ name, id, platform string }{
			{"无效账号", "0", service.PlatformOpenAI},
			{"非OpenAI账号", "7", service.PlatformAnthropic},
		} {
			t.Run(endpoint.method+endpoint.suffix+account.name, func(t *testing.T) {
				svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 7, Platform: account.platform}}
				h := &AccountHandler{adminService: svc, accountTestService: &service.AccountTestService{}}
				router := healthyTurnStateDynamicTestRouter(h)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(endpoint.method, "/accounts/"+account.id+"/dynamic"+endpoint.suffix, strings.NewReader(`{}`)))
				require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
			})
		}
	}
}

func TestHealthyTurnStateDynamicInputValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct{ name, method, suffix, body string }{
		{"非法JSON", http.MethodPut, "/config", `{`},
		{"未知协议", http.MethodPut, "/config", `{"protocol":"ftp","target_count":5,"max_attempts":100}`},
		{"空目标", http.MethodPut, "/config", `{"protocol":"http","target_count":0,"max_attempts":100}`},
		{"目标过大", http.MethodPut, "/config", `{"protocol":"http","target_count":101,"max_attempts":1000}`},
		{"次数过大", http.MethodPut, "/config", `{"protocol":"http","target_count":5,"max_attempts":1001}`},
		{"次数小于目标", http.MethodPut, "/config", `{"protocol":"http","target_count":5,"max_attempts":4}`},
		{"非法传输", http.MethodPost, "/runs", `{"transport":"ftp"}`},
		{"模型过长", http.MethodPost, "/runs", `{"model":"` + strings.Repeat("a", 201) + `"}`},
		{"任务标识过长", http.MethodPost, "/runs/" + strings.Repeat("a", 65) + "/step", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 7, Platform: service.PlatformOpenAI}}
			h := &AccountHandler{adminService: svc, accountTestService: &service.AccountTestService{}}
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, "/accounts/7/dynamic"+tc.suffix, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			healthyTurnStateDynamicTestRouter(h).ServeHTTP(recorder, req)
			require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
		})
	}
}

func healthyTurnStateDynamicTestRouter(h *AccountHandler) *gin.Engine {
	router := gin.New()
	router.GET("/accounts/:id/dynamic/config", h.GetHealthyTurnStateDynamicConfig)
	router.PUT("/accounts/:id/dynamic/config", h.SaveHealthyTurnStateDynamicConfig)
	router.POST("/accounts/:id/dynamic/runs", h.StartHealthyTurnStateDynamic)
	router.POST("/accounts/:id/dynamic/runs/:run_id/step", h.StepHealthyTurnStateDynamic)
	router.POST("/accounts/:id/dynamic/runs/:run_id/stop", h.StopHealthyTurnStateDynamic)
	return router
}
