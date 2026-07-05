package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupEntityToService_PreservesMessagesDispatchModelConfig(t *testing.T) {
	group := &dbent.Group{
		ID:                    1,
		Name:                  "openai-dispatch",
		Platform:              service.PlatformOpenAI,
		Status:                service.StatusActive,
		SubscriptionType:      service.SubscriptionTypeStandard,
		RateMultiplier:        1,
		AllowMessagesDispatch: true,
		DefaultMappedModel:    "gpt-5.4",
		MessagesDispatchModelConfig: service.OpenAIMessagesDispatchModelConfig{
			OpusMappedModel:   "gpt-5.4-nano",
			SonnetMappedModel: "gpt-5.3-codex",
			HaikuMappedModel:  "gpt-5.4-mini",
			ExactModelMappings: map[string]string{
				"claude-sonnet-4.5": "gpt-5.4-nano",
			},
		},
	}

	got := groupEntityToService(group)
	require.NotNil(t, got)
	require.Equal(t, group.MessagesDispatchModelConfig, got.MessagesDispatchModelConfig)
}

func TestAPIKeyRepository_GetByKeyForAuth_PreservesMessagesDispatchModelConfig_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-dispatch-unit@test.com")

	group, err := client.Group.Create().
		SetName("g-auth-dispatch-unit").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetAllowMessagesDispatch(true).
		SetDefaultMappedModel("gpt-5.4").
		SetMessagesDispatchModelConfig(service.OpenAIMessagesDispatchModelConfig{
			OpusMappedModel:   "gpt-5.4-nano",
			SonnetMappedModel: "gpt-5.3-codex",
			HaikuMappedModel:  "gpt-5.4-mini",
			ExactModelMappings: map[string]string{
				"claude-sonnet-4.5": "gpt-5.4-nano",
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-dispatch-unit",
		Name:    "Dispatch Key Unit",
		GroupID: &group.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, key.Name, got.Name)
	require.NotNil(t, got.Group)
	require.Equal(t, group.MessagesDispatchModelConfig, got.Group.MessagesDispatchModelConfig)
}

func TestAPIKeyRepository_PreservesOpenAIFastModeEnabled_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "openai-fast-mode-key@test.com")

	key := &service.APIKey{
		UserID:                user.ID,
		Key:                   "sk-openai-fast-mode",
		Name:                  "OpenAI Fast Mode",
		Status:                service.StatusActive,
		OpenAIFastModeEnabled: true,
	}
	require.NoError(t, repo.Create(ctx, key))

	gotByID, err := repo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.True(t, gotByID.OpenAIFastModeEnabled)

	gotForAuth, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.True(t, gotForAuth.OpenAIFastModeEnabled)
}

func TestAPIKeyRepository_GetByKeyForAuth_PreservesCacheHitQuarterToInput_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-cache-hit-unit@test.com")

	group, err := client.Group.Create().
		SetName("g-auth-cache-hit-quarter-unit").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetCacheHitQuarterToInputEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-cache-hit-quarter-unit",
		Name:    "Cache Hit Quarter Key Unit",
		GroupID: &group.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.True(t, got.Group.CacheHitQuarterToInput)
}

func TestAPIKeyRepository_GetByKeyForAuth_PreservesImageEnhancementConfig_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-image-enhancement-unit@test.com")

	targetGroup, err := client.Group.Create().
		SetName("g-auth-image-enhancement-target-unit").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetAllowImageGeneration(true).
		Save(ctx)
	require.NoError(t, err)

	group, err := client.Group.Create().
		SetName("g-auth-image-enhancement-unit").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeStandard).
		SetRateMultiplier(1).
		SetAllowImageGeneration(true).
		SetImage2kEnhancementEnabled(true).
		SetImage4kEnhancementEnabled(true).
		SetImage4kEnhancementGroupID(targetGroup.ID).
		SetImage4kEnhancementModel("gpt-image-2").
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID:  user.ID,
		Key:     "sk-getbykey-auth-image-enhancement-unit",
		Name:    "Image Enhancement Key Unit",
		GroupID: &group.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.Group)
	require.True(t, got.Group.AllowImageGeneration)
	require.True(t, got.Group.Image2KEnhancementEnabled)
	require.True(t, got.Group.Image4KEnhancementEnabled)
	require.NotNil(t, got.Group.Image4KEnhancementGroupID)
	require.Equal(t, targetGroup.ID, *got.Group.Image4KEnhancementGroupID)
	require.NotNil(t, got.Group.Image4KEnhancementModel)
	require.Equal(t, "gpt-image-2", *got.Group.Image4KEnhancementModel)
}

func TestAPIKeyRepository_GetByKeyForAuth_FiltersExpiredAllowedGroups_SQLite(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "getbykey-auth-active-groups@test.com")

	permanentGroup, err := client.Group.Create().
		SetName("g-auth-permanent").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	activeTemporaryGroup, err := client.Group.Create().
		SetName("g-auth-temporary-active").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	expiredTemporaryGroup, err := client.Group.Create().
		SetName("g-auth-temporary-expired").
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	_, err = client.UserAllowedGroup.Create().
		SetUserID(user.ID).
		SetGroupID(permanentGroup.ID).
		SetSource(service.UserAllowedGroupSourceManual).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserAllowedGroup.Create().
		SetUserID(user.ID).
		SetGroupID(activeTemporaryGroup.ID).
		SetSource(service.UserAllowedGroupSourceAffiliatePaymentReward).
		SetExpiresAt(now.Add(time.Hour)).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.UserAllowedGroup.Create().
		SetUserID(user.ID).
		SetGroupID(expiredTemporaryGroup.ID).
		SetSource(service.UserAllowedGroupSourceAffiliatePaymentReward).
		SetExpiresAt(now.Add(-time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{
		UserID: user.ID,
		Key:    "sk-getbykey-auth-active-groups",
		Name:   "Active Groups Key",
		Status: service.StatusActive,
	}
	require.NoError(t, repo.Create(ctx, key))

	got, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, got.User)
	require.ElementsMatch(t, []int64{permanentGroup.ID, activeTemporaryGroup.ID}, got.User.AllowedGroups)
	require.NotContains(t, got.User.AllowedGroups, expiredTemporaryGroup.ID)
}
