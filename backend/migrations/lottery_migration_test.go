package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLotteryMigrationKeepsChancesAndRewardsAuditable(t *testing.T) {
	content, err := FS.ReadFile("233_lottery.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_lottery_settings")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_lottery_rule_versions")
	require.Contains(t, sql, "effective_date DATE NOT NULL UNIQUE")
	require.Contains(t, sql, "award_mode VARCHAR(32) NOT NULL DEFAULT 'daily_once'")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_lottery_daily_awards")
	require.Contains(t, sql, "PRIMARY KEY (user_id, usage_date)")
	require.Contains(t, sql, "awarded_chances BIGINT NOT NULL CHECK (awarded_chances > 0)")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_lottery_user_states")
	require.Contains(t, sql, "available_chances BIGINT NOT NULL DEFAULT 0 CHECK (available_chances >= 0)")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS sub2api_lottery_draws")
	require.Contains(t, sql, "reward_amount DECIMAL(20,8) NOT NULL DEFAULT 0 CHECK (reward_amount >= 0)")
	require.Contains(t, sql, "random_roll INTEGER NOT NULL CHECK (random_roll BETWEEN 0 AND 9999)")
	require.Contains(t, sql, "VALUES ('lottery_enabled', 'false', NOW())")
	require.Contains(t, sql, "CREATE OR REPLACE FUNCTION sync_sub2api_lottery_enabled()")
	require.Contains(t, sql, "current_setting('TimeZone')")
}
