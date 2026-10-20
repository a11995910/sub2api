package service

import (
	"context"

	"github.com/gin-gonic/gin"
)

type accountPromptTestContextKey struct{}

// TestPromptAccountConnection 复用指定账号的真实请求和流解析，替换题目与思考强度。
// 与连通性测试分支隔离，成功后不自动清除原账号的限流或调度状态。
func (s *AccountTestService) TestPromptAccountConnection(c *gin.Context, accountID int64, modelID, prompt, effort string) error {
	ctx := context.WithValue(c.Request.Context(), accountPromptTestContextKey{}, true)
	ctx = withPelicanTestOptions(ctx, pelicanTestOptions{prompt: prompt, reasoningEffort: effort})
	c.Request = c.Request.WithContext(ctx)
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || account == nil {
		return s.sendErrorAndEnd(c, "Account not found")
	}
	if account.Platform != PlatformOpenAI || account.IsSyntheticUITest() {
		return s.sendErrorAndEnd(c, "Custom prompt tests require a real OpenAI account")
	}
	return s.TestAccountConnection(c, accountID, modelID, prompt, AccountTestModeDefault)
}
