package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/service/modelleaderboard"
	"github.com/gin-gonic/gin"
)

// RegisterModelLeaderboardRoutes 公开评测数据不包含用户分组；可用分组仍由模型广场原有权限过滤。
func RegisterModelLeaderboardRoutes(v1 *gin.RouterGroup, optionalJWT middleware.OptionalJWTAuthMiddleware, settings *service.SettingService, limiter *middleware.PanelRateLimiter, dataDir string) {
	store := modelleaderboard.New(dataDir)
	route := v1.Group("/model-leaderboard")
	route.Use(limiter.PublicIP(), gin.HandlerFunc(optionalJWT), middleware.BackendModeUserGuard(settings))
	route.GET("", func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		response.Success(c, store.Get())
	})
}
