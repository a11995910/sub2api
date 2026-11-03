//go:build integration || pelicanpostgres

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserPreferredAccountPersistence(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := newUserRepositoryWithSQL(client, integrationDB)
	user := &service.User{Email: "preferred-account@test.invalid", PasswordHash: "test", Role: service.RoleUser, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, user))
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM api_keys WHERE user_id=$1", user.ID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id=$1", user.ID)
	})
	require.Zero(t, user.PreferredAccountID)
	key := &service.APIKey{UserID: user.ID, Name: "test", Key: "preferred-account-test-key", Status: service.StatusActive}
	keyRepo := NewAPIKeyRepository(client, integrationDB)
	require.NoError(t, keyRepo.Create(ctx, key))
	stale, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	user.PreferredAccountID = 123
	require.NoError(t, repo.Update(ctx, user, service.UserUpdateFields{PreferredAccountID: true}))
	loaded, err := repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(123), loaded.PreferredAccountID)
	keys, err := keyRepo.GetByKey(ctx, key.Key)
	require.NoError(t, err)
	require.Equal(t, int64(123), keys.User.PreferredAccountID, "认证仓储加载优先账号")
	users, _, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 100})
	require.NoError(t, err)
	found := false
	for _, item := range users {
		if item.ID == user.ID {
			found = true
			require.Equal(t, int64(123), item.PreferredAccountID)
		}
	}
	require.True(t, found, "管理端列表返回已保存配置")
	stale.Username = "updated"
	require.NoError(t, repo.Update(ctx, stale, service.UserUpdateFields{Username: true}))
	loaded, err = repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, int64(123), loaded.PreferredAccountID, "陈旧资料更新不能覆盖偏好")
	hashed := sha256.Sum256([]byte(key.Key))
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM auth_cache_invalidation_outbox WHERE cache_key=$1", hex.EncodeToString(hashed[:])).Scan(&count))
	require.Positive(t, count, "数据库触发器为偏好变更生成缓存失效补偿")
	loaded.PreferredAccountID = 0
	require.NoError(t, repo.Update(ctx, loaded, service.UserUpdateFields{PreferredAccountID: true}))
	loaded, err = repo.GetByID(ctx, user.ID)
	require.NoError(t, err)
	require.Zero(t, loaded.PreferredAccountID)
	_, err = integrationDB.ExecContext(ctx, "UPDATE users SET preferred_account_id=-1 WHERE id=$1", user.ID)
	require.Error(t, err, "数据库拒绝负数账号 ID")
}
