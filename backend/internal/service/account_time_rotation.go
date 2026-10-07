package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var errAccountTimeRotationRepositoryUnavailable = errors.New("account time rotation repository is unavailable")

// 时段轮候固定按北京时间每日循环，不依赖服务器时区或系统时区数据库。
var accountRotationLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type AccountRotationSlot struct {
	Start            string  `json:"start"`
	End              string  `json:"end"`
	AccountIDs       []int64 `json:"account_ids"`
	ActivePriority   int     `json:"active_priority"`
	InactivePriority int     `json:"inactive_priority"`
}

// AccountSmartRotationPeriod 描述智能轮候在一天中的一个非跨午夜时段。
type AccountSmartRotationPeriod struct {
	Start        string `json:"start"`
	End          string `json:"end"`
	PrimaryCount int    `json:"primary_count"`
}

type AccountSmartRotationConfig struct {
	TTFTThresholdSeconds int     `json:"ttft_threshold_seconds"`
	SlowRequestCount     int     `json:"slow_request_count"`
	HealthyRequestCount  int     `json:"healthy_request_count"`
	SampleWindowMinutes  int     `json:"sample_window_minutes"`
	CooldownMinutes      int     `json:"cooldown_minutes"`
	WaitingPriority      int     `json:"waiting_priority"`
	ProbeIntervalSeconds int     `json:"probe_interval_seconds"`
	AccountIDs           []int64 `json:"account_ids"`
	// 以下字段只用于读取历史配置，不再参与智能调度。
	Periods             []AccountSmartRotationPeriod `json:"periods"`
	RotationMinutes     int                          `json:"rotation_minutes"`
	QuotaReservePercent int                          `json:"quota_reserve_percent"`
}

type AccountTimeRotationConfig struct {
	Enabled  bool                        `json:"enabled"`
	Revision int64                       `json:"revision"`
	Slots    []AccountRotationSlot       `json:"slots"`
	Mode     string                      `json:"mode"`
	Smart    *AccountSmartRotationConfig `json:"smart,omitempty"`
}

func DefaultAccountTimeRotationConfig() *AccountTimeRotationConfig {
	return &AccountTimeRotationConfig{Mode: "manual", Smart: defaultSmartRotationConfig(), Slots: []AccountRotationSlot{
		{Start: "00:00", End: "08:00", AccountIDs: []int64{}, ActivePriority: 1, InactivePriority: 50},
		{Start: "08:00", End: "16:00", AccountIDs: []int64{}, ActivePriority: 1, InactivePriority: 50},
		{Start: "16:00", End: "24:00", AccountIDs: []int64{}, ActivePriority: 1, InactivePriority: 50},
	}}
}

func defaultSmartRotationConfig() *AccountSmartRotationConfig {
	return &AccountSmartRotationConfig{
		AccountIDs:           []int64{},
		TTFTThresholdSeconds: 20, SlowRequestCount: 3, HealthyRequestCount: 3,
		SampleWindowMinutes: 10, CooldownMinutes: 30, WaitingPriority: 50, ProbeIntervalSeconds: 60,
	}
}

func rotationMinute(value string, allowEndOfDay bool) (int, error) {
	if allowEndOfDay && value == "24:00" {
		return 1440, nil
	}
	if len(value) != 5 || value[2] != ':' {
		return 0, fmt.Errorf("时间必须使用 HH:mm 格式")
	}
	for _, i := range []int{0, 1, 3, 4} {
		if value[i] < '0' || value[i] > '9' {
			return 0, fmt.Errorf("时间必须使用 HH:mm 格式")
		}
	}
	hour, _ := strconv.Atoi(value[:2])
	minute, _ := strconv.Atoi(value[3:])
	if hour > 23 || minute > 59 {
		return 0, fmt.Errorf("时间超出有效范围")
	}
	return hour*60 + minute, nil
}

func (c *AccountTimeRotationConfig) Validate() error {
	invalid := func(message string) error { return infraerrors.BadRequest("INVALID_TIME_ROTATION", message) }
	if c.Mode == "" {
		c.Mode = "manual"
	}
	if c.Mode != "manual" && c.Mode != "smart" {
		return invalid("轮候模式必须是 manual 或 smart")
	}
	if c.Smart == nil {
		c.Smart = defaultSmartRotationConfig()
	}
	c.Smart.fillLegacyHealthPolicy()
	if c.Mode == "smart" {
		// 智能配置可以独立提交；保留默认手动时段，便于后续切回手动模式。
		if len(c.Slots) == 0 {
			c.Slots = DefaultAccountTimeRotationConfig().Slots
		}
		if c.Enabled && len(c.Smart.AccountIDs) == 0 {
			return invalid("智能轮候启用时必须选择至少一个账号")
		}
		seenSmart := make(map[int64]bool, len(c.Smart.AccountIDs))
		for _, id := range c.Smart.AccountIDs {
			if id <= 0 {
				return invalid("智能轮候账号 ID 无效")
			}
			if seenSmart[id] {
				return invalid(fmt.Sprintf("智能轮候账号 %d 重复选择", id))
			}
			seenSmart[id] = true
		}
		if err := c.Smart.validateHealthPolicy(); err != nil {
			return invalid(err.Error())
		}
	}
	if len(c.Slots) != 3 {
		return invalid("时段轮候必须设置 3 个时段")
	}
	seen := make(map[int64]bool)
	for i, slot := range c.Slots {
		if slot.AccountIDs == nil {
			c.Slots[i].AccountIDs = []int64{}
		}
		start, err := rotationMinute(slot.Start, false)
		if err != nil {
			return invalid(fmt.Sprintf("时段 %d 开始时间无效：%s", i+1, err))
		}
		end, err := rotationMinute(slot.End, true)
		if err != nil {
			return invalid(fmt.Sprintf("时段 %d 结束时间无效：%s", i+1, err))
		}
		if start == end {
			return invalid(fmt.Sprintf("时段 %d 的开始和结束时间不能相同", i+1))
		}
		if slot.ActivePriority < 1 || slot.ActivePriority > 2147483647 || slot.InactivePriority < 1 || slot.InactivePriority > 2147483647 {
			return invalid("优先级必须是 1 至 2147483647 的整数，数值越小越优先")
		}
		for _, id := range slot.AccountIDs {
			if id <= 0 {
				return invalid("账号 ID 无效")
			}
			if seen[id] {
				return invalid(fmt.Sprintf("账号 %d 重复选择，每个账号只能归属一个时段", id))
			}
			seen[id] = true
		}
	}
	return nil
}

// Priorities 使用左闭右开区间，支持跨午夜；不同账号组的时段允许重叠。
func (c *AccountTimeRotationConfig) Priorities(now time.Time) map[int64]int {
	result := make(map[int64]int)
	if !c.Enabled || c.Mode == "smart" {
		return result
	}
	local := now.In(accountRotationLocation)
	minute := local.Hour()*60 + local.Minute()
	for _, slot := range c.Slots {
		start, err := rotationMinute(slot.Start, false)
		if err != nil {
			continue
		}
		end, err := rotationMinute(slot.End, true)
		if err != nil {
			continue
		}
		active := minute >= start && minute < end
		if end < start {
			active = minute >= start || minute < end
		}
		priority := slot.InactivePriority
		if active {
			priority = slot.ActivePriority
		}
		for _, id := range slot.AccountIDs {
			result[id] = priority
		}
	}
	return result
}

type AccountTimeRotationRepository interface {
	Get(context.Context) (*AccountTimeRotationConfig, error)
	// Apply 在同一事务中串行化配置保存、优先级恢复和调度通知；nil 表示按现有配置轮候。
	Apply(context.Context, *AccountTimeRotationConfig, time.Time) (*AccountTimeRotationConfig, error)
}

type AccountTimeRotationService struct {
	repo        AccountTimeRotationRepository
	cancel      context.CancelFunc
	start       sync.Once
	stop        sync.Once
	wg          sync.WaitGroup
	snapshotMu  sync.RWMutex
	snapshot    *AccountTimeRotationConfig
	refreshedAt time.Time
	health      map[int64]*AccountSmartRotationHealth
	persistMu   sync.Mutex
}

func NewAccountTimeRotationService(repo AccountTimeRotationRepository) *AccountTimeRotationService {
	return &AccountTimeRotationService{repo: repo}
}

func (s *AccountTimeRotationService) Get(ctx context.Context) (*AccountTimeRotationConfig, error) {
	if s == nil || s.repo == nil {
		return nil, errAccountTimeRotationRepositoryUnavailable
	}
	config, err := s.repo.Get(ctx)
	if err != nil {
		return nil, err
	}
	return cloneAccountTimeRotationConfig(config), nil
}
func (s *AccountTimeRotationService) Save(ctx context.Context, config *AccountTimeRotationConfig) (*AccountTimeRotationConfig, error) {
	if config == nil {
		return nil, infraerrors.BadRequest("INVALID_TIME_ROTATION", "轮候配置不能为空")
	}
	if s == nil || s.repo == nil {
		return nil, errAccountTimeRotationRepositoryUnavailable
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	result, err := s.repo.Apply(ctx, config, time.Now())
	if err != nil {
		return nil, err
	}
	s.publish(result, time.Now())
	return cloneAccountTimeRotationConfig(result), nil
}

// Snapshot 返回最近一次成功应用的独立配置副本。读取配置不续期；超过 45 秒未成功应用时返回 nil。
func (s *AccountTimeRotationService) Snapshot(now time.Time) (*AccountTimeRotationConfig, time.Time) {
	s.snapshotMu.RLock()
	defer s.snapshotMu.RUnlock()
	if s.snapshot == nil || s.refreshedAt.IsZero() || now.Sub(s.refreshedAt) > 45*time.Second {
		return nil, s.refreshedAt
	}
	return cloneAccountTimeRotationConfig(s.snapshot), s.refreshedAt
}

func (s *AccountTimeRotationService) publish(config *AccountTimeRotationConfig, refreshedAt time.Time) {
	if config == nil {
		return
	}
	s.snapshotMu.Lock()
	defer s.snapshotMu.Unlock()
	if s.snapshot != nil && config.Revision < s.snapshot.Revision {
		return
	}
	s.reconcileHealthLocked(config)
	s.snapshot = cloneAccountTimeRotationConfig(config)
	s.refreshedAt = refreshedAt
}

func cloneAccountTimeRotationConfig(config *AccountTimeRotationConfig) *AccountTimeRotationConfig {
	if config == nil {
		return nil
	}
	copyConfig := *config
	copyConfig.Slots = make([]AccountRotationSlot, len(config.Slots))
	for i, slot := range config.Slots {
		copyConfig.Slots[i] = slot
		copyConfig.Slots[i].AccountIDs = append([]int64{}, slot.AccountIDs...)
	}
	if config.Smart != nil {
		smart := *config.Smart
		smart.AccountIDs = append([]int64{}, config.Smart.AccountIDs...)
		smart.Periods = append([]AccountSmartRotationPeriod(nil), config.Smart.Periods...)
		copyConfig.Smart = &smart
	}
	return &copyConfig
}

func (s *AccountTimeRotationService) Start() {
	if s == nil || s.repo == nil {
		slog.Error("时段轮候启动失败：repository 不可用")
		return
	}
	s.start.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			tick := time.NewTicker(15 * time.Second)
			defer tick.Stop()
			for {
				runCtx, runCancel := context.WithTimeout(ctx, 30*time.Second)
				result, err := s.repo.Apply(runCtx, nil, time.Now())
				runCancel()
				if err == nil {
					if loadErr := s.restoreHealth(ctx, result); loadErr != nil {
						slog.Error("智能轮候状态恢复失败", "error", loadErr)
					} else {
						s.publish(result, time.Now())
						if saveErr := s.persistHealth(ctx); saveErr != nil {
							slog.Error("智能轮候状态保存失败", "error", saveErr)
						}
					}
				}
				if err != nil && ctx.Err() == nil {
					slog.Error("时段轮候执行失败，将在下一轮重试", "error", err)
				}
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
				}
			}
		}()
	})
}

func (s *AccountTimeRotationService) Stop() {
	s.stop.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.persistHealth(ctx); err != nil {
			slog.Error("智能轮候退出时保存失败", "error", err)
		}
	})
}
