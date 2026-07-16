package service

import "time"

// CheckinSettings 是用户侧签到运行时配置。
type CheckinSettings struct {
	Enabled       bool    `json:"enabled"`
	Content       string  `json:"content"`
	DailyReward   float64 `json:"daily_reward"`
	ExtraReward4  float64 `json:"extra_reward_4"`
	ExtraReward16 float64 `json:"extra_reward_16"`
}

// CheckinRecord 表示一次实际签到记录。
type CheckinRecord struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	CheckinDate      time.Time `json:"checkin_date"`
	DailyReward      float64   `json:"daily_reward"`
	ExtraReward      float64   `json:"extra_reward"`
	MonthCount       int       `json:"month_count"`
	ConsecutiveCount int       `json:"consecutive_count"`
	ExtraMilestones  []int     `json:"extra_milestones"`
	CheckedInAt      time.Time `json:"checked_in_at"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type CheckinMonthSummary struct {
	Year                int             `json:"year"`
	Month               int             `json:"month"`
	Today               string          `json:"today"`
	TodayChecked        bool            `json:"today_checked"`
	MonthCount          int             `json:"month_count"`
	ConsecutiveCount    int             `json:"consecutive_count"`
	ConsecutiveCycleDay int             `json:"consecutive_cycle_day"`
	DaysInMonth         int             `json:"days_in_month"`
	Records             []CheckinRecord `json:"records"`
	NextExtraMilestone  *int            `json:"next_extra_milestone,omitempty"`
}

type CheckinOverview struct {
	Settings CheckinSettings     `json:"settings"`
	Summary  CheckinMonthSummary `json:"summary"`
}

type CheckinResult struct {
	Record     CheckinRecord       `json:"record"`
	Summary    CheckinMonthSummary `json:"summary"`
	Reward     float64             `json:"reward"`
	NewBalance float64             `json:"new_balance"`
}
