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

	require.NoError(t, cache.SetOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro", service.OpenAIVideoRequestProfileLegacy, service.OpenAIVideoProtocolVideos, time.Minute))
	protocol, err := cache.GetOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro", service.OpenAIVideoRequestProfileLegacy)
	require.NoError(t, err)
	require.Equal(t, service.OpenAIVideoProtocolVideos, protocol)

	keys := mr.Keys()
	require.Len(t, keys, 1)
	require.NotContains(t, keys[0], "jing-video-2-pro")
	require.Greater(t, mr.TTL(keys[0]), time.Duration(0))

	require.NoError(t, cache.DeleteOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro", service.OpenAIVideoRequestProfileLegacy))
	_, err = cache.GetOpenAIVideoProtocol(ctx, 42, "jing-video-2-pro", service.OpenAIVideoRequestProfileLegacy)
	require.ErrorIs(t, err, redis.Nil)
}

func TestGatewayCacheOpenAIVideoProtocolSeparatesRequestProfiles(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}
	ctx := context.Background()

	require.NoError(t, cache.SetOpenAIVideoProtocol(ctx, 42, "model", service.OpenAIVideoRequestProfileLegacy, service.OpenAIVideoProtocolChatCompletions, time.Minute))
	require.NoError(t, cache.SetOpenAIVideoProtocol(ctx, 42, "model", service.OpenAIVideoRequestProfileUnifiedJSON, service.OpenAIVideoProtocolVideos, time.Minute))

	legacy, err := cache.GetOpenAIVideoProtocol(ctx, 42, "model", service.OpenAIVideoRequestProfileLegacy)
	require.NoError(t, err)
	unified, err := cache.GetOpenAIVideoProtocol(ctx, 42, "model", service.OpenAIVideoRequestProfileUnifiedJSON)
	require.NoError(t, err)
	require.Equal(t, service.OpenAIVideoProtocolChatCompletions, legacy)
	require.Equal(t, service.OpenAIVideoProtocolVideos, unified)
	require.Len(t, mr.Keys(), 2)
}

func TestGatewayCacheOpenAIVideoProtocolRejectsInvalidValue(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	cache := &gatewayCache{rdb: client}

	err := cache.SetOpenAIVideoProtocol(context.Background(), 42, "model", service.OpenAIVideoRequestProfileLegacy, service.OpenAIVideoProtocol("invalid"), time.Minute)
	require.ErrorContains(t, err, "invalid video protocol")
}
