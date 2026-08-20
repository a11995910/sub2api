package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	ModelTestRequestHeader       = service.ModelTestRequestHeader
	ModelTestAuthorizationHeader = service.ModelTestAuthorizationHeader
)

// NewModelTestAuthMiddleware 创建测试台双重身份校验中间件。
// API Key 鉴权必须先执行；本中间件再校验面板 JWT 与 API Key 是否属于同一用户。
func NewModelTestAuthMiddleware(
	authService *service.AuthService,
	userService *service.UserService,
	settingService *service.SettingService,
	auditService *service.AuditLogService,
) ModelTestAuthMiddleware {
	return ModelTestAuthMiddleware(func(c *gin.Context) {
		rawMode := strings.TrimSpace(c.GetHeader(ModelTestRequestHeader))
		if rawMode == "" {
			c.Next()
			return
		}

		mode, ok := service.NormalizeModelTestMode(rawMode)
		if !ok {
			AbortWithError(c, http.StatusBadRequest, "INVALID_MODEL_TEST_MODE", "Invalid model test request mode")
			return
		}
		if !modelTestModeMatchesRequest(c, mode) {
			AbortWithError(c, http.StatusBadRequest, "MODEL_TEST_ENDPOINT_MISMATCH", "Model test mode does not match the request endpoint")
			return
		}

		apiKey, ok := GetAPIKeyFromContext(c)
		if !ok || apiKey == nil || apiKey.User == nil {
			AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_API_KEY_REQUIRED", "A valid API key is required for model testing")
			return
		}

		token, ok := extractModelTestBearerToken(c.GetHeader(ModelTestAuthorizationHeader))
		if !ok || authService == nil || userService == nil {
			AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_AUTH_REQUIRED", "A valid panel session is required for model testing")
			return
		}

		claims, err := authService.ValidateToken(token)
		if err != nil {
			if errors.Is(err, service.ErrTokenExpired) {
				AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_TOKEN_EXPIRED", "Panel session has expired")
				return
			}
			AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_INVALID_TOKEN", "Invalid panel session")
			return
		}
		if claims.UserID != apiKey.User.ID || apiKey.UserID != claims.UserID {
			AbortWithError(c, http.StatusForbidden, "MODEL_TEST_API_KEY_FORBIDDEN", "The selected API key does not belong to the current user")
			return
		}
		user, err := userService.GetByID(c.Request.Context(), claims.UserID)
		if err != nil {
			AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_USER_NOT_FOUND", "User not found")
			return
		}
		if !user.IsActive() {
			AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_USER_INACTIVE", "User account is not active")
			return
		}
		if claims.TokenVersion != user.TokenVersion {
			AbortWithError(c, http.StatusUnauthorized, "MODEL_TEST_TOKEN_REVOKED", "Panel session has been revoked")
			return
		}
		if !enforceSessionBinding(c, authService, settingService, auditService, claims) {
			return
		}
		if !service.SetTrustedModelTestMode(c, mode) {
			AbortWithError(c, http.StatusBadRequest, "INVALID_MODEL_TEST_MODE", "Invalid model test request mode")
			return
		}

		c.Next()
	})
}

func extractModelTestBearerToken(value string) (string, bool) {
	parts := strings.SplitN(strings.TrimSpace(value), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

func modelTestModeMatchesRequest(c *gin.Context, mode string) bool {
	if c == nil || c.Request == nil || c.Request.Method != http.MethodPost {
		return false
	}
	switch mode {
	case service.ModelTestModeText:
		return c.Request.URL.Path == "/v1/chat/completions"
	case service.ModelTestModeImage:
		return c.Request.URL.Path == "/v1/images/generations" || c.Request.URL.Path == "/v1/images/edits"
	case service.ModelTestModeVideo:
		return c.Request.URL.Path == "/v1/videos"
	default:
		return false
	}
}
