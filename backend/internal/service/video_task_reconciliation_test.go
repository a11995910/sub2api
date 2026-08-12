package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoTaskReconciliationExplicitFailureReleasesReservation(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, Platform: PlatformOpenAI, UpstreamTaskID: "task-1", AccountID: 17, TaskStatus: VideoTaskStatusPending, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeReconciliationBillingRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}, due: []*VideoTaskBilling{task}}
	billing := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{})
	worker := NewVideoTaskReconciliationService(billing, &fakeVideoTaskQueryGateway{result: &OpenAIForwardResult{VideoStatus: "failed", VideoErrorMessage: "generation failed"}}, &fakeVideoTaskAccountRepo{account: &Account{ID: 17, Platform: PlatformOpenAI}})

	err := worker.ReconcileOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, []string{"poll:failed", "release"}, repo.events)
}

func TestVideoTaskReconciliationTransientFailureRetainsReservation(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, Platform: PlatformOpenAI, UpstreamTaskID: "task-1", AccountID: 17, TaskStatus: VideoTaskStatusPending, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeReconciliationBillingRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}, due: []*VideoTaskBilling{task}}
	billing := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{})
	worker := NewVideoTaskReconciliationService(billing, &fakeVideoTaskQueryGateway{err: errors.New("upstream timeout")}, &fakeVideoTaskAccountRepo{account: &Account{ID: 17, Platform: PlatformOpenAI}})

	err := worker.ReconcileOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, []string{"poll:unknown"}, repo.events)
	require.Equal(t, VideoTaskBillingReserved, task.BillingStatus)
}

func TestVideoTaskReconciliationExpiredSubmissionNeverRecreatesTask(t *testing.T) {
	deadline := time.Now().Add(-time.Minute)
	task := &VideoTaskBilling{ID: 9, AccountID: 17, TaskStatus: VideoTaskStatusSubmitting, BillingStatus: VideoTaskBillingReserved, SubmissionDeadline: &deadline}
	repo := &fakeReconciliationBillingRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}, due: []*VideoTaskBilling{task}}
	billing := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{})
	gateway := &fakeVideoTaskQueryGateway{}
	worker := NewVideoTaskReconciliationService(billing, gateway, &fakeVideoTaskAccountRepo{account: &Account{ID: 17}})

	err := worker.ReconcileOnce(context.Background())

	require.NoError(t, err)
	require.Equal(t, []string{"submission_unknown"}, repo.events)
	require.Zero(t, gateway.calls)
}

type fakeReconciliationBillingRepo struct {
	fakeVideoTaskBillingRepo
	due []*VideoTaskBilling
}

func (r *fakeReconciliationBillingRepo) ClaimDue(_ context.Context, _ int, _ time.Duration) ([]*VideoTaskBilling, error) {
	return r.due, nil
}

type fakeVideoTaskQueryGateway struct {
	result *OpenAIForwardResult
	err    error
	calls  int
}

func (g *fakeVideoTaskQueryGateway) QueryOpenAIVideoTask(context.Context, *Account, string) (*OpenAIForwardResult, error) {
	g.calls++
	return g.result, g.err
}

func (g *fakeVideoTaskQueryGateway) QueryGrokVideoTask(context.Context, *Account, string) (*OpenAIForwardResult, error) {
	g.calls++
	return g.result, g.err
}
