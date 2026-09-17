//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIHealthyTurnStateProbeSharedConcurrency(t *testing.T) {
	for _, busy := range []bool{true, false} {
		slots := &protectedTestSlots{held: busy}
		upstream := &healthyTurnStateUpstream{responses: []*http.Response{healthyTurnStateResponse(503, "无效状态", "失败")}}
		gateway := &OpenAIGatewayService{httpUpstream: upstream}
		svc := &AccountTestService{openaiGatewayService: gateway, concurrencyService: NewConcurrencyService(slots)}
		result, err := svc.ProbeOpenAIHealthyTurnState(context.Background(), healthyTurnStateProbeAccount(), "gpt-5.4", "http")
		require.NoError(t, err)
		require.Equal(t, 2, slots.limit)
		if busy {
			require.Equal(t, "blocked", result.Status)
			require.Empty(t, upstream.requests)
			require.Empty(t, slots.releasedAccountIDs)
		} else {
			require.Equal(t, "failed", result.Status)
			require.False(t, slots.held)
			require.Equal(t, []int64{99}, slots.releasedAccountIDs)
		}
	}
}
