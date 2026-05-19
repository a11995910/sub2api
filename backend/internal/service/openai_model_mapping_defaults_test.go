//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEnsureRequiredOpenAIModelMappings_AddsRequiredModelsToGPT5Whitelist(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{
			"gpt-5.4": "gpt-5.4",
		},
	}

	got := ensureRequiredOpenAIModelMappings(PlatformOpenAI, credentials)
	originalMapping := credentials["model_mapping"].(map[string]any)
	updatedMapping := got["model_mapping"].(map[string]any)

	require.NotContains(t, originalMapping, "codex-auto-review")
	for _, model := range openAIRequiredModelMappings {
		require.Equal(t, model, updatedMapping[model])
	}
	require.Equal(t, "gpt-5.4", originalMapping["gpt-5.4"])
}

func TestEnsureRequiredOpenAIModelMappings_AddsRequiredModelsToCodexWhitelist(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{
			"codex-auto-review": "codex-auto-review",
		},
	}

	got := ensureRequiredOpenAIModelMappings(PlatformOpenAI, credentials)
	updatedMapping := got["model_mapping"].(map[string]any)

	for _, model := range openAIRequiredModelMappings {
		require.Equal(t, model, updatedMapping[model])
	}
}

func TestEnsureRequiredOpenAIModelMappings_AddsExplicitModelsWhenWildcardExists(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{
			"gpt-5.*": "gpt-5.4",
		},
	}

	got := ensureRequiredOpenAIModelMappings(PlatformOpenAI, credentials)
	updatedMapping := got["model_mapping"].(map[string]any)

	require.Equal(t, "gpt-5.4", updatedMapping["gpt-5.*"])
	require.Equal(t, "codex-auto-review", updatedMapping["codex-auto-review"])
	require.Equal(t, "gpt-image-2", updatedMapping["gpt-image-2"])
}

func TestEnsureRequiredOpenAIModelMappings_SkipsImageOnlyWhitelist(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{
			"gpt-image-1": "gpt-image-1",
		},
	}

	got := ensureRequiredOpenAIModelMappings(PlatformOpenAI, credentials)

	require.Equal(t, credentials, got)
	require.Equal(t, map[string]any{"gpt-image-1": "gpt-image-1"}, got["model_mapping"])
}

func TestEnsureRequiredOpenAIModelMappings_SkipsCompleteWhitelist(t *testing.T) {
	mapping := map[string]any{}
	for _, model := range openAIRequiredModelMappings {
		mapping[model] = model
	}
	credentials := map[string]any{"model_mapping": mapping}

	got := ensureRequiredOpenAIModelMappings(PlatformOpenAI, credentials)

	require.Equal(t, credentials, got)
}

func TestEnsureRequiredOpenAIModelMappings_DoesNotNarrowEmptyMapping(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{},
	}

	got := ensureRequiredOpenAIModelMappings(PlatformOpenAI, credentials)

	require.Empty(t, got["model_mapping"].(map[string]any))
}

func TestEnsureRequiredOpenAIModelMappings_SkipsNonOpenAI(t *testing.T) {
	credentials := map[string]any{
		"model_mapping": map[string]any{
			"gpt-5.4": "gpt-5.4",
		},
	}

	got := ensureRequiredOpenAIModelMappings(PlatformAnthropic, credentials)

	require.NotContains(t, got["model_mapping"], openAIRequiredGPT55Model)
}

func TestCredentialsMayNeedRequiredOpenAIModelMappings(t *testing.T) {
	require.True(t, credentialsMayNeedRequiredOpenAIModelMappings(map[string]any{
		"model_mapping": map[string]any{"gpt-5.4": "gpt-5.4"},
	}))
	require.True(t, credentialsMayNeedRequiredOpenAIModelMappings(map[string]any{
		"model_mapping": map[string]any{"gpt-5.*": "gpt-5.4"},
	}))
	require.False(t, credentialsMayNeedRequiredOpenAIModelMappings(map[string]any{
		"model_mapping": map[string]any{"gpt-image-1": "gpt-image-1"},
	}))
	require.False(t, credentialsMayNeedRequiredOpenAIModelMappings(map[string]any{
		"model_mapping": map[string]any{},
	}))
}

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

func TestAdminServiceCreateAccount_EnsuresOpenAIRequiredModelMappings(t *testing.T) {
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
	require.Equal(t, "codex-auto-review", mapping["codex-auto-review"])
	require.Equal(t, openAIRequiredGPT55Model, mapping[openAIRequiredGPT55Model])
	require.Equal(t, "gpt-image-2", mapping["gpt-image-2"])
}

func TestAdminServiceUpdateAccount_EnsuresOpenAIRequiredModelMappings(t *testing.T) {
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
	require.Equal(t, "codex-auto-review", mapping["codex-auto-review"])
	require.Equal(t, openAIRequiredGPT55Model, mapping[openAIRequiredGPT55Model])
	require.Equal(t, "gpt-image-2", mapping["gpt-image-2"])
}
