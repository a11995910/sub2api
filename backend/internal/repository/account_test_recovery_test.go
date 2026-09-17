package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountTestRecovery_ConflictDoesNotReportRecovery(t *testing.T) {
	executor := &recordingSQLExecutor{result: rowsAffectedResult(0)}
	repo := newAccountRepositoryWithSQL(nil, executor, nil)
	result, err := repo.RecoverAccountTestIfUnchanged(context.Background(), &service.AccountTestObservation{
		AccountID: 42, UpdatedAt: time.Now(), Succeeded: true, ClearError: true,
		ModelRateLimitKeys: []string{"gpt-5.4"},
	})
	require.NoError(t, err)
	require.False(t, result.ClearedError)
	require.False(t, result.ClearedRateLimit)
	require.Len(t, executor.execQueries, 1)
}

func TestAccountTestRecovery_NoSuccessfulObservationNeverWrites(t *testing.T) {
	for _, observation := range []*service.AccountTestObservation{nil, {AccountID: 42, UpdatedAt: time.Now(), ClearError: true}, {AccountID: 42, Succeeded: true, ClearError: true}} {
		executor := &recordingSQLExecutor{result: rowsAffectedResult(1)}
		repo := newAccountRepositoryWithSQL(nil, executor, nil)
		result, err := repo.RecoverAccountTestIfUnchanged(context.Background(), observation)
		require.NoError(t, err)
		require.False(t, result.ClearedError)
		require.Empty(t, executor.execQueries)
	}
}
