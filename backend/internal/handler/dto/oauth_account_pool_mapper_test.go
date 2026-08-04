package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOAuthAccountPoolFromServiceMasksIdentifiersForRegularUsers(t *testing.T) {
	pool := &service.OAuthAccountPool{
		Summary: service.OAuthAccountPoolSummary{AccountCount: 1, Requests: 120, Tokens: 12000},
		Groups: []service.OAuthAccountPoolGroup{{
			Summary: service.OAuthAccountPoolSummary{AccountCount: 1, Requests: 120, Tokens: 12000},
			Accounts: []service.OAuthAccountPoolAccount{{
				Identifier: "1072688154@qq.com",
				Stats: service.OAuthAccountPoolAccountStats{
					FiveHour: service.OAuthAccountPoolRequestTokenStats{Requests: 5, Tokens: 500},
					SevenDay: service.OAuthAccountPoolRequestTokenStats{Requests: 70, Tokens: 7000},
					Total:    service.OAuthAccountPoolRequestTokenStats{Requests: 120, Tokens: 12000},
				},
			}},
		}},
	}

	regularUser := OAuthAccountPoolFromService(pool, false)
	admin := OAuthAccountPoolFromService(pool, true)

	require.Equal(t, "1072******@qq.com", regularUser.Groups[0].Accounts[0].Identifier)
	require.Equal(t, "1072688154@qq.com", admin.Groups[0].Accounts[0].Identifier)
	require.Nil(t, regularUser.Summary.Requests)
	require.Nil(t, regularUser.Summary.Tokens)
	require.Nil(t, regularUser.Groups[0].Summary.Requests)
	require.Nil(t, regularUser.Groups[0].Summary.Tokens)
	require.Nil(t, regularUser.Groups[0].Accounts[0].Stats)
	require.Equal(t, int64(120), *admin.Summary.Requests)
	require.Equal(t, int64(12000), *admin.Summary.Tokens)
	require.Equal(t, int64(120), admin.Groups[0].Accounts[0].Stats.Total.Requests)

	regularPayload, err := json.Marshal(regularUser)
	require.NoError(t, err)
	require.NotContains(t, string(regularPayload), `"requests"`)
	require.NotContains(t, string(regularPayload), `"tokens"`)
	require.NotContains(t, string(regularPayload), `"stats"`)
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
