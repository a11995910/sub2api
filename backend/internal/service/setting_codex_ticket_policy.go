package service

import (
	"context"
	"time"
)

// OpenAICodexTicketPolicy 控制采集验证与业务请求的门票行为。
type OpenAICodexTicketPolicy struct {
	ModelMismatchInvalidation bool
	UseHarvestProxy           bool
}

type cachedOpenAICodexTicketPolicy struct {
	value     OpenAICodexTicketPolicy
	expiresAt time.Time
}

func defaultOpenAICodexTicketPolicy() OpenAICodexTicketPolicy {
	return OpenAICodexTicketPolicy{ModelMismatchInvalidation: true, UseHarvestProxy: true}
}

// GetOpenAICodexTicketPolicy 缺失设置时保留原有行为；保存后立即失效缓存。
func (s *SettingService) GetOpenAICodexTicketPolicy(ctx context.Context) OpenAICodexTicketPolicy {
	fallback := defaultOpenAICodexTicketPolicy()
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.openAICodexTicketPolicyMu.Lock()
	defer s.openAICodexTicketPolicyMu.Unlock()
	if cached := s.openAICodexTicketPolicyCache; cached != nil {
		fallback = cached.value
		if time.Now().Before(cached.expiresAt) {
			return fallback
		}
	}
	if ctx.Err() != nil {
		return fallback
	}
	dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	values, err := s.settingRepo.GetMultiple(dbCtx, []string{
		SettingKeyOpenAICodexTicketModelMismatchInvalidation,
		SettingKeyOpenAICodexTicketUseHarvestProxy,
	})
	if err != nil {
		// 存储短暂故障时保留已知策略，并避免业务请求排队重复查询。
		s.openAICodexTicketPolicyCache = &cachedOpenAICodexTicketPolicy{value: fallback, expiresAt: time.Now().Add(time.Second)}
		return fallback
	}
	policy := OpenAICodexTicketPolicy{
		ModelMismatchInvalidation: values[SettingKeyOpenAICodexTicketModelMismatchInvalidation] != "false",
		UseHarvestProxy:           values[SettingKeyOpenAICodexTicketUseHarvestProxy] != "false",
	}
	s.openAICodexTicketPolicyCache = &cachedOpenAICodexTicketPolicy{value: policy, expiresAt: time.Now().Add(5 * time.Second)}
	return policy
}

func (s *SettingService) InvalidateOpenAICodexTicketPolicyCache() {
	if s == nil {
		return
	}
	s.openAICodexTicketPolicyMu.Lock()
	defer s.openAICodexTicketPolicyMu.Unlock()
	if s.openAICodexTicketPolicyCache != nil {
		s.openAICodexTicketPolicyCache.expiresAt = time.Time{}
	}
}

func (s *OpenAIGatewayService) openAICodexTicketPolicy(ctx context.Context) OpenAICodexTicketPolicy {
	return s.settingService.GetOpenAICodexTicketPolicy(ctx)
}

func (s *OpenAIGatewayService) openAICodexTicketRequestProxy(ctx context.Context, ticket *openAICodexTicket, fallback string) string {
	if ticket != nil && s.openAICodexTicketPolicy(ctx).UseHarvestProxy {
		return ticket.ProxyURL
	}
	return fallback
}
