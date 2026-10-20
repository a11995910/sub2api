package admin

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

// PromptTestCapabilities 供外部管理工具确认接口支持自定义题目，避免误发默认连通性题目。
func (h *AccountHandler) PromptTestCapabilities(c *gin.Context) {
	response.Success(c, gin.H{"version": 1})
}

func validateAccountPromptTest(req TestAccountRequest) string {
	if strings.TrimSpace(req.Prompt) == "" || len([]rune(req.Prompt)) > 20000 {
		return "自定义测试题目不能为空且不能超过 20000 字符"
	}
	if req.ModelID != "gpt-6-astra" && req.ModelID != "gpt-5.6-sol" {
		return "请选择支持的自定义测试模型"
	}
	switch req.ReasoningEffort {
	case "", "low", "medium", "high", "xhigh", "max", "ultra":
	default:
		return "测试思考强度无效"
	}
	if req.ImageDataURL != "" || req.AudioDataURL != "" {
		return "自定义题目测试仅支持文本"
	}
	return ""
}
