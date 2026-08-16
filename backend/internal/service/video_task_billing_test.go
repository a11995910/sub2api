package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVideoTaskBillingServiceApplyCompletedSettlesUsageThenCaptures(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskBillingService(repo, nil, usage)

	err := svc.ApplyOutcome(context.Background(), task, VideoTaskOutcome{
		Status: VideoTaskStatusCompleted, ResponseJSON: json.RawMessage(`{"status":"completed","url":"https://example.com/video.mp4"}`),
	})

	require.NoError(t, err)
	require.Equal(t, []string{"poll:completed", "settling", "usage", "capture"}, append(repo.events[:2], append(usage.events, repo.events[2:]...)...))
	require.InDelta(t, 1.25, repo.capturedCost, 0.000001)
}

func TestVideoTaskBillingServiceApplyFailedReleasesWithoutUsage(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskBillingService(repo, nil, usage)

	err := svc.ApplyOutcome(context.Background(), task, VideoTaskOutcome{Status: VideoTaskStatusFailed, ErrorMessage: "generation failed"})

	require.NoError(t, err)
	require.Equal(t, []string{"poll:failed", "release"}, repo.events)
	require.Empty(t, usage.events)
}

func TestVideoTaskBillingServiceApplyUnknownKeepsReservation(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskBillingService(repo, nil, usage)

	err := svc.ApplyOutcome(context.Background(), task, VideoTaskOutcome{Status: VideoTaskStatusUnknown, ErrorMessage: "upstream timeout"})

	require.NoError(t, err)
	require.Equal(t, []string{"poll:unknown"}, repo.events)
	require.Empty(t, usage.events)
	require.Zero(t, repo.capturedCost)
}

func TestVideoTaskBillingServiceApplyOutcomeIgnoresAlreadyCapturedTask(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, TaskStatus: VideoTaskStatusCompleted, BillingStatus: VideoTaskBillingCaptured}
	repo := &fakeVideoTaskBillingRepo{task: task}
	svc := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{})

	err := svc.ApplyOutcome(context.Background(), task, VideoTaskOutcome{Status: VideoTaskStatusCompleted})

	require.NoError(t, err)
	require.Empty(t, repo.events)
}

func TestVideoTaskBillingServiceApplyUnknownDoesNotOverwriteSettlingCompletion(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusCompleted, BillingStatus: VideoTaskBillingSettling}
	repo := &fakeVideoTaskBillingRepo{task: task}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskBillingService(repo, nil, usage)

	err := svc.ApplyOutcome(context.Background(), task, VideoTaskOutcome{Status: VideoTaskStatusUnknown, ErrorMessage: "upstream timeout"})

	require.NoError(t, err)
	require.Equal(t, VideoTaskStatusCompleted, task.TaskStatus)
	require.Equal(t, VideoTaskBillingSettling, task.BillingStatus)
	require.Empty(t, repo.events)
	require.Empty(t, usage.events)
}

func TestVideoTaskBillingServiceReservePricesBeforeCreatingHold(t *testing.T) {
	repo := &fakeVideoTaskBillingRepo{}
	estimator := &fakeVideoTaskCostEstimator{cost: &CostBreakdown{ActualCost: 1.25}}
	cache := &fakeVideoTaskBalanceCache{}
	svc := NewVideoTaskBillingService(repo, estimator, nil, cache)

	task, err := svc.Reserve(context.Background(), VideoTaskReserveInput{
		RequestID: "request-1", Platform: PlatformOpenAI, UserID: 7, APIKeyID: 11, GroupID: ptrVideoInt64(13), AccountID: 17,
		APIKey: &APIKey{ID: 11}, Model: "video-model", UpstreamModel: "video-model", Resolution: "720p", DurationSeconds: 8,
	})

	require.NoError(t, err)
	require.Equal(t, VideoTaskStatusSubmitting, task.TaskStatus)
	require.Equal(t, VideoTaskBillingReserved, task.BillingStatus)
	require.InDelta(t, 1.25, task.EstimatedCost, 0.000001)
	require.True(t, task.SubmissionDeadline.After(time.Now().UTC()))
	require.Equal(t, 1, estimator.calls)
	require.Equal(t, []int64{7}, cache.invalidatedUserIDs)
}

func TestVideoTaskBillingServiceReserveStoresQuantizedActualCostSnapshot(t *testing.T) {
	repo := &fakeVideoTaskBillingRepo{}
	estimator := &fakeVideoTaskCostEstimator{cost: &CostBreakdown{TotalCost: 0.0000625, ActualCost: 0.000078125, BillingMode: string(BillingModeVideo)}}
	svc := NewVideoTaskBillingService(repo, estimator, nil)

	task, err := svc.Reserve(context.Background(), VideoTaskReserveInput{
		RequestID: "request-quantized", Platform: PlatformOpenAI, UserID: 7, APIKeyID: 11, AccountID: 17,
		APIKey: &APIKey{ID: 11}, Model: "video-model", Resolution: "720p", DurationSeconds: 8,
	})

	require.NoError(t, err)
	require.Equal(t, 0.00007813, task.EstimatedCost)
	var usageContext VideoTaskUsageContext
	require.NoError(t, json.Unmarshal(task.UsageContextJSON, &usageContext))
	require.NotNil(t, usageContext.CostSnapshot)
	require.Equal(t, task.EstimatedCost, usageContext.CostSnapshot.ActualCost)
	require.Equal(t, 0.0000625, usageContext.CostSnapshot.TotalCost)
}

func TestVideoTaskBillingServiceApplyFailedInvalidatesBalanceCache(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, UserID: 7, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	cache := &fakeVideoTaskBalanceCache{}
	svc := NewVideoTaskBillingService(repo, nil, &fakeVideoTaskUsageRecorder{}, cache)

	err := svc.ApplyOutcome(context.Background(), task, VideoTaskOutcome{Status: VideoTaskStatusFailed, ErrorMessage: "generation failed"})

	require.NoError(t, err)
	require.Equal(t, []int64{7}, cache.invalidatedUserIDs)
}

func TestVideoTaskBillingServiceObserveCreatedAttachesPendingTask(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, TaskStatus: VideoTaskStatusSubmitting, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	svc := NewVideoTaskBillingService(repo, nil, nil)

	attached, err := svc.ObserveCreated(context.Background(), task, "upstream-1", &OpenAIForwardResult{
		VideoStatus:       "queued",
		VideoResponseJSON: json.RawMessage(`{"id":"upstream-1","status":"queued"}`),
	})

	require.NoError(t, err)
	require.Equal(t, "upstream-1", attached.UpstreamTaskID)
	require.Equal(t, VideoTaskStatusPending, attached.TaskStatus)
	require.Equal(t, []string{"attach:pending"}, repo.events)
}

func TestVideoTaskBillingServiceMarkSubmissionUnknownRetainsReservation(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, TaskStatus: VideoTaskStatusSubmitting, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	svc := NewVideoTaskBillingService(repo, nil, nil)

	err := svc.MarkSubmissionUnknown(context.Background(), task, errors.New("upstream timeout"))

	require.NoError(t, err)
	require.Equal(t, VideoTaskStatusSubmissionUnknown, task.TaskStatus)
	require.Equal(t, VideoTaskBillingManualReview, task.BillingStatus)
	require.Equal(t, "upstream timeout", task.LastPollError)
	require.Equal(t, []string{"submission_unknown"}, repo.events)
}

func TestVideoTaskBillingServiceResolveOwnedTaskRejectsOwnershipMismatch(t *testing.T) {
	task := &VideoTaskBilling{ID: 9, Platform: PlatformOpenAI, UpstreamTaskID: "upstream-1", UserID: 7, APIKeyID: 11, AccountID: 17}
	repo := &fakeVideoTaskBillingRepo{task: task}
	svc := NewVideoTaskBillingService(repo, nil, nil)

	_, err := svc.ResolveOwnedTask(context.Background(), PlatformOpenAI, "upstream-1", 8, 11)

	require.ErrorIs(t, err, ErrVideoTaskBillingNotFound)
}

func TestClassifyVideoTaskResultRequiresArtifactBeforeCompletion(t *testing.T) {
	outcome := ClassifyVideoTaskResult(&OpenAIForwardResult{
		VideoStatus:       "completed",
		VideoResponseJSON: json.RawMessage(`{"status":"completed"}`),
	})

	require.Equal(t, VideoTaskStatusUnknown, outcome.Status)
	require.Contains(t, outcome.ErrorMessage, "artifact")
}

func TestClassifyVideoTaskResultDoesNotReleaseFailedTaskWithArtifact(t *testing.T) {
	outcome := ClassifyVideoTaskResult(&OpenAIForwardResult{
		VideoStatus:            "failed",
		VideoArtifactAvailable: true,
		VideoResponseJSON:      json.RawMessage(`{"status":"failed","url":"https://example.com/video.mp4"}`),
	})

	require.Equal(t, VideoTaskStatusCompleted, outcome.Status)
}

func TestCangyuanFailedVideoResponseReleasesReservation(t *testing.T) {
	body := []byte(`{
		"id":"task-cangyuan-failed",
		"status":"failed",
		"error":{"message":"reference video is invalid"}
	}`)
	parsed, err := ParseOpenAIVideoResult(body)
	require.NoError(t, err)
	outcome := ClassifyVideoTaskResult(&OpenAIForwardResult{
		VideoStatus:       parsed.Status,
		VideoErrorMessage: parsed.ErrorMessage,
		VideoResponseJSON: body,
	})
	require.Equal(t, VideoTaskStatusFailed, outcome.Status)
	require.Equal(t, "reference video is invalid", outcome.ErrorMessage)

	task := &VideoTaskBilling{ID: 9, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskBillingService(repo, nil, usage)

	require.NoError(t, svc.ApplyOutcome(context.Background(), task, outcome))
	require.Equal(t, []string{"poll:failed", "release"}, repo.events)
	require.Empty(t, usage.events)
	require.Zero(t, repo.capturedCost)
}

func TestCangyuanCompletedMetadataVideoURLCapturesReservation(t *testing.T) {
	body := []byte(`{
		"id":"task-cangyuan-completed",
		"status":"completed",
		"metadata":{"video_url":"https://cdn.test/task-cangyuan-completed.mp4"}
	}`)
	parsed, err := ParseOpenAIVideoResult(body)
	require.NoError(t, err)
	outcome := ClassifyVideoTaskResult(&OpenAIForwardResult{
		VideoStatus:            parsed.Status,
		VideoArtifactAvailable: parsed.VideoURL != "",
		VideoResponseJSON:      body,
	})
	require.Equal(t, VideoTaskStatusCompleted, outcome.Status)

	task := &VideoTaskBilling{ID: 9, EstimatedCost: 1.25, TaskStatus: VideoTaskStatusProcessing, BillingStatus: VideoTaskBillingReserved}
	repo := &fakeVideoTaskBillingRepo{task: task}
	usage := &fakeVideoTaskUsageRecorder{}
	svc := NewVideoTaskBillingService(repo, nil, usage)

	require.NoError(t, svc.ApplyOutcome(context.Background(), task, outcome))
	require.Equal(t, []string{"poll:completed", "settling", "capture"}, repo.events)
	require.Equal(t, []string{"usage"}, usage.events)
	require.InDelta(t, 1.25, repo.capturedCost, 0.000001)
}

func TestVideoTaskSubmissionUncertainIncludesTransportAndServerFailures(t *testing.T) {
	require.True(t, IsVideoTaskSubmissionUncertain(&UpstreamFailoverError{StatusCode: 429}))
	require.True(t, IsVideoTaskSubmissionUncertain(&UpstreamFailoverError{StatusCode: 502}))
	require.False(t, IsVideoTaskSubmissionUncertain(&UpstreamFailoverError{StatusCode: 400}))
	require.True(t, IsVideoTaskSubmissionUncertain(NewVideoTaskSubmissionUncertainError(errors.New("response parse failed"))))
}

func TestEstimateVideoCostUsesExistingPerSecondPricingAndMultiplier(t *testing.T) {
	price := 0.08
	svc := newOpenAIRecordUsageServiceForTest(nil, nil, nil, nil)
	apiKey := &APIKey{Group: &Group{VideoPrice720P: &price}}

	cost, err := svc.EstimateVideoCost(context.Background(), apiKey, "video-model", "720p", 10)

	require.NoError(t, err)
	expected := svc.billingService.CalculateVideoCost("video-model", "720p", 1, 10, &VideoPriceConfig{Price720P: &price}, 1.1)
	require.InDelta(t, expected.ActualCost, cost.ActualCost, 0.00000001)
}

func TestVideoTaskUsageServiceRebuildsPreheldUsageFromDurableIDs(t *testing.T) {
	usageContext, err := json.Marshal(VideoTaskUsageContext{
		InboundEndpoint: "/v1/videos", UpstreamEndpoint: "/v1/videos", UserAgent: "test-client",
		IPAddress: "127.0.0.1", SessionID: "session-1", RequestPayloadHash: "payload-hash", QuotaPlatform: PlatformOpenAI,
	})
	require.NoError(t, err)
	recorder := &fakeDeferredOpenAIUsageRecorder{}
	apiKeys := &fakeVideoTaskAPIKeyRepo{key: &APIKey{ID: 11, UserID: 7}}
	users := &fakeVideoTaskUserRepo{user: &User{ID: 7}}
	accounts := &fakeVideoTaskAccountRepo{account: &Account{ID: 17}}
	svc := NewVideoTaskUsageService(recorder, apiKeys, users, accounts, nil)

	err = svc.RecordDeferredVideoUsage(context.Background(), &VideoTaskBilling{
		ID: 9, UserID: 7, APIKeyID: 11, AccountID: 17, UpstreamTaskID: "upstream-1", EstimatedCost: 1.25,
		Model: "video-model", UpstreamModel: "upstream-video-model", Resolution: "720p", DurationSeconds: 10,
		ReferenceImageCount: 1, UsageContextJSON: usageContext,
	})

	require.NoError(t, err)
	require.NotNil(t, recorder.input)
	require.Equal(t, "video_task:9:capture", recorder.input.Result.RequestID)
	require.Equal(t, 1, recorder.input.Result.VideoCount)
	require.True(t, recorder.input.BalanceAlreadyHeld)
	require.NotNil(t, recorder.input.PrecalculatedCost)
	require.InDelta(t, 1.25, recorder.input.PrecalculatedCost.ActualCost, 0.000001)
	require.Equal(t, "test-client", recorder.input.UserAgent)
	require.Equal(t, "payload-hash", recorder.input.RequestPayloadHash)
}

func TestVideoTaskUsageServiceUsesSnapshotGroupAfterAPIKeyMoves(t *testing.T) {
	oldGroupID := int64(13)
	newGroupID := int64(14)
	recorder := &fakeDeferredOpenAIUsageRecorder{}
	apiKeys := &fakeVideoTaskAPIKeyRepo{key: &APIKey{ID: 11, UserID: 7, GroupID: &newGroupID, Group: &Group{ID: newGroupID}}}
	svc := NewVideoTaskUsageService(recorder, apiKeys, &fakeVideoTaskUserRepo{user: &User{ID: 7}}, &fakeVideoTaskAccountRepo{account: &Account{ID: 17}}, nil)

	err := svc.RecordDeferredVideoUsage(context.Background(), &VideoTaskBilling{
		ID: 9, UserID: 7, APIKeyID: 11, GroupID: &oldGroupID, AccountID: 17,
		Model: "video-model", Resolution: "720p", DurationSeconds: 10, EstimatedCost: 1.25,
	})

	require.NoError(t, err)
	require.Equal(t, oldGroupID, *recorder.input.APIKey.GroupID)
	require.Nil(t, recorder.input.APIKey.Group)
	require.InDelta(t, 1.25, recorder.input.PrecalculatedCost.ActualCost, 0.000001)
}

func TestVideoTaskUsageServiceUsesSoftDeletedAPIKeySnapshot(t *testing.T) {
	groupID := int64(13)
	recorder := &fakeDeferredOpenAIUsageRecorder{}
	apiKeys := &fakeVideoTaskAPIKeyRepo{key: &APIKey{ID: 11, UserID: 7, GroupID: &groupID, Quota: 10, RateLimit5h: 5}, activeErr: ErrAPIKeyNotFound}
	svc := NewVideoTaskUsageService(recorder, apiKeys, &fakeVideoTaskUserRepo{user: &User{ID: 7}}, &fakeVideoTaskAccountRepo{account: &Account{ID: 17}}, nil)

	err := svc.RecordDeferredVideoUsage(context.Background(), &VideoTaskBilling{
		ID: 9, UserID: 7, APIKeyID: 11, GroupID: &groupID, AccountID: 17,
		Model: "video-model", Resolution: "720p", DurationSeconds: 10, EstimatedCost: 1.25,
	})

	require.NoError(t, err)
	require.Equal(t, 1, apiKeys.deletedLookupCalls)
	require.Equal(t, int64(11), recorder.input.APIKey.ID)
	require.Zero(t, recorder.input.APIKey.Quota)
	require.False(t, recorder.input.APIKey.HasRateLimits())
}

type fakeVideoTaskBalanceCache struct {
	invalidatedUserIDs []int64
}

func (c *fakeVideoTaskBalanceCache) InvalidateUserBalance(_ context.Context, userID int64) error {
	c.invalidatedUserIDs = append(c.invalidatedUserIDs, userID)
	return nil
}

type fakeVideoTaskBillingRepo struct {
	task         *VideoTaskBilling
	events       []string
	capturedCost float64
}

func (r *fakeVideoTaskBillingRepo) ReserveAndCreate(_ context.Context, task *VideoTaskBilling) error {
	r.task = task
	return nil
}

func (r *fakeVideoTaskBillingRepo) GetByID(_ context.Context, _ int64) (*VideoTaskBilling, error) {
	return r.task, nil
}

func (r *fakeVideoTaskBillingRepo) GetByTask(_ context.Context, _, _ string) (*VideoTaskBilling, error) {
	return r.task, nil
}

func (r *fakeVideoTaskBillingRepo) AttachUpstreamTask(_ context.Context, _ int64, taskID, status string, response json.RawMessage) (*VideoTaskBilling, error) {
	r.events = append(r.events, "attach:"+status)
	r.task.UpstreamTaskID = taskID
	r.task.TaskStatus = status
	r.task.ResponseJSON = response
	return r.task, nil
}

func (r *fakeVideoTaskBillingRepo) MarkSubmissionUnknown(_ context.Context, _ int64, reason string) error {
	r.events = append(r.events, "submission_unknown")
	r.task.TaskStatus = VideoTaskStatusSubmissionUnknown
	r.task.BillingStatus = VideoTaskBillingManualReview
	r.task.LastPollError = reason
	return nil
}

func (r *fakeVideoTaskBillingRepo) ClaimDue(_ context.Context, _ int, _ time.Duration) ([]*VideoTaskBilling, error) {
	return nil, nil
}

func (r *fakeVideoTaskBillingRepo) UpdatePoll(_ context.Context, _ int64, outcome VideoTaskOutcome, _ time.Time) (*VideoTaskBilling, error) {
	r.events = append(r.events, "poll:"+outcome.Status)
	r.task.TaskStatus = outcome.Status
	r.task.ResponseJSON = outcome.ResponseJSON
	r.task.LastPollError = outcome.ErrorMessage
	return r.task, nil
}

func (r *fakeVideoTaskBillingRepo) BeginSettlement(_ context.Context, _ int64) (*VideoTaskBilling, error) {
	r.events = append(r.events, "settling")
	r.task.BillingStatus = VideoTaskBillingSettling
	return r.task, nil
}

func (r *fakeVideoTaskBillingRepo) Capture(_ context.Context, _ int64, cost float64) error {
	r.events = append(r.events, "capture")
	r.capturedCost = cost
	return nil
}

func (r *fakeVideoTaskBillingRepo) Release(_ context.Context, _ int64, _ string) error {
	r.events = append(r.events, "release")
	return nil
}

type fakeVideoTaskUsageRecorder struct {
	events []string
}

type fakeVideoTaskCostEstimator struct {
	cost  *CostBreakdown
	err   error
	calls int
}

func (e *fakeVideoTaskCostEstimator) EstimateVideoCost(_ context.Context, _ *APIKey, _, _ string, _ int) (*CostBreakdown, error) {
	e.calls++
	return e.cost, e.err
}

func ptrVideoInt64(value int64) *int64 {
	return &value
}

type fakeDeferredOpenAIUsageRecorder struct {
	input *OpenAIRecordUsageInput
}

func (r *fakeDeferredOpenAIUsageRecorder) RecordUsage(_ context.Context, input *OpenAIRecordUsageInput) error {
	r.input = input
	return nil
}

type fakeVideoTaskAPIKeyRepo struct {
	APIKeyRepository
	key                *APIKey
	activeErr          error
	deletedLookupCalls int
}

func (r *fakeVideoTaskAPIKeyRepo) GetByID(_ context.Context, _ int64) (*APIKey, error) {
	return r.key, r.activeErr
}

func (r *fakeVideoTaskAPIKeyRepo) GetByIDIncludingDeleted(_ context.Context, _ int64) (*APIKey, error) {
	r.deletedLookupCalls++
	return r.key, nil
}

type fakeVideoTaskUserRepo struct {
	UserRepository
	user *User
}

func (r *fakeVideoTaskUserRepo) GetByID(_ context.Context, _ int64) (*User, error) {
	return r.user, nil
}

type fakeVideoTaskAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *fakeVideoTaskAccountRepo) GetByID(_ context.Context, _ int64) (*Account, error) {
	return r.account, nil
}

func (r *fakeVideoTaskUsageRecorder) RecordDeferredVideoUsage(_ context.Context, _ *VideoTaskBilling) error {
	r.events = append(r.events, "usage")
	return nil
}
