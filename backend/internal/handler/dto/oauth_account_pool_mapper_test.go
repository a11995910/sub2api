package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOAuthAccountPoolFromServiceMasksIdentifiersForRegularUsers(t *testing.T) {
	expiresAt := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	pool := &service.OAuthAccountPool{
		Summary: service.OAuthAccountPoolSummary{AccountCount: 1},
		Groups: []service.OAuthAccountPoolGroup{{
			Summary: service.OAuthAccountPoolSummary{AccountCount: 1},
			Accounts: []service.OAuthAccountPoolAccount{{
				Identifier: "1072688154@qq.com",
				ExpiresAt:  &expiresAt,
				FiveHour:   &service.UsageProgress{Utilization: 24},
				SevenDay:   &service.UsageProgress{Utilization: 51},
			}},
		}},
	}

	regularUser := OAuthAccountPoolFromService(pool, false)
	admin := OAuthAccountPoolFromService(pool, true)

	require.Equal(t, "1072******@qq.com", regularUser.Groups[0].Accounts[0].Identifier)
	require.Equal(t, "1072688154@qq.com", admin.Groups[0].Accounts[0].Identifier)
	require.Equal(t, expiresAt, *regularUser.Groups[0].Accounts[0].ExpiresAt)
	require.NotNil(t, regularUser.Groups[0].Accounts[0].Usage)
	require.InDelta(t, 24.0, regularUser.Groups[0].Accounts[0].Usage.FiveHour.Utilization, 1e-9)
	require.NotNil(t, admin.Groups[0].Accounts[0].Usage)

	regularPayload, err := json.Marshal(regularUser)
	require.NoError(t, err)
	require.NotContains(t, string(regularPayload), `"requests"`)
	require.NotContains(t, string(regularPayload), `"tokens"`)
	require.NotContains(t, string(regularPayload), `"stats"`)
	require.Contains(t, string(regularPayload), `"usage"`)
	require.Contains(t, string(regularPayload), `"expires_at"`)

	adminPayload, err := json.Marshal(admin)
	require.NoError(t, err)
	require.NotContains(t, string(adminPayload), `"requests"`)
	require.NotContains(t, string(adminPayload), `"tokens"`)
	require.NotContains(t, string(adminPayload), `"stats"`)
}

func TestMaskOAuthAccountIdentifierHandlesShortAndMalformedValues(t *testing.T) {
	tests := map[string]string{
		"":                 "",
		"a@example.com":    "*@example.com",
		"abcd@example.com": "abc*@example.com",
		"abcdef":           "abcd**",
	}

	for input, expected := range tests {
		require.Equal(t, expected, maskOAuthAccountIdentifier(input))
	}
}
