package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelPricingCurrencyMigration(t *testing.T) {
	content, err := FS.ReadFile("231_channel_pricing_currency.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS price_currency VARCHAR(3) NOT NULL DEFAULT 'USD'")
	require.Contains(t, sql, "conname = 'channel_model_pricing_price_currency_check'")
	require.Contains(t, sql, "CHECK (price_currency IN ('USD', 'CNY'))")
}
