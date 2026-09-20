package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 只有显式提供测试库才执行，每次仅在自身随机 schema 内创建和清理数据。
func contentModerationPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("CONTENT_MODERATION_TEST_POSTGRES_DSN"))
	if dsn == "" {
		t.Skip("未指定 CONTENT_MODERATION_TEST_POSTGRES_DSN，跳过真实 PostgreSQL 验证")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	schema := "moderation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quotedSchema := pq.QuoteIdentifier(schema)
	_, err = base.ExecContext(ctx, "CREATE SCHEMA "+quotedSchema)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, cleanupErr := base.ExecContext(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE")
		require.NoError(t, cleanupErr)
	})

	isolatedDSN := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, parseErr := url.Parse(dsn)
		require.NoError(t, parseErr)
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		isolatedDSN = parsed.String()
	}
	db, err := sql.Open("postgres", isolatedDSN)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	_, err = db.ExecContext(ctx, `
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
CREATE TABLE users (id BIGINT PRIMARY KEY, status TEXT NOT NULL);
CREATE TABLE api_keys (id BIGINT PRIMARY KEY);
CREATE TABLE groups (id BIGINT PRIMARY KEY);
INSERT INTO users VALUES (7, 'active');
INSERT INTO api_keys VALUES (8);
INSERT INTO groups VALUES (9);`)
	require.NoError(t, err)
	return db
}

func TestContentModerationPostgresMigrationAndInputRoundTrip(t *testing.T) {
	db := contentModerationPostgresDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	applyMigration := func(name string) {
		t.Helper()
		migration, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, "真实执行迁移 %s", name)
	}
	applyMigration("135_content_moderation.sql")
	applyMigration("156_content_moderation_matched_keyword.sql")

	var historicalID int64
	err := db.QueryRowContext(ctx, `
INSERT INTO content_moderation_logs (request_id, input_excerpt, flagged, created_at)
VALUES ('historical', '历史摘要', TRUE, NOW() - INTERVAL '30 days') RETURNING id`).Scan(&historicalID)
	require.NoError(t, err)
	applyMigration("244_content_moderation_input_content.sql")
	applyMigration("244_content_moderation_input_content.sql")
	t.Log("135、156、244 迁移真实执行成功，244 重复执行成功")

	var dataType, nullable string
	var defaultValue sql.NullString
	err = db.QueryRowContext(ctx, `
SELECT data_type, is_nullable, column_default
FROM information_schema.columns
WHERE table_schema = current_schema() AND table_name = 'content_moderation_logs' AND column_name = 'input_content'`).
		Scan(&dataType, &nullable, &defaultValue)
	require.NoError(t, err)
	require.Equal(t, "jsonb", dataType)
	require.Equal(t, "YES", nullable)
	require.False(t, defaultValue.Valid, "历史记录不能通过默认值伪造原始输入")
	repo := NewContentModerationRepository(db)
	historical, err := repo.GetLog(ctx, historicalID)
	require.NoError(t, err)
	require.Nil(t, historical.InputContent)
	require.Equal(t, "历史摘要", historical.InputExcerpt)

	text := strings.Repeat("原始输入🙂\n第二行包含引号\"与反斜杠\\。", 5000) + "最后一句完整保留"
	userID, apiKeyID, groupID := int64(7), int64(8), int64(9)
	latency, queueDelay := 21, 3
	log := &service.ContentModerationLog{
		RequestID:         "long-input",
		UserID:            &userID,
		UserEmail:         "test@example.invalid",
		APIKeyID:          &apiKeyID,
		APIKeyName:        "测试密钥",
		GroupID:           &groupID,
		GroupName:         "测试分组",
		Endpoint:          "/v1/responses",
		Provider:          "openai",
		Model:             "test-model",
		Mode:              "pre_block",
		Action:            "keyword_block",
		Flagged:           true,
		HighestCategory:   "keyword",
		HighestScore:      1,
		MatchedKeyword:    "测试关键词",
		CategoryScores:    map[string]float64{"keyword": 1},
		ThresholdSnapshot: map[string]float64{"keyword": 0.8},
		InputExcerpt:      "原始输入摘要",
		InputContent: &service.ContentModerationLogInput{
			Text: text, TextRunes: utf8.RuneCountInString(text), ImageCount: 2,
		},
		UpstreamLatencyMS: &latency,
		QueueDelayMS:      &queueDelay,
	}
	require.NoError(t, repo.CreateLog(ctx, log))
	require.Positive(t, log.ID)
	require.False(t, log.CreatedAt.IsZero())
	// 重建仓储再读取，确认内容来自数据库而非内存对象。
	got, err := NewContentModerationRepository(db).GetLog(ctx, log.ID)
	require.NoError(t, err)
	require.Equal(t, log.InputContent, got.InputContent)
	require.Equal(t, "active", got.UserStatus)
	require.Equal(t, log.UserID, got.UserID)
	require.Equal(t, log.APIKeyID, got.APIKeyID)
	require.Equal(t, log.GroupID, got.GroupID)
	require.Equal(t, log.CategoryScores, got.CategoryScores)
	require.Equal(t, log.ThresholdSnapshot, got.ThresholdSnapshot)
	require.Equal(t, log.MatchedKeyword, got.MatchedKeyword)
	require.Equal(t, log.UpstreamLatencyMS, got.UpstreamLatencyMS)
	require.Equal(t, log.QueueDelayMS, got.QueueDelayMS)
	require.Equal(t, log.InputExcerpt, got.InputExcerpt)
	t.Logf("新记录原文 %d 字符、%d 字节及图片数量成功往返", utf8.RuneCountInString(text), len(text))

	// 模拟迁移前应用的写入列清单：新增可空字段不得阻止旧版本写日志。
	var oldWriterID int64
	err = db.QueryRowContext(ctx, `
INSERT INTO content_moderation_logs (
    request_id, user_id, user_email, api_key_id, api_key_name, group_id, group_name,
    endpoint, provider, model, mode, action, flagged, highest_category, highest_score,
    category_scores, threshold_snapshot, input_excerpt, upstream_latency_ms, error,
    violation_count, auto_banned, email_sent, queue_delay_ms, matched_keyword
) VALUES (
    'old-writer', 7, '', 8, '', 9, '', '/v1/responses', 'openai', 'test-model', 'async', 'pass',
    FALSE, '', 0, '{}'::jsonb, '{}'::jsonb, '旧版本写入摘要', NULL, '', 0, FALSE, FALSE, NULL, ''
) RETURNING id`).Scan(&oldWriterID)
	require.NoError(t, err)
	oldWriterLog, err := repo.GetLog(ctx, oldWriterID)
	require.NoError(t, err)
	require.Nil(t, oldWriterLog.InputContent)
	require.Equal(t, "旧版本写入摘要", oldWriterLog.InputExcerpt)
	items, page, err := repo.ListLogs(ctx, service.ContentModerationLogFilter{
		Pagination: pagination.PaginationParams{Page: 1, PageSize: 20},
	})
	require.NoError(t, err)
	require.EqualValues(t, 3, page.Total)
	require.Len(t, items, 3)
	for _, item := range items {
		require.Nil(t, item.InputContent, "列表不能预载原始输入")
	}
	encoded, err := json.Marshal(items)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "input_content")
	require.NotContains(t, string(encoded), "最后一句完整保留")
	t.Log("历史记录与旧版本写入兼容，列表响应不包含原始输入")

	_, err = db.ExecContext(ctx, "UPDATE content_moderation_logs SET created_at = NOW() - INTERVAL '30 days' WHERE id = $1", oldWriterID)
	require.NoError(t, err)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	cleanup, err := repo.CleanupExpiredLogs(ctx, cutoff, cutoff)
	require.NoError(t, err)
	require.EqualValues(t, 1, cleanup.DeletedHit)
	require.EqualValues(t, 1, cleanup.DeletedNonHit)
	for _, deletedID := range []int64{historicalID, oldWriterID} {
		_, err := repo.GetLog(ctx, deletedID)
		require.ErrorIs(t, err, service.ErrContentModerationLogNotFound)
	}
	retained, err := repo.GetLog(ctx, log.ID)
	require.NoError(t, err)
	require.Equal(t, text, retained.InputContent.Text)
	t.Log("保留期清理删除过期命中与未命中记录，近期原文仍可读取")
}
