package service

import "time"

// smartRotationPlanForAccounts 只读取内存快照和当前请求候选，不访问数据库，避免
// 智能轮候给账号选择热路径增加数据库往返。快照过期或配置不是 smart 时返回 nil，
// 调度器继续使用原有优先级、负载和粘性规则。
func (s *OpenAIGatewayService) smartRotationPlanForAccounts(accounts []*Account, now time.Time) *AccountSmartRotationPlan {
	if s == nil || s.accountTimeRotation == nil || len(accounts) == 0 {
		return nil
	}
	config, _ := s.accountTimeRotation.Snapshot(now)
	if config == nil || !config.Enabled || config.Mode != "smart" || config.Smart == nil {
		return nil
	}
	plan := BuildAccountSmartRotationPlan(config, accounts, now)
	if plan == nil {
		return nil
	}
	// 类型变更等失效配置只用于状态展示，不给合法的池外请求增加惩罚。
	for id, tier := range plan.Tiers {
		if tier >= 3 {
			delete(plan.Tiers, id)
		}
	}
	if len(plan.Tiers) == 0 {
		return nil
	}
	return plan
}

// smartRotationTier 将池外账号置于普通备用层。同层仍按原有优先级和负载
// 排序；使用完整层级保证比较关系可传递，避免部分候选入池时排序不稳定。
func smartRotationTier(plan *AccountSmartRotationPlan, account *Account) int {
	if plan != nil && account != nil {
		if tier, ok := plan.Tiers[account.ID]; ok {
			return tier
		}
	}
	return 1
}

func compareSmartRotationTier(plan *AccountSmartRotationPlan, a, b *Account) (bool, bool) {
	ta, tb := smartRotationTier(plan, a), smartRotationTier(plan, b)
	return ta < tb, ta != tb
}

// smartRotationEnabled 使智能模式在旧的批量负载开关关闭时仍能探测备用容量。
func (s *OpenAIGatewayService) smartRotationEnabled(now time.Time) bool {
	if s == nil || s.accountTimeRotation == nil {
		return false
	}
	config, _ := s.accountTimeRotation.Snapshot(now)
	return config != nil && config.Enabled && config.Mode == "smart" && config.Smart != nil
}

// compact 的已知不支持候选保留原有陈旧快照复查兜底，但不占用主力名额。
func (s *OpenAIGatewayService) smartRotationPlanForRequest(accounts []*Account, requireCompact bool, now time.Time) *AccountSmartRotationPlan {
	if !requireCompact {
		return s.smartRotationPlanForAccounts(accounts, now)
	}
	eligible := make([]*Account, 0, len(accounts))
	for _, account := range accounts {
		if account != nil && openAICompactSupportTier(account) > 0 {
			eligible = append(eligible, account)
		}
	}
	return s.smartRotationPlanForAccounts(eligible, now)
}
