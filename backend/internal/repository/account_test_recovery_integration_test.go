//go:build integration

package repository

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *AccountRepoSuite) TestSuccessfulTestRecoveryPreservesOtherModelAndQuotaState() {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	account := &service.Account{Name: "测试有限恢复", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusError, ErrorMessage: "旧认证错误", Concurrency: 1,
		Credentials: map[string]any{}, Extra: map[string]any{
			"model_rate_limits": map[string]any{
				"gpt-5.4":     map[string]any{"rate_limit_reset_at": past.Format(time.RFC3339)},
				"other-model": map[string]any{"rate_limit_reset_at": future.Format(time.RFC3339)},
			}, "antigravity_quota_scopes": map[string]any{"gemini": true},
		}}
	s.Require().NoError(s.repo.Create(s.ctx, account))
	s.Require().NoError(s.repo.SetRateLimited(s.ctx, account.ID, past))
	s.Require().NoError(s.repo.SetOverloaded(s.ctx, account.ID, past))
	s.Require().NoError(s.repo.SetTempUnschedulable(s.ctx, account.ID, past, "旧临时错误"))
	observed, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	result, err := s.repo.RecoverAccountTestIfUnchanged(s.ctx, &service.AccountTestObservation{
		AccountID: account.ID, UpdatedAt: observed.UpdatedAt, StartedAt: time.Now(), Succeeded: true,
		ClearError: true, ClearRateLimit: true, ClearOverload: true, ClearTempUnsched: true,
		ModelRateLimitKeys: []string{"gpt-5.4"},
		PendingExtra:       map[string]any{"codex_5h_used_percent": 20.0},
	})
	s.Require().NoError(err)
	s.True(result.ClearedError)
	s.True(result.ClearedRateLimit)
	current, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Equal(service.StatusActive, current.Status)
	s.Nil(current.RateLimitResetAt)
	s.Nil(current.OverloadUntil)
	s.Nil(current.TempUnschedulableUntil)
	s.Equal(observed.Schedulable, current.Schedulable)
	limits := current.Extra["model_rate_limits"].(map[string]any)
	s.NotContains(limits, "gpt-5.4")
	s.Contains(limits, "other-model")
	s.Contains(current.Extra, "antigravity_quota_scopes")
	s.Equal(20.0, current.Extra["codex_5h_used_percent"])
}

func (s *AccountRepoSuite) TestSuccessfulTestRecoveryDoesNotEraseSameErrorRewrittenDuringTest() {
	account := &service.Account{Name: "测试恢复竞争", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusError, ErrorMessage: "相同错误", Credentials: map[string]any{}, Extra: map[string]any{}}
	s.Require().NoError(s.repo.Create(s.ctx, account))
	observed, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Require().NoError(s.repo.SetError(s.ctx, account.ID, "相同错误"))
	s.Require().NoError(s.repo.UpdateExtra(s.ctx, account.ID, map[string]any{"codex_5h_used_percent": 100.0}))
	result, err := s.repo.RecoverAccountTestIfUnchanged(s.ctx, &service.AccountTestObservation{
		AccountID: account.ID, UpdatedAt: observed.UpdatedAt, Succeeded: true, ClearError: true,
		PendingExtra: map[string]any{"codex_5h_used_percent": 20.0},
	})
	s.Require().NoError(err)
	s.False(result.ClearedError)
	current, err := s.repo.GetByID(s.ctx, account.ID)
	s.Require().NoError(err)
	s.Equal(service.StatusError, current.Status)
	s.Equal("相同错误", current.ErrorMessage)
	s.Equal(100.0, current.Extra["codex_5h_used_percent"])
}

func (s *AccountRepoSuite) TestSuccessfulTestObservationKeepsNewerUsageWithoutAutoRecovery() {
	for _, newer := range []bool{false, true} {
		account := &service.Account{Name: "测试观测竞争", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusError, ErrorMessage: "仍需恢复", Credentials: map[string]any{}, Extra: map[string]any{}}
		s.Require().NoError(s.repo.Create(s.ctx, account))
		observed, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		if newer {
			s.Require().NoError(s.repo.UpdateExtra(s.ctx, account.ID, map[string]any{"codex_5h_used_percent": 100.0}))
		}
		err = s.repo.SaveAccountTestExtraIfUnchanged(s.ctx, &service.AccountTestObservation{
			AccountID: account.ID, UpdatedAt: observed.UpdatedAt, ClearError: true,
			PendingExtra: map[string]any{"codex_5h_used_percent": 20.0},
		})
		s.Require().NoError(err)
		current, err := s.repo.GetByID(s.ctx, account.ID)
		s.Require().NoError(err)
		s.Equal(service.StatusError, current.Status)
		s.Equal(map[bool]float64{false: 20, true: 100}[newer], current.Extra["codex_5h_used_percent"])
	}
}
