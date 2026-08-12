package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMigration220PreservesOpenAIVideoPricing(t *testing.T) {
	content, err := FS.ReadFile("220_clear_non_grok_video_generation_config.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "platform IS DISTINCT FROM 'openai'")
	require.Contains(t, sql, "platform IS DISTINCT FROM 'composite'")
}

func TestMigration221RestoresOpenAIVideoPricingFromMigration220Backup(t *testing.T) {
	content, err := FS.ReadFile("221_restore_openai_video_pricing.sql")
	require.NoError(t, err)

	sql := string(content)
	require.Contains(t, sql, "FROM groups_video_price_backup_220")
	require.Contains(t, sql, "g.platform = 'openai'")
	require.Contains(t, sql, "b.platform = 'openai'")
	require.Contains(t, sql, "video_model_prices = b.video_model_prices")
}
