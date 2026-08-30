package service

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	LotteryMaxPrizes        = 5
	lotteryMaxPrizeIDLength = 80
	lotteryProbabilityScale = 10_000
	LotteryThanksPrizeID    = "thanks"
	lotteryMaxRewardText    = "999999999999.99999999"
)

var lotteryMaxRewardAmount = decimal.RequireFromString(lotteryMaxRewardText)

type LotteryAwardMode string

const (
	LotteryAwardModeDailyOnce    LotteryAwardMode = "daily_once"
	LotteryAwardModePerThreshold LotteryAwardMode = "per_threshold"
)

type LotteryPrize struct {
	ID                     string  `json:"id"`
	Name                   string  `json:"name"`
	RewardAmount           float64 `json:"reward_amount"`
	ProbabilityBasisPoints int     `json:"probability_basis_points"`
}

type LotteryConfig struct {
	Enabled              bool             `json:"enabled"`
	UsageThresholdTokens int64            `json:"usage_threshold_tokens"`
	AwardMode            LotteryAwardMode `json:"award_mode"`
	Prizes               []LotteryPrize   `json:"prizes"`
}

type LotterySelection struct {
	Prize                  *LotteryPrize
	Roll                   int
	ThanksProbabilityBasis int
}

func NormalizeLotteryConfig(config LotteryConfig) (LotteryConfig, error) {
	if config.UsageThresholdTokens <= 0 {
		return LotteryConfig{}, errors.New("每日用量门槛必须大于 0")
	}
	if config.AwardMode == "" {
		config.AwardMode = LotteryAwardModeDailyOnce
	}
	if config.AwardMode != LotteryAwardModeDailyOnce && config.AwardMode != LotteryAwardModePerThreshold {
		return LotteryConfig{}, errors.New("抽奖机会发放模式无效")
	}
	if len(config.Prizes) > LotteryMaxPrizes {
		return LotteryConfig{}, fmt.Errorf("奖品最多只能配置 %d 个", LotteryMaxPrizes)
	}

	normalized := LotteryConfig{
		Enabled:              config.Enabled,
		UsageThresholdTokens: config.UsageThresholdTokens,
		AwardMode:            config.AwardMode,
		Prizes:               make([]LotteryPrize, 0, len(config.Prizes)),
	}
	seenIDs := make(map[string]struct{}, len(config.Prizes))
	totalProbability := 0
	for index, prize := range config.Prizes {
		prize.ID = strings.TrimSpace(prize.ID)
		prize.Name = strings.TrimSpace(prize.Name)
		if prize.ID == "" || prize.ID == LotteryThanksPrizeID || len([]rune(prize.ID)) > lotteryMaxPrizeIDLength {
			return LotteryConfig{}, fmt.Errorf("第 %d 个奖品 ID 无效", index+1)
		}
		if _, exists := seenIDs[prize.ID]; exists {
			return LotteryConfig{}, errors.New("奖品 ID 不能重复")
		}
		seenIDs[prize.ID] = struct{}{}
		if prize.Name == "" || len([]rune(prize.Name)) > 40 {
			return LotteryConfig{}, fmt.Errorf("第 %d 个奖品名称不能为空且不能超过 40 个字符", index+1)
		}
		if math.IsNaN(prize.RewardAmount) || math.IsInf(prize.RewardAmount, 0) || prize.RewardAmount <= 0 {
			return LotteryConfig{}, fmt.Errorf("第 %d 个奖品余额必须大于 0", index+1)
		}
		rewardAmount := decimal.NewFromFloat(prize.RewardAmount)
		if rewardAmount.GreaterThan(lotteryMaxRewardAmount) {
			return LotteryConfig{}, fmt.Errorf("第 %d 个奖品余额不能超过 %s", index+1, lotteryMaxRewardText)
		}
		rewardAmount = rewardAmount.Round(UsageBillingMonetaryScale)
		if !rewardAmount.IsPositive() {
			return LotteryConfig{}, fmt.Errorf("第 %d 个奖品余额不能小于 0.00000001", index+1)
		}
		prize.RewardAmount, _ = rewardAmount.Float64()
		if prize.ProbabilityBasisPoints <= 0 || prize.ProbabilityBasisPoints > lotteryProbabilityScale {
			return LotteryConfig{}, fmt.Errorf("第 %d 个奖品概率必须在 0.01%% 到 100%% 之间", index+1)
		}
		totalProbability += prize.ProbabilityBasisPoints
		if totalProbability > lotteryProbabilityScale {
			return LotteryConfig{}, errors.New("奖品概率总和不能超过 100%")
		}
		normalized.Prizes = append(normalized.Prizes, prize)
	}
	return normalized, nil
}

func LotteryThanksProbabilityBasisPoints(config LotteryConfig) int {
	total := 0
	for _, prize := range config.Prizes {
		total += prize.ProbabilityBasisPoints
	}
	remaining := lotteryProbabilityScale - total
	if remaining < 0 {
		return 0
	}
	return remaining
}

// SelectLotteryPrize 使用 [0, 9999] 的安全随机整数选择奖品；nil 表示“谢谢惠顾”。
func SelectLotteryPrize(config LotteryConfig, roll int) (LotterySelection, error) {
	normalized, err := NormalizeLotteryConfig(config)
	if err != nil {
		return LotterySelection{}, err
	}
	if roll < 0 || roll >= lotteryProbabilityScale {
		return LotterySelection{}, errors.New("随机数超出允许范围")
	}
	cursor := 0
	for index := range normalized.Prizes {
		cursor += normalized.Prizes[index].ProbabilityBasisPoints
		if roll < cursor {
			prize := normalized.Prizes[index]
			return LotterySelection{
				Prize:                  &prize,
				Roll:                   roll,
				ThanksProbabilityBasis: LotteryThanksProbabilityBasisPoints(normalized),
			}, nil
		}
	}
	return LotterySelection{
		Roll:                   roll,
		ThanksProbabilityBasis: LotteryThanksProbabilityBasisPoints(normalized),
	}, nil
}
