package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketInvalidationEscapesStickyAndWaitSelection(t *testing.T) {
	for _, advanced := range []string{"false", "true"} {
		t.Run(advanced, func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			defer resetOpenAIAdvancedSchedulerSettingCacheForTest()
			ctx := context.Background()
			groupID := int64(10103)
			first, second := ticketTestAccount(41), ticketTestAccount(42)
			for _, a := range []*Account{first, second} {
				a.Status = StatusActive
				a.Schedulable = true
				a.Concurrency = 1
				a.GroupIDs = []int64{groupID}
			}
			second.Priority = 5
			cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:ticket-session": 41}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
			firstTicket, secondTicket := boundTicket("gpt-6-astra", "http://192.0.2.1:8080"), boundTicket("gpt-6-astra", "http://192.0.2.2:8080")
			svc.storeOpenAICodexTicket(ctx, first, firstTicket)
			svc.storeOpenAICodexTicket(ctx, second, secondTicket)
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{*first, *second}}
			svc.cache = cache
			svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService(advanced)
			svc.concurrencyService = NewConcurrencyService(schedulerTestConcurrencyCache{})
			selectAccount := func() (*AccountSelectionResult, error) {
				selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "ticket-session", "gpt-6-astra", nil, OpenAIUpstreamTransportAny, false)
				return selection, err
			}
			selection, err := selectAccount()
			require.NoError(t, err)
			require.Equal(t, first.ID, selection.Account.ID)
			require.NotNil(t, selection.CheckAvailability)
			require.NoError(t, selection.CheckAvailability())
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			// 模拟已选号之后，另一请求将当前代票据判定失效；不等待后台采集。
			retired := *svc.lookupOpenAICodexTicket(first, firstTicket.Model)
			retired.Invalidated = true
			retired.ExpiresAt = time.Now()
			svc.openaiCodexTickets.Store(openAICodexTicketKey(first.ID, retired.Model), &retired)
			require.ErrorIs(t, selection.CheckAvailability(), ErrOpenAICodexTicketUnavailable)
			next, err := selectAccount()
			require.NoError(t, err)
			require.Equal(t, second.ID, next.Account.ID, "粘性会话不能困在失效账号")
			if next.ReleaseFunc != nil {
				next.ReleaseFunc()
			}
			retiredSecond := *svc.lookupOpenAICodexTicket(second, secondTicket.Model)
			retiredSecond.Invalidated = true
			svc.openaiCodexTickets.Store(openAICodexTicketKey(second.ID, retiredSecond.Model), &retiredSecond)
			_, err = selectAccount()
			require.Error(t, err, "全部缺票应立即返回无可用账号，不能等待采票")
		})
	}
}
