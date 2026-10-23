package service

import (
	"context"
	"time"
)

// 只在测试请求发送前使用，真正测试产生的错误仍按测试失败记录。
type candyTestSkippedError struct{ reason string }

func (e *candyTestSkippedError) Error() string { return e.reason }

func (s *AccountTestService) checkCandyAccountAvailable(ctx context.Context, account *Account, model string) error {
	skip := func(reason string) error { return &candyTestSkippedError{reason: reason} }
	if account == nil {
		return skip("账号不存在，已跳过测试")
	}
	if account.IsRateLimited() {
		return skip("账号处于 429 限流冷却，已跳过测试")
	}
	if !account.IsSchedulable() {
		return skip("账号已停用、暂停调度、过期或处于故障冷却，已跳过测试")
	}
	if model == "" {
		model = AccountIntelligenceDefaultModel
	}
	if account.isModelRateLimitedWithContext(ctx, model) {
		return skip("测试模型处于限流或故障冷却，已跳过测试")
	}
	if gateway := s.openaiGatewayService; gateway != nil {
		ctx = gateway.withOpenAIQuotaAutoPauseContext(ctx)
		if paused, _ := shouldAutoPauseOpenAIAccountByQuota(ctx, account); paused {
			return skip("账号额度保护已暂停调度，已跳过测试")
		}
		if err := gateway.accountTimeRotation.CheckRequestProtection(account, time.Now()); err != nil {
			return skip("账号正在休息、限速或保护状态不可用，已跳过测试")
		}
		if gateway.isOpenAIAccountSchedulingBlocked(ctx, account, model) || gateway.isOpenAIProxyStreamQuarantined(ctx, account) {
			return skip("账号正在休息或请求通道暂不可用，已跳过测试")
		}
	}
	return nil
}
