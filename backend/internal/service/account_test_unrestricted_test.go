//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type unrestrictedAccountTestRepo struct {
	rateLimitClearRepoStub
	updatedExtra map[string]any
}

func (r *unrestrictedAccountTestRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updatedExtra = updates
	return nil
}

func newUnrestrictedAccountTest() (*AccountTestService, *unrestrictedAccountTestRepo, *queuedHTTPUpstream) {
	until := time.Now().Add(time.Hour)
	account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError,
		Concurrency: 1, RateLimitResetAt: &until, OverloadUntil: &until, TempUnschedulableUntil: &until,
		Credentials: map[string]any{"access_token": "test-token"}}
	setAccountModelRateLimitSnapshot(account, "gpt-5.4", until, "429", time.Now())
	repo := &unrestrictedAccountTestRepo{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: account}}
	response := healthyTurnStateResponse(http.StatusOK, "", healthyTurnStateSSE())
	response.Header.Set("x-codex-primary-used-percent", "20")
	response.Header.Set("x-codex-primary-reset-after-seconds", "120")
	response.Header.Set("x-codex-primary-window-minutes", "300")
	upstream := &queuedHTTPUpstream{responses: []*http.Response{response}}
	return &AccountTestService{accountRepo: repo, httpUpstream: upstream}, repo, upstream
}

func TestAccountTestService_CooldownDoesNotBlockExplicitTests(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "手动测试", true: "后台测试"}[background], func(t *testing.T) {
			svc, repo, upstream := newUnrestrictedAccountTest()
			if background {
				result, err := svc.RunTestBackground(context.Background(), 42, "gpt-5.4")
				require.NoError(t, err)
				require.Equal(t, "success", result.Status)
			} else {
				c, recorder := newTestContext()
				require.NoError(t, svc.TestAccountConnection(c, 42, "gpt-5.4", "", ""))
				require.Contains(t, recorder.Body.String(), `"success":true`)
			}
			require.Len(t, upstream.requests, 1, "冷却中的账号仍可发送测试")
			require.Equal(t, 20.0, repo.updatedExtra["codex_5h_used_percent"], "测试用量直接保存")
		})
	}
}

type unrestrictedPlanRepo struct{ ScheduledTestPlanRepository }

func (*unrestrictedPlanRepo) UpdateAfterRun(context.Context, int64, time.Time, time.Time) error {
	return nil
}

type unrestrictedResultRepo struct{ ScheduledTestResultRepository }

func (*unrestrictedResultRepo) Create(_ context.Context, result *ScheduledTestResult) (*ScheduledTestResult, error) {
	return result, nil
}
func (*unrestrictedResultRepo) PruneOldResults(context.Context, int64, int) error { return nil }

func TestScheduledTestRecovery_OnlyEnabledSuccessfulPlanRecovers(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, success := range []bool{false, true} {
			svc, repo, upstream := newUnrestrictedAccountTest()
			if !success {
				upstream.responses[0] = newJSONResponse(http.StatusServiceUnavailable, "测试上游暂不可用")
			}
			runner := &ScheduledTestRunnerService{accountTestSvc: svc, planRepo: &unrestrictedPlanRepo{},
				rateLimitSvc: NewRateLimitService(repo, nil, &config.Config{}, nil, nil),
				scheduledSvc: &ScheduledTestService{resultRepo: &unrestrictedResultRepo{}}}
			runner.runOnePlan(context.Background(), &ScheduledTestPlan{ID: 1, AccountID: 42, ModelID: "gpt-5.4", AutoRecover: enabled, CronExpression: "* * * * *"})
			require.Len(t, upstream.requests, 1)
			want := 0
			if enabled && success {
				want = 1
			}
			require.Equal(t, want, repo.clearErrorCalls)
			require.Equal(t, want, repo.clearRateLimitCalls)
			require.Equal(t, want, repo.clearModelRateLimitCalls)
		}
	}
}
