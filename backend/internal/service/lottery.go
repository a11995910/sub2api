package service

import (
	"context"
	"time"
)

type LotteryConfigSnapshot struct {
	Config    LotteryConfig
	Version   int64
	UpdatedAt time.Time
}

type LotteryPrizeInput struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	RewardAmount       float64 `json:"reward_amount"`
	ProbabilityPercent float64 `json:"probability_percent"`
}

type LotteryConfigInput struct {
	UsageThresholdM float64             `json:"usage_threshold_m"`
	AwardMode       LotteryAwardMode    `json:"award_mode"`
	Prizes          []LotteryPrizeInput `json:"prizes"`
}

type LotteryPrizeView struct {
	ID                 string  `json:"id"`
	Name               string  `json:"name"`
	RewardAmount       float64 `json:"reward_amount"`
	ProbabilityPercent float64 `json:"probability_percent"`
	IsThanks           bool    `json:"is_thanks"`
}

type LotteryConfigView struct {
	Enabled                  bool               `json:"enabled"`
	UsageThresholdM          float64            `json:"usage_threshold_m"`
	UsageThresholdTokens     int64              `json:"usage_threshold_tokens"`
	AwardMode                LotteryAwardMode   `json:"award_mode"`
	Prizes                   []LotteryPrizeView `json:"prizes"`
	ThanksProbabilityPercent float64            `json:"thanks_probability_percent"`
	Version                  int64              `json:"version"`
	UpdatedAt                time.Time          `json:"updated_at"`
}

type LotteryUserState struct {
	AvailableChances      int64            `json:"available_chances"`
	TotalEarned           int64            `json:"total_earned"`
	TotalDrawn            int64            `json:"total_drawn"`
	TodayUsageTokens      int64            `json:"today_usage_tokens"`
	TodayThreshold        int64            `json:"today_threshold_tokens"`
	TodayAwardMode        LotteryAwardMode `json:"today_award_mode"`
	TodayAwardedChances   int64            `json:"today_awarded_chances"`
	TodayNextTargetTokens int64            `json:"today_next_target_tokens"`
	TodayQualified        bool             `json:"today_qualified"`
}

type LotteryDraw struct {
	ID                     int64     `json:"id"`
	UserID                 int64     `json:"user_id"`
	UserEmail              string    `json:"user_email,omitempty"`
	PrizeID                string    `json:"prize_id"`
	PrizeName              string    `json:"prize_name"`
	RewardAmount           float64   `json:"reward_amount"`
	ProbabilityBasisPoints int       `json:"-"`
	ProbabilityPercent     float64   `json:"probability_percent"`
	RandomRoll             int       `json:"-"`
	ConfigVersion          int64     `json:"config_version"`
	ChanceBefore           int64     `json:"chance_before"`
	ChanceAfter            int64     `json:"chance_after"`
	BalanceAfter           float64   `json:"balance_after"`
	CreatedAt              time.Time `json:"created_at"`
}

type LotteryOverview struct {
	Config           LotteryConfigView `json:"config"`
	State            LotteryUserState  `json:"state"`
	TodayUsageM      float64           `json:"today_usage_m"`
	TodayNextTargetM float64           `json:"today_next_target_m"`
	TodayProgress    float64           `json:"today_progress_percent"`
	RecentDraws      []LotteryDraw     `json:"recent_draws"`
}

type LotteryDrawResult struct {
	Draw             LotteryDraw `json:"draw"`
	AvailableChances int64       `json:"available_chances"`
	NewBalance       float64     `json:"new_balance"`
}

type LotteryDrawPage struct {
	Items    []LotteryDraw `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"page_size"`
}

type LotteryRepository interface {
	GetConfig(ctx context.Context) (LotteryConfigSnapshot, error)
	SaveConfig(ctx context.Context, config LotteryConfig, updatedBy int64) (LotteryConfigSnapshot, error)
	ReconcileUserChances(ctx context.Context, userID int64) error
	GetUserState(ctx context.Context, userID int64) (LotteryUserState, error)
	CommitDraw(ctx context.Context, userID int64, selection LotterySelection, configVersion int64) (LotteryDraw, error)
	ListUserDraws(ctx context.Context, userID int64, limit int) ([]LotteryDraw, error)
	ListDraws(ctx context.Context, page, pageSize int) ([]LotteryDraw, int64, error)
}
