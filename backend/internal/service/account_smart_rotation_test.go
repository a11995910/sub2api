package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func smartRotationTestConfig(ids ...int64) *AccountTimeRotationConfig {
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Enabled, cfg.Mode, cfg.Smart.AccountIDs = true, "smart", ids
	return cfg
}

func smartRotationTestAccount(id int64) *Account {
	return &Account{ID: id, Name: "测试账号", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Priority: 7}
}

func TestSmartRotationHealthLifecycle(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1)
	svc := NewAccountTimeRotationService(nil)
	svc.publish(cfg, now)
	a := smartRotationTestAccount(1)
	observe := func(ms int, at time.Time) { svc.publish(cfg, at); svc.ObserveHealth(a, true, &ms, 25*time.Second, at) }
	state := func(at time.Time) AccountSmartRotationAccount {
		svc.publish(cfg, at)
		return svc.HealthPlan(cfg, []*Account{a}, at).Accounts[0]
	}
	observe(20000, now)
	observe(21000, now.Add(time.Second))
	require.Equal(t, "normal", state(now.Add(time.Second)).Role)
	observe(20000, now.Add(2*time.Second))
	cooling := state(now.Add(2 * time.Second))
	require.Equal(t, "cooling", cooling.Role)
	require.Equal(t, 50, cooling.EffectivePriority)
	require.Equal(t, 7, a.Priority)
	require.Equal(t, int64(25000), *cooling.LastDurationMs)
	until := now.Add(30*time.Minute + 2*time.Second)
	require.Equal(t, until, *cooling.CooldownUntil)
	observe(1000, until.Add(-time.Second))
	require.Equal(t, "cooling", state(until.Add(-time.Second)).Role, "等待期的绿色不能提前恢复")
	require.Equal(t, "recovering", state(until).Role)
	require.Equal(t, "recovering", state(until.Add(time.Minute)).Role, "没有真实请求不能凭时间恢复")
	for i := 0; i < 3; i++ {
		observe(1000, until.Add(time.Duration(i+1)*time.Minute))
	}
	require.Equal(t, "normal", state(until.Add(3*time.Minute)).Role)
	require.Equal(t, 7, state(until.Add(3*time.Minute)).EffectivePriority)
}

func TestSmartRotationWindowFailuresAndReentry(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1)
	svc := NewAccountTimeRotationService(nil)
	a := smartRotationTestAccount(1)
	observe := func(success bool, ms *int, at time.Time) {
		svc.publish(cfg, at)
		svc.ObserveHealth(a, success, ms, 0, at)
	}
	slow, green := 20000, 1000
	observe(true, &slow, now)
	observe(true, &slow, now.Add(time.Minute))
	observe(true, &slow, now.Add(10*time.Minute))
	require.Equal(t, 1, svc.health[1].SlowStreak, "旧窗口的慢样本不能累计")
	observe(true, &green, now.Add(11*time.Minute))
	require.Zero(t, svc.health[1].SlowStreak)
	observe(true, &slow, now.Add(12*time.Minute))
	observe(false, nil, now.Add(13*time.Minute))
	require.Zero(t, svc.health[1].SlowStreak)
	for i := 0; i < 3; i++ {
		observe(true, &slow, now.Add(time.Duration(14+i)*time.Minute))
	}
	until := svc.health[1].CooldownUntil
	observe(true, &green, until)
	observe(true, nil, until.Add(time.Minute))
	require.Zero(t, svc.health[1].HealthyStreak, "缺失首字不能冒充绿色")
	for i := 0; i < 3; i++ {
		observe(true, &slow, until.Add(time.Duration(i+2)*time.Minute))
	}
	require.Equal(t, "cooling", svc.health[1].State)
	require.Equal(t, until.Add(34*time.Minute), svc.health[1].CooldownUntil)
}

func TestSmartRotationProbeConcurrencyAndInterval(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1)
	svc := NewAccountTimeRotationService(nil)
	svc.publish(cfg, now)
	svc.health[1] = &AccountSmartRotationHealth{State: "recovering"}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var releases []func()
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, ok := svc.acquireProbe(1, now)
			if ok {
				mu.Lock()
				releases = append(releases, release)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Len(t, releases, 1, "并发选择只能放行一个观察请求")
	releases[0]()
	releases[0]()
	svc.publish(cfg, now.Add(59*time.Second))
	_, ok := svc.acquireProbe(1, now.Add(59*time.Second))
	require.False(t, ok)
	svc.publish(cfg, now.Add(time.Minute))
	release, ok := svc.acquireProbe(1, now.Add(time.Minute))
	require.True(t, ok)
	release()
}

func TestSmartRotationPlanEligibilityAndPriority(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1, 2, 3)
	svc := NewAccountTimeRotationService(nil)
	svc.publish(cfg, now)
	a := smartRotationTestAccount(1)
	a.Priority = 70
	b := smartRotationTestAccount(2)
	b.Schedulable = false
	svc.health[1] = &AccountSmartRotationHealth{State: "cooling", CooldownUntil: now.Add(time.Minute)}
	plan := svc.HealthPlan(cfg, []*Account{a, b}, now)
	require.Equal(t, 70, plan.Accounts[0].EffectivePriority, "不能把原低优先级账号提权到50")
	require.Equal(t, "unavailable", plan.Accounts[1].Role)
	require.Equal(t, "unavailable", plan.Accounts[2].Role)
	require.NotContains(t, plan.Tiers, int64(2))
	require.NotContains(t, plan.Tiers, int64(3))
	a.Priority = 9
	svc.health[1].State = "normal"
	require.Equal(t, 9, svc.HealthPlan(cfg, []*Account{a}, now).Accounts[0].EffectivePriority, "恢复使用管理员最新设置")
	cfg.Enabled = false
	cfg.Revision++
	svc.publish(cfg, now)
	require.Empty(t, svc.health)
	require.Nil(t, svc.HealthPlan(cfg, []*Account{a}, now))
}

type healthRepoStub struct {
	snapshotRepoStub
	saved *AccountSmartRotationHealthSnapshot
}

func (r *healthRepoStub) LoadHealth(context.Context) (*AccountSmartRotationHealthSnapshot, error) {
	return r.saved, nil
}
func (r *healthRepoStub) SaveHealth(_ context.Context, s *AccountSmartRotationHealthSnapshot) error {
	r.saved = s
	return nil
}

func TestSmartRotationHealthRestartAndRevision(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1)
	cfg.Revision = 4
	repo := &healthRepoStub{snapshotRepoStub: snapshotRepoStub{config: cfg}}
	svc := NewAccountTimeRotationService(repo)
	svc.publish(cfg, now)
	until := now.Add(30 * time.Minute)
	svc.health[1] = &AccountSmartRotationHealth{State: "cooling", CooldownUntil: until}
	require.NoError(t, svc.persistHealth(context.Background()))
	restarted := NewAccountTimeRotationService(repo)
	require.NoError(t, restarted.restoreHealth(context.Background(), cfg))
	restarted.publish(cfg, now)
	require.Equal(t, until, restarted.health[1].CooldownUntil)
	cfg.Revision++
	changed := NewAccountTimeRotationService(repo)
	require.NoError(t, changed.restoreHealth(context.Background(), cfg))
	changed.publish(cfg, now)
	require.Empty(t, changed.health, "历史配置的运行状态不能覆盖新配置")
}

func TestSmartRotationOnlySelectedOAuthAccounts(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1)
	svc := NewAccountTimeRotationService(nil)
	svc.publish(cfg, now)
	slow := 25000
	for _, a := range []*Account{smartRotationTestAccount(2), {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: new(int64)}} {
		for i := 0; i < 3; i++ {
			svc.ObserveHealth(a, true, &slow, 0, now)
		}
	}
	require.Empty(t, svc.health)
}

func TestSmartRotationPolicyValidationAndLegacyDefaults(t *testing.T) {
	legacy := &AccountTimeRotationConfig{Mode: "smart", Enabled: true, Smart: &AccountSmartRotationConfig{AccountIDs: []int64{1}, Periods: []AccountSmartRotationPeriod{{Start: "00:00", End: "24:00", PrimaryCount: 4}}, RotationMinutes: 60}}
	require.NoError(t, legacy.Validate())
	require.Equal(t, 20, legacy.Smart.TTFTThresholdSeconds)
	require.Equal(t, 30, legacy.Smart.CooldownMinutes)
	for name, edit := range map[string]func(*AccountSmartRotationConfig){
		"首字零阈值":   func(c *AccountSmartRotationConfig) { c.TTFTThresholdSeconds = 0 },
		"不能单次触发":  func(c *AccountSmartRotationConfig) { c.SlowRequestCount = 1 },
		"不能单次恢复":  func(c *AccountSmartRotationConfig) { c.HealthyRequestCount = 1 },
		"负等待":     func(c *AccountSmartRotationConfig) { c.CooldownMinutes = -1 },
		"等待溢出":    func(c *AccountSmartRotationConfig) { c.CooldownMinutes = 1441 },
		"负优先级":    func(c *AccountSmartRotationConfig) { c.WaitingPriority = -1 },
		"无效观察间隔":  func(c *AccountSmartRotationConfig) { c.ProbeIntervalSeconds = 0 },
		"窗口不足以恢复": func(c *AccountSmartRotationConfig) { c.SampleWindowMinutes = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := smartRotationTestConfig(1)
			edit(cfg.Smart)
			require.Error(t, cfg.Validate())
		})
	}
}

func TestSmartRotationForwardMetricsAndDisconnect(t *testing.T) {
	now := time.Now()
	cfg := smartRotationTestConfig(1)
	rotation := NewAccountTimeRotationService(nil)
	rotation.publish(cfg, now)
	svc := &OpenAIGatewayService{accountTimeRotation: rotation}
	a := smartRotationTestAccount(1)
	rotation.health[1] = &AccountSmartRotationHealth{State: "recovering", CooldownUntil: now.Add(-time.Hour)}
	green := 1000
	svc.ReportOpenAIAccountForwardResult(a, "gpt-5", true, &OpenAIForwardResult{FirstTokenMs: &green, Duration: time.Minute})
	require.Equal(t, 1, rotation.health[1].HealthyStreak)
	require.Equal(t, int64(60000), *rotation.health[1].LastDurationMs, "长回复不应单凭返回时间判慢")
	svc.ReportOpenAIAccountForwardResult(a, "gpt-5", true, &OpenAIForwardResult{FirstTokenMs: &green, Duration: time.Minute, ClientDisconnect: true})
	require.Zero(t, rotation.health[1].HealthyStreak)
	svc.ReportOpenAIAccountForwardResult(a, "gpt-5", true, &OpenAIForwardResult{Duration: time.Second})
	require.Zero(t, rotation.health[1].HealthyStreak)
	svc.ObserveOpenAIAccountHealthFailure(context.Background(), a, context.DeadlineExceeded)
	require.Zero(t, rotation.health[1].HealthyStreak)
}

func TestSmartRotationLegacyManualCanSwitchToHealth(t *testing.T) {
	cfg := DefaultAccountTimeRotationConfig()
	cfg.Smart = &AccountSmartRotationConfig{AccountIDs: []int64{1}, RotationMinutes: 60}
	require.NoError(t, cfg.Validate())
	require.Equal(t, 20, cfg.Smart.TTFTThresholdSeconds)
	cfg.Mode = "smart"
	cfg.Enabled = true
	require.NoError(t, cfg.Validate())
}

func TestSmartRotationActualFirstOutput(t *testing.T) {
	for _, tc := range []struct {
		name, payload     string
		managed, hasToken bool
	}{
		{"普通账号保留原首字口径", `{"type":"response.created","response":{"id":"r1"}}`, false, true},
		{"智能账号忽略初始化", `{"type":"response.created","response":{"id":"r1"}}`, true, false},
		{"智能账号忽略空增量", `{"type":"response.output_text.delta","delta":""}`, true, false},
		{"智能账号统计真实内容", `{"type":"response.output_text.delta","delta":"你好"}`, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			rotation := NewAccountTimeRotationService(nil)
			cfg := smartRotationTestConfig(1)
			cfg.Enabled = tc.managed
			rotation.publish(cfg, now)
			svc := &OpenAIGatewayService{accountTimeRotation: rotation}
			account := smartRotationTestAccount(1)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + tc.payload + "\n\n"))}
			result, err := svc.handleChatStreamingResponse(resp, c, account, "gpt-5", "gpt-5", "gpt-5", now.Add(-25*time.Second), 0)
			require.Error(t, err, "测试流故意省略终态，以检查首字样本")
			require.NotNil(t, result)
			if tc.hasToken {
				require.NotNil(t, result.FirstTokenMs)
				require.GreaterOrEqual(t, *result.FirstTokenMs, 25000)
			} else {
				require.Nil(t, result.FirstTokenMs)
			}
		})
	}
}

func TestSmartRotationUnavailableAccountsStayUnavailable(t *testing.T) {
	now := time.Now()
	future, past, parent := now.Add(time.Hour), now.Add(-time.Hour), int64(100)
	for name, edit := range map[string]func(*Account){
		"禁用":   func(a *Account) { a.Status = "disabled" },
		"停调":   func(a *Account) { a.Schedulable = false },
		"到期":   func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past },
		"过载":   func(a *Account) { a.OverloadUntil = &future },
		"限流":   func(a *Account) { a.RateLimitResetAt = &future },
		"临时冷却": func(a *Account) { a.TempUnschedulableUntil = &future },
		"影子账号": func(a *Account) { a.ParentAccountID = &parent },
		"其他平台": func(a *Account) { a.Platform = "anthropic" },
		"其他类型": func(a *Account) { a.Type = AccountTypeAPIKey },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := smartRotationTestConfig(1)
			svc := NewAccountTimeRotationService(nil)
			svc.publish(cfg, now)
			a := smartRotationTestAccount(1)
			edit(a)
			svc.health[1] = &AccountSmartRotationHealth{State: "recovering"}
			plan := svc.HealthPlan(cfg, []*Account{a}, now)
			require.Equal(t, "unavailable", plan.Accounts[0].Role)
			require.NotContains(t, plan.Tiers, int64(1))
		})
	}
}
