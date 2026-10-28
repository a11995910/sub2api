package admin

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetExcelBPSConfig(c *gin.Context) {
	cfg, err := h.settingService.GetExcelBPSConfig(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "Excel/BPS 配置读取失败")
		return
	}
	response.Success(c, cfg)
}
func (h *SettingHandler) SaveExcelBPSConfig(c *gin.Context) {
	var request struct {
		Enabled  *bool                               `json:"enabled"`
		Defaults *service.ExcelBPSDefaults           `json:"defaults"`
		Images   *service.ExcelBPSImageRelaySettings `json:"images"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.Error(c, 400, "配置格式无效")
		return
	}
	cfg, err := h.settingService.GetExcelBPSConfig(c.Request.Context())
	if err != nil {
		response.Error(c, 503, "配置读取失败")
		return
	}
	if request.Enabled != nil {
		cfg.Enabled = *request.Enabled
	}
	if request.Defaults != nil {
		cfg.Defaults = *request.Defaults
	}
	if request.Images != nil {
		cfg.Images = *request.Images
	}
	if err := h.settingService.SaveExcelBPSConfig(c.Request.Context(), cfg); err != nil {
		if errors.Is(err, service.ErrExcelBPSConfigSave) {
			response.Error(c, 503, err.Error())
		} else {
			response.Error(c, 400, err.Error())
		}
		return
	}
	h.GetExcelBPSConfig(c)
}
