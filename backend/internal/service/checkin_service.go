package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	apptimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

var (
	ErrCheckinDisabled       = infraerrors.BadRequest("CHECKIN_DISABLED", "daily check-in is disabled")
	ErrCheckinAlreadyChecked = infraerrors.Conflict("CHECKIN_ALREADY_CHECKED", "already checked in today")
)

type CheckinRepository interface {
	Create(ctx context.Context, record *CheckinRecord) error
	GetByUserAndDate(ctx context.Context, userID int64, date time.Time) (*CheckinRecord, error)
	ListByUserAndDateRange(ctx context.Context, userID int64, start, end time.Time) ([]CheckinRecord, error)
	CountByUserAndDateRange(ctx context.Context, userID int64, start, end time.Time) (int, error)
}

type checkinBalanceCreditRepository interface {
	AddBalance(ctx context.Context, id int64, amount float64) error
}

type CheckinService struct {
	repo                 CheckinRepository
	userRepo             UserRepository
	settingService       *SettingService
	entClient            *dbent.Client
	authCacheInvalidator APIKeyAuthCacheInvalidator
	billingCacheService  *BillingCacheService
	now                  func() time.Time
}

func NewCheckinService(
	repo CheckinRepository,
	userRepo UserRepository,
	settingService *SettingService,
	entClient *dbent.Client,
	authCacheInvalidator APIKeyAuthCacheInvalidator,
	billingCacheService *BillingCacheService,
) *CheckinService {
	return &CheckinService{
		repo:                 repo,
		userRepo:             userRepo,
		settingService:       settingService,
		entClient:            entClient,
		authCacheInvalidator: authCacheInvalidator,
		billingCacheService:  billingCacheService,
		now:                  time.Now,
	}
}

func (s *CheckinService) GetOverview(ctx context.Context, userID int64) (*CheckinOverview, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return nil, err
	}
	summary, err := s.buildMonthSummary(ctx, userID, s.today())
	if err != nil {
		return nil, err
	}
	return &CheckinOverview{Settings: settings, Summary: summary}, nil
}

func (s *CheckinService) Checkin(ctx context.Context, userID int64) (*CheckinResult, error) {
	settings, err := s.loadSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled || settings.DailyReward <= 0 {
		return nil, ErrCheckinDisabled
	}
	today := s.today()
	monthStart, monthEnd := monthBounds(today)

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin checkin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)

	existing, err := s.repo.GetByUserAndDate(txCtx, userID, today)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrCheckinAlreadyChecked
	}

	countBefore, err := s.repo.CountByUserAndDateRange(txCtx, userID, monthStart, monthEnd)
	if err != nil {
		return nil, fmt.Errorf("count monthly checkins: %w", err)
	}
	monthCount := countBefore + 1
	consecutiveCount, err := s.nextConsecutiveCheckinCount(txCtx, userID, today)
	if err != nil {
		return nil, fmt.Errorf("calculate consecutive checkins: %w", err)
	}
	backfillSixteenReward := false
	if settings.ExtraReward16 > 0 && consecutiveCount >= CheckinExtraMilestoneSecondDefault {
		backfillSixteenReward, err = s.shouldBackfillSixteenDayReward(txCtx, userID, today, consecutiveCount)
		if err != nil {
			return nil, fmt.Errorf("check historical sixteen-day reward: %w", err)
		}
	}
	rewardConsecutiveCount := checkinConsecutiveCountForReward(consecutiveCount, backfillSixteenReward)
	extraReward, milestones := checkinExtraReward(settings, rewardConsecutiveCount)
	record := &CheckinRecord{
		UserID:           userID,
		CheckinDate:      today,
		DailyReward:      settings.DailyReward,
		ExtraReward:      extraReward,
		MonthCount:       monthCount,
		ConsecutiveCount: rewardConsecutiveCount,
		ExtraMilestones:  milestones,
		CheckedInAt:      s.now().UTC(),
	}
	if err := s.repo.Create(txCtx, record); err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrCheckinAlreadyChecked
		}
		return nil, fmt.Errorf("create checkin record: %w", err)
	}
	totalReward := settings.DailyReward + extraReward
	if err := s.addRewardBalance(txCtx, userID, totalReward); err != nil {
		return nil, fmt.Errorf("update user balance: %w", err)
	}

	if err := tx.Commit(); err != nil {
		if isUniqueConstraintError(err) {
			return nil, ErrCheckinAlreadyChecked
		}
		return nil, fmt.Errorf("commit checkin tx: %w", err)
	}

	s.invalidateBalanceCaches(ctx, userID)
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get updated user: %w", err)
	}
	summary, err := s.buildMonthSummary(ctx, userID, today)
	if err != nil {
		return nil, err
	}
	return &CheckinResult{
		Record:     *record,
		Summary:    summary,
		Reward:     totalReward,
		NewBalance: user.Balance,
	}, nil
}

func (s *CheckinService) loadSettings(ctx context.Context) (CheckinSettings, error) {
	if s.settingService == nil {
		return CheckinSettings{}, ErrCheckinDisabled
	}
	settings, err := s.settingService.GetPublicSettings(ctx)
	if err != nil {
		return CheckinSettings{}, fmt.Errorf("get checkin settings: %w", err)
	}
	return CheckinSettings{
		Enabled:       settings.CheckinEnabled,
		Content:       settings.CheckinContent,
		DailyReward:   settings.CheckinDailyReward,
		ExtraReward4:  settings.CheckinExtraReward4,
		ExtraReward16: settings.CheckinExtraReward16,
	}, nil
}

func (s *CheckinService) buildMonthSummary(ctx context.Context, userID int64, today time.Time) (CheckinMonthSummary, error) {
	monthStart, monthEnd := monthBounds(today)
	records, err := s.repo.ListByUserAndDateRange(ctx, userID, monthStart, monthEnd)
	if err != nil {
		return CheckinMonthSummary{}, fmt.Errorf("list monthly checkins: %w", err)
	}
	todayChecked := false
	var todayRecord *CheckinRecord
	for _, record := range records {
		if sameDate(record.CheckinDate, today) {
			todayChecked = true
			todayRecord = &record
			break
		}
	}
	consecutiveCount, err := s.currentConsecutiveCheckinCount(ctx, userID, today, todayRecord)
	if err != nil {
		return CheckinMonthSummary{}, fmt.Errorf("calculate current consecutive checkins: %w", err)
	}
	monthCount := len(records)
	return CheckinMonthSummary{
		Year:                monthStart.Year(),
		Month:               int(monthStart.Month()),
		Today:               formatDate(today),
		TodayChecked:        todayChecked,
		MonthCount:          monthCount,
		ConsecutiveCount:    consecutiveCount,
		ConsecutiveCycleDay: checkinCycleDay(consecutiveCount),
		DaysInMonth:         int(monthEnd.Sub(monthStart).Hours() / 24),
		Records:             records,
		NextExtraMilestone:  nextCheckinExtraMilestone(consecutiveCount),
	}, nil
}

func (s *CheckinService) nextConsecutiveCheckinCount(ctx context.Context, userID int64, today time.Time) (int, error) {
	count, err := s.consecutiveCheckinCountAt(ctx, userID, truncateDate(today).AddDate(0, 0, -1))
	if err != nil {
		return 0, err
	}
	return count + 1, nil
}

func (s *CheckinService) currentConsecutiveCheckinCount(ctx context.Context, userID int64, today time.Time, todayRecord *CheckinRecord) (int, error) {
	if todayRecord != nil {
		if todayRecord.ConsecutiveCount > 0 {
			return todayRecord.ConsecutiveCount, nil
		}
		return s.consecutiveCheckinCountAt(ctx, userID, today)
	}
	return s.consecutiveCheckinCountAt(ctx, userID, truncateDate(today).AddDate(0, 0, -1))
}

func (s *CheckinService) consecutiveCheckinCountAt(ctx context.Context, userID int64, day time.Time) (int, error) {
	record, err := s.repo.GetByUserAndDate(ctx, userID, day)
	if err != nil {
		return 0, err
	}
	if record == nil {
		return 0, nil
	}
	if record.ConsecutiveCount > 0 {
		return record.ConsecutiveCount, nil
	}
	return s.legacyConsecutiveCheckinCountAt(ctx, userID, day)
}

func (s *CheckinService) legacyConsecutiveCheckinCountAt(ctx context.Context, userID int64, day time.Time) (int, error) {
	day = truncateDate(day)
	// 旧记录的连续值均为 0。一次读取截至目标日的历史记录，避免沿日期递归产生 N+1 查询。
	records, err := s.repo.ListByUserAndDateRange(ctx, userID, time.Time{}, day.AddDate(0, 0, 1))
	if err != nil {
		return 0, err
	}
	recordByDate := make(map[string]CheckinRecord, len(records))
	for _, legacyRecord := range records {
		recordByDate[formatDate(legacyRecord.CheckinDate)] = legacyRecord
	}

	count := 0
	for cursor := day; ; cursor = cursor.AddDate(0, 0, -1) {
		legacyRecord, ok := recordByDate[formatDate(cursor)]
		if !ok {
			return count, nil
		}
		count++
		if legacyRecord.ConsecutiveCount > 0 {
			return legacyRecord.ConsecutiveCount + count - 1, nil
		}
	}
}

func (s *CheckinService) shouldBackfillSixteenDayReward(ctx context.Context, userID int64, today time.Time, consecutiveCount int) (bool, error) {
	if consecutiveCount < CheckinExtraMilestoneSecondDefault {
		return false, nil
	}
	// 兼容周期奖励上线前的历史记录：连续链已达 16 天但从未留下第 16 天里程碑时，下一次签到补发一次。
	start := truncateDate(today).AddDate(0, 0, -consecutiveCount)
	records, err := s.repo.ListByUserAndDateRange(ctx, userID, start, truncateDate(today))
	if err != nil {
		return false, err
	}
	for _, record := range records {
		if hasCheckinMilestone(record, CheckinExtraMilestoneSecondDefault) {
			return false, nil
		}
	}
	return true, nil
}

func checkinConsecutiveCountForReward(consecutiveCount int, backfillSixteenReward bool) int {
	if backfillSixteenReward && consecutiveCount >= CheckinExtraMilestoneSecondDefault {
		return CheckinExtraMilestoneSecondDefault
	}
	return consecutiveCount
}

func hasCheckinMilestone(record CheckinRecord, milestone int) bool {
	for _, value := range record.ExtraMilestones {
		if value == milestone {
			return true
		}
	}
	return false
}

func (s *CheckinService) today() time.Time {
	now := time.Now
	if s != nil && s.now != nil {
		now = s.now
	}
	return truncateDate(now())
}

func (s *CheckinService) invalidateBalanceCaches(ctx context.Context, userID int64) {
	if s.authCacheInvalidator != nil {
		s.authCacheInvalidator.InvalidateAuthCacheByUserID(ctx, userID)
	}
	if s.billingCacheService == nil {
		return
	}
	go func() {
		cacheCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.billingCacheService.InvalidateUserBalance(cacheCtx, userID)
	}()
}

func (s *CheckinService) addRewardBalance(ctx context.Context, userID int64, amount float64) error {
	if amount <= 0 {
		return nil
	}
	if repo, ok := s.userRepo.(checkinBalanceCreditRepository); ok {
		return repo.AddBalance(ctx, userID, amount)
	}
	// 兼容测试桩或旧实现；真实仓储实现了 AddBalance，不会把签到奖励计入累计充值。
	return s.userRepo.UpdateBalance(ctx, userID, amount)
}

func checkinExtraReward(settings CheckinSettings, consecutiveCount int) (float64, []int) {
	switch checkinCycleDay(consecutiveCount) {
	case CheckinExtraMilestoneFirstDefault:
		if settings.ExtraReward4 > 0 {
			return settings.ExtraReward4, []int{CheckinExtraMilestoneFirstDefault}
		}
	case CheckinExtraMilestoneSecondDefault:
		if settings.ExtraReward16 > 0 {
			return settings.ExtraReward16, []int{CheckinExtraMilestoneSecondDefault}
		}
	}
	return 0, []int{}
}

func nextCheckinExtraMilestone(consecutiveCount int) *int {
	cycleDay := checkinCycleDay(consecutiveCount)
	if cycleDay < CheckinExtraMilestoneFirstDefault {
		v := CheckinExtraMilestoneFirstDefault
		return &v
	}
	if cycleDay < CheckinExtraMilestoneSecondDefault {
		v := CheckinExtraMilestoneSecondDefault
		return &v
	}
	v := CheckinExtraMilestoneFirstDefault
	return &v
}

func checkinCycleDay(consecutiveCount int) int {
	if consecutiveCount <= 0 {
		return 0
	}
	// 连续总天数继续跨月累计，奖励档位按固定 16 天周期映射，避免受月份边界影响。
	return (consecutiveCount-1)%CheckinRewardCycleDays + 1
}

func monthBounds(day time.Time) (time.Time, time.Time) {
	day = truncateDate(day)
	start := time.Date(day.Year(), day.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func truncateDate(t time.Time) time.Time {
	loc := apptimezone.Location()
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

func sameDate(a, b time.Time) bool {
	return truncateDate(a).Equal(truncateDate(b))
}

func formatDate(t time.Time) string {
	return truncateDate(t).Format("2006-01-02")
}

func isUniqueConstraintError(err error) bool {
	var constraint *dbent.ConstraintError
	return errors.As(err, &constraint)
}
