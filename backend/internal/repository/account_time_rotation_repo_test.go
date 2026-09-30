package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type rotationStateArgument struct {
	check func(accountTimeRotationState) bool
}

func (a rotationStateArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var state accountTimeRotationState
	return json.Unmarshal([]byte(raw), &state) == nil && a.check(state)
}

func TestAccountTimeRotationTransactionalLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name          string
		current       int
		eligible      bool
		missing       bool
		noOriginal    bool
		replacement   string
		writePriority int
		forget        bool
	}{
		{name: "首次加入保存原优先级", current: 70, eligible: true, noOriginal: true, writePriority: 1},
		{name: "到点切换", current: 50, eligible: true, writePriority: 1},
		{name: "重试幂等不重复写入", current: 1, eligible: true},
		{name: "停用恢复原优先级", current: 1, eligible: true, replacement: "disable", writePriority: 70, forget: true},
		{name: "移除恢复原优先级", current: 1, eligible: true, replacement: "remove", writePriority: 70, forget: true},
		{name: "账号类型改变恢复优先级", current: 1, writePriority: 70, forget: true},
		{name: "账号已删除不阻塞其他账号", missing: true, forget: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			repo := NewAccountTimeRotationRepository(db)
			config := service.DefaultAccountTimeRotationConfig()
			config.Enabled = true
			config.Slots[0].AccountIDs = []int64{1}
			originals := map[int64]int{1: 70}
			if tc.noOriginal {
				originals = map[int64]int{}
			}
			raw, err := json.Marshal(accountTimeRotationState{Config: *config, OriginalPriorities: originals})
			require.NoError(t, err)
			var replacement *service.AccountTimeRotationConfig
			if tc.replacement != "" {
				replacement = service.DefaultAccountTimeRotationConfig()
				replacement.Enabled = tc.replacement != "disable"
				if tc.replacement != "remove" {
					replacement.Slots[0].AccountIDs = []int64{1}
				}
			}
			mock.ExpectBegin()
			if replacement != nil {
				mock.ExpectExec("INSERT INTO settings").WillReturnResult(sqlmock.NewResult(0, 0))
			}
			mock.ExpectQuery("SELECT value FROM settings.*FOR UPDATE").WithArgs(accountTimeRotationKey).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(raw)))
			rows := sqlmock.NewRows([]string{"id", "priority", "eligible"})
			if !tc.missing {
				rows.AddRow(int64(1), tc.current, tc.eligible)
			}
			mock.ExpectQuery("SELECT id, priority.*FROM accounts.*FOR UPDATE").WithArgs(sqlmock.AnyArg()).WillReturnRows(rows)
			if tc.writePriority != 0 {
				mock.ExpectExec("UPDATE accounts SET priority").WithArgs(tc.writePriority, int64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(1), nil, nil, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
			}
			if tc.noOriginal || tc.forget || replacement != nil {
				mock.ExpectExec("UPDATE settings SET value").WithArgs(rotationStateArgument{check: func(state accountTimeRotationState) bool {
					if tc.forget {
						return len(state.OriginalPriorities) == 0
					}
					return state.OriginalPriorities[1] == 70
				}}, accountTimeRotationKey).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			mock.ExpectCommit()
			now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
			result, err := repo.Apply(context.Background(), replacement, now)
			require.NoError(t, err)
			if replacement != nil {
				require.Equal(t, int64(1), result.Revision)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAccountTimeRotationOutboxFailureRollsBackPriority(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	config := service.DefaultAccountTimeRotationConfig()
	config.Enabled = true
	config.Slots[0].AccountIDs = []int64{1}
	raw, err := json.Marshal(accountTimeRotationState{Config: *config, OriginalPriorities: map[int64]int{1: 70}})
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT value FROM settings.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(raw)))
	mock.ExpectQuery("SELECT id, priority.*FROM accounts.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"id", "priority", "eligible"}).AddRow(1, 50, true))
	mock.ExpectExec("UPDATE accounts SET priority").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnError(errors.New("模拟通知写入失败"))
	mock.ExpectRollback()
	_, err = NewAccountTimeRotationRepository(db).Apply(context.Background(), nil, time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC))
	require.ErrorContains(t, err, "模拟通知写入失败")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountTimeRotationRejectsStaleRevisionAndInvalidAccounts(t *testing.T) {
	for _, kind := range []string{"过期版本", "账号不存在", "非OAuth账号"} {
		t.Run(kind, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			existing := service.DefaultAccountTimeRotationConfig()
			existing.Revision = 2
			raw, err := json.Marshal(accountTimeRotationState{Config: *existing, OriginalPriorities: map[int64]int{}})
			require.NoError(t, err)
			next := service.DefaultAccountTimeRotationConfig()
			next.Enabled = true
			next.Slots[0].AccountIDs = []int64{1}
			if kind != "过期版本" {
				next.Revision = 2
			}
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO settings").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectQuery("SELECT value FROM settings.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(string(raw)))
			if kind != "过期版本" {
				rows := sqlmock.NewRows([]string{"id", "priority", "eligible"})
				if kind == "非OAuth账号" {
					rows.AddRow(1, 70, false)
				}
				mock.ExpectQuery("SELECT id, priority.*FROM accounts.*FOR UPDATE").WillReturnRows(rows)
			}
			mock.ExpectRollback()
			_, err = NewAccountTimeRotationRepository(db).Apply(context.Background(), next, time.Now())
			require.Error(t, err)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
