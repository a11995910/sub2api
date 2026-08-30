package service

import (
	"context"
	"fmt"
	"time"
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

type OAuthAccountPoolRequestTokenStats struct {
	Requests int64
	Tokens   int64
}

type OAuthAccountPoolAccountStats struct {
	FiveHour OAuthAccountPoolRequestTokenStats
	SevenDay OAuthAccountPoolRequestTokenStats
	Total    OAuthAccountPoolRequestTokenStats
}

type OAuthAccountPoolStatsWindow struct {
	AccountID     int64
	FiveHourStart time.Time
	SevenDayStart time.Time
}

// OAuthAccountPoolStatsReader 批量读取号池账号的窗口及累计统计。
type OAuthAccountPoolStatsReader interface {
	GetOAuthAccountPoolStats(ctx context.Context, windows []OAuthAccountPoolStatsWindow) (map[int64]OAuthAccountPoolAccountStats, error)
}

type oauthAccountPoolConcurrencyReader interface {
	GetAccountConcurrencyBatch(ctx context.Context, accountIDs []int64) (map[int64]int, error)
}

type OAuthAccountPoolAccount struct {
	Identifier         string
	PlanType           string
	CurrentConcurrency int
	Concurrency        int
	ExpiresAt          *time.Time
	FiveHour           *UsageProgress
	SevenDay           *UsageProgress
}

type OAuthAccountPoolSummary struct {
	AccountCount int
}

type OAuthAccountPoolGroup struct {
	Name     string
	Accounts []OAuthAccountPoolAccount
	Summary  OAuthAccountPoolSummary
}

type OAuthAccountPool struct {
	Groups  []OAuthAccountPoolGroup
	Summary OAuthAccountPoolSummary
}

// OAuthAccountPoolService 只读取当前用户可访问分组中的 OAuth 账号缓存快照。
type OAuthAccountPoolService struct {
	apiKeyService       oauthAccountPoolGroupAccessReader
	accountRepo         OAuthAccountPoolRepository
	accountUsageService *AccountUsageService
	concurrencyReader   oauthAccountPoolConcurrencyReader
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
		concurrencyReader:   apiKeyService,
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
	if s.accountUsageService == nil {
		return nil, fmt.Errorf("oauth account pool dependencies are unavailable")
	}

	usageByAccountID := make(map[int64]*UsageInfo, len(bindings))
	accountIDs := make([]int64, 0, len(bindings))
	for i := range bindings {
		binding := &bindings[i]
		if _, exists := usageByAccountID[binding.Account.ID]; exists {
			continue
		}
		usage := s.accountUsageService.BuildCachedUsage(&binding.Account)
		usageByAccountID[binding.Account.ID] = usage
		accountIDs = append(accountIDs, binding.Account.ID)
	}
	concurrencyByAccountID := make(map[int64]int, len(accountIDs))
	if s.concurrencyReader != nil {
		if counts, concurrencyErr := s.concurrencyReader.GetAccountConcurrencyBatch(ctx, accountIDs); concurrencyErr == nil {
			concurrencyByAccountID = counts
		}
	}

	accountsByGroupID := make(map[int64][]OAuthAccountPoolAccount, len(visibleGroups))
	for i := range bindings {
		binding := &bindings[i]
		usage := usageByAccountID[binding.Account.ID]
		accountsByGroupID[binding.GroupID] = append(accountsByGroupID[binding.GroupID], OAuthAccountPoolAccount{
			Identifier:         ResolveOAuthAccountDisplayIdentifier(&binding.Account),
			PlanType:           OAuthAccountPlanLabel(ResolveOAuthAccountPlanType(&binding.Account)),
			CurrentConcurrency: concurrencyByAccountID[binding.Account.ID],
			Concurrency:        binding.Account.Concurrency,
			ExpiresAt:          ResolveOAuthAccountDisplayExpiresAt(&binding.Account),
			FiveHour:           usage.FiveHour,
			SevenDay:           usage.SevenDay,
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
			Summary:  OAuthAccountPoolSummary{AccountCount: len(accounts)},
		})
	}
	result.Summary.AccountCount = len(accountIDs)
	return result, nil
}
