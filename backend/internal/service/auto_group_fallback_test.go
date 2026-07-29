package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type autoFallbackGroupRepoStub struct {
	groupRepoNoop
	groups  map[int64]*Group
	created *Group
	updated *Group
}

type autoFallbackAccountRepoStub struct {
	AccountRepository
	accounts []Account
}

func (s *autoFallbackAccountRepoStub) GetByID(_ context.Context, id int64) (*Account, error) {
	for i := range s.accounts {
		if s.accounts[i].ID == id {
			return &s.accounts[i], nil
		}
	}
	return nil, ErrAccountNotFound
}

func (s *autoFallbackAccountRepoStub) ListSchedulableByGroupIDAndPlatform(_ context.Context, groupID int64, platform string) ([]Account, error) {
	accounts := make([]Account, 0)
	for i := range s.accounts {
		account := s.accounts[i]
		if account.Platform == platform && openAIStickyAccountMatchesGroup(&account, &groupID) {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (s *autoFallbackAccountRepoStub) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, _ bool) ([]Account, error) {
	accounts := make([]Account, 0)
	for i := range s.accounts {
		account := s.accounts[i]
		platformMatched := false
		for _, platform := range platforms {
			if account.Platform == platform {
				platformMatched = true
				break
			}
		}
		if platformMatched && openAIStickyAccountMatchesGroup(&account, groupID) {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (s *autoFallbackGroupRepoStub) Create(_ context.Context, group *Group) error {
	s.created = group
	if group.ID == 0 {
		group.ID = 100
	}
	return nil
}

func (s *autoFallbackGroupRepoStub) GetByID(_ context.Context, id int64) (*Group, error) {
	return s.GetByIDLite(context.Background(), id)
}

func (s *autoFallbackGroupRepoStub) GetByIDLite(_ context.Context, id int64) (*Group, error) {
	group, ok := s.groups[id]
	if !ok {
		return nil, ErrGroupNotFound
	}
	return group, nil
}

func (s *autoFallbackGroupRepoStub) Update(_ context.Context, group *Group) error {
	s.updated = group
	return nil
}

func autoFallbackTestGroup(id int64, rate float64, next *int64) *Group {
	return &Group{
		ID:                  id,
		Name:                "group",
		Platform:            PlatformOpenAI,
		Status:              StatusActive,
		SubscriptionType:    SubscriptionTypeStandard,
		RateMultiplier:      rate,
		AutoFallbackGroupID: next,
		Hydrated:            true,
	}
}

func TestAdvanceAutoGroupFallback_ChainsAndUpdatesEffectiveBillingGroup(t *testing.T) {
	proID := int64(73)
	fullPriceID := int64(74)
	plus := autoFallbackTestGroup(72, 0.12, &proID)
	pro := autoFallbackTestGroup(proID, 0.18, &fullPriceID)
	fullPrice := autoFallbackTestGroup(fullPriceID, 0.26, nil)
	repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{
		plus.ID: plus, pro.ID: pro, fullPrice.ID: fullPrice,
	}}
	apiKey := &APIKey{
		ID:                       9,
		GroupID:                  &plus.ID,
		Group:                    plus,
		AutoGroupFallbackEnabled: true,
	}
	ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
	diagnose := func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	next, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", diagnose)
	require.True(t, ok)
	require.Equal(t, proID, *next)
	require.Same(t, pro, apiKey.Group)

	next, ok = advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", diagnose)
	require.True(t, ok)
	require.Equal(t, fullPriceID, *next)
	require.Same(t, fullPrice, apiKey.Group)
	require.Equal(t, 0.26, apiKey.Group.RateMultiplier, "计费必须读取实际承接组倍率")
}

func TestOpenAIGatewayAutoGroupFallback_SelectsTargetGroupAccount(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	proID := int64(73)
	plus := autoFallbackTestGroup(72, 0.12, &proID)
	pro := autoFallbackTestGroup(proID, 0.18, nil)
	groupRepo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{
		plus.ID: plus,
		pro.ID:  pro,
	}}
	cooldownUntil := time.Now().Add(time.Hour)
	accountRepo := &autoFallbackAccountRepoStub{accounts: []Account{
		{
			ID:               7201,
			Platform:         PlatformOpenAI,
			Type:             AccountTypeAPIKey,
			Status:           StatusActive,
			Schedulable:      true,
			Concurrency:      1,
			RateLimitResetAt: &cooldownUntil,
			AccountGroups:    []AccountGroup{{GroupID: plus.ID}},
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"},
			},
		},
		{
			ID:            7301,
			Platform:      PlatformOpenAI,
			Type:          AccountTypeAPIKey,
			Status:        StatusActive,
			Schedulable:   true,
			Concurrency:   1,
			AccountGroups: []AccountGroup{{GroupID: pro.ID}},
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gpt-5.6-sol": "gpt-5.6-sol"},
			},
		},
	}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        accountRepo,
		groupRepo:          groupRepo,
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	apiKey := &APIKey{
		ID:                       9,
		GroupID:                  &plus.ID,
		Group:                    plus,
		AutoGroupFallbackEnabled: true,
	}
	ctx := WithAutoGroupFallbackState(context.Background(), apiKey)

	selection, _, err := svc.SelectAccountWithScheduler(
		ctx,
		apiKey.GroupID,
		"",
		"fallback-session",
		"gpt-5.6-sol",
		nil,
		OpenAIUpstreamTransportAny,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(7301), selection.Account.ID)
	require.Equal(t, proID, *apiKey.GroupID)
	require.Same(t, pro, apiKey.Group)
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestOpenAIGatewayAutoGroupFallback_UsesTargetMessagesDispatchModel(t *testing.T) {
	resetOpenAIAdvancedSchedulerSettingCacheForTest()

	proID := int64(73)
	plus := autoFallbackTestGroup(72, 0.12, &proID)
	plus.MessagesDispatchModelConfig.SonnetMappedModel = "plus-sonnet"
	pro := autoFallbackTestGroup(proID, 0.18, nil)
	pro.MessagesDispatchModelConfig.SonnetMappedModel = "pro-sonnet"
	groupRepo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{
		plus.ID: plus,
		pro.ID:  pro,
	}}
	cooldownUntil := time.Now().Add(time.Hour)
	accountRepo := &autoFallbackAccountRepoStub{accounts: []Account{
		{
			ID:               7201,
			Platform:         PlatformOpenAI,
			Type:             AccountTypeAPIKey,
			Status:           StatusActive,
			Schedulable:      true,
			Concurrency:      1,
			RateLimitResetAt: &cooldownUntil,
			AccountGroups:    []AccountGroup{{GroupID: plus.ID}},
			Credentials: map[string]any{
				"model_mapping": map[string]any{"plus-sonnet": "plus-sonnet"},
			},
		},
		{
			ID:            7301,
			Platform:      PlatformOpenAI,
			Type:          AccountTypeAPIKey,
			Status:        StatusActive,
			Schedulable:   true,
			Concurrency:   1,
			AccountGroups: []AccountGroup{{GroupID: pro.ID}},
			Credentials: map[string]any{
				"model_mapping": map[string]any{"pro-sonnet": "pro-sonnet"},
			},
		},
	}}
	cfg := &config.Config{}
	cfg.Gateway.Scheduling.LoadBatchEnabled = false
	svc := &OpenAIGatewayService{
		accountRepo:        accountRepo,
		groupRepo:          groupRepo,
		cache:              &schedulerTestGatewayCache{},
		cfg:                cfg,
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	apiKey := &APIKey{
		ID:                       9,
		GroupID:                  &plus.ID,
		Group:                    plus,
		AutoGroupFallbackEnabled: true,
	}
	ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
	ctx = WithAutoGroupFallbackMessagesModel(ctx, "claude-sonnet-4-5")

	selection, _, err := svc.SelectAccountWithSchedulerForCapability(
		ctx,
		apiKey.GroupID,
		"",
		"fallback-messages-session",
		"plus-sonnet",
		nil,
		OpenAIUpstreamTransportAny,
		OpenAIEndpointCapabilityChatCompletions,
		false,
		false,
		true,
		PlatformOpenAI,
	)

	require.NoError(t, err)
	require.NotNil(t, selection)
	require.NotNil(t, selection.Account)
	require.Equal(t, int64(7301), selection.Account.ID)
	require.Equal(t, proID, *apiKey.GroupID)
	require.Equal(t, "pro-sonnet", apiKey.Group.ResolveMessagesDispatchModel("claude-sonnet-4-5"))
	if selection.ReleaseFunc != nil {
		selection.ReleaseFunc()
	}
}

func TestAdvanceAutoGroupFallback_UsesResolvedCompositeTargetPlatform(t *testing.T) {
	targetID := int64(73)
	source := autoFallbackTestGroup(72, 0.12, &targetID)
	source.Platform = PlatformComposite
	target := autoFallbackTestGroup(targetID, 0.18, nil)
	target.Platform = PlatformComposite
	repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{targetID: target}}
	apiKey := &APIKey{GroupID: &source.ID, Group: source, AutoGroupFallbackEnabled: true}
	ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
	ctx = WithResolvedTargetPlatform(ctx, PlatformOpenAI)
	diagnosedPlatform := ""

	_, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", func(_ context.Context, _ *int64, _ string, platform string) ModelAvailabilityDiagnosis {
		diagnosedPlatform = platform
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	})

	require.True(t, ok)
	require.Equal(t, PlatformOpenAI, diagnosedPlatform)
	require.Equal(t, targetID, *apiKey.GroupID)
}

func TestAutoGroupFallback_RecordUsageUsesEffectiveGroup(t *testing.T) {
	proID := int64(73)
	plus := autoFallbackTestGroup(72, 0.12, &proID)
	pro := autoFallbackTestGroup(proID, 0.18, nil)
	groupRepo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{proID: pro}}
	apiKey := &APIKey{
		ID:                       9,
		GroupID:                  &plus.ID,
		Group:                    plus,
		AutoGroupFallbackEnabled: true,
	}
	ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
	_, ok := advanceAutoGroupFallback(ctx, groupRepo, apiKey.GroupID, "gpt-5.6-sol", func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	})
	require.True(t, ok)

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "resp_auto_group_fallback",
			Usage:     OpenAIUsage{InputTokens: 1000, OutputTokens: 100},
			Model:     "gpt-5.1",
			Duration:  time.Second,
		},
		APIKey:  apiKey,
		User:    &User{ID: 10},
		Account: &Account{ID: 7301, Type: AccountTypeAPIKey},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.NotNil(t, usageRepo.lastLog.GroupID)
	require.Equal(t, proID, *usageRepo.lastLog.GroupID)
	require.InDelta(t, pro.RateMultiplier, usageRepo.lastLog.RateMultiplier, 1e-12)
}

func TestAdvanceAutoGroupFallback_RequiresKeyOptInAndModelOwnership(t *testing.T) {
	targetID := int64(73)
	source := autoFallbackTestGroup(72, 0.12, &targetID)
	target := autoFallbackTestGroup(targetID, 0.18, nil)
	repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{targetID: target}}

	t.Run("Key 关闭时不承接", func(t *testing.T) {
		apiKey := &APIKey{GroupID: &source.ID, Group: source, AutoGroupFallbackEnabled: false}
		ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
		_, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis {
			return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
		})
		require.False(t, ok)
		require.Same(t, source, apiKey.Group)
	})

	t.Run("模型不属于当前组时不承接", func(t *testing.T) {
		apiKey := &APIKey{GroupID: &source.ID, Group: source, AutoGroupFallbackEnabled: true}
		ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
		_, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "unknown-model", func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis {
			return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: false}
		})
		require.False(t, ok)
		require.Same(t, source, apiKey.Group)
	})
}

func TestAdvanceAutoGroupFallback_RejectsInvalidTargetAndCycle(t *testing.T) {
	targetID := int64(73)
	source := autoFallbackTestGroup(72, 0.12, &targetID)
	diagnose := func(context.Context, *int64, string, string) ModelAvailabilityDiagnosis {
		return ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true}
	}

	for name, mutate := range map[string]func(*Group){
		"停用目标":  func(group *Group) { group.Status = StatusDisabled },
		"跨平台目标": func(group *Group) { group.Platform = PlatformAnthropic },
		"订阅目标":  func(group *Group) { group.SubscriptionType = SubscriptionTypeSubscription },
	} {
		t.Run(name, func(t *testing.T) {
			target := autoFallbackTestGroup(targetID, 0.18, nil)
			mutate(target)
			repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{targetID: target}}
			apiKey := &APIKey{GroupID: &source.ID, Group: source, AutoGroupFallbackEnabled: true}
			ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
			_, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", diagnose)
			require.False(t, ok)
			require.Same(t, source, apiKey.Group)
		})
	}

	t.Run("用户已屏蔽目标公开分组", func(t *testing.T) {
		target := autoFallbackTestGroup(targetID, 0.18, nil)
		repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{targetID: target}}
		apiKey := &APIKey{
			GroupID:                  &source.ID,
			Group:                    source,
			User:                     &User{ID: 10, BlockedGroups: []int64{targetID}},
			AutoGroupFallbackEnabled: true,
		}
		ctx := WithAutoGroupFallbackState(context.Background(), apiKey)

		_, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", diagnose)

		require.False(t, ok)
		require.Same(t, source, apiKey.Group)
	})

	t.Run("循环链在已访问分组前停止", func(t *testing.T) {
		sourceID := source.ID
		target := autoFallbackTestGroup(targetID, 0.18, &sourceID)
		repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{sourceID: source, targetID: target}}
		apiKey := &APIKey{GroupID: &source.ID, Group: source, AutoGroupFallbackEnabled: true}
		ctx := WithAutoGroupFallbackState(context.Background(), apiKey)
		_, ok := advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", diagnose)
		require.True(t, ok)
		_, ok = advanceAutoGroupFallback(ctx, repo, apiKey.GroupID, "gpt-5.6-sol", diagnose)
		require.False(t, ok)
		require.Same(t, target, apiKey.Group)
	})
}

func TestAPIKeyAuthSnapshot_PreservesAutoGroupFallbackSettings(t *testing.T) {
	targetID := int64(73)
	groupID := int64(72)
	apiKey := &APIKey{
		ID:                       9,
		UserID:                   10,
		GroupID:                  &groupID,
		AutoGroupFallbackEnabled: true,
		User:                     &User{ID: 10},
		Group:                    autoFallbackTestGroup(groupID, 0.12, &targetID),
	}
	service := &APIKeyService{}

	snapshot := service.snapshotFromAPIKey(context.Background(), apiKey)
	require.NotNil(t, snapshot)
	require.True(t, snapshot.AutoGroupFallbackEnabled)
	require.Equal(t, targetID, *snapshot.Group.AutoFallbackGroupID)

	restored := service.snapshotToAPIKey("sk-test", snapshot)
	require.True(t, restored.AutoGroupFallbackEnabled)
	require.Equal(t, targetID, *restored.Group.AutoFallbackGroupID)
}

func TestResolveAutoGroupFallbackEnabled_DefaultsToTrue(t *testing.T) {
	require.True(t, resolveAutoGroupFallbackEnabled(nil))
	enabled := true
	require.True(t, resolveAutoGroupFallbackEnabled(&enabled))
	disabled := false
	require.False(t, resolveAutoGroupFallbackEnabled(&disabled))
}

func TestAdminGroupAutoFallbackValidationAndPersistence(t *testing.T) {
	proID := int64(73)
	fullPriceID := int64(74)
	pro := autoFallbackTestGroup(proID, 0.18, &fullPriceID)
	fullPrice := autoFallbackTestGroup(fullPriceID, 0.26, nil)
	repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{proID: pro, fullPriceID: fullPrice}}
	admin := &adminServiceImpl{groupRepo: repo}

	created, err := admin.CreateGroup(context.Background(), &CreateGroupInput{
		Name:                "plus",
		Platform:            PlatformOpenAI,
		RateMultiplier:      0.12,
		SubscriptionType:    SubscriptionTypeStandard,
		AutoFallbackGroupID: &proID,
	})
	require.NoError(t, err)
	require.Equal(t, proID, *created.AutoFallbackGroupID)
	require.Same(t, created, repo.created)

	zero := int64(0)
	repo.groups[created.ID] = created
	updated, err := admin.UpdateGroup(context.Background(), created.ID, &UpdateGroupInput{AutoFallbackGroupID: &zero})
	require.NoError(t, err)
	require.Nil(t, updated.AutoFallbackGroupID)
	require.Same(t, updated, repo.updated)
}

func TestAdminGroupAutoFallbackValidationRejectsUnsafeChains(t *testing.T) {
	sourceID := int64(72)
	targetID := int64(73)
	tests := map[string]struct {
		sourceSubscription string
		target             *Group
		targetID           int64
	}{
		"来源为订阅组": {
			sourceSubscription: SubscriptionTypeSubscription,
			target:             autoFallbackTestGroup(targetID, 0.18, nil),
			targetID:           targetID,
		},
		"目标已停用": {
			sourceSubscription: SubscriptionTypeStandard,
			target: func() *Group {
				group := autoFallbackTestGroup(targetID, 0.18, nil)
				group.Status = StatusDisabled
				return group
			}(),
			targetID: targetID,
		},
		"目标跨平台": {
			sourceSubscription: SubscriptionTypeStandard,
			target: func() *Group {
				group := autoFallbackTestGroup(targetID, 0.18, nil)
				group.Platform = PlatformAnthropic
				return group
			}(),
			targetID: targetID,
		},
		"目标为订阅组": {
			sourceSubscription: SubscriptionTypeStandard,
			target: func() *Group {
				group := autoFallbackTestGroup(targetID, 0.18, nil)
				group.SubscriptionType = SubscriptionTypeSubscription
				return group
			}(),
			targetID: targetID,
		},
		"自引用": {
			sourceSubscription: SubscriptionTypeStandard,
			target:             autoFallbackTestGroup(sourceID, 0.12, nil),
			targetID:           sourceID,
		},
		"循环链": {
			sourceSubscription: SubscriptionTypeStandard,
			target:             autoFallbackTestGroup(targetID, 0.18, &sourceID),
			targetID:           targetID,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			repo := &autoFallbackGroupRepoStub{groups: map[int64]*Group{test.target.ID: test.target}}
			admin := &adminServiceImpl{groupRepo: repo}
			err := admin.validateAutoFallbackGroup(
				context.Background(),
				sourceID,
				PlatformOpenAI,
				test.sourceSubscription,
				test.targetID,
			)
			require.Error(t, err)
		})
	}
}
