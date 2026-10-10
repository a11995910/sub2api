package service

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
)

// Excel 客户端凭据只能用于 BPS，不能回退到原生 Codex。
func (a *Account) IsExcelOAuth() bool {
	return a != nil && a.IsOpenAIOAuthLike() && strings.TrimSpace(a.GetCredential("client_id")) == openai.ExcelClientID
}

var errExcelOAuthRouteUnavailable = errors.New("an enabled Excel BPS route is required for Excel OAuth credentials and the requested model; native Codex is unavailable")

const excelOAuthRouteErrorCode = "excel_oauth_route_unavailable"

func writeExcelOAuthRouteError(c *gin.Context) error {
	c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
		"type": "invalid_request_error", "code": excelOAuthRouteErrorCode,
		"message": errExcelOAuthRouteUnavailable.Error(),
	}})
	return errExcelOAuthRouteUnavailable
}
