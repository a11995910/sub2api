package service

import (
	"context"
	"fmt"
)

// OAuthAccountPoolBinding 是仓储返回的分组与 OAuth 账号绑定。
// Account 只应加载号池展示和缓存额度解析所需字段。
type OAuthAccountPoolBinding struct {
	GroupID int64
	Account Account
}

// OAuthAccountPoolRepository 提供用户号池所需的最小只读查询能力。
type OAuthAccountPoolRepository interface {
	ListActiveOAuthByGroupIDs(ctx context.Context, groupIDs []int64) ([]OAuthAccountPoolBinding, error)
}

type oauthAccountPoolGroupAccessReader interface {
	GetAvailableGroups(ctx context.Context, userID int64) ([]Group, error)
}

type OAuthAccountPoolAccount struct {
	Name     string
	FiveHour *UsageProgress
	SevenDay *UsageProgress
}

type OAuthAccountPoolGroup struct {
	Name     string
	Accounts []OAuthAccountPoolAccount
}

type OAuthAccountPool struct {
	Groups []OAuthAccountPoolGroup
}

// OAuthAccountPoolService 只读取当前用户可访问分组中的 OAuth 账号缓存快照。
type OAuthAccountPoolService struct {
	apiKeyService       oauthAccountPoolGroupAccessReader
	accountRepo         OAuthAccountPoolRepository
	accountUsageService *AccountUsageService
}

func NewOAuthAccountPoolService(
	apiKeyService *APIKeyService,
	accountRepo OAuthAccountPoolRepository,
	accountUsageService *AccountUsageService,
) *OAuthAccountPoolService {
	return &OAuthAccountPoolService{
		apiKeyService:       apiKeyService,
		accountRepo:         accountRepo,
		accountUsageService: accountUsageService,
	}
}

// GetForUser 返回当前用户有权访问且显式公开号池的分组。
// 此方法不会访问 OAuth 上游，也不会更新账号数据。
func (s *OAuthAccountPoolService) GetForUser(ctx context.Context, userID int64) (*OAuthAccountPool, error) {
	groups, err := s.apiKeyService.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get available groups: %w", err)
	}

	visibleGroups := make([]Group, 0, len(groups))
	groupIDs := make([]int64, 0, len(groups))
	for i := range groups {
		if !groups[i].OAuthPoolVisible {
			continue
		}
		visibleGroups = append(visibleGroups, groups[i])
		groupIDs = append(groupIDs, groups[i].ID)
	}
	if len(groupIDs) == 0 {
		return &OAuthAccountPool{Groups: []OAuthAccountPoolGroup{}}, nil
	}

	bindings, err := s.accountRepo.ListActiveOAuthByGroupIDs(ctx, groupIDs)
	if err != nil {
		return nil, fmt.Errorf("list visible oauth accounts: %w", err)
	}
	accountsByGroupID := make(map[int64][]OAuthAccountPoolAccount, len(visibleGroups))
	for i := range bindings {
		binding := &bindings[i]
		usage := s.accountUsageService.BuildCachedUsage(&binding.Account)
		accountsByGroupID[binding.GroupID] = append(accountsByGroupID[binding.GroupID], OAuthAccountPoolAccount{
			Name:     binding.Account.Name,
			FiveHour: usage.FiveHour,
			SevenDay: usage.SevenDay,
		})
	}

	result := &OAuthAccountPool{Groups: make([]OAuthAccountPoolGroup, 0, len(visibleGroups))}
	for i := range visibleGroups {
		accounts := accountsByGroupID[visibleGroups[i].ID]
		if len(accounts) == 0 {
			continue
		}
		result.Groups = append(result.Groups, OAuthAccountPoolGroup{
			Name:     visibleGroups[i].Name,
			Accounts: accounts,
		})
	}
	return result, nil
}
