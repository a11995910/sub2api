package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoTaskReviewConfirmFailedReleasesManualReview(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UserID: 7, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusSubmissionUnknown, BillingStatus: VideoTaskBillingManualReview}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	cache := &fakeVideoTaskBalanceCache{}
	billing := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{}, cache)
	svc := NewVideoTaskReviewService(repo, billing, nil, nil)

	err := svc.ConfirmFailed(context.Background(), 9, "上游控制台确认无任务")

	require.NoError(t, err)
	require.Equal(t, []string{"review_release"}, repo.events)
	require.Equal(t, []int64{7}, cache.invalidatedUserIDs)
}

func TestVideoTaskReviewConfirmSucceededSettlesManualReview(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UserID: 7, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusSubmissionUnknown, BillingStatus: VideoTaskBillingManualReview}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	usage := &fakeVideoTaskUsageRecorder{}
	billing := NewVideoTaskBillingService(repo, nil, usage)
	svc := NewVideoTaskReviewService(repo, billing, nil, nil)

	err := svc.ConfirmSucceeded(context.Background(), 9, "上游账单确认已生成")

	require.NoError(t, err)
	require.Equal(t, []string{"manual_settling", "capture"}, repo.events)
	require.Equal(t, []string{"usage"}, usage.events)
}

func TestVideoTaskReviewConfirmFailedRejectsProcessingReservedTask(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UserID: 7, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	svc := NewVideoTaskReviewService(repo, NewVideoTaskBillingService(repo, nil, nil), nil, nil)

	err := svc.ConfirmFailed(context.Background(), 9, "人工确认失败")

	require.ErrorIs(t, err, ErrVideoTaskBillingInvalidState)
	require.Empty(t, repo.events)
}

func TestVideoTaskReviewConfirmSucceededRetriesSettlingTask(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UserID: 7, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusCompleted, BillingStatus: VideoTaskBillingSettling}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskReviewService(repo, NewVideoTaskBillingService(repo, nil, usage), nil, nil)

	err := svc.ConfirmSucceeded(context.Background(), 9, "重试结算")

	require.NoError(t, err)
	require.Equal(t, []string{"usage", "capture"}, append(usage.events, repo.events...))
}

func TestVideoTaskReviewRecheckRequiresUpstreamTaskID(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, BillingStatus: VideoTaskBillingManualReview}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	svc := NewVideoTaskReviewService(repo, NewVideoTaskBillingService(repo, nil, nil), nil, nil)

	err := svc.Recheck(context.Background(), 9)

	require.ErrorIs(t, err, ErrVideoTaskReviewCannotRecheck)
}

func TestVideoTaskReviewRecheckRejectsProcessingReservedTask(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UpstreamTaskID: "task-1", TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	svc := NewVideoTaskReviewService(repo, NewVideoTaskBillingService(repo, nil, nil), &fakeVideoTaskQueryGateway{}, nil)

	err := svc.Recheck(context.Background(), 9)

	require.ErrorIs(t, err, ErrVideoTaskBillingInvalidState)
}

func TestVideoTaskReviewRecheckFailedStatusDoesNotClaimArtifactAbsence(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UserID: 7, AccountID: 17, Platform: PlatformOpenAI, UpstreamTaskID: "task-1", TaskStatus: VideoTaskStatusUnknown, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskReviewRepo{fakeVideoTaskBillingRepo: fakeVideoTaskBillingRepo{task: task}}
	billing := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{})
	gateway := &fakeVideoTaskQueryGateway{result: &OpenAIForwardResult{
		VideoStatus:            "failed",
		VideoArtifactAvailable: true,
	}}
	svc := NewVideoTaskReviewService(repo, billing, gateway, &fakeVideoTaskAccountRepo{account: &Account{ID: 17, Platform: PlatformOpenAI}})

	err := svc.Recheck(context.Background(), 9)

	require.NoError(t, err)
	require.Equal(t, "人工确认失败: 重新核对确认上游明确失败", repo.releaseReason)
}

type fakeVideoTaskReviewRepo struct {
	fakeVideoTaskBillingRepo
	releaseReason string
}

func (r *fakeVideoTaskReviewRepo) ListReviewTasks(context.Context, VideoTaskReviewFilter) ([]VideoTaskReviewItem, int64, error) {
	return nil, 0, nil
}

func (r *fakeVideoTaskReviewRepo) BeginManualSettlement(_ context.Context, _ int64, reason string) (*VideoTaskBilling, error) {
	if reason == "" {
		return nil, errors.New("reason required")
	}
	r.events = append(r.events, "manual_settling")
	r.task.TaskStatus = VideoTaskStatusCompleted
	r.task.BillingStatus = VideoTaskBillingSettling
	return r.task, nil
}

func (r *fakeVideoTaskReviewRepo) ReleaseReviewedFailure(_ context.Context, _ int64, reason string) (*VideoTaskBilling, error) {
	if reason == "" || !isVideoTaskReviewCandidate(r.task) || r.task.BillingStatus == VideoTaskBillingSettling {
		return nil, ErrVideoTaskBillingInvalidState
	}
	r.events = append(r.events, "review_release")
	r.releaseReason = reason
	r.task.BillingStatus = VideoTaskBillingReleased
	return r.task, nil
}

func (r *fakeVideoTaskReviewRepo) UpdateReviewObservation(_ context.Context, _ int64, outcome VideoTaskOutcome) error {
	r.events = append(r.events, "review:"+outcome.Status)
	return nil
}
