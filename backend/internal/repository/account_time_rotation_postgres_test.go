package repository

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 显式指定本地测试库，每次使用独立随机 schema，不接触现有业务数据。
func rotationPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TIME_ROTATION_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("未指定 TIME_ROTATION_TEST_POSTGRES_DSN，跳过真实 PostgreSQL 验证")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, base.Close()) })
	schema := "rotation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := base.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
		require.NoError(t, err)
	})
	isolated := dsn + " search_path=" + schema
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		require.NoError(t, err)
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		isolated = parsed.String()
	}
	db, err := sql.Open("postgres", isolated)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	_, err = db.Exec(`
 CREATE TABLE settings (key VARCHAR(100) PRIMARY KEY, value TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL);
 CREATE TABLE accounts (id BIGINT PRIMARY KEY, platform TEXT NOT NULL, type TEXT NOT NULL, parent_account_id BIGINT, priority INT NOT NULL, deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE TABLE scheduler_outbox (id BIGSERIAL PRIMARY KEY, event_type TEXT NOT NULL, account_id BIGINT, group_id BIGINT, payload JSONB, dedup_key TEXT, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
 CREATE UNIQUE INDEX ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL;
 INSERT INTO accounts (id, platform, type, priority) VALUES (1,'openai','oauth',70),(2,'openai','oauth',80),(3,'openai','apikey',90);
 `)
	require.NoError(t, err)
	return db
}

func TestAccountTimeRotationPostgresLifecycle(t *testing.T) {
	db := rotationPostgresDB(t)
	repo := NewAccountTimeRotationRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
	priorities := func() []int {
		t.Helper()
		var a, b int
		require.NoError(t, db.QueryRowContext(ctx, "SELECT priority FROM accounts WHERE id=1").Scan(&a))
		require.NoError(t, db.QueryRowContext(ctx, "SELECT priority FROM accounts WHERE id=2").Scan(&b))
		return []int{a, b}
	}
	config, err := repo.Get(ctx)
	require.NoError(t, err)
	config.Enabled = true
	config.Slots[0].AccountIDs = []int64{1}
	config.Slots[1].AccountIDs = []int64{2}
	config, err = repo.Apply(ctx, config, now)
	require.NoError(t, err)
	require.Equal(t, []int{1, 50}, priorities())
	_, err = repo.Apply(ctx, nil, now.Add(8*time.Hour))
	require.NoError(t, err)
	require.Equal(t, []int{50, 1}, priorities())
	// 实例重启后仍从持久化的原值恢复，不能把轮候值当作原值。
	repo = NewAccountTimeRotationRepository(db)
	config.Enabled = false
	config, err = repo.Apply(ctx, config, now)
	require.NoError(t, err)
	require.Equal(t, []int{70, 80}, priorities())
	_, err = db.ExecContext(ctx, "UPDATE accounts SET priority=75 WHERE id=1")
	require.NoError(t, err)
	config.Enabled = true
	config, err = repo.Apply(ctx, config, now)
	require.NoError(t, err)
	config.Slots[0].AccountIDs = []int64{}
	config, err = repo.Apply(ctx, config, now)
	require.NoError(t, err)
	require.Equal(t, []int{75, 50}, priorities())
	// 无效账号保存必须保留配置版本和全部原有优先级。
	revision := config.Revision
	config.Slots[0].AccountIDs = []int64{3}
	_, err = repo.Apply(ctx, config, now)
	require.Error(t, err)
	saved, err := repo.Get(ctx)
	require.NoError(t, err)
	require.Equal(t, revision, saved.Revision)
	require.Equal(t, []int{75, 50}, priorities())
	// 模拟 outbox 存储故障，验证账号与配置都回滚。
	_, err = db.ExecContext(ctx, `CREATE FUNCTION reject_rotation_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION '模拟通知写入失败'; END $$;
 CREATE TRIGGER reject_rotation_event BEFORE INSERT ON scheduler_outbox FOR EACH ROW EXECUTE FUNCTION reject_rotation_event();`)
	require.NoError(t, err)
	saved.Enabled = false
	_, err = repo.Apply(ctx, saved, now)
	require.ErrorContains(t, err, "模拟通知写入失败")
	require.Equal(t, []int{75, 50}, priorities())
	saved, err = repo.Get(ctx)
	require.NoError(t, err)
	require.True(t, saved.Enabled)
	require.Equal(t, revision, saved.Revision)
}

func TestAccountTimeRotationPostgresConcurrentSave(t *testing.T) {
	db := rotationPostgresDB(t)
	repo := NewAccountTimeRotationRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			config := service.DefaultAccountTimeRotationConfig()
			config.Enabled = true
			config.Slots[0].AccountIDs = []int64{1}
			_, err := repo.Apply(ctx, config, time.Now())
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			require.Contains(t, err.Error(), "其他管理员修改")
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	config, err := repo.Get(ctx)
	require.NoError(t, err)
	config.Enabled = false
	_, err = repo.Apply(ctx, config, time.Now())
	require.NoError(t, err)
	var priority int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT priority FROM accounts WHERE id=1").Scan(&priority))
	require.Equal(t, 70, priority)
}
