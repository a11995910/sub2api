//go:build unit

package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type videoBindingCacheStub struct {
	values map[string]int64
}

func (s *videoBindingCacheStub) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (s *videoBindingCacheStub) GetSessionAccountID(_ context.Context, groupID int64, sessionHash string) (int64, error) {
	value, ok := s.values[s.key(groupID, sessionHash)]
	if !ok {
		return 0, redis.Nil
	}
	return value, nil
}

func (s *videoBindingCacheStub) SetSessionAccountID(_ context.Context, groupID int64, sessionHash string, accountID int64, _ time.Duration) error {
	if s.values == nil {
		s.values = make(map[string]int64)
	}
	s.values[s.key(groupID, sessionHash)] = accountID
	return nil
}

func (s *videoBindingCacheStub) RefreshSessionTTL(context.Context, int64, string, time.Duration) error {
	return nil
}

func (s *videoBindingCacheStub) DeleteSessionAccountID(_ context.Context, groupID int64, sessionHash string) error {
	delete(s.values, s.key(groupID, sessionHash))
	return nil
}

func (s *videoBindingCacheStub) SetGrokVideoPendingBilling(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return nil
}

func (s *videoBindingCacheStub) GetGrokVideoPendingBilling(_ context.Context, _ string) ([]byte, error) {
	return nil, nil
}

func (s *videoBindingCacheStub) ClaimGrokVideoBilled(_ context.Context, _ string, _ time.Duration) (bool, error) {
	return true, nil
}

func (s *videoBindingCacheStub) ReleaseGrokVideoBilled(_ context.Context, _ string) error {
	return nil
}

func (s *videoBindingCacheStub) SetReasoningContent(_ context.Context, _ string, _ string, _ time.Duration) error {
	return nil
}

func (s *videoBindingCacheStub) GetReasoningContent(_ context.Context, _ string) (string, error) {
	return "", ErrReasoningContentNotFound
}

func TestVideoTaskSessionHashIsScopedToOwner(t *testing.T) {
	base := VideoTaskSessionHash("task-1", 10, 20)
	require.NotEmpty(t, base)
	require.NotEqual(t, base, VideoTaskSessionHash("task-1", 11, 20))
	require.NotEqual(t, base, VideoTaskSessionHash("task-1", 10, 21))
	require.Empty(t, VideoTaskSessionHash("", 10, 20))
}

func TestBindAndResolveVideoTaskAccount(t *testing.T) {
	cache := &videoBindingCacheStub{}
	svc := &OpenAIGatewayService{cache: cache}
	groupID := int64(7)
	ctx := context.Background()

	require.NoError(t, svc.BindVideoTaskAccount(ctx, &groupID, "task-1", 10, 20, 30))
	accountID, err := svc.ResolveVideoTaskAccount(ctx, &groupID, "task-1", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(30), accountID)

	_, err = svc.ResolveVideoTaskAccount(ctx, &groupID, "task-1", 11, 20)
	require.True(t, errors.Is(err, redis.Nil))
	_, err = svc.ResolveVideoTaskAccount(ctx, &groupID, "task-1", 10, 21)
	require.True(t, errors.Is(err, redis.Nil))
}

func TestResolveGrokMediaVideoRequestAccountFallsBackToLegacyBinding(t *testing.T) {
	cache := &videoBindingCacheStub{}
	svc := &OpenAIGatewayService{cache: cache}
	groupID := int64(7)
	ctx := context.Background()
	legacyHash := svc.openAISessionCacheKey(legacyGrokMediaVideoRequestSessionHash("task-old", 10, 20))
	require.NoError(t, cache.SetSessionAccountID(ctx, groupID, legacyHash, 30, time.Minute))

	accountID, err := svc.ResolveGrokMediaVideoRequestAccount(ctx, &groupID, "task-old", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(30), accountID)
}

func TestResolveVideoTaskAccountFallsBackToPersistentTestTaskAndRebindsCache(t *testing.T) {
	cache := &videoBindingCacheStub{}
	store := newMemoryVideoTestTaskStore()
	store.seed(VideoTestTask{
		ID: "local-1", UserID: 10, APIKeyID: 20, GroupID: 7, AccountID: 30,
		UpstreamTaskID: "task-persisted", Status: VideoTestTaskStatusQueued,
	})
	svc := &OpenAIGatewayService{cache: cache}
	svc.SetVideoTestTaskService(NewVideoTestTaskService(store))
	groupID := int64(7)

	accountID, err := svc.ResolveVideoTaskAccount(context.Background(), &groupID, "task-persisted", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(30), accountID)

	accountID, err = svc.ResolveVideoTaskAccount(context.Background(), &groupID, "task-persisted", 10, 20)
	require.NoError(t, err)
	require.Equal(t, int64(30), accountID)

	_, err = svc.ResolveVideoTaskAccount(context.Background(), &groupID, "task-persisted", 11, 20)
	require.Error(t, err)
}

func TestResolveVideoTaskAccountFallsBackToBillingLedgerBeforeModelTestStore(t *testing.T) {
	cache := &videoBindingCacheStub{}
	billing := NewVideoTaskBillingService(&fakeVideoTaskBillingRepo{task: &VideoTaskBilling{
		Platform: PlatformOpenAI, UpstreamTaskID: "task-billed", UserID: 10, APIKeyID: 20, GroupID: ptrVideoInt64(7), AccountID: 30,
	}}, nil, nil)
	svc := &OpenAIGatewayService{cache: cache}
	svc.SetVideoTaskBillingService(billing)
	groupID := int64(7)

	accountID, err := svc.ResolveVideoTaskAccount(context.Background(), &groupID, "task-billed", 10, 20)

	require.NoError(t, err)
	require.Equal(t, int64(30), accountID)
	require.NotEmpty(t, cache.values)
	_, err = svc.ResolveVideoTaskAccount(context.Background(), &groupID, "task-billed", 11, 20)
	require.Error(t, err)
}
