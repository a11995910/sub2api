package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetExcelBPS(c *gin.Context) {
	settings, err := h.settingService.GetExcelBPSSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
func (h *SettingHandler) SaveExcelBPS(c *gin.Context) {
	var settings service.ExcelBPSSettings
	if err := c.ShouldBindJSON(&settings); err != nil {
		response.BadRequest(c, "Excel/BPS 配置格式无效")
		return
	}
	if err := h.settingService.SaveExcelBPSSettings(c.Request.Context(), &settings); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.GetExcelBPS(c)
}
func (h *SettingHandler) GetExcelBPSDefaults(c *gin.Context) {
	defaults, err := h.settingService.GetExcelBPSDefaults(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"excel_bps": defaults})
}
func (h *SettingHandler) SaveExcelBPSDefaults(c *gin.Context) {
	var request struct {
		ExcelBPS service.ExcelBPSDefaults `json:"excel_bps"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.BadRequest(c, "Excel/BPS 默认配置格式无效")
		return
	}
	if err := h.settingService.SaveExcelBPSDefaults(c.Request.Context(), request.ExcelBPS); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	h.GetExcelBPSDefaults(c)
}
