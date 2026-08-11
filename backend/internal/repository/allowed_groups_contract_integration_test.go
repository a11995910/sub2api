//go:build integration

package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/ent/schema/mixins"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func uniqueTestValue(t *testing.T, prefix string) string {
	t.Helper()
	safeName := strings.NewReplacer("/", "_", " ", "_").Replace(t.Name())
	return fmt.Sprintf("%s-%s", prefix, safeName)
}

func TestUserRepository_RemoveGroupFromAllowedGroups_RemovesAllOccurrences(t *testing.T) {
	ctx := context.Background()
	entClient := testEntClient(t)

	targetGroup, err := entClient.Group.Create().
		SetName(uniqueTestValue(t, "target-group")).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	otherGroup, err := entClient.Group.Create().
		SetName(uniqueTestValue(t, "other-group")).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := mixins.SkipSoftDelete(context.Background())
		require.NoError(t, entClient.Group.DeleteOneID(targetGroup.ID).Exec(cleanupCtx))
		require.NoError(t, entClient.Group.DeleteOneID(otherGroup.ID).Exec(cleanupCtx))
		_, err := integrationDB.ExecContext(context.Background(),
			"DELETE FROM scheduler_outbox WHERE group_id = $1 OR group_id = $2",
			targetGroup.ID, otherGroup.ID,
		)
		require.NoError(t, err)
	})

	repo := newUserRepositoryWithSQL(entClient, integrationDB)

	u1 := &service.User{
		Email:         uniqueTestValue(t, "u1") + "@example.com",
		PasswordHash:  "test-password-hash",
		Role:          service.RoleUser,
		Status:        service.StatusActive,
		Concurrency:   5,
		AllowedGroups: []int64{targetGroup.ID, otherGroup.ID},
	}
	require.NoError(t, repo.Create(ctx, u1))
	t.Cleanup(func() {
		require.NoError(t, entClient.User.DeleteOneID(u1.ID).Exec(mixins.SkipSoftDelete(context.Background())))
	})

	u2 := &service.User{
		Email:         uniqueTestValue(t, "u2") + "@example.com",
		PasswordHash:  "test-password-hash",
		Role:          service.RoleUser,
		Status:        service.StatusActive,
		Concurrency:   5,
		AllowedGroups: []int64{targetGroup.ID},
	}
	require.NoError(t, repo.Create(ctx, u2))
	t.Cleanup(func() {
		require.NoError(t, entClient.User.DeleteOneID(u2.ID).Exec(mixins.SkipSoftDelete(context.Background())))
	})

	u3 := &service.User{
		Email:         uniqueTestValue(t, "u3") + "@example.com",
		PasswordHash:  "test-password-hash",
		Role:          service.RoleUser,
		Status:        service.StatusActive,
		Concurrency:   5,
		AllowedGroups: []int64{otherGroup.ID},
	}
	require.NoError(t, repo.Create(ctx, u3))
	t.Cleanup(func() {
		require.NoError(t, entClient.User.DeleteOneID(u3.ID).Exec(mixins.SkipSoftDelete(context.Background())))
	})

	affected, err := repo.RemoveGroupFromAllowedGroups(ctx, targetGroup.ID)
	require.NoError(t, err)
	require.Equal(t, int64(2), affected)

	u1After, err := repo.GetByID(ctx, u1.ID)
	require.NoError(t, err)
	require.NotContains(t, u1After.AllowedGroups, targetGroup.ID)
	require.Contains(t, u1After.AllowedGroups, otherGroup.ID)

	u2After, err := repo.GetByID(ctx, u2.ID)
	require.NoError(t, err)
	require.NotContains(t, u2After.AllowedGroups, targetGroup.ID)
}

func TestGroupRepository_DeleteCascade_PreservesApiKeyGroupID(t *testing.T) {
	ctx := context.Background()
	entClient := testEntClient(t)

	targetGroup, err := entClient.Group.Create().
		SetName(uniqueTestValue(t, "delete-cascade-target")).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	otherGroup, err := entClient.Group.Create().
		SetName(uniqueTestValue(t, "delete-cascade-other")).
		SetStatus(service.StatusActive).
		Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx := mixins.SkipSoftDelete(context.Background())
		require.NoError(t, entClient.Group.DeleteOneID(targetGroup.ID).Exec(cleanupCtx))
		require.NoError(t, entClient.Group.DeleteOneID(otherGroup.ID).Exec(cleanupCtx))
		_, err := integrationDB.ExecContext(context.Background(),
			"DELETE FROM scheduler_outbox WHERE group_id = $1 OR group_id = $2",
			targetGroup.ID, otherGroup.ID,
		)
		require.NoError(t, err)
	})

	userRepo := newUserRepositoryWithSQL(entClient, integrationDB)
	groupRepo := newGroupRepositoryWithSQL(entClient, integrationDB)
	apiKeyRepo := newAPIKeyRepositoryWithSQL(entClient, integrationDB)

	u := &service.User{
		Email:         uniqueTestValue(t, "cascade-user") + "@example.com",
		PasswordHash:  "test-password-hash",
		Role:          service.RoleUser,
		Status:        service.StatusActive,
		Concurrency:   5,
		AllowedGroups: []int64{targetGroup.ID, otherGroup.ID},
	}
	require.NoError(t, userRepo.Create(ctx, u))
	t.Cleanup(func() {
		require.NoError(t, entClient.User.DeleteOneID(u.ID).Exec(mixins.SkipSoftDelete(context.Background())))
	})

	keyValue := uniqueTestValue(t, "sk-test-delete-cascade")
	key := &service.APIKey{
		UserID:  u.ID,
		Key:     keyValue,
		Name:    "test key",
		GroupID: &targetGroup.ID,
		Status:  service.StatusActive,
	}
	require.NoError(t, apiKeyRepo.Create(ctx, key))
	t.Cleanup(func() {
		require.NoError(t, entClient.APIKey.DeleteOneID(key.ID).Exec(mixins.SkipSoftDelete(context.Background())))
		_, err := integrationDB.ExecContext(context.Background(),
			"DELETE FROM auth_cache_invalidation_outbox WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')",
			keyValue,
		)
		require.NoError(t, err)
	})

	_, err = groupRepo.DeleteCascade(ctx, targetGroup.ID)
	require.NoError(t, err)

	// Deleted group should be hidden by default queries (soft-delete semantics).
	_, err = groupRepo.GetByID(ctx, targetGroup.ID)
	require.ErrorIs(t, err, service.ErrGroupNotFound)

	activeGroups, err := groupRepo.ListActive(ctx)
	require.NoError(t, err)
	for _, g := range activeGroups {
		require.NotEqual(t, targetGroup.ID, g.ID)
	}

	// User.allowed_groups should no longer include the deleted group.
	uAfter, err := userRepo.GetByID(ctx, u.ID)
	require.NoError(t, err)
	require.NotContains(t, uAfter.AllowedGroups, targetGroup.ID)
	require.Contains(t, uAfter.AllowedGroups, otherGroup.ID)

	// API keys keep their group_id so auth can reject keys bound to a deleted group.
	keyAfter, err := apiKeyRepo.GetByID(ctx, key.ID)
	require.NoError(t, err)
	require.NotNil(t, keyAfter.GroupID)
	require.Equal(t, targetGroup.ID, *keyAfter.GroupID)
	require.Nil(t, keyAfter.Group)
}
