package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPromptMarketRoutesArePublicReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterUserRoutes(
		v1,
		&handler.Handlers{
			CreativeDrawing: handler.NewCreativeDrawingHandler(&service.CreativeDrawingService{}),
		},
		servermiddleware.JWTAuthMiddleware(func(c *gin.Context) {
			c.AbortWithStatus(http.StatusUnauthorized)
		}),
		servermiddleware.AuditLogMiddleware(func(c *gin.Context) {
			c.Next()
		}),
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/creative-drawing/prompt-market/libraries/invalid/prompts", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "CREATIVE_DRAWING_PROMPT_MARKET_LIBRARY_INVALID")
}
