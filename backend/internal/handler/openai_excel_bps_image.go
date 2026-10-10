package handler

import "github.com/gin-gonic/gin"

func (h *OpenAIGatewayHandler) ServeExcelBPSImage(c *gin.Context) {
	h.gatewayService.ServeExcelBPSImage(c)
}
