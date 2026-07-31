//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

// rpmUserRepoStub 复用 admin_service_update_balance_test.go 的基础 stub 结构，
// 只在 Update 时把入参克隆一份，便于断言修改后的 RPMLimit。
type rpmUserRepoStub struct {
	*userRepoStub
	lastUpdated *User
}

func (s *rpmUserRepoStub) Update(_ context.Context, user *User, _ UserUpdateFields) error {
	if user == nil {
		return nil
	}
	clone := *user
	s.lastUpdated = &clone
	if s.userRepoStub != nil {
		s.userRepoStub.user = &clone
	}
	return nil
}

func TestAdminService_UpdateUser_InvalidatesAuthCacheOnRPMLimitChange(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "u@example.com", RPMLimit: 10}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}

	newRPM := 60
	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
		RPMLimit: &newRPM,
	})
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Equal(t, 60, updated.RPMLimit)
	require.Equal(t, []int64{42}, invalidator.userIDs, "仅修改 RPMLimit 也应失效 API Key 认证缓存")
}

func TestAdminService_UpdateUser_NoInvalidateWhenRPMLimitUnchanged(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "u@example.com", RPMLimit: 10, Username: "old"}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo:             repo,
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}

	newName := "new"
	sameRPM := 10
	_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{
		Username: &newName,
		RPMLimit: &sameRPM,
	})
	require.NoError(t, err)
	require.Empty(t, invalidator.userIDs, "只改 username 不应触发认证缓存失效")
}

func TestAdminService_UpdateUser_SyncsPublicGroupBlacklist(t *testing.T) {
	base := &userRepoStub{user: &User{ID: 42, Email: "u@example.com", BlockedGroups: []int64{3}}}
	repo := &rpmUserRepoStub{userRepoStub: base}
	invalidator := &authCacheInvalidatorStub{}
	svc := &adminServiceImpl{
		userRepo: repo,
		groupRepo: &groupRepoStub{groupsByID: map[int64]*Group{
			7: {ID: 7, SubscriptionType: SubscriptionTypeStandard},
		}},
		redeemCodeRepo:       &redeemRepoStub{},
		authCacheInvalidator: invalidator,
	}
	blockedGroups := []int64{7, 7}

	updated, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{BlockedGroups: &blockedGroups})

	require.NoError(t, err)
	require.Equal(t, []int64{7}, updated.BlockedGroups)
	require.Equal(t, []int64{42}, invalidator.userIDs)
}

func TestAdminService_UpdateUser_RejectsNonPublicBlockedGroup(t *testing.T) {
	for name, group := range map[string]*Group{
		"专属标准分组": {ID: 7, IsExclusive: true, SubscriptionType: SubscriptionTypeStandard},
		"订阅分组":   {ID: 7, SubscriptionType: SubscriptionTypeSubscription},
	} {
		t.Run(name, func(t *testing.T) {
			svc := &adminServiceImpl{
				userRepo:  &userRepoStub{user: &User{ID: 42, Email: "u@example.com"}},
				groupRepo: &groupRepoStub{groupsByID: map[int64]*Group{7: group}},
			}
			blockedGroups := []int64{7}

			_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{BlockedGroups: &blockedGroups})

			require.Error(t, err)
			require.Equal(t, "INVALID_BLOCKED_GROUP", infraerrors.Reason(err))
		})
	}
}

func TestAdminService_UpdateUser_PropagatesBlockedGroupRepositoryError(t *testing.T) {
	repoErr := errors.New("database unavailable")
	svc := &adminServiceImpl{
		userRepo:  &userRepoStub{user: &User{ID: 42, Email: "u@example.com"}},
		groupRepo: &groupRepoStub{getByIDLiteErr: repoErr},
	}
	blockedGroups := []int64{7}

	_, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{BlockedGroups: &blockedGroups})

	require.ErrorIs(t, err, repoErr)
	require.Empty(t, infraerrors.Reason(err))
}
