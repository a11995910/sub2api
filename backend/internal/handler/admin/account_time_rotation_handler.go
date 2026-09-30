package admin

import (
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AccountTimeRotationHandler struct {
	svc      *service.AccountTimeRotationService
	accounts service.AccountRepository
}

func NewAccountTimeRotationHandler(svc *service.AccountTimeRotationService, accounts service.AccountRepository) *AccountTimeRotationHandler {
	return &AccountTimeRotationHandler{svc: svc, accounts: accounts}
}

type accountTimeRotationStatus struct {
	Mode           string                                `json:"mode"`
	Enabled        bool                                  `json:"enabled"`
	Revision       int64                                 `json:"revision"`
	Ready          bool                                  `json:"ready"`
	UpdatedAt      *time.Time                            `json:"updated_at"`
	Period         *service.AccountSmartRotationPeriod   `json:"period"`
	NextRotationAt *time.Time                            `json:"next_rotation_at"`
	Accounts       []service.AccountSmartRotationAccount `json:"accounts"`
	Scope          string                                `json:"scope"`
}

// Status 展示已保存且被执行器确认的计划，不以读取接口续期执行器健康状态。
func (h *AccountTimeRotationHandler) Status(c *gin.Context) {
	var groupID int64
	if values, present := c.Request.URL.Query()["group_id"]; present {
		parsed, err := strconv.ParseInt(values[0], 10, 64)
		if err != nil || parsed <= 0 {
			response.BadRequest(c, "分组 ID 必须为正整数")
			return
		}
		groupID = parsed
	}
	config, err := h.svc.Get(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	now := time.Now()
	snapshot, refreshedAt := h.svc.Snapshot(now)
	result := accountTimeRotationStatus{
		Mode: config.Mode, Enabled: config.Enabled, Revision: config.Revision,
		Ready:    snapshot != nil && snapshot.Revision == config.Revision,
		Accounts: []service.AccountSmartRotationAccount{}, Scope: "all",
	}
	if result.Mode == "" {
		result.Mode = "manual"
	}
	if !refreshedAt.IsZero() {
		result.UpdatedAt = &refreshedAt
	}
	if groupID > 0 {
		result.Scope = "group"
	}
	if !result.Ready || !config.Enabled || result.Mode != "smart" || config.Smart == nil {
		response.Success(c, result)
		return
	}
	accounts, err := h.accounts.GetByIDs(c.Request.Context(), config.Smart.AccountIDs)
	if response.ErrorFrom(c, err) {
		return
	}
	if groupID > 0 {
		// 限定计划的账号池，避免将其他分组或已删除账号显示为本组候补。
		filtered := make([]*service.Account, 0, len(accounts))
		ids := make([]int64, 0, len(accounts))
		for _, account := range accounts {
			if account == nil || !rotationAccountInGroup(account, groupID) {
				continue
			}
			filtered = append(filtered, account)
			ids = append(ids, account.ID)
		}
		accounts = filtered
		config.Smart.AccountIDs = ids
	}
	if plan := service.BuildAccountSmartRotationPlan(config, accounts, now); plan != nil {
		result.Period = plan.Period
		result.NextRotationAt = &plan.NextRotationAt
		if plan.Accounts != nil {
			result.Accounts = plan.Accounts
		}
	}
	response.Success(c, result)
}

func rotationAccountInGroup(account *service.Account, groupID int64) bool {
	for _, id := range account.GroupIDs {
		if id == groupID {
			return true
		}
	}
	for _, group := range account.AccountGroups {
		if group.GroupID == groupID {
			return true
		}
	}
	return false
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
