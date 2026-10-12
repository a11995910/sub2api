package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type PelicanGroupTestHandler struct {
	svc *service.PelicanGroupTestService
}

func NewPelicanGroupTestHandler(svc *service.PelicanGroupTestService) *PelicanGroupTestHandler {
	return &PelicanGroupTestHandler{svc: svc}
}

func (h *PelicanGroupTestHandler) ListPlans(c *gin.Context) {
	plans, err := h.svc.ListPlans(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, plans)
}

func (h *PelicanGroupTestHandler) CreatePlan(c *gin.Context) {
	var input service.PelicanGroupTestPlanInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	plan, err := h.svc.CreatePlan(c.Request.Context(), input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, plan)
}

func (h *PelicanGroupTestHandler) UpdatePlan(c *gin.Context) {
	id, ok := pelicanGroupTestID(c, "id")
	if !ok {
		return
	}
	var input service.PelicanGroupTestPlanInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	plan, err := h.svc.UpdatePlan(c.Request.Context(), id, input)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, plan)
}

func (h *PelicanGroupTestHandler) DeletePlan(c *gin.Context) {
	id, ok := pelicanGroupTestID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.DeletePlan(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *PelicanGroupTestHandler) RunPlan(c *gin.Context) {
	id, ok := pelicanGroupTestID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.RunNow(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"started": true})
}

func (h *PelicanGroupTestHandler) ListResults(c *gin.Context) {
	var planID int64
	if raw := c.Query("plan_id"); raw != "" {
		var err error
		planID, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || planID < 0 {
			response.BadRequest(c, "invalid plan_id")
			return
		}
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.svc.ListResults(c.Request.Context(), planID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

func (h *PelicanGroupTestHandler) GetResult(c *gin.Context) {
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

func pelicanGroupTestID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid id")
		return 0, false
	}
	return id, true
}
