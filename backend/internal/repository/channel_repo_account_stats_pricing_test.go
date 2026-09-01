//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountStatsPricingExtendedFieldsRoundTrip(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &channelRepository{db: db}
	loadedAt := time.Date(2026, time.September, 1, 8, 0, 0, 0, time.UTC)

	mock.ExpectQuery(`(?s)SELECT .*price_currency.*image_input_price, image_output_price.*FROM channel_account_stats_model_pricing`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "rule_id", "platform", "models", "billing_mode", "price_currency", "input_price", "output_price",
			"cache_write_price", "cache_read_price", "image_input_price", "image_output_price",
			"per_request_price", "created_at", "updated_at",
		}).AddRow(
			int64(11), int64(7), "openai", `["gpt-image-2"]`, service.BillingModeToken, service.PriceCurrencyCNY,
			0.001, 0.002, 0.003, 0.0005, 0.004, 0.01, nil, loadedAt, loadedAt,
		))
	mock.ExpectQuery(`(?s)SELECT id, pricing_id, min_tokens, max_tokens, tier_label,.*input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier.*FROM channel_account_stats_pricing_intervals`).
		WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "pricing_id", "min_tokens", "max_tokens", "tier_label",
			"input_price", "output_price", "cache_write_price", "cache_read_price",
			"input_multiplier", "output_multiplier", "cache_write_multiplier", "cache_read_multiplier",
			"per_request_price", "sort_order", "created_at", "updated_at",
		}).AddRow(
			int64(21), int64(11), 200000, 1000000, "long-context",
			nil, nil, nil, nil,
			2.0, 1.5, 1.25, 0.8,
			nil, 1, loadedAt, loadedAt,
		))

	pricingByRule, err := repo.batchLoadAccountStatsModelPricing(context.Background(), []int64{7})
	require.NoError(t, err)
	require.Len(t, pricingByRule[7], 1)
	require.Equal(t, service.PriceCurrencyCNY, pricingByRule[7][0].PriceCurrency)
	require.NotNil(t, pricingByRule[7][0].ImageInputPrice)
	require.InDelta(t, 0.004, *pricingByRule[7][0].ImageInputPrice, 1e-12)
	require.Len(t, pricingByRule[7][0].Intervals, 1)
	loadedInterval := pricingByRule[7][0].Intervals[0]
	require.NotNil(t, loadedInterval.InputMultiplier)
	require.NotNil(t, loadedInterval.OutputMultiplier)
	require.NotNil(t, loadedInterval.CacheWriteMultiplier)
	require.NotNil(t, loadedInterval.CacheReadMultiplier)
	require.InDelta(t, 2.0, *loadedInterval.InputMultiplier, 1e-12)
	require.InDelta(t, 1.5, *loadedInterval.OutputMultiplier, 1e-12)
	require.InDelta(t, 1.25, *loadedInterval.CacheWriteMultiplier, 1e-12)
	require.InDelta(t, 0.8, *loadedInterval.CacheReadMultiplier, 1e-12)

	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_account_stats_model_pricing (rule_id, platform, models, billing_mode, price_currency, input_price, output_price, cache_write_price, cache_read_price, image_input_price, image_output_price, per_request_price)")).
		WithArgs(
			int64(7), "openai", []byte(`["gpt-image-2"]`), service.BillingModeToken, service.PriceCurrencyCNY,
			sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(),
			sqlmock.AnyArg(), sqlmock.AnyArg(), nil,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(12), time.Time{}, time.Time{}))
	mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO channel_account_stats_pricing_intervals (pricing_id, min_tokens, max_tokens, tier_label, input_price, output_price, cache_write_price, cache_read_price, input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier, per_request_price, sort_order)")).
		WithArgs(
			int64(12), 200000, 1000000, "long-context",
			nil, nil, nil, nil,
			2.0, 1.5, 1.25, 0.8,
			nil, 1,
		).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(int64(22), time.Time{}, time.Time{}))
	pricing := &service.ChannelModelPricing{
		Platform:         "openai",
		Models:           []string{"gpt-image-2"},
		BillingMode:      service.BillingModeToken,
		PriceCurrency:    service.PriceCurrencyCNY,
		InputPrice:       float64Ptr(0.001),
		OutputPrice:      float64Ptr(0.002),
		CacheWritePrice:  float64Ptr(0.003),
		CacheReadPrice:   float64Ptr(0.0005),
		ImageInputPrice:  float64Ptr(0.004),
		ImageOutputPrice: float64Ptr(0.01),
		Intervals: []service.PricingInterval{
			{
				MinTokens:            200000,
				MaxTokens:            intPtr(1000000),
				TierLabel:            "long-context",
				InputMultiplier:      float64Ptr(2.0),
				OutputMultiplier:     float64Ptr(1.5),
				CacheWriteMultiplier: float64Ptr(1.25),
				CacheReadMultiplier:  float64Ptr(0.8),
				SortOrder:            1,
			},
		},
	}
	require.NoError(t, createAccountStatsModelPricingTx(context.Background(), tx, 7, pricing))
	mock.ExpectCommit()
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func float64Ptr(value float64) *float64 {
	return &value
}

func intPtr(value int) *int {
	return &value
}
