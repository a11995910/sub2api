package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCandyRoutesRequireAdminAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{}}
	auth := middleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
		} else {
			c.AbortWithStatus(http.StatusForbidden)
		}
	})
	audit := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	step := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), h, auth, audit, step, nil, nil)
	for _, route := range []struct{ method, path string }{{"GET", "settings"}, {"PUT", "settings"}, {"GET", "accounts"}, {"POST", "run"}, {"POST", "stop"}, {"GET", "results"}, {"GET", "results/1"}} {
		for _, signedIn := range []bool{false, true} {
			req := httptest.NewRequest(route.method, "/api/v1/admin/candy-tests/"+route.path, nil)
			want := http.StatusUnauthorized
			if signedIn {
				req.Header.Set("Authorization", "Bearer 测试用户")
				want = http.StatusForbidden
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, want, w.Code, "%s %s", route.method, route.path)
		}
	}
}
