package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestContentModerationInputReadRecordsAccessWithoutResponseContent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			repository := &auditCaptureRepository{}
			auditService := service.NewAuditLogService(repository, nil)
			auditService.Start()
			t.Cleanup(auditService.Stop)

			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyUser), AuthSubject{UserID: 77})
				c.Set(string(ContextKeyUserRole), "admin")
				c.Next()
			})
			router.Use(gin.HandlerFunc(NewAuditLogMiddleware(auditService)))
			router.GET("/api/v1/admin/risk-control/logs/:id", func(c *gin.Context) {
				c.JSON(status, gin.H{
					"input_content": gin.H{"text": "audit-canary-original-input"},
				})
			})
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/admin/risk-control/logs/42", nil)
			router.ServeHTTP(recorder, request)
			require.Equal(t, status, recorder.Code)
			require.Contains(t, recorder.Body.String(), "audit-canary-original-input")
			auditService.Stop()

			repository.mu.Lock()
			logs := append([]*service.AuditLog(nil), repository.logs...)
			repository.mu.Unlock()
			require.Len(t, logs, 1, "查看原始输入必须保留访问记录，包括读取失败")
			entry := logs[0]
			require.Equal(t, "admin.risk_control.log.read", entry.Action)
			require.Equal(t, http.MethodGet, entry.Method)
			require.Equal(t, "/api/v1/admin/risk-control/logs/:id", entry.Path)
			require.Equal(t, status, entry.StatusCode)
			require.NotNil(t, entry.ActorUserID)
			require.Equal(t, int64(77), *entry.ActorUserID)
			require.Equal(t, map[string]any{"params": map[string]string{"id": "42"}}, entry.Extra)
			require.Empty(t, entry.RequestBody)
			encoded, err := json.Marshal(entry)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "audit-canary-original-input", "访问审计不能复制用户原始输入")
			require.NotContains(t, string(encoded), "input_content")
		})
	}
}
