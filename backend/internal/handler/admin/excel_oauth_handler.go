package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func excelOAuthAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "账号 ID 无效")
		return 0, false
	}
	return id, true
}

func (h *OpenAIOAuthHandler) ExcelAuthorizationStatus(c *gin.Context) {
	id, ok := excelOAuthAccountID(c)
	if !ok {
		return
	}
	status, err := h.openaiOAuthService.ExcelAuthorizationStatus(c.Request.Context(), id)
	if err != nil {
		response.Error(c, 503, "Excel 授权状态不可用，请重新授权或稍后重试")
		return
	}
	response.Success(c, status)
}

func (h *OpenAIOAuthHandler) GenerateExcelAccountAuthURL(c *gin.Context) {
	id, ok := excelOAuthAccountID(c)
	if !ok {
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account.Platform != service.PlatformOpenAI || account.Type != service.AccountTypeOAuth || account.IsShadow() || account.IsOpenAIAgentIdentity() || account.IsOpenAIPersonalAccessToken() || account.IsExcelOAuth() {
		response.BadRequest(c, "此账号不支持独立 Excel 授权")
		return
	}
	result, err := h.openaiOAuthService.GenerateAuthURL(c.Request.Context(), account.ProxyID, "", service.PlatformOpenAI, "excel")
	if err != nil {
		response.Error(c, 502, "生成 Excel 授权链接失败")
		return
	}
	response.Success(c, result)
}

func (h *OpenAIOAuthHandler) ExchangeExcelAccountCode(c *gin.Context) {
	id, ok := excelOAuthAccountID(c)
	if !ok {
		return
	}
	var req OpenAIExchangeCodeRequest
	if c.ShouldBindJSON(&req) != nil {
		response.BadRequest(c, "请提供授权会话、回调授权码和 state")
		return
	}
	err := h.openaiOAuthService.ExchangeExcelAccountCode(c.Request.Context(), id, &service.OpenAIExchangeCodeInput{SessionID: req.SessionID, Code: req.Code, State: req.State})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"authorized": true})
}

// 自动授权接口不返回密码、TOTP 或授权令牌。
func (h *OpenAIOAuthHandler) ExcelAutoStatus(c *gin.Context) {
	id, ok := excelOAuthAccountID(c)
	if !ok {
		return
	}
	status, err := h.openaiOAuthService.ExcelAutoStatus(c.Request.Context(), id)
	if err != nil {
		response.Error(c, 503, "自动授权状态不可用，请稍后重试")
		return
	}
	response.Success(c, status)
}
func (h *OpenAIOAuthHandler) SaveExcelAuto(c *gin.Context) {
	id, ok := excelOAuthAccountID(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16384)
	var input service.ExcelAutoInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "登录资料格式无效或超出大小限制")
		return
	}
	if err := h.openaiOAuthService.SaveExcelAuto(c.Request.Context(), id, input); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"saved": true})
}
