package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSelectLotteryPrizeUsesConfiguredRangesAndComputedThanksRange(t *testing.T) {
	config := LotteryConfig{
		UsageThresholdTokens: 1_000_000,
		Prizes: []LotteryPrize{
			{ID: "small", Name: "1 灵石", RewardAmount: 1, ProbabilityBasisPoints: 2500},
			{ID: "large", Name: "10 灵石", RewardAmount: 10, ProbabilityBasisPoints: 500},
		},
	}

	first, err := SelectLotteryPrize(config, 2499)
	require.NoError(t, err)
	require.NotNil(t, first.Prize)
	require.Equal(t, "small", first.Prize.ID)

	second, err := SelectLotteryPrize(config, 2500)
	require.NoError(t, err)
	require.NotNil(t, second.Prize)
	require.Equal(t, "large", second.Prize.ID)

	thanks, err := SelectLotteryPrize(config, 3000)
	require.NoError(t, err)
	require.Nil(t, thanks.Prize)
	require.Equal(t, 7000, thanks.ThanksProbabilityBasis)
}

func TestNormalizeLotteryConfigAllowsNoPrizeAndRejectsMoreThanFive(t *testing.T) {
	empty, err := NormalizeLotteryConfig(LotteryConfig{UsageThresholdTokens: 1})
	require.NoError(t, err)
	require.Equal(t, lotteryProbabilityScale, LotteryThanksProbabilityBasisPoints(empty))

	prizes := make([]LotteryPrize, 0, LotteryMaxPrizes+1)
	for index := 0; index < LotteryMaxPrizes+1; index++ {
		prizes = append(prizes, LotteryPrize{
			ID:                     string(rune('a' + index)),
			Name:                   "奖品",
			RewardAmount:           1,
			ProbabilityBasisPoints: 1,
		})
	}
	_, err = NormalizeLotteryConfig(LotteryConfig{UsageThresholdTokens: 1, Prizes: prizes})
	require.Error(t, err)
}

func TestNormalizeLotteryConfigRejectsProbabilityAboveOneHundredPercent(t *testing.T) {
	_, err := NormalizeLotteryConfig(LotteryConfig{
		UsageThresholdTokens: 1,
		Prizes: []LotteryPrize{
			{ID: "a", Name: "A", RewardAmount: 1, ProbabilityBasisPoints: 6000},
			{ID: "b", Name: "B", RewardAmount: 2, ProbabilityBasisPoints: 4001},
		},
	})
	require.Error(t, err)
}

func TestNormalizeLotteryConfigDefaultsAndValidatesAwardMode(t *testing.T) {
	config, err := NormalizeLotteryConfig(LotteryConfig{UsageThresholdTokens: 1_000_000})
	require.NoError(t, err)
	require.Equal(t, LotteryAwardModeDailyOnce, config.AwardMode)

	_, err = NormalizeLotteryConfig(LotteryConfig{UsageThresholdTokens: 1_000_000, AwardMode: "unknown"})
	require.Error(t, err)
}

func TestNormalizeLotteryConfigQuantizesAndBoundsRewardAmount(t *testing.T) {
	config, err := NormalizeLotteryConfig(LotteryConfig{
		UsageThresholdTokens: 1_000_000,
		Prizes: []LotteryPrize{{
			ID:                     "precise",
			Name:                   "精度测试",
			RewardAmount:           1.234567895,
			ProbabilityBasisPoints: 1,
		}},
	})
	require.NoError(t, err)
	require.InDelta(t, 1.2345679, config.Prizes[0].RewardAmount, 0.000000001)

	_, err = NormalizeLotteryConfig(LotteryConfig{
		UsageThresholdTokens: 1_000_000,
		Prizes: []LotteryPrize{{
			ID:                     "tiny",
			Name:                   "过小奖项",
			RewardAmount:           0.000000001,
			ProbabilityBasisPoints: 1,
		}},
	})
	require.ErrorContains(t, err, "不能小于")

	_, err = NormalizeLotteryConfig(LotteryConfig{
		UsageThresholdTokens: 1_000_000,
		Prizes: []LotteryPrize{{
			ID:                     "huge",
			Name:                   "过大奖项",
			RewardAmount:           1_000_000_000_000,
			ProbabilityBasisPoints: 1,
		}},
	})
	require.ErrorContains(t, err, "不能超过")
}
