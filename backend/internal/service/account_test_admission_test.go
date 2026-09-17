//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type protectedTestRepo struct {
	openAIAccountTestRepo
	account       *Account
	parent        *Account
	extraWrites   int
	recoveryCalls int
}

func (r *protectedTestRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.parent != nil && id == r.parent.ID {
		return r.parent, nil
	}
	return r.account, nil
}
func (r *protectedTestRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.extraWrites++
	r.account.UpdatedAt = r.account.UpdatedAt.Add(time.Second)
	return r.openAIAccountTestRepo.UpdateExtra(ctx, id, updates)
}
func (r *protectedTestRepo) RecoverAccountTestIfUnchanged(_ context.Context, observation *AccountTestObservation) (*SuccessfulTestRecoveryResult, error) {
	r.recoveryCalls++
	result := &SuccessfulTestRecoveryResult{}
	if !observation.UpdatedAt.Equal(r.account.UpdatedAt) {
		return result, nil
	}
	if observation.ClearError {
		r.account.Status = StatusActive
		result.ClearedError = true
	}
	if len(observation.PendingExtra) > 0 {
		_ = r.UpdateExtra(context.Background(), observation.AccountID, observation.PendingExtra)
	}
	return result, nil
}

func (r *protectedTestRepo) SaveAccountTestExtraIfUnchanged(ctx context.Context, observation *AccountTestObservation) error {
	if !observation.UpdatedAt.Equal(r.account.UpdatedAt) {
		return nil
	}
	return r.UpdateExtra(ctx, observation.AccountID, observation.PendingExtra)
}

type protectedTestUpstream struct {
	queuedHTTPUpstream
	onRequest func()
}

func (u *protectedTestUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if u.onRequest != nil {
		u.onRequest()
	}
	return u.queuedHTTPUpstream.DoWithTLS(req, proxy, id, concurrency, profile)
}

type protectedTestSlots struct {
	stubConcurrencyCacheForTest
	held     bool
	limit    int
	acquires int
}

func (c *protectedTestSlots) AcquireAccountSlot(ctx context.Context, id int64, limit int, request string) (bool, error) {
	c.acquires++
	c.limit = limit
	if c.held {
		return false, nil
	}
	if c.acquireErr != nil {
		return false, c.acquireErr
	}
	c.held = true
	return true, nil
}
func (c *protectedTestSlots) ReleaseAccountSlot(ctx context.Context, id int64, request string) error {
	c.held = false
	return c.stubConcurrencyCacheForTest.ReleaseAccountSlot(ctx, id, request)
}

func newProtectedAccountTest() (*AccountTestService, *protectedTestRepo, *protectedTestUpstream, *protectedTestSlots) {
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError,
		Concurrency: 3, UpdatedAt: time.Now().Add(-time.Minute), Credentials: map[string]any{"access_token": "test-token"}}
	repo := &protectedTestRepo{account: account}
	response := newJSONResponse(http.StatusOK, "data: {\"type\":\"response.completed\"}\n\n")
	response.Header.Set("x-codex-primary-used-percent", "20")
	response.Header.Set("x-codex-primary-reset-after-seconds", "120")
	response.Header.Set("x-codex-primary-window-minutes", "300")
	upstream := &protectedTestUpstream{queuedHTTPUpstream: queuedHTTPUpstream{responses: []*http.Response{response}}}
	slots := &protectedTestSlots{}
	svc := &AccountTestService{accountRepo: repo, httpUpstream: upstream, concurrencyService: NewConcurrencyService(slots)}
	return svc, repo, upstream, slots
}

func TestAccountTestAdmission_ActiveCooldownNeverCallsUpstream(t *testing.T) {
	for _, kind := range []string{"账号限流", "过载", "临时暂停", "映射模型", "模型别名", "图片模型共享额度"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, upstream, slots := newProtectedAccountTest()
			until := time.Now().Add(time.Hour)
			model := "gpt-5.4"
			switch kind {
			case "账号限流":
				repo.account.RateLimitResetAt = &until
			case "过载":
				repo.account.OverloadUntil = &until
			case "临时暂停":
				repo.account.TempUnschedulableUntil = &until
			case "映射模型":
				repo.account.Credentials["model_mapping"] = map[string]any{"gpt-5.4": "limited-model"}
				setAccountModelRateLimitSnapshot(repo.account, "limited-model", until, "429", time.Now())
			case "模型别名":
				model = "gpt-5.6"
				setAccountModelRateLimitSnapshot(repo.account, "gpt-5.6-sol", until, "429", time.Now())
			case "图片模型共享额度":
				model = "gpt-image-2"
				setAccountModelRateLimitSnapshot(repo.account, openAIImageGenerationRateLimitKey, until, "429", time.Now())
			}
			c, recorder := newTestContext()
			err := svc.TestAccountConnection(c, 42, model, "", "")
			require.Error(t, err)
			require.Contains(t, recorder.Body.String(), "后重新测试")
			require.Empty(t, upstream.requests)
			require.Zero(t, slots.acquires)
		})
	}
}

func TestAccountTestAdmission_SharedCapacityAndFailureRelease(t *testing.T) {
	for _, kind := range []string{"业务已占满", "缓存故障", "上游失败"} {
		t.Run(kind, func(t *testing.T) {
			svc, _, upstream, slots := newProtectedAccountTest()
			switch kind {
			case "业务已占满":
				slots.held = true
			case "缓存故障":
				slots.acquireErr = errors.New("redis unavailable")
			case "上游失败":
				upstream.responses = nil
			}
			c, _ := newTestContext()
			err := svc.TestAccountConnection(c, 42, "gpt-5.4", "", "")
			require.Error(t, err)
			require.Equal(t, 3, slots.limit)
			if kind == "上游失败" {
				require.False(t, slots.held)
				require.Equal(t, []int64{42}, slots.releasedAccountIDs)
			} else {
				require.Empty(t, upstream.requests)
				require.Empty(t, slots.releasedAccountIDs)
			}
		})
	}
}

func TestAccountTestRecovery_UsageWritesAfterRecoveryAndNewStateIsPreserved(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "成功恢复后保存用量", true: "保留测试中的新错误"}[concurrent], func(t *testing.T) {
			svc, repo, upstream, slots := newProtectedAccountTest()
			rateLimit := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
			upstream.onRequest = func() {
				require.True(t, slots.held)
				if concurrent {
					repo.account.UpdatedAt = repo.account.UpdatedAt.Add(time.Second)
				}
			}
			c, _ := newTestContext()
			called := false
			err := svc.TestAccountConnection(c, 42, "gpt-5.4", "", "", AccountTestOptions{OnSuccess: func(ctx context.Context, observation *AccountTestObservation) {
				called = true
				require.Zero(t, repo.extraWrites)
				result, err := rateLimit.RecoverAccountAfterSuccessfulTest(ctx, 42, observation)
				require.NoError(t, err)
				require.Equal(t, !concurrent, result.ClearedError)
			}})
			require.NoError(t, err)
			require.True(t, called)
			require.Equal(t, map[bool]int{false: 1, true: 0}[concurrent], repo.extraWrites)
			if !concurrent {
				require.NotEmpty(t, repo.updatedExtra)
			}
			require.False(t, slots.held)
			require.Equal(t, map[bool]string{false: StatusActive, true: StatusError}[concurrent], repo.account.Status)
		})
	}
}

func TestAccountTestObservation_OnlyExpiredTestedModelCanRecover(t *testing.T) {
	now := time.Now()
	account := &Account{ID: 1, Platform: PlatformOpenAI, UpdatedAt: now.Add(-time.Hour)}
	setAccountModelRateLimitSnapshot(account, "gpt-5.4", now.Add(-time.Minute), "429", now.Add(-time.Hour))
	setAccountModelRateLimitSnapshot(account, "other-model", now.Add(time.Hour), "429", now)
	require.NoError(t, accountTestCooldown(context.Background(), account, "gpt-5.4", now))
	observation := newAccountTestObservation(context.Background(), account, "gpt-5.4", now)
	require.Equal(t, []string{"gpt-5.4"}, observation.ModelRateLimitKeys)
}

func TestAccountTestRecovery_FailedResponseKeepsUsageWithoutRecovering(t *testing.T) {
	svc, repo, upstream, slots := newProtectedAccountTest()
	upstream.responses[0].StatusCode = http.StatusInternalServerError
	c, _ := newTestContext()
	err := svc.TestAccountConnection(c, 42, "gpt-5.4", "", "", AccountTestOptions{OnSuccess: func(context.Context, *AccountTestObservation) {
		t.Fatal("失败请求不能恢复账号")
	}})
	require.Error(t, err)
	require.Equal(t, 1, repo.extraWrites)
	require.NotEmpty(t, repo.updatedExtra)
	require.Equal(t, StatusError, repo.account.Status)
	require.False(t, slots.held)
}

func TestAccountTestRecovery_ShadowUsesOwnSlotAndRecoveryIdentity(t *testing.T) {
	svc, repo, _, slots := newProtectedAccountTest()
	parentID := int64(99)
	repo.parent = &Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError,
		Credentials: map[string]any{"access_token": "parent-test-token"}, UpdatedAt: time.Now().Add(-time.Minute)}
	repo.account.ParentAccountID = &parentID
	repo.account.QuotaDimension = QuotaDimensionSpark
	repo.account.Credentials = map[string]any{}
	rateLimit := NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	c, _ := newTestContext()
	err := svc.TestAccountConnection(c, 42, "gpt-5.3-codex-spark", "", "", AccountTestOptions{OnSuccess: func(ctx context.Context, observation *AccountTestObservation) {
		require.Equal(t, int64(42), observation.AccountID)
		wrong, err := rateLimit.RecoverAccountAfterSuccessfulTest(ctx, parentID, observation)
		require.NoError(t, err)
		require.False(t, wrong.ClearedError)
		result, err := rateLimit.RecoverAccountAfterSuccessfulTest(ctx, 42, observation)
		require.NoError(t, err)
		require.True(t, result.ClearedError)
	}})
	require.NoError(t, err)
	require.Equal(t, []int64{42}, slots.releasedAccountIDs)
	require.Equal(t, StatusError, repo.parent.Status)
	require.Equal(t, StatusActive, repo.account.Status)
}

func TestAccountTestRecovery_ObservationCannotOverwriteNewerUsage(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "未启用恢复", true: "测试失败"}[failed], func(t *testing.T) {
			svc, repo, upstream, _ := newProtectedAccountTest()
			if failed {
				upstream.responses[0].StatusCode = http.StatusInternalServerError
			}
			upstream.onRequest = func() { _ = repo.UpdateExtra(context.Background(), 42, map[string]any{"codex_5h_used_percent": 100.0}) }
			c, _ := newTestContext()
			err := svc.TestAccountConnection(c, 42, "gpt-5.4", "", "")
			if failed {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 100.0, repo.updatedExtra["codex_5h_used_percent"])
			require.Equal(t, 1, repo.extraWrites)
		})
	}
}

type earlyGrokRecoveryRepo struct {
	protectedTestRepo
	clears int
}

func (r *earlyGrokRecoveryRepo) ClearRateLimitIfObserved(context.Context, int64, time.Time, time.Time) (bool, error) {
	r.clears++
	return true, nil
}

func TestAccountTestRecovery_GrokHeadersDoNotRecoverBeforeCompletion(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	account := &Account{ID: 42, Platform: PlatformGrok, Type: AccountTypeOAuth, RateLimitedAt: &past, RateLimitResetAt: &past}
	repo := &earlyGrokRecoveryRepo{protectedTestRepo: protectedTestRepo{account: account}}
	svc := &AccountTestService{accountRepo: repo}
	svc.observeGrokTestResponse(context.Background(), account, newJSONResponse(http.StatusOK, ""))
	require.Zero(t, repo.clears, "HTTP 响应头成功不能提前清除尚未完整测试的账号状态")
}

type protectedPlanRepo struct{ ScheduledTestPlanRepository }

func (*protectedPlanRepo) UpdateAfterRun(context.Context, int64, time.Time, time.Time) error {
	return nil
}

type protectedResultRepo struct{ ScheduledTestResultRepository }

func (*protectedResultRepo) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	return result, nil
}
func (*protectedResultRepo) PruneOldResults(context.Context, int64, int) error { return nil }

func TestScheduledTestRecovery_OnlyEnabledSuccessfulPlanRecovers(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "未启用自动恢复", true: "启用自动恢复"}[enabled], func(t *testing.T) {
			svc, repo, _, _ := newProtectedAccountTest()
			runner := &ScheduledTestRunnerService{accountTestSvc: svc, planRepo: &protectedPlanRepo{},
				rateLimitSvc: NewRateLimitService(repo, nil, &config.Config{}, nil, nil),
				scheduledSvc: &ScheduledTestService{resultRepo: &protectedResultRepo{}}}
			runner.runOnePlan(context.Background(), &ScheduledTestPlan{ID: 1, AccountID: 42, ModelID: "gpt-5.4", AutoRecover: enabled, CronExpression: "* * * * *"})
			require.Equal(t, map[bool]int{false: 0, true: 1}[enabled], repo.recoveryCalls)
			require.Equal(t, map[bool]string{false: StatusError, true: StatusActive}[enabled], repo.account.Status)
			require.Equal(t, 1, repo.extraWrites)
		})
	}
}
