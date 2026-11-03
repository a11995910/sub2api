//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func preferredTestAccounts(platform string) []Account {
	return []Account{
		{ID: 1, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 5, Priority: 1, GroupIDs: []int64{10}, AccountGroups: []AccountGroup{{AccountID: 1, GroupID: 10}}},
		{ID: 2, Platform: platform, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 5, Priority: 100, GroupIDs: []int64{10}, AccountGroups: []AccountGroup{{AccountID: 2, GroupID: 10}}},
	}
}

func TestUserPreferredAccount_OpenAISchedulers(t *testing.T) {
	for _, advanced := range []string{"true", "false"} {
		for _, tc := range []struct {
			name     string
			id       int64
			mutate   func(*Account)
			excluded bool
			busy     bool
			sticky   int64
			want     int64
		}{
			{name: "同组优先", id: 2, want: 2},
			{name: "覆盖普通会话粘性", id: 2, sticky: 1, want: 2},
			{name: "未设置保持原调度", want: 1},
			{name: "已删除自动回退", id: 99, want: 1},
			{name: "其他分组不生效", id: 2, mutate: func(a *Account) { a.GroupIDs = []int64{20}; a.AccountGroups = nil }, want: 1},
			{name: "停用自动回退", id: 2, mutate: func(a *Account) { a.Status = StatusDisabled }, want: 1},
			{name: "不可调度自动回退", id: 2, mutate: func(a *Account) { a.Schedulable = false }, want: 1},
			{name: "模型不支持自动回退", id: 2, mutate: func(a *Account) {
				a.Credentials = map[string]any{"model_mapping": map[string]any{"other-model": "other-model"}}
			}, want: 1},
			{name: "限流自动回退", id: 2, mutate: func(a *Account) { until := time.Now().Add(time.Hour); a.RateLimitResetAt = &until }, want: 1},
			{name: "故障重试排除优先账号", id: 2, excluded: true, want: 1},
			{name: "并发满自动回退", id: 2, busy: true, want: 1},
			{name: "优先账号粘性且并发满仍回退", id: 2, busy: true, sticky: 2, want: 1},
		} {
			t.Run(advanced+"/"+tc.name, func(t *testing.T) {
				accounts := preferredTestAccounts(PlatformOpenAI)
				if tc.mutate != nil {
					tc.mutate(&accounts[1])
				}
				cache := &schedulerTestGatewayCache{sessionBindings: map[string]int64{}}
				if tc.sticky > 0 {
					cache.sessionBindings["openai:session"] = tc.sticky
				}
				cfg := newSchedulerTestSubscriptionPriorityConfig()
				cfg.Gateway.Scheduling.LoadBatchEnabled = true
				svc := &OpenAIGatewayService{
					accountRepo: schedulerGroupAwareOpenAIAccountRepo{schedulerTestOpenAIAccountRepo{accounts: accounts}},
					cache:       cache, cfg: cfg,
					rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService(advanced),
					concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{acquireResults: map[int64]bool{1: true, 2: !tc.busy}}),
				}
				groupID := int64(10)
				excluded := map[int64]struct{}{}
				if tc.excluded {
					excluded[2] = struct{}{}
				}
				selection, decision, err := svc.SelectAccountWithScheduler(WithUserPreferredAccount(context.Background(), tc.id), &groupID, "", "session", "gpt-5.1", excluded, OpenAIUpstreamTransportAny, false)
				require.NoError(t, err)
				require.NotNil(t, selection)
				require.Equal(t, tc.want, selection.Account.ID)
				require.True(t, selection.Acquired)
				if tc.want == 2 {
					require.Equal(t, "user_preferred", decision.Layer)
				}
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
				require.Equal(t, 100, accounts[1].Priority, "不修改共享账号优先级")
				require.Len(t, excluded, map[bool]int{true: 1, false: 0}[tc.excluded], "不修改调用方的排除列表")
			})
		}
	}
}

func TestUserPreferredAccount_PreservesRequiredBindings(t *testing.T) {
	svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: preferredTestAccounts(PlatformOpenAI)}}
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	groupID := int64(10)
	for _, req := range []OpenAIAccountScheduleRequest{
		{PreviousResponseID: "resp-original"}, {GuardianParentAccountID: 1}, {DisableStickyEscape: true}, {PreserveStickyBinding: true},
	} {
		req.GroupID = &groupID
		req.Platform = PlatformOpenAI
		selected, err := scheduler.selectUserPreferredAccount(WithUserPreferredAccount(context.Background(), 2), &req)
		require.NoError(t, err)
		require.Nil(t, selected)
	}
}

func TestUserPreferredAccount_Gateway(t *testing.T) {
	for _, loadBatch := range []bool{true, false} {
		for _, tc := range []struct {
			name                      string
			id                        int64
			outside, busy, restricted bool
			want                      int64
		}{
			{name: "同组优先", id: 2, want: 2}, {name: "未设置", want: 1},
			{name: "跨组忽略", id: 2, outside: true, want: 1},
			{name: "并发满回退", id: 2, busy: true, want: 1},
			{name: "渠道限制仍生效", id: 1, restricted: true, want: 2},
		} {
			t.Run(tc.name+map[bool]string{true: "/负载", false: "/旧版"}[loadBatch], func(t *testing.T) {
				f := newLoadAwareRestrictionFixture(t, tc.restricted, nil, nil)
				f.svc.cfg.Gateway.Scheduling.LoadBatchEnabled = loadBatch
				repo := f.svc.accountRepo.(*mockAccountRepoForPlatform)
				for i := range repo.accounts {
					repo.accounts[i].GroupIDs = []int64{10}
					repo.accounts[i].AccountGroups = []AccountGroup{{GroupID: 10}}
				}
				if tc.outside {
					repo.accounts[1].GroupIDs = []int64{20}
					repo.accounts[1].AccountGroups = []AccountGroup{{GroupID: 20}}
				}
				f.concurrencyCache.acquireResults = map[int64]bool{1: true, 2: !tc.busy}
				result, err := f.svc.SelectAccountWithLoadAwareness(WithUserPreferredAccount(f.ctx, tc.id), &f.groupID, "", "claude-fable-5-1", nil, "", 42)
				require.NoError(t, err)
				require.Equal(t, tc.want, result.Account.ID)
				if result.ReleaseFunc != nil {
					result.ReleaseFunc()
				}
			})
		}
	}
}

type preferredUserUpdateRepo struct {
	*rpmUserRepoStub
	fields UserUpdateFields
}

func (r *preferredUserUpdateRepo) Update(ctx context.Context, user *User, fields UserUpdateFields) error {
	r.fields = fields
	return r.rpmUserRepoStub.Update(ctx, user, fields)
}

type preferredAdminAccountRepo struct {
	AccountRepository
	err error
}

func (r preferredAdminAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.err != nil {
		return nil, r.err
	}
	if id != 2 {
		return nil, ErrAccountNotFound
	}
	return &Account{ID: id}, nil
}
func TestUserPreferredAccount_AdminUpdate(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		input                *int64
		old, want            int64
		invalid, repoFailure bool
	}{
		{name: "设置", input: preferredIDPtr(2), want: 2},
		{name: "清除", input: preferredIDPtr(0), old: 2},
		{name: "省略不变", old: 2, want: 2},
		{name: "相同值", input: preferredIDPtr(2), old: 2, want: 2},
		{name: "拒绝负数", input: preferredIDPtr(-1), invalid: true},
		{name: "拒绝已删账号", input: preferredIDPtr(99), invalid: true},
		{name: "数据库错误不上报为账号不存在", input: preferredIDPtr(2), repoFailure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &preferredUserUpdateRepo{rpmUserRepoStub: &rpmUserRepoStub{userRepoStub: &userRepoStub{user: &User{ID: 42, PreferredAccountID: tc.old}}}}
			invalidator := &authCacheInvalidatorStub{}
			accountRepo := preferredAdminAccountRepo{}
			if tc.repoFailure {
				accountRepo.err = errors.New("数据库不可用")
			}
			svc := &adminServiceImpl{userRepo: repo, accountRepo: accountRepo, authCacheInvalidator: invalidator}
			user, err := svc.UpdateUser(context.Background(), 42, &UpdateUserInput{PreferredAccountID: tc.input})
			if tc.invalid || tc.repoFailure {
				require.Error(t, err)
				require.Nil(t, repo.lastUpdated)
				if tc.invalid {
					require.Equal(t, "INVALID_PREFERRED_ACCOUNT", infraerrors.Reason(err))
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, user.PreferredAccountID)
			require.Equal(t, tc.input != nil, repo.fields.PreferredAccountID)
			require.False(t, repo.fields.RPMLimit)
			require.Equal(t, tc.old != tc.want, len(invalidator.userIDs) > 0)
		})
	}
}
func preferredIDPtr(id int64) *int64 { return &id }

func TestUserPreferredAccount_AuthSnapshotRoundTrip(t *testing.T) {
	svc := &APIKeyService{}
	for _, id := range []int64{0, 2} {
		key := &APIKey{ID: 7, UserID: 42, User: &User{ID: 42, PreferredAccountID: id}}
		snapshot := svc.snapshotFromAPIKey(context.Background(), key)
		result := svc.snapshotToAPIKey("test-key", snapshot)
		require.Equal(t, id, result.User.PreferredAccountID)
	}
}

func TestUserPreferredAccount_PreviousResponseOwnerWins(t *testing.T) {
	for _, advanced := range []string{"true", "false"} {
		t.Run(advanced, func(t *testing.T) {
			groupID := int64(10)
			accounts := preferredTestAccounts(PlatformOpenAI)
			for i := range accounts {
				accounts[i].Extra = map[string]any{"openai_apikey_responses_websockets_v2_enabled": true}
			}
			svc := newLegacySchedulerDecisionTestService(accounts, true, schedulerTestConcurrencyCache{})
			svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService(advanced)
			ctx := WithUserPreferredAccount(context.Background(), 2)
			require.NoError(t, svc.getOpenAIWSStateStore().BindResponseAccount(ctx, groupID, "resp_preferred_test", 1, time.Hour))
			selected, decision, err := svc.SelectAccountWithScheduler(ctx, &groupID, "resp_preferred_test", "session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, int64(1), selected.Account.ID)
			require.Equal(t, openAIAccountScheduleLayerPreviousResponse, decision.Layer)
			if selected.ReleaseFunc != nil {
				selected.ReleaseFunc()
			}
		})
	}
}

func TestUserPreferredAccount_GeminiAndBatchImage(t *testing.T) {
	groupID := int64(10)
	accounts := preferredTestAccounts(PlatformGemini)
	repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{1: &accounts[0], 2: &accounts[1]}}
	svc := &GeminiMessagesCompatService{accountRepo: repo, cache: &mockGatewayCacheForPlatform{}, groupRepo: &mockGroupRepoForGateway{groups: map[int64]*Group{10: {ID: 10, Platform: PlatformGemini}}}, cfg: testConfig()}
	selected, err := svc.SelectAccountForModelWithExclusions(WithUserPreferredAccount(context.Background(), 2), &groupID, "", "gemini-2.5-flash", nil)
	require.NoError(t, err)
	require.Equal(t, int64(2), selected.ID)
	selected, err = svc.SelectAccountForModelWithExclusions(WithUserPreferredAccount(context.Background(), 2), &groupID, "", "gemini-2.5-flash", map[int64]struct{}{2: {}})
	require.NoError(t, err)
	require.Equal(t, int64(1), selected.ID)

	batch, _, _, _, _ := newTestBatchImagePublicService(true)
	images := []Account{testBatchImageAccount(101, AccountTypeAPIKey), testBatchImageAccount(202, AccountTypeAPIKey)}
	images[0].Priority = 1
	images[1].Priority = 100
	for i := range images {
		images[i].GroupIDs = []int64{groupID}
	}
	batch.AccountRepo = &publicBatchImageAccountRepo{accounts: images}
	owner := testBatchImageOwner()
	owner.GroupID = &groupID
	_, selected, err = batch.selectProviderAndAccount(WithUserPreferredAccount(context.Background(), 202), owner, BatchImageProviderGeminiAPI, "gemini-2.5-flash-image")
	require.NoError(t, err)
	require.Equal(t, int64(202), selected.ID)
	images[1].Schedulable = false
	batch.AccountRepo = &publicBatchImageAccountRepo{accounts: images}
	_, selected, err = batch.selectProviderAndAccount(WithUserPreferredAccount(context.Background(), 202), owner, BatchImageProviderGeminiAPI, "gemini-2.5-flash-image")
	require.NoError(t, err)
	require.Equal(t, int64(101), selected.ID)
}
