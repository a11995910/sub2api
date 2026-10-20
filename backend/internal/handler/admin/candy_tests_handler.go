package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type CandyTestHandler struct{ svc *service.CandyTestService }

func NewCandyTestHandler(svc *service.CandyTestService) *CandyTestHandler {
	return &CandyTestHandler{svc: svc}
}

func (h *CandyTestHandler) GetSettings(c *gin.Context) {
	settings, err := h.svc.GetSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *CandyTestHandler) UpdateSettings(c *gin.Context) {
	var input service.CandyTestSettingsInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "糖果测试设置无效")
		return
	}
	settings, err := h.svc.UpdateSettings(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *CandyTestHandler) Run(c *gin.Context) {
	var input service.CandyTestStart
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "测试账号选择无效")
		return
	}
	batch, err := h.svc.Start(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, batch)
}

func (h *CandyTestHandler) Stop(c *gin.Context) {
	var input struct {
		RunID string `json:"run_id"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "测试运行标识无效")
		return
	}
	if err := h.svc.Stop(c.Request.Context(), input.RunID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"stopped": true})
}

func (h *CandyTestHandler) ListResults(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	results, total, err := h.svc.ListResults(c.Request.Context(), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, results, total, page, pageSize)
}

func (h *CandyTestHandler) GetResult(c *gin.Context) {
	id, ok := pelicanGroupTestID(c, "id")
	if !ok {
		return
	}
	result, err := h.svc.GetResult(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
