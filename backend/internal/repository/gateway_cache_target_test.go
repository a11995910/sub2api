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

	first, err := cache.AdjustCacheHitToTarget(ctx, "duplicate-operation", 7, 8, 9000, 50, 86400, 111, 100, 80)
	require.NoError(t, err)
	require.Zero(t, first.ShiftedTokens)
	require.Equal(t, int64(100), first.CumulativePromptTokens)
	require.Equal(t, int64(80), first.CumulativeCacheReadTokens)

	replayed, err := cache.AdjustCacheHitToTarget(ctx, "duplicate-operation", 7, 8, 9000, 50, 86400, 111, 100, 80)
	require.NoError(t, err)
	require.Equal(t, first, replayed)

	// 新 operation 只能看到两个逻辑请求的累计值；若重放被重复累计，分母会是 300。
	second, err := cache.AdjustCacheHitToTarget(ctx, "duplicate-operation-next", 7, 8, 9000, 50, 86400, 111, 100, 100)
	require.NoError(t, err)
	require.Zero(t, second.ShiftedTokens)
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

	const requests = 100
	var wg sync.WaitGroup
	shifts := make(chan int, requests)
	errs := make(chan error, requests)
	for i := range requests {
		operationID := fmt.Sprintf("concurrent-%d", i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			adjustment, err := cache.AdjustCacheHitToTarget(ctx, operationID, 11, 22, 9000, 50, 86400, 202, 100, 94)
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
	// 原始累计命中 9400/10000；并发原子更新后应落在 90% 到 90.5% 容差带内，
	// 且所有请求划拨总量与最终缓存累计严格守恒。
	cacheKept := 9400 - totalShifted
	require.GreaterOrEqual(t, cacheKept, 9000)
	require.LessOrEqual(t, cacheKept, 9050)
	require.Equal(t, 9400, cacheKept+totalShifted)
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

func TestGatewayCacheAdjustCacheHitToTarget_DoesNotShiftHistoricalToleranceBuffer(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()

	atUpperBound, err := cache.AdjustCacheHitToTarget(ctx, "tolerance-upper", 31, 32, 9000, 50, 86400, 401, 10000, 9050)
	require.NoError(t, err)
	require.Zero(t, atUpperBound.ShiftedTokens)

	// 触发时回到目标需要 51 token，但本次只有 1 个缓存读取 token，因此最多只能划 1 个。
	crossed, err := cache.AdjustCacheHitToTarget(ctx, "tolerance-crossed", 31, 32, 9000, 50, 86400, 401, 1, 1)
	require.NoError(t, err)
	require.Equal(t, 1, crossed.ShiftedTokens)
	require.Equal(t, int64(10001), crossed.CumulativePromptTokens)
	require.Equal(t, int64(9050), crossed.CumulativeCacheReadTokens)
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
	// 150/140，超过 90.5% 触发线，划拨 5 token 后回到 90%。
	second, err := cache.adjustCacheHitToTargetAt(ctx, "decay-second", 41, 42, 9000, 50, 86400, 501, 100, 100, startedAt.Add(24*time.Hour))
	require.NoError(t, err)
	require.Equal(t, 5, second.ShiftedTokens)
	require.Equal(t, int64(150), second.CumulativePromptTokens)
	require.Equal(t, int64(135), second.CumulativeCacheReadTokens)
}
