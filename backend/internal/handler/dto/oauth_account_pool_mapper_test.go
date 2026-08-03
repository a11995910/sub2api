package dto

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOAuthAccountPoolFromServiceMasksIdentifiersForRegularUsers(t *testing.T) {
	pool := &service.OAuthAccountPool{
		Groups: []service.OAuthAccountPoolGroup{{
			Accounts: []service.OAuthAccountPoolAccount{{Identifier: "1072688154@qq.com"}},
		}},
	}

	regularUser := OAuthAccountPoolFromService(pool, false)
	admin := OAuthAccountPoolFromService(pool, true)

	require.Equal(t, "1072******@qq.com", regularUser.Groups[0].Accounts[0].Identifier)
	require.Equal(t, "1072688154@qq.com", admin.Groups[0].Accounts[0].Identifier)
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
