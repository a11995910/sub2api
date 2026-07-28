package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration192AddsDisabledOAuthPoolVisibilityByDefault(t *testing.T) {
	content, err := FS.ReadFile("192_group_oauth_pool_visible.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "oauth_pool_visible BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS")
}
