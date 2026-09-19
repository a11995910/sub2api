package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// healthyTurnStateDynamicAccount 保证配置和运行都绑定路径中的现存 OpenAI 账号。
func (h *AccountHandler) healthyTurnStateDynamicAccount(c *gin.Context) *service.Account {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "无效的账号 ID")
		return nil
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil || account == nil {
		response.NotFound(c, "账号不存在")
		return nil
	}
	if account.Platform != service.PlatformOpenAI {
		response.BadRequest(c, "仅支持 OpenAI 账号")
		return nil
	}
	if h.accountTestService == nil {
		response.InternalError(c, "动态代理采集服务暂不可用")
		return nil
	}
	return account
}

// GetHealthyTurnStateDynamicConfig 只返回脱敏设置，完整提取地址不会进入账号列表。
func (h *AccountHandler) GetHealthyTurnStateDynamicConfig(c *gin.Context) {
	account := h.healthyTurnStateDynamicAccount(c)
	if account == nil {
		return
	}
	settings, err := h.accountTestService.GetHealthyTurnStateDynamicConfig(c.Request.Context(), account.ID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

// SaveHealthyTurnStateDynamicConfig 单独保存采集设置，不保存未提交的账号编辑表单。
func (h *AccountHandler) SaveHealthyTurnStateDynamicConfig(c *gin.Context) {
	account := h.healthyTurnStateDynamicAccount(c)
	if account == nil {
		return
	}
	var input struct {
		UpdateDefaultModels *bool    `json:"update_default_models"`
		UpdateSharedProxy   *bool    `json:"update_shared_proxy"`
		APIURL              string   `json:"api_url" binding:"max=4096"`
		Protocol            string   `json:"protocol" binding:"required,oneof=http https socks5h"`
		TargetCount         int      `json:"target_count" binding:"min=1,max=100"`
		MaxAttempts         int      `json:"max_attempts" binding:"min=1,max=1000,gtefield=TargetCount"`
		Models              []string `json:"models" binding:"required,min=1,max=100,dive,required,max=200"`
		Transport           string   `json:"transport" binding:"omitempty,oneof=http websocket"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "提取设置无效：目标数量应为 1–100，尝试上限应为 1–1000 且不小于目标")
		return
	}
	settings, err := h.accountTestService.SaveHealthyTurnStateDynamicConfig(c.Request.Context(), account.ID, service.HealthyTurnStateDynamicConfigInput{
		APIURL: input.APIURL, Protocol: input.Protocol, TargetCount: input.TargetCount, MaxAttempts: input.MaxAttempts,
		Models: input.Models, Transport: input.Transport, UpdateSharedProxy: input.UpdateSharedProxy, UpdateDefaultModels: input.UpdateDefaultModels,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *AccountHandler) StartHealthyTurnStateDynamic(c *gin.Context) {
	account := h.healthyTurnStateDynamicAccount(c)
	if account == nil {
		return
	}
	var input struct {
		Model     string `json:"model" binding:"max=200"`
		Transport string `json:"transport" binding:"omitempty,oneof=http websocket"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "无效的采集模型或传输方式")
		return
	}
	run, err := h.accountTestService.StartHealthyTurnStateDynamic(c.Request.Context(), account, service.HealthyTurnStateDynamicStartInput{
		Model: input.Model, Transport: input.Transport,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, run)
}

func (h *AccountHandler) StepHealthyTurnStateDynamic(c *gin.Context) {
	h.advanceHealthyTurnStateDynamic(c, false)
}

func (h *AccountHandler) StopHealthyTurnStateDynamic(c *gin.Context) {
	h.advanceHealthyTurnStateDynamic(c, true)
}

func (h *AccountHandler) advanceHealthyTurnStateDynamic(c *gin.Context, stop bool) {
	account := h.healthyTurnStateDynamicAccount(c)
	if account == nil {
		return
	}
	runID := c.Param("run_id")
	if runID == "" || len(runID) > 64 {
		response.BadRequest(c, "无效的采集任务 ID")
		return
	}
	var run *service.HealthyTurnStateDynamicRun
	var err error
	if stop {
		run, err = h.accountTestService.StopHealthyTurnStateDynamic(c.Request.Context(), account.ID, runID)
	} else {
		run, err = h.accountTestService.StepHealthyTurnStateDynamic(c.Request.Context(), account.ID, runID)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, run)
}
