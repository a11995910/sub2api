package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountHealthyTurnStateProbeValidation(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name, id, body, platform string
		want                     int
	}{
		{"非法账号", "0", `{}`, "openai", 400},
		{"错误平台", "7", `{}`, "grok", 400},
		{"非法 JSON", "7", `{`, "openai", 400},
		{"非法出口", "7", `{"proxy_id":-1}`, "openai", 400},
		{"非法传输", "7", `{"transport":"ftp"}`, "openai", 400},
		{"停用代理", "7", `{"proxy_id":11}`, "openai", 400},
		{"过期代理", "7", `{"proxy_id":12}`, "openai", 400},
		{"有效代理但服务未就绪", "7", `{"proxy_id":13}`, "openai", 503},
		{"直连但服务未就绪", "7", `{"proxy_id":0}`, "openai", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			svc := &availableModelsAdminService{stubAdminService: newStubAdminService(), account: service.Account{ID: 7, Platform: tc.platform}}
			svc.proxies = []service.Proxy{{ID: 11, Status: service.StatusDisabled}, {ID: 12, Status: service.StatusActive, ExpiresAt: &expired}, {ID: 13, Status: service.StatusActive}}
			handler := &AccountHandler{adminService: svc, accountTestService: &service.AccountTestService{}}
			router := gin.New()
			router.POST("/accounts/:id/healthy-turn-state/test", handler.ProbeHealthyTurnState)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/accounts/"+tc.id+"/healthy-turn-state/test", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, tc.want, recorder.Code, recorder.Body.String())
			require.Nil(t, svc.account.ProxyID, "测试不能修改账号代理")
			require.Nil(t, svc.account.Proxy)
		})
	}
}
