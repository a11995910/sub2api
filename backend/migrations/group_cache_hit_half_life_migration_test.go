package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration229AddsGroupCacheHitHalfLife(t *testing.T) {
	content, err := FS.ReadFile("229_group_cache_hit_half_life.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS cache_hit_half_life_days DECIMAL(8,2) NOT NULL DEFAULT 1.00")
	require.Contains(t, sql, "COMMENT ON COLUMN groups.cache_hit_half_life_days")
	require.Contains(t, sql, "时间半衰期（天）")
}
