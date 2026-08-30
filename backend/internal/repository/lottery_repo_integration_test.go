//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLotteryRepositoryReconcileSupportsBothAwardModesWithoutDuplicateGrants(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	repo := &lotteryRepository{db: integrationDB}
	settingRepo := NewSettingRepository(client)
	usageRepo := NewUsageLogRepository(client, integrationDB)
	stamp := time.Now().UnixNano()
	require.NoError(t, settingRepo.Set(ctx, service.SettingKeyLotteryEnabled, "true"))
	t.Cleanup(func() {
		require.NoError(t, settingRepo.Set(context.Background(), service.SettingKeyLotteryEnabled, "false"))
	})
	snapshot, err := repo.GetConfig(ctx)
	require.NoError(t, err)
	require.True(t, snapshot.Config.Enabled)

	createUsage := func(label string, tokens int) int64 {
		user := mustCreateUser(t, client, &service.User{
			Email: fmt.Sprintf("lottery-%s-%d@example.com", label, stamp),
		})
		apiKey := mustCreateApiKey(t, client, &service.APIKey{
			UserID: user.ID,
			Key:    fmt.Sprintf("sk-lottery-%s-%d", label, stamp),
			Name:   "lottery",
		})
		account := mustCreateAccount(t, client, &service.Account{
			Name: fmt.Sprintf("lottery-%s-%d", label, stamp),
		})
		_, err := usageRepo.Create(ctx, &service.UsageLog{
			UserID:      user.ID,
			APIKeyID:    apiKey.ID,
			AccountID:   account.ID,
			RequestID:   uuid.NewString(),
			Model:       "lottery-test",
			InputTokens: tokens,
			TotalCost:   0.01,
			ActualCost:  0.01,
			CreatedAt:   time.Now(),
		})
		require.NoError(t, err)
		return user.ID
	}

	perThresholdUserID := createUsage("threshold", 3_400_000)
	_, err = repo.SaveConfig(ctx, service.LotteryConfig{
		UsageThresholdTokens: 1_000_000,
		AwardMode:            service.LotteryAwardModePerThreshold,
	}, perThresholdUserID)
	require.NoError(t, err)

	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- repo.ReconcileUserChances(ctx, perThresholdUserID) }()
	}
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)
	perThresholdState, err := repo.GetUserState(ctx, perThresholdUserID)
	require.NoError(t, err)
	require.Equal(t, int64(3), perThresholdState.AvailableChances)
	require.Equal(t, int64(3), perThresholdState.TodayAwardedChances)

	dailyOnceUserID := createUsage("daily", 3_400_000)
	_, err = repo.SaveConfig(ctx, service.LotteryConfig{
		UsageThresholdTokens: 1_000_000,
		AwardMode:            service.LotteryAwardModeDailyOnce,
	}, dailyOnceUserID)
	require.NoError(t, err)
	require.NoError(t, repo.ReconcileUserChances(ctx, dailyOnceUserID))
	require.NoError(t, repo.ReconcileUserChances(ctx, dailyOnceUserID))
	dailyOnceState, err := repo.GetUserState(ctx, dailyOnceUserID)
	require.NoError(t, err)
	require.Equal(t, int64(1), dailyOnceState.AvailableChances)
	require.Equal(t, int64(1), dailyOnceState.TodayAwardedChances)
}
