package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetHealthyTurnState 返回持久化统计，不返回状态头、散列或凭据。
func (h *AccountHandler) GetHealthyTurnState(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的账号 ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.NotFound(c, "账号不存在")
		return
	}
	if account.Platform != service.PlatformOpenAI {
		response.BadRequest(c, "仅支持 OpenAI 账号")
		return
	}
	if h.accountTestService == nil {
		response.InternalError(c, "健康状态头统计暂不可用")
		return
	}
	stats, err := h.accountTestService.HealthyTurnStateStats(c.Request.Context(), id)
	if err != nil {
		response.InternalError(c, "健康状态头统计读取失败，请稍后重试")
		return
	}
	response.Success(c, stats)
}
