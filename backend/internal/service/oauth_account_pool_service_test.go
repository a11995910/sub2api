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

func (s *oauthPoolRepoStub) ListActiveOAuthByGroupIDs(_ context.Context, groupIDs []int64) ([]OAuthAccountPoolBinding, error) {
	s.requestedGroupIDs = append([]int64(nil), groupIDs...)
	return append([]OAuthAccountPoolBinding(nil), s.bindings...), nil
}

func TestOAuthAccountPoolServiceFiltersGroupsAndBuildsCachedUsage(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	repo := &oauthPoolRepoStub{bindings: []OAuthAccountPoolBinding{
		{
			GroupID: 2,
			Account: Account{
				Name:     "OAuth Pro",
				Platform: PlatformOpenAI,
				Type:     AccountTypeOAuth,
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
	svc := &OAuthAccountPoolService{
		apiKeyService: oauthPoolGroupAccessStub{groups: []Group{
			{ID: 1, Name: "关闭分组", OAuthPoolVisible: false},
			{ID: 2, Name: "公开分组", OAuthPoolVisible: true},
			{ID: 3, Name: "空分组", OAuthPoolVisible: true},
		}},
		accountRepo:         repo,
		accountUsageService: &AccountUsageService{},
	}

	pool, err := svc.GetForUser(context.Background(), 9)

	require.NoError(t, err)
	require.Equal(t, []int64{2, 3}, repo.requestedGroupIDs)
	require.Len(t, pool.Groups, 1)
	require.Equal(t, "公开分组", pool.Groups[0].Name)
	require.Len(t, pool.Groups[0].Accounts, 1)
	require.Equal(t, "OAuth Pro", pool.Groups[0].Accounts[0].Name)
	require.InDelta(t, 24.5, pool.Groups[0].Accounts[0].FiveHour.Utilization, 1e-9)
	require.InDelta(t, 51.0, pool.Groups[0].Accounts[0].SevenDay.Utilization, 1e-9)
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
