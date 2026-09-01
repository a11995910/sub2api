package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountStatsPricingFieldsMigration(t *testing.T) {
	content, err := FS.ReadFile("234_account_stats_image_input_price.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql,
		"ALTER TABLE channel_account_stats_model_pricing ADD COLUMN IF NOT EXISTS image_input_price NUMERIC(20,12)")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS price_currency VARCHAR(3) NOT NULL DEFAULT 'USD'")
	require.Contains(t, sql,
		"conname = 'channel_account_stats_model_pricing_price_currency_check' AND conrelid = 'channel_account_stats_model_pricing'::regclass")
	require.Contains(t, sql, "CHECK (price_currency IN ('USD', 'CNY'))")

	for _, column := range []string{
		"input_multiplier NUMERIC(12,6)",
		"output_multiplier NUMERIC(12,6)",
		"cache_write_multiplier NUMERIC(12,6)",
		"cache_read_multiplier NUMERIC(12,6)",
	} {
		require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS "+column)
	}

	for _, column := range []string{
		"input_multiplier",
		"output_multiplier",
		"cache_write_multiplier",
		"cache_read_multiplier",
	} {
		constraint := "account_stats_pricing_intervals_" + column + "_positive"
		require.Contains(t, sql,
			"conname = '"+constraint+"' AND conrelid = 'channel_account_stats_pricing_intervals'::regclass")
		require.Contains(t, sql,
			"ALTER TABLE channel_account_stats_pricing_intervals ADD CONSTRAINT "+constraint+
				" CHECK ("+column+" IS NULL OR "+column+" > 0)")
	}
}
