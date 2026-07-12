package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/checkinrecord"
	apptimezone "github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type checkinRepository struct {
	client *dbent.Client
}

func NewCheckinRepository(client *dbent.Client) service.CheckinRepository {
	return &checkinRepository{client: client}
}

func (r *checkinRepository) Create(ctx context.Context, record *service.CheckinRecord) error {
	client := clientFromContext(ctx, r.client)
	created, err := client.CheckinRecord.Create().
		SetUserID(record.UserID).
		SetCheckinDate(normalizeCheckinDate(record.CheckinDate)).
		SetDailyReward(record.DailyReward).
		SetExtraReward(record.ExtraReward).
		SetMonthCount(record.MonthCount).
		SetConsecutiveCount(record.ConsecutiveCount).
		SetExtraMilestones(record.ExtraMilestones).
		SetCheckedInAt(record.CheckedInAt).
		Save(ctx)
	if err != nil {
		return err
	}
	applyCheckinRecordEntity(record, created)
	return nil
}

func (r *checkinRepository) GetByUserAndDate(ctx context.Context, userID int64, date time.Time) (*service.CheckinRecord, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.CheckinRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckinDateEQ(normalizeCheckinDate(date)),
		).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	out := checkinRecordEntityToService(m)
	return &out, nil
}

func (r *checkinRepository) ListByUserAndDateRange(ctx context.Context, userID int64, start, end time.Time) ([]service.CheckinRecord, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.CheckinRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckinDateGTE(normalizeCheckinDate(start)),
			checkinrecord.CheckinDateLT(normalizeCheckinDate(end)),
		).
		Order(dbent.Asc(checkinrecord.FieldCheckinDate)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.CheckinRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, checkinRecordEntityToService(row))
	}
	return out, nil
}

func (r *checkinRepository) CountByUserAndDateRange(ctx context.Context, userID int64, start, end time.Time) (int, error) {
	client := clientFromContext(ctx, r.client)
	return client.CheckinRecord.Query().
		Where(
			checkinrecord.UserIDEQ(userID),
			checkinrecord.CheckinDateGTE(normalizeCheckinDate(start)),
			checkinrecord.CheckinDateLT(normalizeCheckinDate(end)),
		).
		Count(ctx)
}

func checkinRecordEntityToService(m *dbent.CheckinRecord) service.CheckinRecord {
	if m == nil {
		return service.CheckinRecord{}
	}
	return service.CheckinRecord{
		ID:              m.ID,
		UserID:          m.UserID,
		CheckinDate:     normalizeCheckinDate(m.CheckinDate),
		DailyReward:     m.DailyReward,
		ExtraReward:     m.ExtraReward,
		MonthCount:      m.MonthCount,
		ConsecutiveCount: m.ConsecutiveCount,
		ExtraMilestones: append([]int{}, m.ExtraMilestones...),
		CheckedInAt:     m.CheckedInAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func applyCheckinRecordEntity(dst *service.CheckinRecord, src *dbent.CheckinRecord) {
	if dst == nil || src == nil {
		return
	}
	*dst = checkinRecordEntityToService(src)
}

func normalizeCheckinDate(t time.Time) time.Time {
	u := t.UTC()
	if u.Hour() == 0 && u.Minute() == 0 && u.Second() == 0 && u.Nanosecond() == 0 {
		return time.Date(u.Year(), u.Month(), u.Day(), 0, 0, 0, 0, time.UTC)
	}
	loc := apptimezone.Location()
	local := t.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}
