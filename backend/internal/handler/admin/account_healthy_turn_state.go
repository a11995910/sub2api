package admin

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ProbeHealthyTurnState 使用已保存账号配置与临时代理发送一次 hi，不保存账号编辑表单。
func (h *AccountHandler) ProbeHealthyTurnState(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的账号 ID")
		return
	}
	var input struct {
		Model     string `json:"model" binding:"max=200"`
		Transport string `json:"transport" binding:"omitempty,oneof=http websocket"`
		// 缺省使用账号当前代理，0 表示直连，正数表示已保存的代理 ID。
		ProxyID *int64 `json:"proxy_id" binding:"omitempty,gte=0"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "无效的测试参数")
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
	copyAccount := *account
	if input.ProxyID != nil {
		copyAccount.ProxyID, copyAccount.Proxy = nil, nil
		if *input.ProxyID > 0 {
			proxy, proxyErr := h.adminService.GetProxy(c.Request.Context(), *input.ProxyID)
			if proxyErr != nil || proxy == nil {
				response.BadRequest(c, "所选代理不存在")
				return
			}
			copyAccount.ProxyID, copyAccount.Proxy = &proxy.ID, proxy
		}
	}
	if copyAccount.ProxyID != nil && (copyAccount.Proxy == nil || !copyAccount.Proxy.IsActive() || copyAccount.Proxy.IsExpired(time.Now())) {
		response.BadRequest(c, "所选代理不可用或已过期")
		return
	}
	if h.accountTestService == nil {
		response.InternalError(c, "账号测试服务暂不可用")
		return
	}
	result, err := h.accountTestService.ProbeOpenAIHealthyTurnState(c.Request.Context(), &copyAccount, input.Model, input.Transport)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
