package repository

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/imroc/req/v3"
	"github.com/stretchr/testify/require"
)

type recordingRoundTripper struct {
	body string
}

func (rt *recordingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(rt.body)),
	}, nil
}

// The claude.ai steps carry a browser sessionKey and legitimately look like a browser;
// the token endpoint is hit by axios inside Node. They must not share a client, or the
// token request goes out as "Chrome navigating a document" while claiming to be axios.
func TestTokenEndpointsUseTokenClientFactory(t *testing.T) {
	const tokenJSON = `{"access_token":"at","refresh_token":"rt","expires_in":3600}`

	newSvc := func() (*claudeOAuthService, *[]string) {
		var used []string
		svc := &claudeOAuthService{
			baseURL:  "https://claude.ai",
			tokenURL: oauth.TokenURL,
			clientFactory: func(string) (*req.Client, error) {
				used = append(used, "browser")
				return newTestReqClient(&recordingRoundTripper{body: `[{"uuid":"org-1"}]`}), nil
			},
			tokenClientFactory: func(string) (*req.Client, error) {
				used = append(used, "token")
				return newTestReqClient(&recordingRoundTripper{body: tokenJSON}), nil
			},
		}
		return svc, &used
	}

	t.Run("RefreshToken uses the token client", func(t *testing.T) {
		svc, used := newSvc()
		_, err := svc.RefreshToken(context.Background(), "rt", "")
		require.NoError(t, err)
		require.Equal(t, []string{"token"}, *used)
	})

	t.Run("ExchangeCodeForToken uses the token client", func(t *testing.T) {
		svc, used := newSvc()
		_, err := svc.ExchangeCodeForToken(context.Background(), "code", "verifier", "state", "", false)
		require.NoError(t, err)
		require.Equal(t, []string{"token"}, *used)
	})

	t.Run("claude.ai steps keep the browser client", func(t *testing.T) {
		svc, used := newSvc()
		_, err := svc.GetOrganizationUUID(context.Background(), "sess", "")
		require.NoError(t, err)
		require.Equal(t, []string{"browser"}, *used)
	})
}

// Existing call sites (and tests) that only wire clientFactory must keep working.
func TestTokenClientFallsBackToClientFactory(t *testing.T) {
	var used []string
	svc := &claudeOAuthService{
		tokenURL: oauth.TokenURL,
		clientFactory: func(string) (*req.Client, error) {
			used = append(used, "browser")
			return newTestReqClient(&recordingRoundTripper{body: `{"access_token":"at"}`}), nil
		},
	}

	_, err := svc.RefreshToken(context.Background(), "rt", "")
	require.NoError(t, err)
	require.Equal(t, []string{"browser"}, used)
}

// The token client must not carry req's Chrome impersonation headers
// (sec-ch-ua / sec-fetch-dest / accept-language: zh-CN), which contradict the axios
// User-Agent the token requests send.
func TestCreateTokenReqClientHasNoBrowserHeaders(t *testing.T) {
	client, err := createTokenReqClient("")
	require.NoError(t, err)

	for _, header := range []string{"sec-ch-ua", "sec-fetch-dest", "sec-fetch-mode", "upgrade-insecure-requests", "accept-language"} {
		require.Empty(t, client.Headers.Get(header), "token client should not send %s", header)
	}
}

func TestCreateTokenReqClientRejectsInvalidProxy(t *testing.T) {
	_, err := createTokenReqClient("://not-a-proxy")
	require.Error(t, err)
}
