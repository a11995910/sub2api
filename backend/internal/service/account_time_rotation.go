package service

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// 时段轮候固定按北京时间每日循环，不依赖服务器时区或系统时区数据库。
var accountRotationLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

type AccountRotationSlot struct {
	Start            string  `json:"start"`
	End              string  `json:"end"`
	AccountIDs       []int64 `json:"account_ids"`
	ActivePriority   int     `json:"active_priority"`
	InactivePriority int     `json:"inactive_priority"`
}

type AccountTimeRotationConfig struct {
	Enabled  bool                  `json:"enabled"`
	Revision int64                 `json:"revision"`
	Slots    []AccountRotationSlot `json:"slots"`
}

func DefaultAccountTimeRotationConfig() *AccountTimeRotationConfig {
	return &AccountTimeRotationConfig{Slots: []AccountRotationSlot{
		{Start: "00:00", End: "08:00", AccountIDs: []int64{}, ActivePriority: 1, InactivePriority: 50},
		{Start: "08:00", End: "16:00", AccountIDs: []int64{}, ActivePriority: 1, InactivePriority: 50},
		{Start: "16:00", End: "24:00", AccountIDs: []int64{}, ActivePriority: 1, InactivePriority: 50},
	}}
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
	if !c.Enabled {
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
	repo   AccountTimeRotationRepository
	cancel context.CancelFunc
	start  sync.Once
	stop   sync.Once
	wg     sync.WaitGroup
}

func NewAccountTimeRotationService(repo AccountTimeRotationRepository) *AccountTimeRotationService {
	return &AccountTimeRotationService{repo: repo}
}

func (s *AccountTimeRotationService) Get(ctx context.Context) (*AccountTimeRotationConfig, error) {
	return s.repo.Get(ctx)
}
func (s *AccountTimeRotationService) Save(ctx context.Context, config *AccountTimeRotationConfig) (*AccountTimeRotationConfig, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return s.repo.Apply(ctx, config, time.Now())
}

func (s *AccountTimeRotationService) Start() {
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
				_, err := s.repo.Apply(runCtx, nil, time.Now())
				runCancel()
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
	})
}
