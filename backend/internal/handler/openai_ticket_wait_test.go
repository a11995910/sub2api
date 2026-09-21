//go:build unit

package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestOpenAITicketUnavailableInterruptsAccountWait(t *testing.T) {
	for _, tc := range []struct {
		name             string
		acquired         bool
		sequence         []bool
		failAt, releases int
	}{
		{"已抢槽后失效", true, nil, 1, 1},
		{"排队前失效", false, nil, 1, 0},
		{"快速抢槽时失效", false, []bool{true}, 2, 1},
		{"排队中失效", false, []bool{false, false}, 3, 0},
		{"排队抢槽成功时失效", false, []bool{false, true}, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &ticketWaitCache{helperConcurrencyCacheStub: helperConcurrencyCacheStub{accountSeq: tc.sequence}}
			h := &OpenAIGatewayHandler{gatewayService: &service.OpenAIGatewayService{}, concurrencyHelper: NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatNone, 0)}
			c, recorder := newHelperTestContext(http.MethodPost, "/v1/responses")
			failure := localTurnStateFailover("当前账号模型暂时没有可用状态头，请稍后重试")
			checks, schedulerReleases := 0, 0
			selection := &service.AccountSelectionResult{
				Account: &service.Account{ID: 41}, Acquired: tc.acquired,
				WaitPlan: &service.AccountWaitPlan{AccountID: 41, MaxConcurrency: 1, MaxWaiting: 2, Timeout: 10 * time.Second},
				CheckAvailability: func() error {
					checks++
					if checks >= tc.failAt {
						return failure
					}
					return nil
				},
			}
			if tc.acquired {
				selection.ReleaseFunc = func() { schedulerReleases++ }
			}
			started := false
			begin := time.Now()
			release, result, failover := h.acquireResponsesAccountSlotWithFailover(c, nil, "", selection, false, &started, zap.NewNop())
			require.Same(t, failure, failover)
			require.Equal(t, openAISlotAcquireFailed, result)
			require.Nil(t, release)
			require.Less(t, time.Since(begin), 2*time.Second, "不能等待原来的十秒队列超时")
			require.Empty(t, recorder.Body.String(), "重选前不能写出终止响应")
			require.False(t, failover.RetryableOnSameAccount)
			require.Equal(t, tc.releases, cache.accountReleaseCalls+schedulerReleases)
			require.Equal(t, cache.queued, cache.dequeued, "退出队列必须归还等待计数")
		})
	}
}

type ticketWaitCache struct {
	helperConcurrencyCacheStub
	queued, dequeued int
}

func (s *ticketWaitCache) IncrementAccountWaitCount(_ context.Context, _ int64, _ int) (bool, error) {
	s.queued++
	return true, nil
}
func (s *ticketWaitCache) DecrementAccountWaitCount(_ context.Context, _ int64) error {
	s.dequeued++
	return nil
}
