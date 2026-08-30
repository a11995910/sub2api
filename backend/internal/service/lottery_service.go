package service

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

var (
	ErrLotteryDisabled = infraerrors.BadRequest("LOTTERY_DISABLED", "lottery is disabled")
	ErrLotteryNoChance = infraerrors.Conflict("LOTTERY_NO_CHANCE", "no lottery chances available")
)

const lotteryRecentDrawLimit = 10

type LotteryService struct {
	repo                 LotteryRepository
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
	randomRoll           func() (int, error)
}

func NewLotteryService(
	repo LotteryRepository,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	billingCacheService *BillingCacheService,
) *LotteryService {
	return &LotteryService{
		repo:                 repo,
		authCacheInvalidator: authCacheInvalidator,
		billingCacheService:  billingCacheService,
		randomRoll:           secureLotteryRoll,
	}
}

func (s *LotteryService) GetOverview(ctx context.Context, userID int64) (*LotteryOverview, error) {
	snapshot, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取抽奖配置: %w", err)
	}
	if !snapshot.Config.Enabled {
		return nil, ErrLotteryDisabled
	}
	if err := s.repo.ReconcileUserChances(ctx, userID); err != nil {
		return nil, fmt.Errorf("核算抽奖机会: %w", err)
	}
	state, err := s.repo.GetUserState(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("读取抽奖机会: %w", err)
	}
	draws, err := s.repo.ListUserDraws(ctx, userID, lotteryRecentDrawLimit)
	if err != nil {
		return nil, fmt.Errorf("读取抽奖记录: %w", err)
	}
	progress := 0.0
	if state.TodayThreshold > 0 {
		progressTokens := state.TodayUsageTokens
		if state.TodayAwardMode == LotteryAwardModePerThreshold {
			consumedTokens := saturatingLotteryMultiply(state.TodayAwardedChances, state.TodayThreshold)
			progressTokens = 0
			if state.TodayUsageTokens > consumedTokens {
				progressTokens = state.TodayUsageTokens - consumedTokens
			}
			state.TodayNextTargetTokens = saturatingLotteryAdd(consumedTokens, state.TodayThreshold)
		} else {
			state.TodayNextTargetTokens = state.TodayThreshold
			if state.TodayAwardedChances > 0 {
				progressTokens = state.TodayThreshold
			}
		}
		progress = math.Min(100, float64(progressTokens)/float64(state.TodayThreshold)*100)
	}
	return &LotteryOverview{
		Config:           lotteryConfigView(snapshot),
		State:            state,
		TodayUsageM:      float64(state.TodayUsageTokens) / 1_000_000,
		TodayNextTargetM: float64(state.TodayNextTargetTokens) / 1_000_000,
		TodayProgress:    progress,
		RecentDraws:      normalizeLotteryDraws(draws),
	}, nil
}

func saturatingLotteryMultiply(left, right int64) int64 {
	if left <= 0 || right <= 0 {
		return 0
	}
	if left > math.MaxInt64/right {
		return math.MaxInt64
	}
	return left * right
}

func saturatingLotteryAdd(left, right int64) int64 {
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func (s *LotteryService) Draw(ctx context.Context, userID int64) (*LotteryDrawResult, error) {
	snapshot, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取抽奖配置: %w", err)
	}
	if !snapshot.Config.Enabled {
		return nil, ErrLotteryDisabled
	}
	if err := s.repo.ReconcileUserChances(ctx, userID); err != nil {
		return nil, fmt.Errorf("核算抽奖机会: %w", err)
	}
	roll, err := s.randomRoll()
	if err != nil {
		return nil, fmt.Errorf("生成安全随机数: %w", err)
	}
	selection, err := SelectLotteryPrize(snapshot.Config, roll)
	if err != nil {
		return nil, fmt.Errorf("选择抽奖结果: %w", err)
	}
	draw, err := s.repo.CommitDraw(ctx, userID, selection, snapshot.Version)
	if err != nil {
		return nil, err
	}
	s.invalidateBalanceCaches(ctx, userID)
	draw = normalizeLotteryDraw(draw)
	return &LotteryDrawResult{
		Draw:             draw,
		AvailableChances: draw.ChanceAfter,
		NewBalance:       draw.BalanceAfter,
	}, nil
}

func (s *LotteryService) GetConfig(ctx context.Context) (LotteryConfigView, error) {
	snapshot, err := s.repo.GetConfig(ctx)
	if err != nil {
		return LotteryConfigView{}, err
	}
	return lotteryConfigView(snapshot), nil
}

func (s *LotteryService) UpdateConfig(ctx context.Context, input LotteryConfigInput, updatedBy int64) (LotteryConfigView, error) {
	config, err := normalizeLotteryConfigInput(input)
	if err != nil {
		return LotteryConfigView{}, infraerrors.BadRequest("LOTTERY_CONFIG_INVALID", err.Error())
	}
	snapshot, err := s.repo.SaveConfig(ctx, config, updatedBy)
	if err != nil {
		return LotteryConfigView{}, err
	}
	return lotteryConfigView(snapshot), nil
}

func (s *LotteryService) ListDraws(ctx context.Context, page, pageSize int) (LotteryDrawPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	items, total, err := s.repo.ListDraws(ctx, page, pageSize)
	if err != nil {
		return LotteryDrawPage{}, err
	}
	return LotteryDrawPage{
		Items:    normalizeLotteryDraws(items),
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func normalizeLotteryConfigInput(input LotteryConfigInput) (LotteryConfig, error) {
	if math.IsNaN(input.UsageThresholdM) || math.IsInf(input.UsageThresholdM, 0) || input.UsageThresholdM <= 0 {
		return LotteryConfig{}, fmt.Errorf("每日用量门槛必须大于 0M")
	}
	thresholdFloat := input.UsageThresholdM * 1_000_000
	if thresholdFloat > math.MaxInt64 {
		return LotteryConfig{}, fmt.Errorf("每日用量门槛过大")
	}
	config := LotteryConfig{
		UsageThresholdTokens: int64(math.Round(thresholdFloat)),
		AwardMode:            input.AwardMode,
		Prizes:               make([]LotteryPrize, 0, len(input.Prizes)),
	}
	for _, item := range input.Prizes {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = uuid.NewString()
		}
		if math.IsNaN(item.ProbabilityPercent) || math.IsInf(item.ProbabilityPercent, 0) {
			return LotteryConfig{}, fmt.Errorf("奖品概率必须是有效数字")
		}
		config.Prizes = append(config.Prizes, LotteryPrize{
			ID:                     id,
			Name:                   item.Name,
			RewardAmount:           item.RewardAmount,
			ProbabilityBasisPoints: int(math.Round(item.ProbabilityPercent * 100)),
		})
	}
	return NormalizeLotteryConfig(config)
}

func lotteryConfigView(snapshot LotteryConfigSnapshot) LotteryConfigView {
	prizes := make([]LotteryPrizeView, 0, len(snapshot.Config.Prizes)+1)
	for _, prize := range snapshot.Config.Prizes {
		prizes = append(prizes, LotteryPrizeView{
			ID:                 prize.ID,
			Name:               prize.Name,
			RewardAmount:       prize.RewardAmount,
			ProbabilityPercent: float64(prize.ProbabilityBasisPoints) / 100,
		})
	}
	thanksPercent := float64(LotteryThanksProbabilityBasisPoints(snapshot.Config)) / 100
	prizes = append(prizes, LotteryPrizeView{
		ID:                 LotteryThanksPrizeID,
		Name:               "谢谢惠顾",
		ProbabilityPercent: thanksPercent,
		IsThanks:           true,
	})
	return LotteryConfigView{
		Enabled:                  snapshot.Config.Enabled,
		UsageThresholdM:          float64(snapshot.Config.UsageThresholdTokens) / 1_000_000,
		UsageThresholdTokens:     snapshot.Config.UsageThresholdTokens,
		AwardMode:                snapshot.Config.AwardMode,
		Prizes:                   prizes,
		ThanksProbabilityPercent: thanksPercent,
		Version:                  snapshot.Version,
		UpdatedAt:                snapshot.UpdatedAt,
	}
}

func normalizeLotteryDraws(items []LotteryDraw) []LotteryDraw {
	if items == nil {
		return []LotteryDraw{}
	}
	for index := range items {
		items[index] = normalizeLotteryDraw(items[index])
	}
	return items
}

func normalizeLotteryDraw(draw LotteryDraw) LotteryDraw {
	draw.ProbabilityPercent = float64(draw.ProbabilityBasisPoints) / 100
	return draw
}

func secureLotteryRoll() (int, error) {
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(lotteryProbabilityScale))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func (s *LotteryService) invalidateBalanceCaches(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_ = s.billingCacheService.InvalidateUserBalance(cacheCtx, userID)
	}
}
