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
	plan := s.accountTimeRotation.HealthPlan(config, accounts, now)
	if plan == nil {
		return nil
	}
	if len(plan.Tiers) == 0 {
		return nil
	}
	return plan
}

// smartRotationTier 将池外账号与正常账号放在同层；观察机会先行，等待账号兜底。
func smartRotationTier(plan *AccountSmartRotationPlan, account *Account) int {
	if plan != nil && account != nil {
		if tier, ok := plan.Tiers[account.ID]; ok {
			return tier
		}
	}
	return 1
}

func compareSmartRotationTier(plan *AccountSmartRotationPlan, a, b *Account) (bool, bool) {
	if plan == nil {
		return false, false
	}
	ta, tb := smartRotationTier(plan, a), smartRotationTier(plan, b)
	if ta != tb {
		return ta < tb, true
	}
	pa, pb := smartRotationPriority(plan, a), smartRotationPriority(plan, b)
	return pa < pb, pa != pb
}

// smartRotationEnabled 使智能模式在旧的批量负载开关关闭时仍能探测备用容量。
func (s *OpenAIGatewayService) smartRotationEnabled(now time.Time) bool {
	if s == nil || s.accountTimeRotation == nil {
		return false
	}
	config, _ := s.accountTimeRotation.Snapshot(now)
	return config != nil && config.Enabled && config.Mode == "smart" && config.Smart != nil
}

// compact 的已知不支持候选保留原有陈旧快照复查兜底，但不授予观察优先。
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

func smartRotationPriority(plan *AccountSmartRotationPlan, account *Account) int {
	if account == nil {
		return 0
	}
	if plan != nil {
		if priority, ok := plan.Priorities[account.ID]; ok {
			return priority
		}
	}
	return account.Priority
}

func smartRotationHasProbe(plan *AccountSmartRotationPlan) bool {
	if plan != nil {
		for _, tier := range plan.Tiers {
			if tier == 0 {
				return true
			}
		}
	}
	return false
}

// 有待观察账号时让可迁移粘性进入候选筛选，避免恢复账号永久没有真实请求。
func (s *OpenAIGatewayService) shouldYieldSmartSticky(id int64, now time.Time) bool {
	if s == nil || s.accountTimeRotation == nil {
		return false
	}
	rotation := s.accountTimeRotation
	if rotation.IsDegraded(id, now) {
		return true
	}
	rotation.snapshotMu.Lock()
	defer rotation.snapshotMu.Unlock()
	if rotation.snapshot == nil || !rotation.snapshot.Enabled || rotation.snapshot.Mode != "smart" || now.Sub(rotation.refreshedAt) > 45*time.Second {
		return false
	}
	for accountID := range rotation.health {
		h := rotation.healthLocked(accountID, now)
		if h.State == "recovering" && !h.ProbeInFlight && !now.Before(h.NextProbeAt) {
			return true
		}
	}
	return false
}

// 智能轮候账号统一按实际内容输出计首字，避免初始化事件掩盖真实等待时间。
func (s *OpenAIGatewayService) smartRotationMeasuresVisibleTTFT(account *Account) bool {
	if s == nil || s.accountTimeRotation == nil || account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth || account.ParentAccountID != nil {
		return false
	}
	rotation := s.accountTimeRotation
	rotation.snapshotMu.RLock()
	defer rotation.snapshotMu.RUnlock()
	return rotation.healthPolicyLocked(account.ID, time.Now()) != nil
}
