package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type excelGrantRepoStub struct {
	AccountRepository
	mu         sync.Mutex
	account    *Account
	ciphertext string
	reject     bool
}

func (r *excelGrantRepoStub) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := *r.account
	copy.Credentials = maps.Clone(r.account.Credentials)
	copy.Extra = maps.Clone(r.account.Extra)
	return &copy, nil
}
func (r *excelGrantRepoStub) GetExcelOAuthGrant(context.Context, int64) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ciphertext, nil
}
func (r *excelGrantRepoStub) CompareAndSwapExcelOAuthGrant(_ context.Context, a *Account, old, replacement string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reject || r.ciphertext != old || a.ID != r.account.ID || !reflect.DeepEqual(a.Credentials, r.account.Credentials) {
		return false, nil
	}
	r.ciphertext = replacement
	return true, nil
}

type excelGrantEncryptor struct{}

func (excelGrantEncryptor) Encrypt(s string) (string, error) {
	return base64.StdEncoding.EncodeToString([]byte(s)), nil
}
func (excelGrantEncryptor) Decrypt(s string) (string, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	return string(b), e
}

type excelGrantClient struct {
	OpenAIOAuthClient
	response      *openai.TokenResponse
	calls         atomic.Int32
	client        string
	beforeRefresh func()
}

func (c *excelGrantClient) RefreshTokenWithClientID(_ context.Context, _, _, client string) (*openai.TokenResponse, error) {
	c.calls.Add(1)
	if c.beforeRefresh != nil {
		c.beforeRefresh()
	}
	c.client = client
	return c.response, nil
}
func (c *excelGrantClient) ExchangeCode(_ context.Context, _, _, _, _, client string) (*openai.TokenResponse, error) {
	c.calls.Add(1)
	c.client = client
	return c.response, nil
}
func excelGrantToken(workspace, user, client string, expires time.Time) *OpenAITokenInfo {
	claims := map[string]any{"client_id": client, "aud": []string{client}, "exp": expires.Unix(), "email": "synthetic@example.invalid", "https://api.openai.com/auth": map[string]any{"chatgpt_account_id": workspace, "chatgpt_user_id": user}}
	raw, _ := json.Marshal(claims)
	token := "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".test"
	return &OpenAITokenInfo{AccessToken: token, IDToken: token, RefreshToken: "synthetic-excel-refresh", ClientID: client, ExpiresAt: expires.Unix()}
}
func excelGrantService(t *testing.T) (*OpenAIOAuthService, *excelGrantRepoStub, *excelGrantClient) {
	t.Helper()
	a := excelTestAccount()
	a.Credentials["client_id"] = openai.ClientID
	a.Credentials["chatgpt_user_id"] = "test-user"
	repo := &excelGrantRepoStub{account: a}
	client := &excelGrantClient{}
	s := NewOpenAIOAuthService(nil, client)
	s.SetExcelOAuthDependencies(repo, excelGrantEncryptor{}, nil)
	t.Cleanup(s.Stop)
	return s, repo, client
}
func TestExcelOAuthIndependentGrantIdentityAndCAS(t *testing.T) {
	s, repo, _ := excelGrantService(t)
	ctx := context.Background()
	primary, _ := json.Marshal(repo.account.Credentials)
	for _, tc := range []struct{ name, workspace, user, client string }{
		{"其他工作区", "other", "test-user", openai.ExcelClientID},
		{"其他用户", "test-workspace", "other", openai.ExcelClientID},
		{"Codex 客户端", "test-workspace", "test-user", openai.ClientID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, s.saveExcelGrant(ctx, repo.account, "", excelGrantToken(tc.workspace, tc.user, tc.client, time.Now().Add(time.Hour))))
			require.Empty(t, repo.ciphertext)
		})
	}
	info := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
	require.NoError(t, s.saveExcelGrant(ctx, repo.account, "", info))
	require.NotContains(t, repo.ciphertext, info.RefreshToken)
	status, err := s.ExcelAuthorizationStatus(ctx, repo.account.ID)
	require.NoError(t, err)
	require.True(t, status.Authorized)
	require.False(t, status.Primary)
	wire, _ := json.Marshal(status)
	require.NotContains(t, string(wire), info.RefreshToken)
	current := repo.ciphertext
	require.Error(t, s.saveExcelGrant(ctx, repo.account, "stale", info))
	require.Equal(t, current, repo.ciphertext)
	after, _ := json.Marshal(repo.account.Credentials)
	require.Equal(t, string(primary), string(after))
}
func TestExcelOAuthConcurrentRefreshRotatesOnce(t *testing.T) {
	s, repo, client := excelGrantService(t)
	ctx := context.Background()
	old := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Minute))
	require.NoError(t, s.saveExcelGrant(ctx, repo.account, "", old))
	fresh := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
	client.response = &openai.TokenResponse{AccessToken: fresh.AccessToken, IDToken: fresh.IDToken, RefreshToken: "rotated-excel", ExpiresIn: 3600}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := s.ExcelAccessToken(ctx, repo.account.ID)
			if err == nil && token != fresh.AccessToken {
				err = errors.New("使用了过期令牌")
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, client.calls.Load())
	require.Equal(t, openai.ExcelClientID, client.client)
	_, _, saved, err := s.readExcelGrant(ctx, repo.account.ID)
	require.NoError(t, err)
	require.Equal(t, "rotated-excel", saved.RefreshToken)
	require.Equal(t, "test-bps-token", repo.account.GetCredential("access_token"))
}
func TestExcelOAuthCodeExchangeAndRefreshFailClosed(t *testing.T) {
	s, repo, client := excelGrantService(t)
	ctx := context.Background()
	_, err := s.ExcelAccessToken(ctx, repo.account.ID)
	require.Error(t, err)
	require.Zero(t, client.calls.Load())
	info := excelGrantToken("test-workspace", "test-user", openai.ExcelClientID, time.Now().Add(time.Hour))
	client.response = &openai.TokenResponse{AccessToken: info.AccessToken, IDToken: info.IDToken, RefreshToken: info.RefreshToken, ExpiresIn: 3600}
	for _, profile := range []string{"codex", "excel"} {
		auth, err := s.GenerateAuthURL(ctx, nil, "", PlatformOpenAI, profile)
		require.NoError(t, err)
		session, _ := s.sessionStore.Get(auth.SessionID)
		input := &OpenAIExchangeCodeInput{SessionID: auth.SessionID, Code: "synthetic-code", State: session.State}
		err = s.ExchangeExcelAccountCode(ctx, repo.account.ID, input)
		if profile == "codex" {
			require.Error(t, err)
			require.Zero(t, client.calls.Load())
		} else {
			require.NoError(t, err)
		}
	}
	require.EqualValues(t, 1, client.calls.Load())
	require.Equal(t, openai.ExcelClientID, client.client)
	require.NotContains(t, fmt.Sprint(repo.account.Credentials), info.RefreshToken)
}
