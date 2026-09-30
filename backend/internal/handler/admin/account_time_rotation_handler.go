package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountTimeRotationHandler struct {
	svc *service.AccountTimeRotationService
}

func NewAccountTimeRotationHandler(svc *service.AccountTimeRotationService) *AccountTimeRotationHandler {
	return &AccountTimeRotationHandler{svc: svc}
}

func (h *AccountTimeRotationHandler) Get(c *gin.Context) {
	config, err := h.svc.Get(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, config)
}

func (h *AccountTimeRotationHandler) Save(c *gin.Context) {
	var config service.AccountTimeRotationConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		response.BadRequest(c, "时段轮候参数格式无效")
		return
	}
	saved, err := h.svc.Save(c.Request.Context(), &config)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, saved)
}
