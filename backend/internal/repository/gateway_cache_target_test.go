package repository

import (
	"context"
	"sync"
	"testing"

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

	shifted, err := cache.AdjustCacheHitToTarget(ctx, 1, 2, 9000, 100, 80)
	require.NoError(t, err)
	require.Zero(t, shifted)

	// 单次请求是 100% 命中，但与上一次累计后恰好为 90%，不应误降。
	shifted, err = cache.AdjustCacheHitToTarget(ctx, 1, 2, 9000, 100, 100)
	require.NoError(t, err)
	require.Zero(t, shifted)

	shifted, err = cache.AdjustCacheHitToTarget(ctx, 1, 2, 9000, 100, 100)
	require.NoError(t, err)
	require.Equal(t, int64(10), shifted)

	// 不同用户使用独立累计状态。
	shifted, err = cache.AdjustCacheHitToTarget(ctx, 3, 2, 9000, 100, 94)
	require.NoError(t, err)
	require.Equal(t, int64(4), shifted)
}

func TestGatewayCacheAdjustCacheHitToTarget_ConcurrentAtomicity(t *testing.T) {
	t.Parallel()
	cache := newCacheHitTargetTestCache(t)
	ctx := context.Background()

	const requests = 100
	var wg sync.WaitGroup
	shifts := make(chan int64, requests)
	errs := make(chan error, requests)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			shifted, err := cache.AdjustCacheHitToTarget(ctx, 11, 22, 9000, 100, 94)
			shifts <- shifted
			errs <- err
		}()
	}
	wg.Wait()
	close(shifts)
	close(errs)

	var totalShifted int64
	for err := range errs {
		require.NoError(t, err)
	}
	for shifted := range shifts {
		totalShifted += shifted
	}
	// 原始累计命中 9400/10000，精确移动 400 后得到 90%。
	require.Equal(t, int64(400), totalShifted)
}
