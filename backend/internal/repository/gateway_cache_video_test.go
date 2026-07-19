package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestGatewayCacheOpenAIVideoProtocolRoundTripAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()

	require.NoError(t, cache.SetOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro", service.OpenAIVideoProtocolVideos, time.Minute))
	protocol, err := cache.GetOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro")
	require.NoError(t, err)
	require.Equal(t, service.OpenAIVideoProtocolVideos, protocol)

	keys := mr.Keys()
	require.Len(t, keys, 1)
	require.NotContains(t, keys[0], "jing-video-2-pro")
	require.Greater(t, mr.TTL(keys[0]), time.Duration(0))

	require.NoError(t, cache.DeleteOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro"))
	_, err = cache.GetOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro")
	require.ErrorIs(t, err, redis.Nil)
}

func TestGatewayCacheOpenAIVideoProtocolRejectsInvalidValue(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}

	err := cache.SetOpenAIVideoProtocol(context.Background(), 42, "model", service.OpenAIVideoProtocol("invalid"), time.Minute)
	require.ErrorContains(t, err, "invalid video protocol")
}
