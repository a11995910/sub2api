package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func RegisterPublicPelicanShowcaseRoutes(v1 *gin.RouterGroup, h *handler.Handlers, apiKeyAuth middleware.APIKeyAuthMiddleware, limiter *middleware.PanelRateLimiter) {
	showcase := v1.Group("/public/pelican-showcase")
	showcase.Use(func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Writer.Header().Add("Vary", "Authorization, X-API-Key, X-Goog-API-Key")
		c.Next()
	})
	showcase.Use(limiter.PublicIP())
	showcase.Use(gin.HandlerFunc(apiKeyAuth))
	showcase.Use(limiter.Global())
	showcase.GET("", h.PelicanShowcase.PublicList)
	showcase.HEAD("", h.PelicanShowcase.PublicList)
	showcase.GET("/items/:id", h.PelicanShowcase.PublicItem)
	showcase.HEAD("/items/:id", h.PelicanShowcase.PublicItem)
}
