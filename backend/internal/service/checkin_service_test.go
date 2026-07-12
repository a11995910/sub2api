package service

import (
	"context"
	"testing"
	"time"

	apptimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type checkinRepositoryStub struct {
	records   map[string]CheckinRecord
	getCalls  int
	listCalls int
}

func (r *checkinRepositoryStub) Create(_ context.Context, record *CheckinRecord) error {
	if r.records == nil {
		r.records = make(map[string]CheckinRecord)
	}
	r.records[formatDate(record.CheckinDate)] = *record
	return nil
}

func (r *checkinRepositoryStub) GetByUserAndDate(_ context.Context, _ int64, date time.Time) (*CheckinRecord, error) {
	r.getCalls++
	record, ok := r.records[formatDate(date)]
	if !ok {
		return nil, nil
	}
	return &record, nil
}

func (r *checkinRepositoryStub) ListByUserAndDateRange(_ context.Context, _ int64, start, end time.Time) ([]CheckinRecord, error) {
	r.listCalls++
	records := make([]CheckinRecord, 0, len(r.records))
	for _, record := range r.records {
		if !record.CheckinDate.Before(start) && record.CheckinDate.Before(end) {
			records = append(records, record)
		}
	}
	return records, nil
}

func (r *checkinRepositoryStub) CountByUserAndDateRange(_ context.Context, _ int64, start, end time.Time) (int, error) {
	records, err := r.ListByUserAndDateRange(context.Background(), 0, start, end)
	return len(records), err
}

func TestCheckinExtraRewardUsesConsecutiveCount(t *testing.T) {
	settings := CheckinSettings{ExtraReward4: 4, ExtraReward16: 16}

	reward, milestones := checkinExtraReward(settings, 4)
	require.InDelta(t, 4, reward, 0.0001)
	require.Equal(t, []int{CheckinExtraMilestoneFirstDefault}, milestones)

	reward, milestones = checkinExtraReward(settings, 16)
	require.InDelta(t, 16, reward, 0.0001)
	require.Equal(t, []int{CheckinExtraMilestoneSecondDefault}, milestones)
}

func TestCheckinServiceNextConsecutiveCountResetsAfterMissedDay(t *testing.T) {
	today := checkinTestDate(2026, time.February, 6)
	svc := &CheckinService{repo: &checkinRepositoryStub{records: map[string]CheckinRecord{
		"2026-02-04": checkinTestRecord(2026, time.February, 4, 4),
	}}}

	count, err := svc.nextConsecutiveCheckinCount(context.Background(), 1, today)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestCheckinServiceNextConsecutiveCountBackfillsLegacyRecordsAcrossMonth(t *testing.T) {
	today := checkinTestDate(2026, time.February, 1)
	svc := &CheckinService{repo: &checkinRepositoryStub{records: map[string]CheckinRecord{
		"2026-01-29": checkinTestRecord(2026, time.January, 29, 0),
		"2026-01-30": checkinTestRecord(2026, time.January, 30, 0),
		"2026-01-31": checkinTestRecord(2026, time.January, 31, 0),
	}}}

	count, err := svc.nextConsecutiveCheckinCount(context.Background(), 1, today)
	require.NoError(t, err)
	require.Equal(t, 4, count)
}

func TestCheckinServiceLegacyStreakBackfillUsesSingleRangeQuery(t *testing.T) {
	today := checkinTestDate(2026, time.February, 1)
	repo := &checkinRepositoryStub{records: map[string]CheckinRecord{
		"2026-01-26": checkinTestRecord(2026, time.January, 26, 0),
		"2026-01-27": checkinTestRecord(2026, time.January, 27, 0),
		"2026-01-28": checkinTestRecord(2026, time.January, 28, 0),
		"2026-01-29": checkinTestRecord(2026, time.January, 29, 0),
		"2026-01-30": checkinTestRecord(2026, time.January, 30, 0),
		"2026-01-31": checkinTestRecord(2026, time.January, 31, 0),
	}}
	svc := &CheckinService{repo: repo}

	count, err := svc.nextConsecutiveCheckinCount(context.Background(), 1, today)

	require.NoError(t, err)
	require.Equal(t, 7, count)
	require.Equal(t, 1, repo.getCalls)
	require.Equal(t, 1, repo.listCalls)
}

func TestCheckinServiceBuildMonthSummaryUsesCurrentConsecutiveCount(t *testing.T) {
	today := checkinTestDate(2026, time.February, 21)
	repo := &checkinRepositoryStub{records: make(map[string]CheckinRecord)}
	for day := 1; day <= 17; day++ {
		repo.records[formatDate(checkinTestDate(2026, time.February, day))] = checkinTestRecord(2026, time.February, day, day)
	}
	for day, count := range map[int]int{19: 1, 20: 2, 21: 3} {
		repo.records[formatDate(checkinTestDate(2026, time.February, day))] = checkinTestRecord(2026, time.February, day, count)
	}
	svc := &CheckinService{repo: repo}

	summary, err := svc.buildMonthSummary(context.Background(), 1, today)
	require.NoError(t, err)
	require.Equal(t, 20, summary.MonthCount)
	require.Equal(t, 3, summary.ConsecutiveCount)
	require.NotNil(t, summary.NextExtraMilestone)
	require.Equal(t, CheckinExtraMilestoneFirstDefault, *summary.NextExtraMilestone)
}

func TestCheckinServiceBuildMonthSummaryKeepsYesterdayStreakBeforeTodayCheckin(t *testing.T) {
	today := checkinTestDate(2026, time.February, 6)
	svc := &CheckinService{repo: &checkinRepositoryStub{records: map[string]CheckinRecord{
		"2026-02-05": checkinTestRecord(2026, time.February, 5, 3),
	}}}

	summary, err := svc.buildMonthSummary(context.Background(), 1, today)
	require.NoError(t, err)
	require.False(t, summary.TodayChecked)
	require.Equal(t, 3, summary.ConsecutiveCount)
}

func TestCheckinServiceBuildMonthSummaryResetsWhenTodayAndYesterdayAreMissing(t *testing.T) {
	today := checkinTestDate(2026, time.February, 6)
	svc := &CheckinService{repo: &checkinRepositoryStub{records: map[string]CheckinRecord{
		"2026-02-04": checkinTestRecord(2026, time.February, 4, 4),
	}}}

	summary, err := svc.buildMonthSummary(context.Background(), 1, today)
	require.NoError(t, err)
	require.False(t, summary.TodayChecked)
	require.Equal(t, 0, summary.ConsecutiveCount)
}

func checkinTestRecord(year int, month time.Month, day, consecutiveCount int) CheckinRecord {
	return CheckinRecord{
		UserID:           1,
		CheckinDate:      checkinTestDate(year, month, day),
		ConsecutiveCount: consecutiveCount,
	}
}

func checkinTestDate(year int, month time.Month, day int) time.Time {
	return truncateDate(time.Date(year, month, day, 12, 0, 0, 0, apptimezone.Location()))
}
