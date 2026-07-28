package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetOAuthAccountPoolStatsScansBatchResult(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("WITH requested_accounts AS").
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"account_id",
			"five_hour_requests",
			"five_hour_tokens",
			"seven_day_requests",
			"seven_day_tokens",
			"total_requests",
			"total_tokens",
		}).AddRow(int64(42), int64(5), int64(500), int64(70), int64(7000), int64(120), int64(12000)))

	repo := newUsageLogRepositoryWithSQL(nil, db)
	now := time.Now()
	stats, err := repo.GetOAuthAccountPoolStats(context.Background(), []service.OAuthAccountPoolStatsWindow{{
		AccountID:     42,
		FiveHourStart: now.Add(-5 * time.Hour),
		SevenDayStart: now.Add(-7 * 24 * time.Hour),
	}})

	require.NoError(t, err)
	require.Equal(t, service.OAuthAccountPoolRequestTokenStats{Requests: 5, Tokens: 500}, stats[42].FiveHour)
	require.Equal(t, service.OAuthAccountPoolRequestTokenStats{Requests: 70, Tokens: 7000}, stats[42].SevenDay)
	require.Equal(t, service.OAuthAccountPoolRequestTokenStats{Requests: 120, Tokens: 12000}, stats[42].Total)
	require.NoError(t, mock.ExpectationsWereMet())
}
