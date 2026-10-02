package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountTimeRotationBoundaries(t *testing.T) {
	config := DefaultAccountTimeRotationConfig()
	config.Enabled = true
	for i := range config.Slots {
		config.Slots[i].AccountIDs = []int64{int64(i + 1)}
	}
	for _, tc := range []struct {
		name, utc string
		expected  map[int64]int
	}{
		{"午夜", "2026-09-29T16:00:00Z", map[int64]int{1: 1, 2: 50, 3: 50}},
		{"第一段结束前", "2026-09-29T23:59:59Z", map[int64]int{1: 1, 2: 50, 3: 50}},
		{"八点切换", "2026-09-30T00:00:00Z", map[int64]int{1: 50, 2: 1, 3: 50}},
		{"十六点切换", "2026-09-30T08:00:00Z", map[int64]int{1: 50, 2: 50, 3: 1}},
		{"日末", "2026-09-30T15:59:59Z", map[int64]int{1: 50, 2: 50, 3: 1}},
		{"次日重复", "2026-09-30T16:00:00Z", map[int64]int{1: 1, 2: 50, 3: 50}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.utc)
			require.NoError(t, err)
			require.Equal(t, tc.expected, config.Priorities(now))
		})
	}
	config.Enabled = false
	require.Empty(t, config.Priorities(time.Now()))
}

func TestAccountTimeRotationCrossMidnightAndOverlap(t *testing.T) {
	config := DefaultAccountTimeRotationConfig()
	config.Enabled = true
	config.Slots[0] = AccountRotationSlot{Start: "22:30", End: "06:15", AccountIDs: []int64{1, 2}, ActivePriority: 3, InactivePriority: 80}
	config.Slots[1].AccountIDs = []int64{3}
	require.NoError(t, config.Validate())
	for _, tc := range []struct{ hour, minute, priority int }{{22, 29, 80}, {22, 30, 3}, {23, 59, 3}, {0, 0, 3}, {6, 14, 3}, {6, 15, 80}} {
		now := time.Date(2026, 9, 30, tc.hour, tc.minute, 0, 0, accountRotationLocation)
		require.Equal(t, tc.priority, config.Priorities(now)[1])
		require.Equal(t, tc.priority, config.Priorities(now)[2])
	}
	config.Slots[0].Start, config.Slots[0].End = "00:00", "24:00"
	require.NoError(t, config.Validate())
	require.Equal(t, 3, config.Priorities(time.Now())[1])
}

func TestAccountTimeRotationValidation(t *testing.T) {
	for name, change := range map[string]func(*AccountTimeRotationConfig){
		"少于三个时段":   func(c *AccountTimeRotationConfig) { c.Slots = c.Slots[:2] },
		"空时间":      func(c *AccountTimeRotationConfig) { c.Slots[0].Start = "" },
		"非标准时间":    func(c *AccountTimeRotationConfig) { c.Slots[0].Start = "8:00" },
		"开始不能为24点": func(c *AccountTimeRotationConfig) { c.Slots[0].Start = "24:00" },
		"无效分钟":     func(c *AccountTimeRotationConfig) { c.Slots[0].End = "08:60" },
		"相同起止":     func(c *AccountTimeRotationConfig) { c.Slots[0].End = "00:00" },
		"零优先级":     func(c *AccountTimeRotationConfig) { c.Slots[0].ActivePriority = 0 },
		"负优先级":     func(c *AccountTimeRotationConfig) { c.Slots[0].InactivePriority = -1 },
		"优先级溢出":    func(c *AccountTimeRotationConfig) { c.Slots[0].ActivePriority = 2147483648 },
		"无效账号":     func(c *AccountTimeRotationConfig) { c.Slots[0].AccountIDs = []int64{0} },
		"同段重复":     func(c *AccountTimeRotationConfig) { c.Slots[0].AccountIDs = []int64{1, 1} },
		"跨段重复": func(c *AccountTimeRotationConfig) {
			c.Slots[0].AccountIDs = []int64{1}
			c.Slots[1].AccountIDs = []int64{1}
		},
	} {
		t.Run(name, func(t *testing.T) { c := DefaultAccountTimeRotationConfig(); change(c); require.Error(t, c.Validate()) })
	}
	require.NoError(t, DefaultAccountTimeRotationConfig().Validate())
}

func TestSmartRotationDefaultsAndValidation(t *testing.T) {
	c := DefaultAccountTimeRotationConfig()
	require.Equal(t, "manual", c.Mode)
	require.Equal(t, 20, c.Smart.TTFTThresholdSeconds)
	require.Equal(t, 30, c.Smart.CooldownMinutes)
	c.Mode, c.Enabled = "smart", true
	c.Smart.AccountIDs = []int64{1, 2}
	require.NoError(t, c.Validate())
	require.Empty(t, c.Priorities(time.Now()))

	c.Smart.SlowRequestCount = 1
	require.Error(t, c.Validate())
	c = DefaultAccountTimeRotationConfig()
	c.Mode, c.Enabled = "smart", true
	c.Smart.AccountIDs = []int64{1, 1}
	require.Error(t, c.Validate())
	c.Smart.AccountIDs = []int64{1}
	c.Smart.SampleWindowMinutes = 1
	require.Error(t, c.Validate())
}

func TestSmartRotationAcceptsConfigurationWithoutManualSlots(t *testing.T) {
	c := &AccountTimeRotationConfig{
		Enabled: true,
		Mode:    "smart",
		Smart: &AccountSmartRotationConfig{
			AccountIDs:          []int64{1},
			Periods:             []AccountSmartRotationPeriod{{Start: "00:00", End: "24:00", PrimaryCount: 1}},
			RotationMinutes:     60,
			QuotaReservePercent: 10,
		},
	}
	require.NoError(t, c.Validate())
	require.Equal(t, DefaultAccountTimeRotationConfig().Slots, c.Slots)
	require.Empty(t, c.Priorities(time.Now()))
}

type snapshotRepoStub struct{ config *AccountTimeRotationConfig }

func (r *snapshotRepoStub) Get(context.Context) (*AccountTimeRotationConfig, error) {
	return cloneAccountTimeRotationConfig(r.config), nil
}
func (r *snapshotRepoStub) Apply(context.Context, *AccountTimeRotationConfig, time.Time) (*AccountTimeRotationConfig, error) {
	return cloneAccountTimeRotationConfig(r.config), nil
}

func TestAccountTimeRotationSnapshotIsFreshAndIsolated(t *testing.T) {
	c := DefaultAccountTimeRotationConfig()
	c.Revision = 2
	c.Slots[0].AccountIDs = []int64{1}
	c.Smart.AccountIDs = []int64{2}
	svc := NewAccountTimeRotationService(&snapshotRepoStub{config: c})
	got, err := svc.Save(context.Background(), c)
	require.NoError(t, err)
	got.Slots[0].AccountIDs[0] = 99
	got.Smart.AccountIDs[0] = 99
	got.Smart.HealthyRequestCount = 99
	snap, refreshed := svc.Snapshot(time.Now().Add(30 * time.Second))
	require.NotNil(t, snap)
	require.Equal(t, int64(2), snap.Revision)
	require.Equal(t, int64(1), snap.Slots[0].AccountIDs[0])
	require.Equal(t, int64(2), snap.Smart.AccountIDs[0])
	require.Equal(t, 3, snap.Smart.HealthyRequestCount)
	require.False(t, refreshed.IsZero())
	snap.Smart.AccountIDs[0] = 88
	boundary, _ := svc.Snapshot(refreshed.Add(45 * time.Second))
	require.NotNil(t, boundary)
	require.Equal(t, int64(2), boundary.Smart.AccountIDs[0])
	stale, last := svc.Snapshot(refreshed.Add(46 * time.Second))
	require.Nil(t, stale)
	require.Equal(t, refreshed, last)
	// 低版本发布不能覆盖当前快照。
	svc.publish(&AccountTimeRotationConfig{Revision: 1}, refreshed.Add(time.Second))
	snap, last = svc.Snapshot(refreshed.Add(2 * time.Second))
	require.Equal(t, int64(2), snap.Revision)
	require.Equal(t, refreshed, last)
}

func TestAccountTimeRotationGetDoesNotRenewRuntimeSnapshot(t *testing.T) {
	c := DefaultAccountTimeRotationConfig()
	c.Revision = 2
	svc := NewAccountTimeRotationService(&snapshotRepoStub{config: c})
	_, err := svc.Get(context.Background())
	require.NoError(t, err)
	snap, refreshed := svc.Snapshot(time.Now())
	require.Nil(t, snap)
	require.True(t, refreshed.IsZero())

	// 即使数据库读取得到更新版本，后台应用失败后的旧快照也不能被管理端访问续期。
	lastApplied := time.Now().Add(-time.Minute)
	previous := DefaultAccountTimeRotationConfig()
	previous.Revision = 1
	svc.publish(previous, lastApplied)
	got, err := svc.Get(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(2), got.Revision)
	snap, refreshed = svc.Snapshot(time.Now())
	require.Nil(t, snap)
	require.Equal(t, lastApplied, refreshed)
}

type rotationWorkerStub struct {
	applied chan struct{}
}

func (r *rotationWorkerStub) Get(context.Context) (*AccountTimeRotationConfig, error) {
	return DefaultAccountTimeRotationConfig(), nil
}
func (r *rotationWorkerStub) Apply(ctx context.Context, _ *AccountTimeRotationConfig, _ time.Time) (*AccountTimeRotationConfig, error) {
	select {
	case r.applied <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestAccountTimeRotationWorkerStartupAndShutdown(t *testing.T) {
	repo := &rotationWorkerStub{applied: make(chan struct{}, 1)}
	svc := NewAccountTimeRotationService(repo)
	svc.Start()
	svc.Start()
	select {
	case <-repo.applied:
	case <-time.After(time.Second):
		t.Fatal("启动后未立即恢复轮候")
	}
	stopped := make(chan struct{})
	go func() { svc.Stop(); svc.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("停止时未取消进行中的操作")
	}
}

// 前端会直接遍历账号数组，深拷贝不能把空数组变成 JSON null。
func TestAccountTimeRotationEmptyAccountsRemainJSONArrays(t *testing.T) {
	svc := NewAccountTimeRotationService(&snapshotRepoStub{config: DefaultAccountTimeRotationConfig()})
	config, err := svc.Get(context.Background())
	require.NoError(t, err)
	raw, err := json.Marshal(config)
	require.NoError(t, err)
	var payload struct {
		Slots []struct {
			AccountIDs []int64 `json:"account_ids"`
		} `json:"slots"`
		Smart struct {
			AccountIDs []int64 `json:"account_ids"`
		} `json:"smart"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	for _, slot := range payload.Slots {
		require.NotNil(t, slot.AccountIDs)
		require.Empty(t, slot.AccountIDs)
	}
	require.NotNil(t, payload.Smart.AccountIDs)
	require.Empty(t, payload.Smart.AccountIDs)
}
