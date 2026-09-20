package admin

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountResponseCodexTicketsUsesConfiguredPolicy(t *testing.T) {
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: map[string]any{"openai_turn_state_mode": "codex_ticket"}}
	h := &AccountHandler{cfg: &config.Config{}}
	require.Empty(t, h.accountResponseFromService(account).CodexTurnTickets)
	require.Empty(t, h.accountListResponseFromService(account).CodexTurnTickets)
	h.cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"configured-model"}, FailClosed: false}
	status := h.accountListResponseFromService(account).CodexTurnTickets
	require.Len(t, status, 1)
	require.Equal(t, "configured-model", status[0].Model)
	require.False(t, status[0].Blocked)
	h.cfg.Gateway.OpenAICodexTicket.FailClosed = true
	require.True(t, h.accountResponseFromService(account).CodexTurnTickets[0].Blocked)
}

func TestAccountListETagTracksCodexTicketHarvestProgress(t *testing.T) {
	items := []dto.AccountListItem{{CodexTurnTickets: []service.OpenAICodexTicketStatus{{Model: "gpt-6-astra", HarvestStatus: "waiting"}}}}
	before := buildAccountsListETag(items, 1, 1, 10, "openai", "", "", "", true)
	attemptAt := time.Now()
	items[0].CodexTurnTickets[0].HarvestStatus = "collecting"
	items[0].CodexTurnTickets[0].LastAttemptAt = &attemptAt
	items[0].CodexTurnTickets[0].AttemptIndex = 1
	items[0].CodexTurnTickets[0].AttemptTotal = 4
	after := buildAccountsListETag(items, 1, 1, 10, "openai", "", "", "", true)
	require.NotEqual(t, before, after, "账号updated_at不变时也必须返回新的采集状态")
}

func TestAccountResponseCodexTicketsReadsLiveSettingsAfterRestart(t *testing.T) {
	cfg := &config.Config{}
	repo := &settingHandlerRepoStub{values: map[string]string{service.SettingKeyOpenAICodexTicketEnabled: "true"}}
	settings := service.NewSettingService(repo, cfg)
	h := &AccountHandler{cfg: cfg}
	h.SetCodexTicketSettings(settings)
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeSetupToken, Extra: map[string]any{"openai_turn_state_mode": "codex_ticket"}}
	require.Len(t, h.accountListResponseFromService(account).CodexTurnTickets, 2)
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled)
	repo.values[service.SettingKeyOpenAICodexTicketEnabled] = "false"
	settings.InvalidateOpenAICodexTicketEnabledCache()
	require.Empty(t, h.accountResponseFromService(account).CodexTurnTickets)
}
