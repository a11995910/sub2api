//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type accountRepoStubForOpenAIModelMapping struct {
	accountRepoStub
	created        *Account
	updated        *Account
	getByIDAccount *Account
}

func (s *accountRepoStubForOpenAIModelMapping) Create(_ context.Context, account *Account) error {
	s.created = account
	account.ID = 101
	return nil
}

func (s *accountRepoStubForOpenAIModelMapping) GetByID(_ context.Context, _ int64) (*Account, error) {
	return s.getByIDAccount, nil
}

func (s *accountRepoStubForOpenAIModelMapping) Update(_ context.Context, account *Account) error {
	s.updated = account
	s.getByIDAccount = account
	return nil
}

func TestAdminServiceCreateAccount_PreservesOpenAIModelMapping(t *testing.T) {
	repo := &accountRepoStubForOpenAIModelMapping{}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name:                 "openai",
		Platform:             PlatformOpenAI,
		Type:                 AccountTypeAPIKey,
		SkipDefaultGroupBind: true,
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.created)
	mapping := repo.created.Credentials["model_mapping"].(map[string]any)
	require.Equal(t, map[string]any{"gpt-5.4": "gpt-5.4"}, mapping)
}

func TestAdminServiceUpdateAccount_PreservesOpenAIModelMapping(t *testing.T) {
	repo := &accountRepoStubForOpenAIModelMapping{
		getByIDAccount: &Account{
			ID:          202,
			Name:        "openai",
			Platform:    PlatformOpenAI,
			Type:        AccountTypeOAuth,
			Credentials: map[string]any{},
			Extra:       map[string]any{},
		},
	}
	svc := &adminServiceImpl{accountRepo: repo}

	_, err := svc.UpdateAccount(context.Background(), 202, &UpdateAccountInput{
		Credentials: map[string]any{
			"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"},
		},
	})

	require.NoError(t, err)
	require.NotNil(t, repo.updated)
	mapping := repo.updated.Credentials["model_mapping"].(map[string]any)
	require.Equal(t, map[string]any{"gpt-5.4": "gpt-5.4"}, mapping)
}
