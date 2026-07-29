package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration193AddsUserBlockedGroupsAndCacheInvalidation(t *testing.T) {
	content, err := FS.ReadFile("193_user_blocked_groups.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS user_blocked_groups")
	require.Contains(t, sql, "PRIMARY KEY (user_id, group_id)")
	require.Contains(t, sql, "ON DELETE CASCADE")
	require.Contains(t, sql, "idx_user_blocked_groups_group_id")
	require.Contains(t, sql, "enqueue_blocked_group_auth_cache_invalidation")
	require.Contains(t, sql, "trg_user_blocked_groups_auth_cache_invalidation")
	require.Contains(t, sql, "auth_cache_invalidation_outbox")
}
