package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type oauthPoolGroupAccessStub struct {
	groups []Group
}

func (s oauthPoolGroupAccessStub) GetAvailableGroups(context.Context, int64) ([]Group, error) {
	return append([]Group(nil), s.groups...), nil
}

type oauthPoolRepoStub struct {
	requestedGroupIDs []int64
	bindings          []OAuthAccountPoolBinding
}

type oauthPoolConcurrencyStub struct {
	requestedAccountIDs []int64
	counts              map[int64]int
}

func (s *oauthPoolConcurrencyStub) GetAccountConcurrencyBatch(_ context.Context, accountIDs []int64) (map[int64]int, error) {
	s.requestedAccountIDs = append([]int64(nil), accountIDs...)
	return s.counts, nil
}

func (s *oauthPoolRepoStub) ListActiveOAuthByGroupIDs(_ context.Context, groupIDs []int64) ([]OAuthAccountPoolBinding, error) {
	s.requestedGroupIDs = append([]int64(nil), groupIDs...)
	return append([]OAuthAccountPoolBinding(nil), s.bindings...), nil
}

func TestOAuthAccountPoolServiceFiltersGroupsAndBuildsCachedUsage(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	accountExpiresAt := resetAt.Add(60 * 24 * time.Hour)
	subscriptionExpiresAt := resetAt.Add(30 * 24 * time.Hour)
	repo := &oauthPoolRepoStub{bindings: []OAuthAccountPoolBinding{
		{
			GroupID: 2,
			Account: Account{
				ID:          101,
				Name:        "OAuth Pro",
				Platform:    PlatformOpenAI,
				Type:        AccountTypeOAuth,
				Concurrency: 15,
				ExpiresAt:   &accountExpiresAt,
				Credentials: map[string]any{
					"email":                   "owner@example.com",
					"plan_type":               "pro",
					"subscription_expires_at": subscriptionExpiresAt.Format(time.RFC3339),
				},
				Extra: map[string]any{
					"codex_5h_used_percent":  24.5,
					"codex_5h_reset_at":      resetAt.Format(time.RFC3339),
					"codex_7d_used_percent":  51.0,
					"codex_7d_reset_at":      resetAt.Add(5 * 24 * time.Hour).Format(time.RFC3339),
					"codex_usage_updated_at": time.Now().UTC().Format(time.RFC3339),
				},
			},
		},
	}}
	concurrencyReader := &oauthPoolConcurrencyStub{counts: map[int64]int{101: 6}}
	svc := &OAuthAccountPoolService{
		apiKeyService: oauthPoolGroupAccessStub{groups: []Group{
			{ID: 1, Name: "关闭分组", OAuthPoolVisible: false},
			{ID: 2, Name: "公开分组", OAuthPoolVisible: true},
			{ID: 3, Name: "空分组", OAuthPoolVisible: true},
		}},
		accountRepo:         repo,
		accountUsageService: &AccountUsageService{},
		concurrencyReader:   concurrencyReader,
	}

	pool, err := svc.GetForUser(context.Background(), 9)

	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, repo.requestedGroupIDs)
	require.Len(t, pool.Groups, 1)
	require.Equal(t, "公开分组", pool.Groups[0].Name)
	require.Len(t, pool.Groups[0].Accounts, 1)
	require.Equal(t, "owner@example.com", pool.Groups[0].Accounts[0].Identifier)
	require.Equal(t, "Pro 20x", pool.Groups[0].Accounts[0].PlanType)
	require.Equal(t, 6, pool.Groups[0].Accounts[0].CurrentConcurrency)
	require.Equal(t, 15, pool.Groups[0].Accounts[0].Concurrency)
	require.Equal(t, subscriptionExpiresAt, *pool.Groups[0].Accounts[0].ExpiresAt)
	require.InDelta(t, 24.5, pool.Groups[0].Accounts[0].FiveHour.Utilization, 1e-9)
	require.InDelta(t, 51.0, pool.Groups[0].Accounts[0].SevenDay.Utilization, 1e-9)
	require.Equal(t, OAuthAccountPoolSummary{AccountCount: 1}, pool.Groups[0].Summary)
	require.Equal(t, pool.Groups[0].Summary, pool.Summary)
	require.Equal(t, []int64{101}, concurrencyReader.requestedAccountIDs)
}

func TestOAuthAccountIdentityNeverFallsBackToCustomName(t *testing.T) {
	parentExpiresAt := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	account := &Account{
		Name: "Pro 正价",
		Extra: map[string]any{
			"email_address": " extra@example.com ",
		},
		Credentials: map[string]any{
			"email":                   "credential@example.com",
			"plan_type":               "k12",
			"subscription_expires_at": "2026-09-24T01:49:07Z",
		},
		ParentDisplayExpiresAt: &parentExpiresAt,
	}

	require.Equal(t, "extra@example.com", ResolveOAuthAccountDisplayIdentifier(account))
	require.Equal(t, "K12", OAuthAccountPlanLabel(ResolveOAuthAccountPlanType(account)))
	require.Empty(t, ResolveOAuthAccountDisplayIdentifier(&Account{Name: "Pro 正价"}))
	require.Equal(t, "Pro 20x", OAuthAccountPlanLabel("chatgpt_pro"))
	require.Equal(t, "Team", OAuthAccountPlanLabel("team"))
	require.Equal(t, "Plus", OAuthAccountPlanLabel("plus"))
	require.Equal(t, "Free", OAuthAccountPlanLabel("basic"))
	require.Equal(t, "future_enterprise", OAuthAccountPlanLabel("future_enterprise"))
	require.Equal(t, time.Date(2026, 9, 24, 1, 49, 7, 0, time.UTC), *ResolveOAuthAccountDisplayExpiresAt(account))
	require.Equal(t, parentExpiresAt, *ResolveOAuthAccountDisplayExpiresAt(&Account{ParentDisplayExpiresAt: &parentExpiresAt}))
}

func TestOAuthPoolStatsWindowStartMatchesCacheTTL(t *testing.T) {
	now := time.Now()
	require.True(t, oauthPoolStatsWindowStartMatches(now, now.Add(30*time.Second)))
	require.True(t, oauthPoolStatsWindowStartMatches(now, now.Add(-oauthPoolStatsCacheTTL)))
	require.False(t, oauthPoolStatsWindowStartMatches(now, now.Add(oauthPoolStatsCacheTTL+time.Second)))
}

func TestBuildCachedUsageSupportsAnthropicPassiveWindowsWithoutRepositories(t *testing.T) {
	resetAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	usage := (&AccountUsageService{}).BuildCachedUsage(&Account{
		Platform:            PlatformAnthropic,
		Type:                AccountTypeOAuth,
		SessionWindowEnd:    &resetAt,
		SessionWindowStatus: "allowed_warning",
		Extra: map[string]any{
			"session_window_utilization":   0.25,
			"passive_usage_7d_utilization": 0.4,
			"passive_usage_7d_reset":       float64(resetAt.Add(6 * 24 * time.Hour).Unix()),
		},
	})

	require.Equal(t, "passive", usage.Source)
	require.NotNil(t, usage.FiveHour)
	require.InDelta(t, 25.0, usage.FiveHour.Utilization, 1e-9)
	require.NotNil(t, usage.SevenDay)
	require.InDelta(t, 40.0, usage.SevenDay.Utilization, 1e-9)
}
