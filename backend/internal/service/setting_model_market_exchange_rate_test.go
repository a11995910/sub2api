package service

import (
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSettingServiceParseSettingsModelMarketUSDToCNYRate(t *testing.T) {
	svc := &SettingService{cfg: &config.Config{}}

	got := svc.parseSettings(map[string]string{
		SettingKeyModelMarketUSDToCNYRate: "7.15",
	})

	require.InDelta(t, 7.15, got.ModelMarketUSDToCNYRate, 1e-9)
}

func TestParseModelMarketUSDToCNYRateDefaultsInvalidValues(t *testing.T) {
	for _, raw := range []string{"", "0", "-1", "NaN", "+Inf", "101"} {
		t.Run(raw, func(t *testing.T) {
			require.Equal(t, DefaultModelMarketUSDToCNYRate, parseModelMarketUSDToCNYRate(raw))
		})
	}
}

func TestValidateModelMarketUSDToCNYRate(t *testing.T) {
	for _, valid := range []float64{0.01, 7.2, 100} {
		require.NoError(t, ValidateModelMarketUSDToCNYRate(valid))
	}

	for _, invalid := range []float64{0, -1, 100.01, math.NaN(), math.Inf(1)} {
		require.Error(t, ValidateModelMarketUSDToCNYRate(invalid))
	}
}

func TestNormalizeModelMarketUSDToCNYRateForUpdateDefaultsOmittedZeroValue(t *testing.T) {
	got, err := normalizeModelMarketUSDToCNYRateForUpdate(0)

	require.NoError(t, err)
	require.Equal(t, DefaultModelMarketUSDToCNYRate, got)
}
