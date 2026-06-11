package admin

import (
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const upstreamRateMonitorPasswordMask = "***"

type UpstreamRateMonitorHandler struct {
	svc *service.UpstreamRateMonitorService
}

func NewUpstreamRateMonitorHandler(svc *service.UpstreamRateMonitorService) *UpstreamRateMonitorHandler {
	return &UpstreamRateMonitorHandler{svc: svc}
}

type upstreamRateMonitorCreateRequest struct {
	Name     string `json:"name" binding:"required,max=100"`
	BaseURL  string `json:"base_url" binding:"required,max=500"`
	Username string `json:"username" binding:"required,max=255"`
	Password string `json:"password" binding:"required,max=2000"`
	Enabled  *bool  `json:"enabled"`
}

type upstreamRateMonitorUpdateRequest struct {
	Name     *string `json:"name" binding:"omitempty,max=100"`
	BaseURL  *string `json:"base_url" binding:"omitempty,max=500"`
	Username *string `json:"username" binding:"omitempty,max=255"`
	Password *string `json:"password" binding:"omitempty,max=2000"`
	Enabled  *bool   `json:"enabled"`
}

type upstreamRateMonitorResponse struct {
	ID                    int64                               `json:"id"`
	Name                  string                              `json:"name"`
	BaseURL               string                              `json:"base_url"`
	Username              string                              `json:"username"`
	PasswordMasked        string                              `json:"password_masked"`
	PasswordDecryptFailed bool                                `json:"password_decrypt_failed"`
	Enabled               bool                                `json:"enabled"`
	LastCheckedAt         *string                             `json:"last_checked_at"`
	LastStatus            string                              `json:"last_status"`
	LastError             string                              `json:"last_error"`
	LastGroupCount        int                                 `json:"last_group_count"`
	LastSnapshot          []upstreamRateGroupSnapshotResponse `json:"last_snapshot"`
	CreatedBy             int64                               `json:"created_by"`
	CreatedAt             string                              `json:"created_at"`
	UpdatedAt             string                              `json:"updated_at"`
}

type upstreamRateGroupSnapshotResponse struct {
	ID                          int64    `json:"id"`
	Name                        string   `json:"name"`
	Description                 string   `json:"description,omitempty"`
	Platform                    string   `json:"platform,omitempty"`
	RateMultiplier              float64  `json:"rate_multiplier"`
	ImageRateMultiplier         *float64 `json:"image_rate_multiplier,omitempty"`
	ImageRateIndependent        bool     `json:"image_rate_independent,omitempty"`
	SubscriptionType            string   `json:"subscription_type,omitempty"`
	IsExclusive                 bool     `json:"is_exclusive"`
	Status                      string   `json:"status,omitempty"`
	RPMLimit                    int      `json:"rpm_limit,omitempty"`
	AllowImageGeneration        bool     `json:"allow_image_generation,omitempty"`
	ImageSuperResolutionEnabled bool     `json:"image_super_resolution_enabled,omitempty"`
	SortOrder                   int      `json:"sort_order,omitempty"`
}

func (h *UpstreamRateMonitorHandler) List(c *gin.Context) {
	page, pageSize := response.ParsePagination(c)
	params := service.UpstreamRateMonitorListParams{
		Page:     page,
		PageSize: pageSize,
		Enabled:  parseListEnabled(c.Query("enabled")),
		Search:   strings.TrimSpace(c.Query("search")),
	}

	items, total, err := h.svc.List(c.Request.Context(), params)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	out := make([]*upstreamRateMonitorResponse, 0, len(items))
	for _, item := range items {
		out = append(out, upstreamRateMonitorToResponse(item))
	}
	response.Paginated(c, out, total, page, pageSize)
}

func (h *UpstreamRateMonitorHandler) Get(c *gin.Context) {
	id, ok := parseUpstreamRateMonitorID(c)
	if !ok {
		return
	}
	item, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, upstreamRateMonitorToResponse(item))
}

func (h *UpstreamRateMonitorHandler) Create(c *gin.Context) {
	var req upstreamRateMonitorCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	subject, _ := middleware2.GetAuthSubjectFromContext(c)
	item, err := h.svc.Create(c.Request.Context(), service.UpstreamRateMonitorCreateParams{
		Name:      req.Name,
		BaseURL:   req.BaseURL,
		Username:  req.Username,
		Password:  req.Password,
		Enabled:   enabled,
		CreatedBy: subject.UserID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Created(c, upstreamRateMonitorToResponse(item))
}

func (h *UpstreamRateMonitorHandler) Update(c *gin.Context) {
	id, ok := parseUpstreamRateMonitorID(c)
	if !ok {
		return
	}
	var req upstreamRateMonitorUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	item, err := h.svc.Update(c.Request.Context(), id, service.UpstreamRateMonitorUpdateParams{
		Name:     req.Name,
		BaseURL:  req.BaseURL,
		Username: req.Username,
		Password: req.Password,
		Enabled:  req.Enabled,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, upstreamRateMonitorToResponse(item))
}

func (h *UpstreamRateMonitorHandler) Delete(c *gin.Context) {
	id, ok := parseUpstreamRateMonitorID(c)
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "upstream rate monitor deleted"})
}

func (h *UpstreamRateMonitorHandler) Refresh(c *gin.Context) {
	id, ok := parseUpstreamRateMonitorID(c)
	if !ok {
		return
	}
	item, err := h.svc.Refresh(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, upstreamRateMonitorToResponse(item))
}

func parseUpstreamRateMonitorID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid upstream rate monitor ID")
		return 0, false
	}
	return id, true
}

func upstreamRateMonitorToResponse(m *service.UpstreamRateMonitor) *upstreamRateMonitorResponse {
	if m == nil {
		return nil
	}
	resp := &upstreamRateMonitorResponse{
		ID:                    m.ID,
		Name:                  m.Name,
		BaseURL:               m.BaseURL,
		Username:              m.Username,
		PasswordMasked:        upstreamRateMonitorPasswordMask,
		PasswordDecryptFailed: m.PasswordDecryptFailed,
		Enabled:               m.Enabled,
		LastStatus:            m.LastStatus,
		LastError:             m.LastError,
		LastGroupCount:        m.LastGroupCount,
		LastSnapshot:          upstreamRateSnapshotToResponse(m.LastSnapshot),
		CreatedBy:             m.CreatedBy,
		CreatedAt:             formatUpstreamRateTime(m.CreatedAt),
		UpdatedAt:             formatUpstreamRateTime(m.UpdatedAt),
	}
	if m.LastCheckedAt != nil {
		value := formatUpstreamRateTime(*m.LastCheckedAt)
		resp.LastCheckedAt = &value
	}
	return resp
}

func upstreamRateSnapshotToResponse(snapshot service.UpstreamRateSnapshot) []upstreamRateGroupSnapshotResponse {
	if snapshot == nil {
		return []upstreamRateGroupSnapshotResponse{}
	}
	out := make([]upstreamRateGroupSnapshotResponse, 0, len(snapshot))
	for _, g := range snapshot {
		out = append(out, upstreamRateGroupSnapshotResponse{
			ID:                          g.ID,
			Name:                        g.Name,
			Description:                 g.Description,
			Platform:                    g.Platform,
			RateMultiplier:              g.RateMultiplier,
			ImageRateMultiplier:         g.ImageRateMultiplier,
			ImageRateIndependent:        g.ImageRateIndependent,
			SubscriptionType:            g.SubscriptionType,
			IsExclusive:                 g.IsExclusive,
			Status:                      g.Status,
			RPMLimit:                    g.RPMLimit,
			AllowImageGeneration:        g.AllowImageGeneration,
			ImageSuperResolutionEnabled: g.ImageSuperResolutionEnabled,
			SortOrder:                   g.SortOrder,
		})
	}
	return out
}

func formatUpstreamRateTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}
