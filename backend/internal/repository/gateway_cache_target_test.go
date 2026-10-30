package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCacheHitTargetTestCache(t *testing.T) *gatewayCache {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return &gatewayCache{rdb: rdb}
}

func TestGatewayCacheAdjustCacheHitToTarget_CumulativeAndIsolated(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()

	adjustment, err := cache.AdjustCacheHitToTarget(ctx, "cumulative-1", 1, 2, 9000, 50, 86400, 101, 100, 80)
	require.NoError(t, err)
	require.Zero(t, adjustment.ShiftedTokens)

	// 单次请求是 100% 命中，但与上一次累计后恰好为 90%，不应误降。
	adjustment, err = cache.AdjustCacheHitToTarget(ctx, "cumulative-2", 1, 2, 9000, 50, 86400, 101, 100, 100)
	require.NoError(t, err)
	require.Zero(t, adjustment.ShiftedTokens)

	adjustment, err = cache.AdjustCacheHitToTarget(ctx, "cumulative-3", 1, 2, 9000, 50, 86400, 101, 100, 100)
	require.NoError(t, err)
	require.Equal(t, 10, adjustment.ShiftedTokens)
	require.Equal(t, int64(300), adjustment.CumulativePromptTokens)
	require.Equal(t, int64(270), adjustment.CumulativeCacheReadTokens)

	// 不同用户使用独立累计状态。
	adjustment, err = cache.AdjustCacheHitToTarget(ctx, "isolated-user", 3, 2, 9000, 50, 86400, 101, 100, 94)
	require.NoError(t, err)
	require.Equal(t, 4, adjustment.ShiftedTokens)

	stateKey, operationKey := buildCacheHitTargetKeys("cumulative-1", 1, 2, 9000, 50, 86400, 101)
	ttl, err := cache.rdb.TTL(ctx, stateKey).Result()
	require.NoError(t, err)
	require.Equal(t, cacheHitTargetTTL, ttl)
	operationTTL, err := cache.rdb.TTL(ctx, operationKey).Result()
	require.NoError(t, err)
	require.Equal(t, cacheHitTargetOperationTTL, operationTTL)
}

func TestGatewayCacheAdjustCacheHitToTarget_DuplicateOperationIsIdempotent(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()
	at := time.Date(2026, time.October, 29, 16, 0, 0, 0, time.UTC)

	first, err := cache.adjustCacheHitToTargetAt(ctx, "duplicate-operation", 7, 8, 9000, 50, 86400, 111, 100, 94, at)
	require.NoError(t, err)
	require.Equal(t, 4, first.ShiftedTokens)
	require.Equal(t, int64(100), first.CumulativePromptTokens)
	require.Equal(t, int64(90), first.CumulativeCacheReadTokens)

	replayed, err := cache.adjustCacheHitToTargetAt(ctx, "duplicate-operation", 7, 8, 9000, 50, 86400, 111, 100, 94, at)
	require.NoError(t, err)
	require.Equal(t, first, replayed)

	// 新 operation 只能看到两个逻辑请求的累计值；若重放被重复累计，分母会是 300。
	second, err := cache.adjustCacheHitToTargetAt(ctx, "duplicate-operation-next", 7, 8, 9000, 50, 86400, 111, 100, 100, at)
	require.NoError(t, err)
	require.Equal(t, 10, second.ShiftedTokens)
	require.Equal(t, int64(200), second.CumulativePromptTokens)
	require.Equal(t, int64(180), second.CumulativeCacheReadTokens)
}

func TestBuildCacheHitTargetKeys_UsesSharedClusterHashTag(t *testing.T) {
	t.Parallel()
	operationID := "request/{must-not-enter-key}"

	stateKey, operationKey := buildCacheHitTargetKeys(operationID, 1, 2, 9000, 50, 86400, 101)

	hashTag := "{u1:g2:t9000:o50:h86400:v101}"
	require.Contains(t, stateKey, hashTag)
	require.Contains(t, operationKey, hashTag)
	require.NotContains(t, stateKey, operationID)
	require.NotContains(t, operationKey, operationID)
	require.True(t, strings.HasSuffix(stateKey, ":state"))
}

func TestGatewayCacheAdjustCacheHitToTarget_ConcurrentAtomicity(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()
	at := time.Date(2026, time.October, 29, 16, 0, 0, 0, time.UTC)

	const requests = 100
	var wg sync.WaitGroup
	shifts := make(chan int, requests)
	errs := make(chan error, requests)
	for i := range requests {
		operationID := fmt.Sprintf("concurrent-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			adjustment, err := cache.adjustCacheHitToTargetAt(ctx, operationID, 11, 22, 9000, 50, 86400, 202, 100, 94, at)
			shifts <- adjustment.ShiftedTokens
			errs <- err
		}()
	}
	wg.Wait()
	close(shifts)
	close(errs)

	var totalShifted int
	for err := range errs {
		require.NoError(t, err)
	}
	for shifted := range shifts {
		totalShifted += shifted
	}
	// 原始累计命中 9400/10000；每条分摊后为 90%，并发的累计状态必须与实际划拨守恒。
	cacheKept := 9400 - totalShifted
	require.Equal(t, 9000, cacheKept)
	stateKey, _ := buildCacheHitTargetKeys("state", 11, 22, 9000, 50, 86400, 202)
	storedPrompt, err := cache.rdb.HGet(ctx, stateKey, "prompt_tokens").Int64()
	require.NoError(t, err)
	storedCache, err := cache.rdb.HGet(ctx, stateKey, "cache_read_tokens").Int64()
	require.NoError(t, err)
	require.Equal(t, int64(10000), storedPrompt)
	require.Equal(t, int64(cacheKept), storedCache)
}

func TestGatewayCacheAdjustCacheHitToTarget_StateVersionIsolation(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()

	first, err := cache.AdjustCacheHitToTarget(ctx, "version-301-first", 21, 22, 9000, 50, 86400, 301, 100, 80)
	require.NoError(t, err)
	require.Zero(t, first.ShiftedTokens)
	second, err := cache.AdjustCacheHitToTarget(ctx, "version-301-second", 21, 22, 9000, 50, 86400, 301, 100, 100)
	require.NoError(t, err)
	require.Zero(t, second.ShiftedTokens)

	// 分组保存后状态代次变化，新代次不继承旧累计。
	reset, err := cache.AdjustCacheHitToTarget(ctx, "version-302-first", 21, 22, 9000, 50, 86400, 302, 100, 100)
	require.NoError(t, err)
	require.Equal(t, 10, reset.ShiftedTokens)
	require.Equal(t, int64(100), reset.CumulativePromptTokens)
}

func TestGatewayCacheAdjustCacheHitToTarget_SpreadsToleranceWithoutDrainingSmallRequest(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()

	atUpperBound, err := cache.AdjustCacheHitToTarget(ctx, "tolerance-upper", 31, 32, 9000, 50, 86400, 401, 10000, 9050)
	require.NoError(t, err)
	require.Equal(t, 50, atUpperBound.ShiftedTokens)

	// 一开始就分摊 50 token；后续只有 1 个缓存 token 的请求不会被清空。
	crossed, err := cache.AdjustCacheHitToTarget(ctx, "tolerance-crossed", 31, 32, 9000, 50, 86400, 401, 1, 1)
	require.NoError(t, err)
	require.Zero(t, crossed.ShiftedTokens)
	require.Equal(t, int64(10001), crossed.CumulativePromptTokens)
	require.Equal(t, int64(9001), crossed.CumulativeCacheReadTokens)
}

func TestGatewayCacheAdjustCacheHitToTarget_DecaysHistoryByHalfLife(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()
	startedAt := time.Date(2026, time.August, 20, 0, 0, 0, 0, time.UTC)

	first, err := cache.adjustCacheHitToTargetAt(ctx, "decay-first", 41, 42, 9000, 50, 86400, 501, 100, 80, startedAt)
	require.NoError(t, err)
	require.Zero(t, first.ShiftedTokens)

	// 一个半衰期后，历史 100/80 先衰减为 50/40；加入本次 100/100 后为
	// 150/140，保留历史低命中产生的额度，只划拨累计超额的 5 token 后回到 90%。
	second, err := cache.adjustCacheHitToTargetAt(ctx, "decay-second", 41, 42, 9000, 50, 86400, 501, 100, 100, startedAt.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 5, second.ShiftedTokens)
	require.Equal(t, int64(150), second.CumulativePromptTokens)
	require.Equal(t, int64(135), second.CumulativeCacheReadTokens)
}

func TestGatewayCacheAdjustCacheHitToTarget_SmoothsExistingHistoricalDebt(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()
	at := time.Date(2026, time.October, 29, 15, 22, 31, 0, time.UTC)
	stateKey, _ := buildCacheHitTargetKeys("seed", 1, 2, 8300, 50, 86400, 1)
	// 回归线上形态：旧算法会在这一条补扣 600012，清空现有的 351360 缓存。
	require.NoError(t, cache.rdb.HSet(ctx, stateKey,
		"prompt_tokens", 109896486,
		"cache_read_tokens", 91755234,
		"last_updated_unix", at.Unix(),
	).Err())
	adjustment, err := cache.adjustCacheHitToTargetAt(ctx, "smooth-debt", 1, 2, 8300, 50, 86400, 1, 352409, 351360, at)
	require.NoError(t, err)
	kept := 351360 - adjustment.ShiftedTokens
	require.Positive(t, adjustment.ShiftedTokens)
	require.Positive(t, kept)
	// 本条只围绕配置的 83% ±0.5% 分摊，不承担全部历史差额。
	hitRate := float64(kept) * 100 / 352409
	require.InDelta(t, 83, hitRate, 0.5002)
	require.Equal(t, int64(110248895), adjustment.CumulativePromptTokens)
	require.Equal(t, int64(91755234+kept), adjustment.CumulativeCacheReadTokens)
}

func TestGatewayCacheAdjustCacheHitToTarget_UsesConfiguredTargetWithoutExtraLimit(t *testing.T) {
	t.Parallel()
	for _, target := range []int64{1000, 8300, 8400, 9500} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			t.Parallel()
			cache := newCacheHitTargetTestCache(t)
			at := time.Date(2026, time.October, 29, 16, 0, 0, 0, time.UTC)
			var shifted int
			for i := range 30 {
				adjustment, err := cache.adjustCacheHitToTargetAt(context.Background(), fmt.Sprint(i), 1, 2, target, 50, 86400, 1, 10000, 10000, at)
				require.NoError(t, err)
				require.Equal(t, int(10000-target), adjustment.ShiftedTokens)
				shifted += adjustment.ShiftedTokens
				require.Equal(t, int64((i+1)*10000-shifted), adjustment.CumulativeCacheReadTokens)
			}
		})
	}
}

func TestGatewayCacheAdjustCacheHitToTarget_BoundsCorrectionByConfiguredTolerance(t *testing.T) {
	t.Parallel()
	for _, tolerance := range []int64{0, 25, 50} {
		t.Run(fmt.Sprint(tolerance), func(t *testing.T) {
			t.Parallel()
			cache := newCacheHitTargetTestCache(t)
			at := time.Date(2026, time.October, 29, 16, 0, 0, 0, time.UTC)
			stateKey, _ := buildCacheHitTargetKeys("seed", 1, 2, 8300, tolerance, 86400, 1)
			require.NoError(t, cache.rdb.HSet(context.Background(), stateKey,
				"prompt_tokens", 10000000, "cache_read_tokens", 9800000, "last_updated_unix", at.Unix(),
			).Err())
			adjustment, err := cache.adjustCacheHitToTargetAt(context.Background(), "correction", 1, 2, 8300, tolerance, 86400, 1, 10000, 10000, at)
			require.NoError(t, err)
			require.Equal(t, int(1700+tolerance), adjustment.ShiftedTokens)
		})
	}
}

func TestGatewayCacheAdjustCacheHitToTarget_PreservesZeroAndTinyUpstreamCache(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		prompt int64
		cache  int64
	}{
		{name: "原本无缓存", prompt: 350000, cache: 0},
		{name: "只有一个缓存Token", prompt: 1, cache: 1},
		{name: "原始命中低于目标", prompt: 350000, cache: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cache := newCacheHitTargetTestCache(t)
			adjustment, err := cache.AdjustCacheHitToTarget(context.Background(), tc.name, 1, 2, 8300, 50, 86400, 1, tc.prompt, tc.cache)
			require.NoError(t, err)
			require.Zero(t, adjustment.ShiftedTokens)
			require.Equal(t, tc.cache, adjustment.CumulativeCacheReadTokens)
		})
	}
}

func TestGatewayCacheAdjustCacheHitToTarget_VariableRequestsStaySmooth(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()
	at := time.Date(2026, time.October, 29, 16, 0, 0, 0, time.UTC)
	prompts := []int64{50000, 350000, 10000, 600000, 100000}
	var totalPrompt, totalRaw, totalShifted int64
	for i := range 200 {
		prompt := prompts[i%len(prompts)]
		raw := prompt * int64(95+i%5) / 100
		adjustment, err := cache.adjustCacheHitToTargetAt(ctx, fmt.Sprint(i), 1, 2, 8300, 50, 86400, 1, prompt, raw, at)
		require.NoError(t, err)
		kept := raw - int64(adjustment.ShiftedTokens)
		require.Positive(t, adjustment.ShiftedTokens, "不应先积攒多个请求再集中划拨")
		require.InDelta(t, 83, float64(kept)*100/float64(prompt), 0.51)
		totalPrompt += prompt
		totalRaw += raw
		totalShifted += int64(adjustment.ShiftedTokens)
		require.Equal(t, totalPrompt, adjustment.CumulativePromptTokens)
		require.Equal(t, totalRaw-totalShifted, adjustment.CumulativeCacheReadTokens)
	}
	require.InDelta(t, 83, float64(totalRaw-totalShifted)*100/float64(totalPrompt), 0.01)
}
