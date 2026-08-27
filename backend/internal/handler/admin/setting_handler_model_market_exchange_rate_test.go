package admin

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpdateSettingsRejectsExplicitZeroModelMarketUSDToCNYRate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{
		service.SettingKeyModelMarketUSDToCNYRate: "7.2",
	})

	rec := doUpdateSettings(t, h, map[string]any{
		"model_market_usd_to_cny_rate": 0,
	}, nil)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "INVALID_MODEL_MARKET_USD_TO_CNY_RATE")
	require.Equal(t, "7.2", repo.values[service.SettingKeyModelMarketUSDToCNYRate])
}

func TestUpdateSettingsPersistsModelMarketUSDToCNYRate(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{})

	rec := doUpdateSettings(t, h, map[string]any{
		"model_market_usd_to_cny_rate": 7.15,
	}, nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "7.15", repo.values[service.SettingKeyModelMarketUSDToCNYRate])
	require.Contains(t, rec.Body.String(), `"model_market_usd_to_cny_rate":7.15`)
}
