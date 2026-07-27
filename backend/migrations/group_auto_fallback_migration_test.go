package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration191AddsGroupFallbackAndDefaultEnabledKeySetting(t *testing.T) {
	content, err := FS.ReadFile("191_group_auto_fallback.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "auto_fallback_group_id BIGINT REFERENCES groups(id) ON DELETE SET NULL")
	require.Contains(t, sql, "auto_group_fallback_enabled BOOLEAN NOT NULL DEFAULT TRUE")
	require.Contains(t, sql, "idx_groups_auto_fallback_group_id")
}
