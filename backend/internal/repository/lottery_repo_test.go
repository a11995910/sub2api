package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLotteryRepositoryCommitDrawCommitsChanceBalanceAndAuditTogether(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}
	createdAt := time.Now()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT available_chances").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_chances"}).AddRow(int64(1)))
	mock.ExpectExec("UPDATE sub2api_lottery_user_states").
		WithArgs(int64(7), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE users").
		WithArgs(int64(7), 2.5).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(12.5))
	mock.ExpectQuery("INSERT INTO sub2api_lottery_draws").
		WithArgs(int64(7), "small", "2.5 灵石", 2.5, 2500, 42, int64(3), int64(1), int64(0), 12.5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(9), createdAt))
	mock.ExpectCommit()

	draw, err := repo.CommitDraw(context.Background(), 7, service.LotterySelection{
		Prize: &service.LotteryPrize{
			ID:                     "small",
			Name:                   "2.5 灵石",
			RewardAmount:           2.5,
			ProbabilityBasisPoints: 2500,
		},
		Roll:                   42,
		ThanksProbabilityBasis: 7500,
	}, 3)

	require.NoError(t, err)
	require.Equal(t, int64(9), draw.ID)
	require.Equal(t, 12.5, draw.BalanceAfter)
	require.Equal(t, int64(0), draw.ChanceAfter)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLotteryRepositoryCommitDrawRollsBackWhenAuditInsertFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT available_chances").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_chances"}).AddRow(int64(1)))
	mock.ExpectExec("UPDATE sub2api_lottery_user_states").
		WithArgs(int64(7), int64(0)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("UPDATE users").
		WithArgs(int64(7), 0.0).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(10.0))
	mock.ExpectQuery("INSERT INTO sub2api_lottery_draws").
		WillReturnError(errors.New("audit insert failed"))
	mock.ExpectRollback()

	_, err = repo.CommitDraw(context.Background(), 7, service.LotterySelection{
		Roll:                   42,
		ThanksProbabilityBasis: 10_000,
	}, 3)

	require.ErrorContains(t, err, "audit insert failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestLotteryRepositoryCommitDrawRejectsMissingChance(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &lotteryRepository{db: db}

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT available_chances").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"available_chances"}).AddRow(int64(0)))
	mock.ExpectRollback()

	_, err = repo.CommitDraw(context.Background(), 7, service.LotterySelection{}, 3)

	require.ErrorIs(t, err, service.ErrLotteryNoChance)
	require.NoError(t, mock.ExpectationsWereMet())
}
