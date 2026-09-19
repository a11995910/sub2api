package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// GetHealthyTurnStateModels 返回上游实际支持且符合账号映射的健康头测试模型。
func (h *AccountHandler) GetHealthyTurnStateModels(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的账号 ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil || account == nil {
		response.NotFound(c, "账号不存在")
		return
	}
	models, err := h.accountTestService.FetchOpenAIHealthyTurnStateModels(c.Request.Context(), account)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, models)
}
