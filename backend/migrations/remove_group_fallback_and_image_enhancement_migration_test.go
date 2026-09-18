package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRemoveGroupFallbackAndImageEnhancementMigration(t *testing.T) {
	sqlBytes, err := FS.ReadFile("241_remove_group_fallback_and_image_enhancement.sql")
	require.NoError(t, err)

	sql := string(sqlBytes)
	require.Contains(t, sql, "DROP INDEX IF EXISTS idx_groups_auto_fallback_group_id")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS auto_group_fallback_enabled")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS auto_fallback_group_id")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS image_super_resolution_enabled")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS image_2k_enhancement_enabled")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS image_2k_enhancement_group_id")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS image_4k_enhancement_enabled")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS image_4k_enhancement_group_id")
	require.Contains(t, sql, "DROP COLUMN IF EXISTS image_4k_enhancement_model")
}
