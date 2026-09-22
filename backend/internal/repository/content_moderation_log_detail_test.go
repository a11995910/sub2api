package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var moderationLogTestColumns = []string{
	"id", "request_id", "user_id", "user_email", "api_key_id", "api_key_name", "group_id", "group_name",
	"endpoint", "provider", "model", "mode", "action", "flagged", "highest_category", "highest_score",
	"category_scores", "threshold_snapshot", "input_excerpt", "upstream_latency_ms", "error",
	"violation_count", "auto_banned", "email_sent", "user_status", "queue_delay_ms", "matched_keyword", "created_at", "engine_meta",
}

func moderationLogTestValues() []driver.Value {
	return []driver.Value{
		int64(42), "请求编号", nil, "", nil, "", nil, "",
		"/v1/responses", "openai", "gpt-test", "pre_block", "keyword_block", true, "keyword", 1.0,
		[]byte(`{"keyword":1}`), []byte(`{}`), "旧摘要", nil, "",
		0, false, false, "", nil, "测试关键词", time.Now(), nil,
	}
}

func TestContentModerationRepositoryGetLog(t *testing.T) {
	text := strings.Repeat("原始输入\n", 500) + "末尾测试关键词"
	content := &service.ContentModerationLogInput{Text: text, TextRunes: len([]rune(text)), ImageCount: 2}
	raw, err := json.Marshal(content)
	require.NoError(t, err)
	for _, tc := range []struct {
		name string
		raw  driver.Value
		want *service.ContentModerationLogInput
	}{
		{name: "保留全文与换行", raw: raw, want: content},
		{name: "历史原文未采集", raw: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			columns := append(append([]string{}, moderationLogTestColumns...), "input_content")
			values := append(moderationLogTestValues(), tc.raw)
			mock.ExpectQuery(`SELECT .*l\.input_content.*WHERE l\.id = \$1`).WithArgs(int64(42)).
				WillReturnRows(sqlmock.NewRows(columns).AddRow(values...))
			log, err := NewContentModerationRepository(db).GetLog(context.Background(), 42)
			require.NoError(t, err)
			require.Equal(t, int64(42), log.ID)
			require.Equal(t, "旧摘要", log.InputExcerpt)
			require.Equal(t, tc.want, log.InputContent)
			require.Nil(t, log.UserID)
			require.Equal(t, float64(1), log.CategoryScores["keyword"])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestContentModerationRepositoryGetLogErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "记录不存在"},
		{name: "数据库不可用", err: errors.New("数据库连接失败")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			query := mock.ExpectQuery(`WHERE l\.id = \$1`).WithArgs(int64(42))
			if tc.err == nil {
				query.WillReturnRows(sqlmock.NewRows(append(append([]string{}, moderationLogTestColumns...), "input_content")))
			} else {
				query.WillReturnError(tc.err)
			}
			log, err := NewContentModerationRepository(db).GetLog(context.Background(), 42)
			require.Nil(t, log)
			if tc.err == nil {
				require.ErrorIs(t, err, service.ErrContentModerationLogNotFound)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestContentModerationRepositoryListDoesNotLoadInput(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if strings.Contains(actual, "input_content") {
			return errors.New("列表不应查询原始输入")
		}
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})))
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(`SELECT COUNT\(\*\)`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
	mock.ExpectQuery(`SELECT .*FROM content_moderation_logs`).WithArgs(20, 0).
		WillReturnRows(sqlmock.NewRows(moderationLogTestColumns).AddRow(moderationLogTestValues()...))
	logs, page, err := NewContentModerationRepository(db).ListLogs(context.Background(), service.ContentModerationLogFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	require.Len(t, logs, 1)
	require.Nil(t, logs[0].InputContent)
	raw, err := json.Marshal(logs)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "input_content")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestContentModerationRepositoryCreateLogPersistsInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input *service.ContentModerationLogInput
	}{
		{name: "正文单独保存", input: &service.ContentModerationLogInput{Text: "首行\n原始输入末尾", TextRunes: 10}},
		{name: "未采集正文保存NULL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			var inputRaw driver.Value
			if tc.input != nil {
				raw, err := json.Marshal(tc.input)
				require.NoError(t, err)
				inputRaw = string(raw)
			}
			args := []driver.Value{
				"", nil, "", nil, "", nil, "", "", "", "", "", "", false, "", float64(0),
				"null", "null", "摘要", nil, "", 0, false, false, nil, "", inputRaw, nil,
			}
			now := time.Now()
			mock.ExpectQuery(regexp.QuoteMeta("INSERT INTO content_moderation_logs") + `.*input_content.*\$26::jsonb`).
				WithArgs(args...).WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(42, now))
			log := &service.ContentModerationLog{InputExcerpt: "摘要", InputContent: tc.input}
			require.NoError(t, NewContentModerationRepository(db).CreateLog(context.Background(), log))
			require.Equal(t, int64(42), log.ID)
			require.Equal(t, now, log.CreatedAt)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
