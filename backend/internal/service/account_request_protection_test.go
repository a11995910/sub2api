package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func protectionTestAccount(id int64) *Account {
	return &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
}
func protectionTestService(t *testing.T, rdb *redis.Client, rpm int) *AccountTimeRotationService {
	t.Helper()
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Enabled = true
	if rpm > 0 {
		cfg.RequestLimits = []AccountRequestLimit{{AccountID: 1, RPM: rpm}}
	}
	svc := NewAccountTimeRotationService(nil)
	svc.protectionRedis = rdb
	svc.publish(cfg, time.Now())
	return svc
}

func TestAccountProtectionStrictRestBoundaries(t *testing.T) {
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Enabled = true
	cfg.StrictRest = true
	cfg.Slots[0] = AccountRotationSlot{Start: "22:30", End: "06:15", AccountIDs: []int64{1}, ActivePriority: 1, InactivePriority: 50}
	root := protectionTestAccount(1)
	shadow := protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	for _, mode := range []string{"manual", "smart"} {
		cfg.Mode = mode
		cfg.Smart.AccountIDs = []int64{1}
		require.NoError(t, cfg.Validate())
		for _, tc := range []struct {
			hour, minute int
			allowed      bool
		}{{22, 29, false}, {22, 30, true}, {0, 0, true}, {6, 14, true}, {6, 15, false}} {
			now := time.Date(2026, 10, 23, tc.hour, tc.minute, 0, 0, accountRotationLocation)
			svc := NewAccountTimeRotationService(nil)
			svc.publish(cfg, now)
			for _, a := range []*Account{root, shadow} {
				require.Equal(t, tc.allowed, svc.CheckRequestProtection(a, now) == nil)
			}
			require.NoError(t, svc.CheckRequestProtection(protectionTestAccount(3), now))
		}
	}
}

func TestAccountProtectionConfigurationAndFailureModes(t *testing.T) {
	cfg := DefaultAccountTimeRotationConfig()
	cfg.StrictRest = true
	cfg.Slots[0].AccountIDs = []int64{1}
	cfg.Slots[0].End = "24:00"
	require.Error(t, cfg.Validate())
	cfg.StrictRest = false
	cfg.RequestLimits = []AccountRequestLimit{{1, 10}, {1, 20}}
	require.Error(t, cfg.Validate())
	cfg.RequestLimits = []AccountRequestLimit{{1, 0}}
	require.Error(t, cfg.Validate())
	svc := protectionTestService(t, nil, 1)
	require.True(t, isAccountProtectionError(svc.AdmitRequest(context.Background(), protectionTestAccount(1))))
	require.NoError(t, svc.AdmitRequest(context.Background(), protectionTestAccount(9)))
	svc.refreshedAt = time.Now().Add(-46 * time.Second)
	require.Error(t, svc.AdmitRequest(context.Background(), protectionTestAccount(1)))
	require.Error(t, NewAccountTimeRotationService(nil).AdmitRequest(context.Background(), protectionTestAccount(1)))
	cfg = DefaultAccountTimeRotationConfig()
	svc.publish(cfg, time.Now())
	require.NoError(t, svc.AdmitRequest(context.Background(), protectionTestAccount(1)))
	cfg.Enabled = true
	cfg.RequestLimits = []AccountRequestLimit{{1, 10}}
	cloned := cloneAccountTimeRotationConfig(cfg)
	cloned.RequestLimits[0].RPM = 20
	require.Equal(t, 10, cfg.RequestLimits[0].RPM)
}

func TestAccountProtectionAtomicSharedRollingWindow(t *testing.T) {
	mr := miniredis.RunT(t)
	now := time.Now()
	mr.SetTime(now)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	instances := []*AccountTimeRotationService{protectionTestService(t, rdb, 7), protectionTestService(t, rdb, 7)}
	root := protectionTestAccount(1)
	shadow := protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a := root
			if i%2 == 1 {
				a = shadow
			}
			if instances[i%2].AdmitRequest(context.Background(), a) == nil {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	require.EqualValues(t, 7, accepted.Load())
	require.NoError(t, instances[0].AdmitRequest(context.Background(), protectionTestAccount(9)))
	mr.SetTime(now.Add(59 * time.Second))
	require.Error(t, protectionTestService(t, rdb, 7).AdmitRequest(context.Background(), root))
	mr.SetTime(now.Add(60 * time.Second))
	require.NoError(t, protectionTestService(t, rdb, 7).AdmitRequest(context.Background(), root))
	state := instances[0].ProtectionStatus(context.Background(), instances[0].snapshot, root, time.Now())
	require.NotNil(t, state.Used)
	require.Equal(t, 1, *state.Used)
	// 降低上限不清空已有窗口，修改配置也不能返还已用次数。
	require.Error(t, protectionTestService(t, rdb, 1).AdmitRequest(context.Background(), root))
	mr.Close()
	require.Error(t, protectionTestService(t, rdb, 7).AdmitRequest(context.Background(), root))
}

type protectionAccountRepo struct {
	AccountRepository
	account *Account
}

func (r protectionAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

type protectionHTTP struct {
	calls   int
	failure error
}

func (h *protectionHTTP) Do(*http.Request, string, int64, int) (*http.Response, error) {
	h.calls++
	if h.failure != nil {
		return nil, h.failure
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("data: 完成\n\n"))}, nil
}
func (h *protectionHTTP) DoWithTLS(r *http.Request, p string, id int64, c int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return h.Do(r, p, id, c)
}

func TestAccountProtectionHTTPAttemptsAndExistingBody(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()
	svc := protectionTestService(t, rdb, 2)
	root := protectionTestAccount(1)
	shadow := protectionTestAccount(2)
	shadow.ParentAccountID = &root.ID
	inner := &protectionHTTP{}
	httpClient := NewProtectedHTTPUpstream(inner, svc, protectionAccountRepo{account: shadow})
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid/v1/responses", nil)
	response, err := httpClient.DoWithTLS(req, "", 2, 1, nil)
	require.NoError(t, err)
	inner.failure = errors.New("模拟上游断开")
	_, err = httpClient.Do(req, "", 2, 1)
	require.ErrorIs(t, err, inner.failure)
	err = svc.waitAdmitRequest(context.Background(), shadow, 20*time.Millisecond)
	require.True(t, isAccountProtectionError(err))
	require.Equal(t, 2, inner.calls)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, err, &failure)
	require.False(t, failure.ShouldReportAccountScheduleFailure())
	require.Equal(t, 429, failure.ClientStatusCode)
	// 下班后只拒绝新请求，已经取得的 SSE 响应体仍可读取和关闭。
	cfg := cloneAccountTimeRotationConfig(svc.snapshot)
	cfg.StrictRest = true
	cfg.Revision++
	minute := time.Now().In(accountRotationLocation).Hour()*60 + time.Now().In(accountRotationLocation).Minute()
	start := (minute + 60) % 1440
	end := (minute + 120) % 1440
	cfg.Slots[0] = AccountRotationSlot{Start: time.Date(2026, 1, 1, start/60, start%60, 0, 0, time.UTC).Format("15:04"), End: time.Date(2026, 1, 1, end/60, end%60, 0, 0, time.UTC).Format("15:04"), AccountIDs: []int64{1}, ActivePriority: 1, InactivePriority: 50}
	svc.publish(cfg, time.Now())
	_, err = httpClient.Do(req, "", 2, 1)
	require.Error(t, err)
	require.Equal(t, 2, inner.calls)
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Contains(t, string(data), "完成")
	require.NoError(t, response.Body.Close())
}

func TestAccountProtectionLaterTurnCannotReplayFirstTurn(t *testing.T) {
	err := accountProtectionError("account_resting", "账号休息中", 60)
	later := newOpenAIWSCurrentTurnFailoverError(err, nil)
	wrapped := wrapOpenAIWSIngressTurnError("write_upstream", later, false)
	payload, current := OpenAIWSCurrentTurnRetryPayload(wrapped)
	require.True(t, current)
	require.Empty(t, payload)
	require.False(t, isOpenAIWSIngressTurnRetryable(wrapped))
	svc := &OpenAIGatewayService{}
	require.False(t, svc.ReportOpenAIAccountScheduleResult(protectionTestAccount(1), "test", false, nil, wrapped))
}

func TestAccountProtectionPluginCannotBypassInternalAttempts(t *testing.T) {
	svc := protectionTestService(t, nil, 1)
	manager := NewPluginManager(nil, nil, nil, PluginHostInfo{}, nil)
	manager.requestProtection = svc
	manager.route.Store(&pluginRoute{rolloutPercent: 100})
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	_, handled, err := manager.RoundTripOpenAIOAuth(context.Background(), req, "", protectionTestAccount(1))
	require.True(t, handled)
	require.True(t, isAccountProtectionError(err))
	require.NoError(t, svc.checkPluginRequestProtection(protectionTestAccount(2)))
}

func TestAccountProtectionStickySelectionCannotUseRestingAccount(t *testing.T) {
	for _, batch := range []bool{false, true} {
		rotation, accounts, _, _ := smartSchedulingFixture()
		cfg := DefaultAccountTimeRotationConfig()
		cfg.Enabled = true
		cfg.StrictRest = true
		local := time.Now().In(accountRotationLocation)
		start := local.Add(time.Hour)
		end := local.Add(2 * time.Hour)
		cfg.Slots[0] = AccountRotationSlot{Start: start.Format("15:04"), End: end.Format("15:04"), AccountIDs: []int64{1}, ActivePriority: 1, InactivePriority: 50}
		rotation.publish(cfg, time.Now())
		gatewayCfg := &config.Config{}
		gatewayCfg.Gateway.Scheduling.LoadBatchEnabled = batch
		svc := &OpenAIGatewayService{cfg: gatewayCfg, accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}), cache: &schedulerTestGatewayCache{}, accountTimeRotation: rotation}
		require.NoError(t, svc.setStickySessionAccountID(context.Background(), nil, "固定会话", 1, time.Hour))
		result, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "固定会话", "gpt-5.1", nil)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEqualValues(t, 1, result.Account.ID)
		if result.ReleaseFunc != nil {
			result.ReleaseFunc()
		}
		// 选中的账号进入休息后，运行时和发送层均拒绝，不依赖优先级刷新。
		cfg.Revision++
		cfg.Slots[0].AccountIDs = append(cfg.Slots[0].AccountIDs, result.Account.ID)
		rotation.publish(cfg, time.Now())
		require.True(t, svc.isOpenAIAccountRequestRuntimeBlocked(result.Account, "gpt-5.1"))
		require.Error(t, rotation.AdmitRequest(context.Background(), result.Account))
	}
}
