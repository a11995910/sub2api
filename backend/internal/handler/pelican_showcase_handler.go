package handler

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type PelicanShowcaseHandler struct {
	showcase *service.PelicanShowcaseService
	public   *pelicanPublicCache
}

func NewPelicanShowcaseHandler(showcase *service.PelicanShowcaseService) *PelicanShowcaseHandler {
	return &PelicanShowcaseHandler{showcase: showcase, public: newPelicanPublicCache(showcase)}
}

func (h *PelicanShowcaseHandler) List(c *gin.Context) {
	view, err := h.showcase.View(c.Request.Context(), time.Now())
	if err != nil {
		response.InternalError(c, "Failed to load pelican showcase")
		return
	}
	response.Success(c, view)
}

func (h *PelicanShowcaseHandler) GetItem(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid item id")
		return
	}
	item, err := h.showcase.Item(c.Request.Context(), id, time.Now())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, item)
}

func (h *PelicanShowcaseHandler) DeleteItem(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid item id")
		return
	}
	if err := h.showcase.Remove(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.public.invalidate()
	response.Success(c, gin.H{"deleted": true})
}

type pelicanShowcaseSettings struct {
	Enabled       bool `json:"enabled"`
	APIEnabled    bool `json:"api_enabled"`
	MaxItems      int  `json:"max_items"`
	AutoCleanup   bool `json:"auto_cleanup"`
	RetentionDays int  `json:"retention_days"`
}

func pelicanShowcaseSettingsFrom(runtime service.PelicanShowcaseRuntime) pelicanShowcaseSettings {
	return pelicanShowcaseSettings{
		Enabled:       runtime.Enabled,
		APIEnabled:    runtime.APIEnabled,
		MaxItems:      runtime.Config.MaxItems,
		AutoCleanup:   runtime.Config.AutoCleanup,
		RetentionDays: runtime.Config.RetentionDays,
	}
}

func (h *PelicanShowcaseHandler) GetSettings(c *gin.Context) {
	runtime, err := h.showcase.Settings(c.Request.Context())
	if err != nil {
		response.InternalError(c, "Failed to load pelican showcase settings")
		return
	}
	response.Success(c, pelicanShowcaseSettingsFrom(runtime))
}

func (h *PelicanShowcaseHandler) UpdateSettings(c *gin.Context) {
	var req struct {
		pelicanShowcaseSettings
		APIEnabled *bool `json:"api_enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid request body")
		return
	}
	runtime, err := h.showcase.UpdateSettings(c.Request.Context(), req.Enabled, service.PelicanShowcaseConfig{
		MaxItems:      req.MaxItems,
		AutoCleanup:   req.AutoCleanup,
		RetentionDays: req.RetentionDays,
	}, req.APIEnabled)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	subject, _ := middleware.GetAuthSubjectFromContext(c)
	h.public.invalidate()
	role, _ := middleware.GetUserRoleFromContext(c)
	changed := []string{service.SettingKeyPelicanShowcaseEnabled, service.SettingKeyPelicanShowcaseConfig}
	if req.APIEnabled != nil {
		changed = append(changed, service.SettingKeyPelicanShowcaseAPIEnabled)
	}
	slog.Info("settings updated", "audit", true, "user_id", subject.UserID, "role", role, "changed", changed)
	response.Success(c, pelicanShowcaseSettingsFrom(runtime))
}
