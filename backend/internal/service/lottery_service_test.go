package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type lotteryRepositoryStub struct {
	mu                  sync.Mutex
	snapshot            LotteryConfigSnapshot
	state               LotteryUserState
	balance             float64
	draws               []LotteryDraw
	reconcileAward      int64
	reconcileAwarded    bool
	reconcileCallCount  int
	commitDrawCallCount int
}

func (r *lotteryRepositoryStub) GetConfig(context.Context) (LotteryConfigSnapshot, error) {
	return r.snapshot, nil
}

func (r *lotteryRepositoryStub) SaveConfig(_ context.Context, config LotteryConfig, _ int64) (LotteryConfigSnapshot, error) {
	r.snapshot.Config = config
	r.snapshot.Version++
	r.snapshot.UpdatedAt = time.Now()
	return r.snapshot, nil
}

func (r *lotteryRepositoryStub) ReconcileUserChances(context.Context, int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconcileCallCount++
	if !r.reconcileAwarded && r.reconcileAward > 0 {
		r.state.AvailableChances += r.reconcileAward
		r.state.TotalEarned += r.reconcileAward
		r.reconcileAwarded = true
	}
	return nil
}

func (r *lotteryRepositoryStub) GetUserState(context.Context, int64) (LotteryUserState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state, nil
}

func (r *lotteryRepositoryStub) CommitDraw(_ context.Context, userID int64, selection LotterySelection, configVersion int64) (LotteryDraw, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commitDrawCallCount++
	if r.state.AvailableChances <= 0 {
		return LotteryDraw{}, ErrLotteryNoChance
	}

	prizeID := LotteryThanksPrizeID
	prizeName := "谢谢惠顾"
	reward := 0.0
	probability := selection.ThanksProbabilityBasis
	if selection.Prize != nil {
		prizeID = selection.Prize.ID
		prizeName = selection.Prize.Name
		reward = selection.Prize.RewardAmount
		probability = selection.Prize.ProbabilityBasisPoints
	}
	before := r.state.AvailableChances
	r.state.AvailableChances--
	r.state.TotalDrawn++
	r.balance += reward
	draw := LotteryDraw{
		ID:                     int64(len(r.draws) + 1),
		UserID:                 userID,
		PrizeID:                prizeID,
		PrizeName:              prizeName,
		RewardAmount:           reward,
		ProbabilityBasisPoints: probability,
		ConfigVersion:          configVersion,
		ChanceBefore:           before,
		ChanceAfter:            r.state.AvailableChances,
		BalanceAfter:           r.balance,
		CreatedAt:              time.Now(),
	}
	r.draws = append(r.draws, draw)
	return draw, nil
}

func (r *lotteryRepositoryStub) ListUserDraws(_ context.Context, _ int64, limit int) ([]LotteryDraw, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit > len(r.draws) {
		limit = len(r.draws)
	}
	return append([]LotteryDraw(nil), r.draws[:limit]...), nil
}

func (r *lotteryRepositoryStub) ListDraws(context.Context, int, int) ([]LotteryDraw, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]LotteryDraw(nil), r.draws...), int64(len(r.draws)), nil
}

func newLotteryServiceTestRepo(prizes []LotteryPrize, chances int64) *lotteryRepositoryStub {
	return &lotteryRepositoryStub{
		snapshot: LotteryConfigSnapshot{
			Config: LotteryConfig{
				Enabled:              true,
				UsageThresholdTokens: 1_000_000,
				AwardMode:            LotteryAwardModeDailyOnce,
				Prizes:               prizes,
			},
			Version:   3,
			UpdatedAt: time.Now(),
		},
		state: LotteryUserState{
			AvailableChances: chances,
			TotalEarned:      chances,
		},
		balance: 10,
	}
}

func TestLotteryServiceDrawNoPrizeAlwaysReturnsThanks(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 1)
	svc := NewLotteryService(repo, nil, nil)
	svc.randomRoll = func() (int, error) { return 9999, nil }

	result, err := svc.Draw(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, LotteryThanksPrizeID, result.Draw.PrizeID)
	require.Zero(t, result.Draw.RewardAmount)
	require.Equal(t, 100.0, result.Draw.ProbabilityPercent)
	require.Zero(t, result.AvailableChances)
	require.Equal(t, 10.0, result.NewBalance)
}

func TestLotteryServiceOverviewRejectsDisabledFeatureBeforeReconcile(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 1)
	repo.snapshot.Config.Enabled = false
	svc := NewLotteryService(repo, nil, nil)

	_, err := svc.GetOverview(context.Background(), 7)

	require.ErrorIs(t, err, ErrLotteryDisabled)
	require.Zero(t, repo.reconcileCallCount)
}

func TestLotteryServiceDrawCreditsConfiguredBalancePrize(t *testing.T) {
	repo := newLotteryServiceTestRepo([]LotteryPrize{{
		ID:                     "small",
		Name:                   "2.5 灵石",
		RewardAmount:           2.5,
		ProbabilityBasisPoints: 2500,
	}}, 1)
	svc := NewLotteryService(repo, nil, nil)
	svc.randomRoll = func() (int, error) { return 0, nil }

	result, err := svc.Draw(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, "small", result.Draw.PrizeID)
	require.Equal(t, 2.5, result.Draw.RewardAmount)
	require.Equal(t, 25.0, result.Draw.ProbabilityPercent)
	require.Equal(t, 12.5, result.NewBalance)
}

func TestLotteryServiceReconcileKeepsEarnedChancesAcrossOverviewLoads(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 2)
	repo.reconcileAward = 1
	svc := NewLotteryService(repo, nil, nil)

	first, err := svc.GetOverview(context.Background(), 7)
	require.NoError(t, err)
	second, err := svc.GetOverview(context.Background(), 7)
	require.NoError(t, err)

	require.Equal(t, int64(3), first.State.AvailableChances)
	require.Equal(t, int64(3), second.State.AvailableChances)
	require.Equal(t, int64(3), second.State.TotalEarned)
	require.Equal(t, 2, repo.reconcileCallCount)
}

func TestLotteryServicePerThresholdShowsProgressToNextChance(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 3)
	repo.snapshot.Config.AwardMode = LotteryAwardModePerThreshold
	repo.state.TodayUsageTokens = 3_400_000
	repo.state.TodayThreshold = 1_000_000
	repo.state.TodayAwardMode = LotteryAwardModePerThreshold
	repo.state.TodayAwardedChances = 3
	svc := NewLotteryService(repo, nil, nil)

	overview, err := svc.GetOverview(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, 40.0, overview.TodayProgress)
	require.Equal(t, int64(4_000_000), overview.State.TodayNextTargetTokens)
	require.Equal(t, 4.0, overview.TodayNextTargetM)
}

func TestLotteryServicePerThresholdKeepsNextMilestoneAfterThresholdIncrease(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 3)
	repo.state.TodayUsageTokens = 3_400_000
	repo.state.TodayThreshold = 5_000_000
	repo.state.TodayAwardMode = LotteryAwardModePerThreshold
	repo.state.TodayAwardedChances = 3
	svc := NewLotteryService(repo, nil, nil)

	overview, err := svc.GetOverview(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, 0.0, overview.TodayProgress)
	require.Equal(t, int64(20_000_000), overview.State.TodayNextTargetTokens)
	require.Equal(t, 20.0, overview.TodayNextTargetM)
}

func TestLotteryServiceDailyOnceRemainsCompleteAfterThresholdIncrease(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 1)
	repo.state.TodayUsageTokens = 3_400_000
	repo.state.TodayThreshold = 5_000_000
	repo.state.TodayAwardMode = LotteryAwardModeDailyOnce
	repo.state.TodayAwardedChances = 1
	svc := NewLotteryService(repo, nil, nil)

	overview, err := svc.GetOverview(context.Background(), 7)

	require.NoError(t, err)
	require.Equal(t, 100.0, overview.TodayProgress)
	require.Equal(t, int64(5_000_000), overview.State.TodayNextTargetTokens)
}

func TestLotteryServiceConcurrentDrawConsumesSingleChanceOnce(t *testing.T) {
	repo := newLotteryServiceTestRepo(nil, 1)
	svc := NewLotteryService(repo, nil, nil)
	svc.randomRoll = func() (int, error) { return 5000, nil }

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := svc.Draw(context.Background(), 7)
			errs <- err
		}()
	}
	close(start)

	var successCount, noChanceCount int
	for range 2 {
		err := <-errs
		switch err {
		case nil:
			successCount++
		case ErrLotteryNoChance:
			noChanceCount++
		default:
			t.Fatalf("unexpected draw error: %v", err)
		}
	}
	require.Equal(t, 1, successCount)
	require.Equal(t, 1, noChanceCount)
	require.Equal(t, 2, repo.commitDrawCallCount)
	require.Len(t, repo.draws, 1)
}

func TestNormalizeLotteryConfigInputRejectsMoreThanFivePrizes(t *testing.T) {
	prizes := make([]LotteryPrizeInput, LotteryMaxPrizes+1)
	for index := range prizes {
		prizes[index] = LotteryPrizeInput{
			Name:               "奖品",
			RewardAmount:       1,
			ProbabilityPercent: 1,
		}
	}

	_, err := normalizeLotteryConfigInput(LotteryConfigInput{
		UsageThresholdM: 1,
		Prizes:          prizes,
	})
	require.ErrorContains(t, err, "最多")
}

func TestNormalizeLotteryConfigInputRejectsProbabilityAboveOneHundredPercent(t *testing.T) {
	_, err := normalizeLotteryConfigInput(LotteryConfigInput{
		UsageThresholdM: 1,
		Prizes: []LotteryPrizeInput{
			{Name: "奖品 A", RewardAmount: 1, ProbabilityPercent: 60},
			{Name: "奖品 B", RewardAmount: 2, ProbabilityPercent: 40.01},
		},
	})
	require.ErrorContains(t, err, "不能超过 100%")
}
