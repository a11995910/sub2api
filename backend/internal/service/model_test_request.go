package service

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const trustedModelTestModeContextKey = "sub2api_trusted_model_test_mode"

const (
	ModelTestRequestHeader             = "X-Sub2API-Model-Test"
	ModelTestAuthorizationHeader       = "X-Sub2API-Model-Test-Authorization"
	modelTestAuthorizationHeaderForLog = "x-sub2api-model-test-authorization"

	ModelTestModeText  = "text"
	ModelTestModeImage = "image"
	ModelTestModeVideo = "video"
)

// NormalizeModelTestMode 只接受测试台当前实际支持的请求类型。
func NormalizeModelTestMode(value string) (string, bool) {
	mode := strings.TrimSpace(value)
	switch mode {
	case ModelTestModeText, ModelTestModeImage, ModelTestModeVideo:
		return mode, true
	default:
		return "", false
	}
}

// SetTrustedModelTestMode 记录已通过面板会话与 API Key 归属校验的测试台请求。
// 调用方必须先完成校验，不能根据客户端可伪造的请求头直接设置。
func SetTrustedModelTestMode(c *gin.Context, mode string) bool {
	if c == nil {
		return false
	}
	normalized, ok := NormalizeModelTestMode(mode)
	if !ok {
		return false
	}
	c.Set(trustedModelTestModeContextKey, normalized)
	return true
}

// TrustedModelTestMode 返回服务端已认证的测试台请求类型。
func TrustedModelTestMode(c *gin.Context) (string, bool) {
	if c == nil {
		return "", false
	}
	value, exists := c.Get(trustedModelTestModeContextKey)
	if !exists {
		return "", false
	}
	mode, ok := value.(string)
	if !ok {
		return "", false
	}
	return NormalizeModelTestMode(mode)
}

// IsTrustedModelTestRequest 判断请求是否已通过站内测试台双重身份校验。
func IsTrustedModelTestRequest(c *gin.Context) bool {
	_, ok := TrustedModelTestMode(c)
	return ok
}
