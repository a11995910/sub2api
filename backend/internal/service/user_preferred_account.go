package service

import (
	"context"
	"strings"
	"time"
)

type userPreferredAccountContextKey struct{}

// WithUserPreferredAccount 仅由认证后的用户配置装入上下文，不接受客户端指定上游账号。
func WithUserPreferredAccount(ctx context.Context, accountID int64) context.Context {
	return context.WithValue(ctx, userPreferredAccountContextKey{}, accountID)
}

func userPreferredAccountID(ctx context.Context) int64 {
	id, _ := ctx.Value(userPreferredAccountContextKey{}).(int64)
	return id
}

func excludePreferredAccount(excluded map[int64]struct{}, id int64) map[int64]struct{} {
	result := cloneExcludedAccountIDs(excluded)
	if result == nil {
		result = make(map[int64]struct{})
	}
	result[id] = struct{}{}
	return result
}

// 优先账号必须实际属于当前分组；简易模式也不能放宽该约束。
func preferredAccountMatchesGroup(account *Account, groupID *int64) bool {
	return groupID != nil && *groupID > 0 && openAIStickyAccountMatchesGroup(account, groupID)
}

// selectUserPreferredAccount 复用已有准入、抢槽和抢槽后复查，不修改共享账号优先级。
// 必须保留上下文的续聊、守护母账号和任务查询继续使用原绑定。
func (s *defaultOpenAIAccountScheduler) selectUserPreferredAccount(ctx context.Context, req *OpenAIAccountScheduleRequest) (*AccountSelectionResult, error) {
	id := userPreferredAccountID(ctx)
	if id <= 0 || req.GroupID == nil || s == nil || s.service == nil ||
		req.DisableStickyEscape || req.PreserveStickyBinding || req.GuardianParentAccountID > 0 ||
		strings.TrimSpace(req.PreviousResponseID) != "" {
		return nil, nil
	}
	if _, excluded := req.ExcludedIDs[id]; excluded {
		return nil, nil
	}
	if req.StickyAccountID > 0 {
		bound, _ := s.service.getSchedulableAccount(ctx, req.StickyAccountID)
		if s.service.accountTimeRotation.preservesProtectedSession(bound) {
			return nil, nil
		}
	}
	if s.service.checkChannelPricingRestriction(ctx, req.GroupID, req.RequestedModel) {
		return nil, nil
	}
	account, err := s.service.getSchedulableAccount(ctx, id)
	if err != nil || !preferredAccountMatchesGroup(account, req.GroupID) {
		return nil, nil
	}
	account = s.service.resolveFreshSchedulableOpenAIAccount(ctx, account, req.Platform, req.RequestedModel, req.RequireCompact, req.RequiredCapability)
	if account == nil || !s.isAccountRequestCompatible(ctx, account, *req) ||
		!s.isAccountTransportCompatible(account, req.RequiredTransport) ||
		!s.service.accountTimeRotation.allowsNewSession(ctx, account) {
		return nil, nil
	}
	if len(s.filterGrokFreeQuotaAccounts(ctx, []Account{*account})) == 0 ||
		isGrokTeamModelRateLimited(account, canonicalOpenAIAccountSchedulingModel(account, req.RequestedModel), time.Now()) ||
		isGrokModelQuotaBlocked(account.ID, canonicalOpenAIAccountSchedulingModel(account, req.RequestedModel), time.Now()) {
		return nil, nil
	}
	probeReq := *req
	probeReq.PreserveStickyBinding = true
	selection, _, err := s.tryAcquireOpenAISelectionOrder(ctx, probeReq, []openAIAccountCandidateScore{{account: account}})
	if err != nil {
		return nil, err
	}
	if selection != nil && !preferredAccountMatchesGroup(selection.Account, req.GroupID) {
		if selection.ReleaseFunc != nil {
			selection.ReleaseFunc()
		}
		selection = nil
	}
	if selection != nil && req.SessionHash != "" {
		_ = s.service.bindOpenAIStickySessionDuringSelection(ctx, req.GroupID, req.SessionHash, selection.Account.ID)
	}
	if selection == nil {
		// 容量已满或抢槽后失效时，避免普通粘性路径再次等待同一账号。
		req.ExcludedIDs = excludePreferredAccount(req.ExcludedIDs, id)
	}
	return selection, nil
}

func (s *GatewayService) userPreferredAccount(ctx context.Context, groupID *int64, group *Group, model, platform string, useMixed bool, excluded map[int64]struct{}) *Account {
	id := userPreferredAccountID(ctx)
	if id <= 0 || groupID == nil {
		return nil
	}
	if _, skip := excluded[id]; skip {
		return nil
	}
	if group == nil {
		var err error
		group, err = s.resolveGroupByID(ctx, *groupID)
		if err != nil {
			return nil
		}
	}
	account, err := s.getSchedulableAccount(ctx, id)
	if err != nil || !preferredAccountMatchesGroup(account, groupID) {
		return nil
	}
	// 使用完整当前记录复查分组与状态，防止快照中已移组或已删除的账号被优先选中。
	if s.accountRepo != nil {
		account, err = s.accountRepo.GetByID(ctx, id)
		if err != nil || !preferredAccountMatchesGroup(account, groupID) {
			return nil
		}
	}
	if !s.isAccountSchedulableForSelection(account) || !s.isAccountAllowedForPlatform(account, platform, useMixed) ||
		(group != nil && group.RequirePrivacySet && !account.IsPrivacySet()) ||
		!s.isGatewayAccountProfitEligible(ctx, account) || s.isAccountBlockedBySchedulingThreshold(ctx, account) ||
		(model != "" && !s.isModelSupportedByAccountWithContext(ctx, account, model)) ||
		!s.isAccountSchedulableForModelSelection(ctx, account, model) || !s.isAccountSchedulableForQuota(account) ||
		!s.isAccountSchedulableForWindowCost(ctx, account, false) || !s.isAccountSchedulableForRPM(ctx, account, false) ||
		s.isStickyAccountUpstreamRestricted(ctx, groupID, account, model) {
		return nil
	}
	return account
}

// token count 等不占生成槽位的入口沿用已有候选准入检查。
func (s *OpenAIGatewayService) preferredOpenAIAccountWithoutSlot(ctx context.Context, groupID *int64, platform, model string, excluded map[int64]struct{}, capability OpenAIEndpointCapability) *Account {
	id := userPreferredAccountID(ctx)
	if id <= 0 || groupID == nil || s.checkChannelPricingRestriction(ctx, groupID, model) {
		return nil
	}
	if _, skip := excluded[id]; skip {
		return nil
	}
	account, err := s.getSchedulableAccount(ctx, id)
	if err != nil || !preferredAccountMatchesGroup(account, groupID) {
		return nil
	}
	selected, _, _ := s.selectBestAccount(ctx, groupID, platform, []Account{*account}, model, excluded, false, capability, false)
	if !preferredAccountMatchesGroup(selected, groupID) {
		return nil
	}
	return selected
}
