//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVideoTaskBillingRepositoryNullableReservationAndFailureRelease(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	group := mustCreateGroup(t, integrationEntClient, &service.Group{
		Name:     fmt.Sprintf("video-billing-group-%d", suffix),
		Platform: service.PlatformOpenAI,
	})
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email:   fmt.Sprintf("video-billing-%d@example.com", suffix),
		Balance: 20,
	})
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name:     fmt.Sprintf("video-billing-account-%d", suffix),
		Platform: service.PlatformOpenAI,
		Type:     service.AccountTypeAPIKey,
	})
	groupID := group.ID
	apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{
		UserID:  user.ID,
		GroupID: &groupID,
		Key:     fmt.Sprintf("sk-video-billing-%d", suffix),
	})
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM video_task_billings WHERE user_id=$1", user.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM api_keys WHERE id=$1", apiKey.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM scheduler_outbox WHERE account_id=$1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM accounts WHERE id=$1", account.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id=$1", user.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM groups WHERE id=$1", group.ID)
	})

	repo := NewVideoTaskBillingRepository(integrationDB)
	deadline := time.Now().Add(-time.Minute)
	newTask := func(requestID string) *service.VideoTaskBilling {
		return &service.VideoTaskBilling{
			RequestID: requestID, Platform: service.PlatformOpenAI,
			UserID: user.ID, APIKeyID: apiKey.ID, GroupID: &groupID, AccountID: account.ID,
			Model: "sd4-seedance-2.0", UpstreamModel: "sd4-seedance-2.0",
			EstimatedCost: 1.25, TaskStatus: service.VideoTaskStatusSubmitting,
			BillingStatus: service.VideoTaskBillingReserved,
			NextPollAt:    deadline, SubmissionDeadline: &deadline,
		}
	}
	first := newTask(fmt.Sprintf("video-first-%d", suffix))
	second := newTask(fmt.Sprintf("video-second-%d", suffix))
	require.NoError(t, repo.ReserveAndCreate(ctx, first))
	require.NoError(t, repo.ReserveAndCreate(ctx, second))

	var nullCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM video_task_billings WHERE id IN ($1,$2) AND upstream_task_id IS NULL",
		first.ID, second.ID).Scan(&nullCount))
	require.Equal(t, 2, nullCount)

	failed, err := repo.UpdatePoll(ctx, first.ID, service.VideoTaskOutcome{
		Status: service.VideoTaskStatusFailed, ErrorMessage: "upstream rejected request",
	}, time.Now())
	require.NoError(t, err)
	require.NoError(t, repo.Release(ctx, failed.ID, failed.LastPollError))

	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(ctx,
		"SELECT balance, frozen_balance FROM users WHERE id=$1", user.ID).Scan(&balance, &frozen))
	require.InDelta(t, 18.75, balance, 0.00000001)
	require.InDelta(t, 1.25, frozen, 0.00000001)

	due, err := repo.ClaimDue(ctx, 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, second.ID, due[0].ID)
	require.Empty(t, due[0].UpstreamTaskID)
	require.Empty(t, due[0].LastPollError)
}
