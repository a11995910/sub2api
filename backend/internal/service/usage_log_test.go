package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type memoryCacheHitTargetTracker struct {
	mu     sync.Mutex
	states map[string][2]int64
}

type failingCacheHitTargetTracker struct {
	err error
}

func (f failingCacheHitTargetTracker) AdjustCacheHitToTarget(
	context.Context,
	string,
	int64, int64, int64, int64, int64, int64, int64, int64,
) (CacheHitTargetAdjustment, error) {
	return CacheHitTargetAdjustment{}, f.err
}

func (m *memoryCacheHitTargetTracker) AdjustCacheHitToTarget(
	_ context.Context,
	_ string,
	userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion, promptTokens, cacheReadTokens int64,
) (CacheHitTargetAdjustment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.states == nil {
		m.states = make(map[string][2]int64)
	}
	key := fmt.Sprintf("%d:%d:%d:%d:%d:%d", userID, groupID, targetBasisPoints, toleranceBasisPoints, halfLifeSeconds, stateVersion)
	state := m.states[key]
	promptTotal := state[0] + promptTokens
	cacheAvailable := state[1] + cacheReadTokens
	targetAllowed := (promptTotal/cacheHitTargetBasisPoints)*targetBasisPoints +
		(promptTotal%cacheHitTargetBasisPoints)*targetBasisPoints/cacheHitTargetBasisPoints
	triggerBasisPoints := targetBasisPoints + toleranceBasisPoints
	triggerAllowed := (promptTotal/cacheHitTargetBasisPoints)*triggerBasisPoints +
		(promptTotal%cacheHitTargetBasisPoints)*triggerBasisPoints/cacheHitTargetBasisPoints
	shifted := int64(0)
	if cacheAvailable > triggerAllowed {
		shifted = min(cacheAvailable-targetAllowed, cacheReadTokens)
	}
	cacheKept := cacheAvailable - shifted
	m.states[key] = [2]int64{promptTotal, cacheKept}
	return CacheHitTargetAdjustment{
		Enabled:                   true,
		ShiftedTokens:             int(shifted),
		CumulativePromptTokens:    promptTotal,
		CumulativeCacheReadTokens: cacheKept,
		StateVersion:              stateVersion,
	}, nil
}

func TestParseUsageRequestType(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name    string
		input   string
		want    RequestType
		wantErr bool
	}

	cases := []testCase{
		{name: "unknown", input: "unknown", want: RequestTypeUnknown},
		{name: "sync", input: "sync", want: RequestTypeSync},
		{name: "stream", input: "stream", want: RequestTypeStream},
		{name: "ws_v2", input: "ws_v2", want: RequestTypeWSV2},
		{name: "case_insensitive", input: "WS_V2", want: RequestTypeWSV2},
		{name: "trim_spaces", input: "  stream  ", want: RequestTypeStream},
		{name: "invalid", input: "xxx", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseUsageRequestType(tc.input)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestRequestTypeNormalizeAndString(t *testing.T) {
	t.Parallel()

	require.Equal(t, RequestTypeUnknown, RequestType(99).Normalize())
	require.Equal(t, "unknown", RequestType(99).String())
	require.Equal(t, "sync", RequestTypeSync.String())
	require.Equal(t, "stream", RequestTypeStream.String())
	require.Equal(t, "ws_v2", RequestTypeWSV2.String())
}

func TestRequestTypeFromLegacy(t *testing.T) {
	t.Parallel()

	require.Equal(t, RequestTypeWSV2, RequestTypeFromLegacy(false, true))
	require.Equal(t, RequestTypeStream, RequestTypeFromLegacy(true, false))
	require.Equal(t, RequestTypeSync, RequestTypeFromLegacy(false, false))
}

func TestApplyLegacyRequestFields(t *testing.T) {
	t.Parallel()

	stream, ws := ApplyLegacyRequestFields(RequestTypeSync, true, true)
	require.False(t, stream)
	require.False(t, ws)

	stream, ws = ApplyLegacyRequestFields(RequestTypeStream, false, true)
	require.True(t, stream)
	require.False(t, ws)

	stream, ws = ApplyLegacyRequestFields(RequestTypeWSV2, false, false)
	require.True(t, stream)
	require.True(t, ws)

	stream, ws = ApplyLegacyRequestFields(RequestTypeUnknown, true, false)
	require.True(t, stream)
	require.False(t, ws)
}

func TestUsageLogSyncRequestTypeAndLegacyFields(t *testing.T) {
	t.Parallel()

	log := &UsageLog{RequestType: RequestTypeWSV2, Stream: false, OpenAIWSMode: false}
	log.SyncRequestTypeAndLegacyFields()

	require.Equal(t, RequestTypeWSV2, log.RequestType)
	require.True(t, log.Stream)
	require.True(t, log.OpenAIWSMode)
}

func TestUsageLogEffectiveRequestTypeFallback(t *testing.T) {
	t.Parallel()

	log := &UsageLog{RequestType: RequestTypeUnknown, Stream: true, OpenAIWSMode: true}
	require.Equal(t, RequestTypeWSV2, log.EffectiveRequestType())
}

func TestUsageLogEffectiveRequestTypeNilReceiver(t *testing.T) {
	t.Parallel()

	var log *UsageLog
	require.Equal(t, RequestTypeUnknown, log.EffectiveRequestType())
}

func TestUsageLogSyncRequestTypeAndLegacyFieldsNilReceiver(t *testing.T) {
	t.Parallel()

	var log *UsageLog
	log.SyncRequestTypeAndLegacyFields()
}

func TestApplyCacheHitTargetToInput_CumulativeUserGroupTarget(t *testing.T) {
	t.Parallel()

	groupID := int64(9)
	apiKey := &APIKey{
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	}
	tracker := &memoryCacheHitTargetTracker{}

	// 首次请求命中率 80%，低于目标，不做调整，但会累计分母。
	first := &UsageTokens{InputTokens: 20, CacheReadTokens: 80}
	adjustment, err := applyCacheHitTargetToInput(context.Background(), first, apiKey, 101, tracker)
	require.NoError(t, err)
	require.Zero(t, adjustment.ShiftedTokens)
	require.Equal(t, 80, first.CacheReadTokens)

	// 第二次请求自身为 100%，但两次累计恰好 90%，因此仍无需调整。
	second := &UsageTokens{CacheReadTokens: 100}
	adjustment, err = applyCacheHitTargetToInput(context.Background(), second, apiKey, 101, tracker)
	require.NoError(t, err)
	require.Zero(t, adjustment.ShiftedTokens)
	require.Equal(t, 100, second.CacheReadTokens)

	// 第三次再全命中时累计将达到 93.33%，只移动 10 token，使累计保持 90%。
	third := &UsageTokens{CacheReadTokens: 100}
	adjustment, err = applyCacheHitTargetToInput(context.Background(), third, apiKey, 101, tracker)
	require.NoError(t, err)
	require.Equal(t, 10, adjustment.ShiftedTokens)
	require.Equal(t, int64(300), adjustment.CumulativePromptTokens)
	require.Equal(t, int64(270), adjustment.CumulativeCacheReadTokens)
	require.InDelta(t, 90, adjustment.CumulativeHitPercent, 1e-9)
	require.Equal(t, 10, third.InputTokens)
	require.Equal(t, 90, third.CacheReadTokens)
}

func TestApplyCacheHitTargetToInput_TrackerUnavailableDoesNotAdjust(t *testing.T) {
	t.Parallel()

	groupID := int64(10)
	apiKey := &APIKey{
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	}
	tokens := &UsageTokens{InputTokens: 6, CacheReadTokens: 94}
	adjustment, err := applyCacheHitTargetToInput(context.Background(), tokens, apiKey, 102, nil)

	require.NoError(t, err)
	require.False(t, adjustment.Enabled)
	require.Equal(t, 6, tokens.InputTokens)
	require.Equal(t, 94, tokens.CacheReadTokens)
}

func TestApplyCacheHitTargetToInput_MissingCumulativeDimensionDoesNotAdjust(t *testing.T) {
	t.Parallel()

	validGroupID := int64(10)
	for _, tc := range []struct {
		name    string
		userID  int64
		groupID int64
	}{
		{name: "missing user", userID: 0, groupID: validGroupID},
		{name: "missing group", userID: 102, groupID: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			groupID := tc.groupID
			apiKey := &APIKey{
				GroupID: &groupID,
				Group: &Group{
					CacheHitQuarterToInput:         true,
					CacheHitTargetPercent:          90,
					CacheHitTargetTolerancePercent: 0.5,
				},
			}
			tokens := &UsageTokens{InputTokens: 6, CacheReadTokens: 94}

			adjustment, err := applyCacheHitTargetToInput(
				context.Background(), tokens, apiKey, tc.userID, &memoryCacheHitTargetTracker{},
			)

			require.NoError(t, err)
			require.False(t, adjustment.Enabled)
			require.Equal(t, 6, tokens.InputTokens)
			require.Equal(t, 94, tokens.CacheReadTokens)
		})
	}
}

func TestApplyCacheHitTargetToInput_TrackerErrorDoesNotAdjust(t *testing.T) {
	t.Parallel()

	groupID := int64(12)
	apiKey := &APIKey{
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	}
	tokens := &UsageTokens{InputTokens: 6, CacheReadTokens: 94}
	trackerErr := errors.New("tracker unavailable")

	adjustment, err := applyCacheHitTargetToInput(
		context.Background(), tokens, apiKey, 104, failingCacheHitTargetTracker{err: trackerErr},
	)

	require.ErrorIs(t, err, trackerErr)
	require.False(t, adjustment.Enabled)
	require.Equal(t, 6, tokens.InputTokens)
	require.Equal(t, 94, tokens.CacheReadTokens)
}

func TestApplyCacheHitTargetToInput_ToleranceAndIntegerRecovery(t *testing.T) {
	t.Parallel()

	groupID := int64(11)
	apiKey := &APIKey{
		GroupID: &groupID,
		Group: &Group{
			ID:                             groupID,
			CacheHitQuarterToInput:         true,
			CacheHitTargetPercent:          90,
			CacheHitTargetTolerancePercent: 0.5,
		},
	}
	tracker := &memoryCacheHitTargetTracker{}

	// 9 个全命中 token 触发后受整数粒度限制，只能保留 8 个，累计为 88.89%。
	first := &UsageTokens{CacheReadTokens: 9}
	adjustment, err := applyCacheHitTargetToInput(context.Background(), first, apiKey, 103, tracker)
	require.NoError(t, err)
	require.Equal(t, 1, adjustment.ShiftedTokens)
	require.InDelta(t, 88.8889, adjustment.CumulativeHitPercent, 0.0001)

	// 下一次命中会自然回升到 90%，未超过 90.5% 上沿，因此不再划拨。
	second := &UsageTokens{CacheReadTokens: 1}
	adjustment, err = applyCacheHitTargetToInput(context.Background(), second, apiKey, 103, tracker)
	require.NoError(t, err)
	require.Zero(t, adjustment.ShiftedTokens)
	require.Equal(t, 1, second.CacheReadTokens)
	require.InDelta(t, 90, adjustment.CumulativeHitPercent, 1e-9)

	// 单个全新累计恰好位于容差上沿也不触发。
	boundary := &UsageTokens{InputTokens: 19, CacheReadTokens: 181}
	adjustment, err = applyCacheHitTargetToInput(context.Background(), boundary, apiKey, 104, tracker)
	require.NoError(t, err)
	require.Zero(t, adjustment.ShiftedTokens)
	require.Equal(t, 181, boundary.CacheReadTokens)
}
