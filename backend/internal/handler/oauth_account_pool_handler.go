package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type OAuthAccountPoolHandler struct {
	service *service.OAuthAccountPoolService
}

func NewOAuthAccountPoolHandler(service *service.OAuthAccountPoolService) *OAuthAccountPoolHandler {
	return &OAuthAccountPoolHandler{service: service}
}

// List 返回当前用户有权查看的 OAuth 账号运行状态。
// GET /api/v1/oauth-account-pool
func (h *OAuthAccountPoolHandler) List(c *gin.Context) {
	subject, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	pool, err := h.service.GetForUser(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	role, _ := middleware2.GetUserRoleFromContext(c)
	response.Success(c, dto.OAuthAccountPoolFromService(pool, role == service.RoleAdmin))
}
