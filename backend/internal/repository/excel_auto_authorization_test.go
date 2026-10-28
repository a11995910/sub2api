package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 使用明确提供的隔离数据库，每次创建独立 schema，不读取业务表。
func TestExcelAutoPostgresConcurrencyAndCAS(t *testing.T) {
	dsn := os.Getenv("EXCEL_AUTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("需要隔离 PostgreSQL 的 EXCEL_AUTO_TEST_DATABASE_URL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer db.Close()
	schema := fmt.Sprintf("excel_auto_%d", time.Now().UnixNano())
	_, err = db.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer db.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	isolated, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer isolated.Close()
	_, err = isolated.Exec(`CREATE TABLE accounts(id BIGINT PRIMARY KEY,platform TEXT,type TEXT,parent_account_id BIGINT,deleted_at TIMESTAMPTZ,status TEXT,schedulable BOOL,credentials JSONB,extra JSONB)`)
	require.NoError(t, err)
	for _, name := range []string{"247_excel_oauth_grants.sql", "248_excel_auto_authorizations.sql"} {
		raw, e := os.ReadFile(filepath.Join("../../migrations", name))
		require.NoError(t, e)
		_, e = isolated.Exec(string(raw))
		require.NoError(t, e)
	}
	_, err = isolated.Exec(`INSERT INTO accounts VALUES(1,'openai','oauth',NULL,NULL,'active',true,'{"client_id":"codex"}','{"openai_excel_bps":true}')`)
	require.NoError(t, err)
	repo := &accountRepository{sql: isolated}
	ctx := context.Background()
	ok, err := repo.SaveExcelAuto(ctx, 1, 0, "encrypted-login", true)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.SaveExcelAuto(ctx, 1, 0, "stale", true)
	require.NoError(t, err)
	require.False(t, ok)
	// 多个 API 副本同时抢占，只有一个拿到任务。
	var mu sync.Mutex
	var jobs []*service.ExcelAutoRecord
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			job, e := repo.ClaimExcelAuto(ctx, fmt.Sprint(i))
			require.NoError(t, e)
			if job != nil {
				mu.Lock()
				jobs = append(jobs, job)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Len(t, jobs, 1)
	job := jobs[0]
	a := &service.Account{ID: 1, Credentials: map[string]any{"client_id": "codex"}}
	// 关闭并清空登录配置后，旧任务不允许保存授权。
	ok, err = repo.SaveExcelAuto(ctx, 1, job.Revision, "", false)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = repo.FinishExcelAuto(ctx, job, a, "", "old-result", "")
	require.NoError(t, err)
	require.False(t, ok)
	grant, err := repo.GetExcelOAuthGrant(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, grant)
	current, err := repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, current.Ciphertext)
	ok, err = repo.SaveExcelAuto(ctx, 1, current.Revision, "new-login", true)
	require.NoError(t, err)
	require.True(t, ok)
	job, err = repo.ClaimExcelAuto(ctx, "new")
	require.NoError(t, err)
	require.NotNil(t, job)
	// 手动授权先完成，自动授权不能覆盖。
	_, err = isolated.Exec(`INSERT INTO account_excel_oauth_grants(account_id,ciphertext) VALUES(1,'manual')`)
	require.NoError(t, err)
	ok, err = repo.FinishExcelAuto(ctx, job, a, "", "automatic", "")
	require.NoError(t, err)
	require.False(t, ok)
	grant, err = repo.GetExcelOAuthGrant(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "manual", grant)
	current, err = repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State)
	require.Greater(t, time.Until(current.NextAttemptAt), 29*time.Minute)
	job, err = repo.ClaimExcelAuto(ctx, "too-soon")
	require.NoError(t, err)
	require.Nil(t, job)
	// 租约超时恢复，旧 run_id 不允许写回。
	_, err = isolated.Exec(`UPDATE account_excel_auto_authorizations SET state='running',run_id='crashed',started_at=NOW()-INTERVAL '16 minutes',next_attempt_at=NOW()`)
	require.NoError(t, err)
	job, err = repo.ClaimExcelAuto(ctx, "recovered")
	require.NoError(t, err)
	require.NotNil(t, job)
	stale := *job
	stale.RunID = "crashed"
	ok, err = repo.FinishExcelAuto(ctx, &stale, a, "manual", "stale", "")
	require.NoError(t, err)
	require.False(t, ok)
	ok, err = repo.FinishExcelAuto(ctx, job, a, "manual", "fresh", "")
	require.NoError(t, err)
	require.True(t, ok)
	current, err = repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "authorized", current.State)
	grant, err = repo.GetExcelOAuthGrant(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "fresh", grant)
	require.NoError(t, repo.RetryExcelAuto(ctx, 1))
	current, err = repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "queued", current.State)
	require.Equal(t, "refresh_failed", current.LastError)
	_, err = isolated.Exec(`UPDATE account_excel_auto_authorizations SET next_attempt_at=NOW() WHERE account_id=1`)
	require.NoError(t, err)
	job, err = repo.ClaimExcelAuto(ctx, "account-changed")
	require.NoError(t, err)
	require.NotNil(t, job)
	_, err = isolated.Exec(`UPDATE accounts SET credentials='{"client_id":"changed"}'::jsonb WHERE id=1`)
	require.NoError(t, err)
	ok, err = repo.FinishExcelAuto(ctx, job, a, "fresh", "obsolete", "")
	require.NoError(t, err)
	require.False(t, ok)
	current, err = repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "failed", current.State)
	require.Equal(t, "account_changed", current.LastError)
	grant, err = repo.GetExcelOAuthGrant(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "fresh", grant)
	// 删除账号时独立凭据和登录资料级联删除。
	_, err = isolated.Exec(`DELETE FROM accounts WHERE id=1`)
	require.NoError(t, err)
	current, err = repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Nil(t, current)
}

func TestExcelAutoAccountSoftDeleteRemovesSecrets(t *testing.T) {
	dsn := os.Getenv("EXCEL_AUTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("需要隔离 PostgreSQL 的 EXCEL_AUTO_TEST_DATABASE_URL")
	}
	root, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer root.Close()
	schema := fmt.Sprintf("excel_delete_%d", time.Now().UnixNano())
	_, err = root.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	defer root.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	isolation, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	defer isolation.Close()
	for _, ddl := range []string{
		`CREATE TABLE accounts(id BIGINT PRIMARY KEY,platform TEXT,type TEXT,parent_account_id BIGINT,deleted_at TIMESTAMPTZ,updated_at TIMESTAMPTZ,status TEXT,schedulable BOOL,credentials JSONB,extra JSONB)`,
		`CREATE TABLE account_groups(account_id BIGINT,group_id BIGINT,priority INT,created_at TIMESTAMPTZ)`,
		`CREATE TABLE scheduled_test_plans(account_id BIGINT)`,
		`CREATE TABLE scheduler_outbox(event_type TEXT,account_id BIGINT,group_id BIGINT,payload JSONB,dedup_key TEXT)`,
		`CREATE UNIQUE INDEX ON scheduler_outbox(dedup_key) WHERE dedup_key IS NOT NULL`,
	} {
		_, err = isolation.Exec(ddl)
		require.NoError(t, err)
	}
	for _, name := range []string{"247_excel_oauth_grants.sql", "248_excel_auto_authorizations.sql"} {
		raw, e := os.ReadFile(filepath.Join("../../migrations", name))
		require.NoError(t, e)
		_, e = isolation.Exec(string(raw))
		require.NoError(t, e)
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, isolation)))
	defer client.Close()
	repo := newAccountRepositoryWithSQL(client, isolation, nil)
	ctx := context.Background()
	_, err = isolation.Exec(`INSERT INTO accounts(id,platform,type,parent_account_id,deleted_at,status,schedulable,credentials,extra) VALUES(1,'openai','oauth',NULL,NULL,'active',true,'{"client_id":"codex"}','{"openai_excel_bps":true}')`)
	require.NoError(t, err)
	ok, err := repo.SaveExcelAuto(ctx, 1, 0, "login-secret", true)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = isolation.Exec(`INSERT INTO account_excel_oauth_grants(account_id,ciphertext) VALUES(1,'grant-secret')`)
	require.NoError(t, err)
	require.NoError(t, repo.Delete(ctx, 1))
	var deletedAt sql.NullTime
	require.NoError(t, isolation.QueryRow(`SELECT deleted_at FROM accounts WHERE id=1`).Scan(&deletedAt))
	require.True(t, deletedAt.Valid, "账号应保留为软删除历史行")
	for _, table := range []string{"account_excel_auto_authorizations", "account_excel_oauth_grants"} {
		var count int
		require.NoError(t, isolation.QueryRow(`SELECT count(*) FROM `+table+` WHERE account_id=1`).Scan(&count))
		require.Zero(t, count, table)
	}
	ok, err = repo.SaveExcelAuto(ctx, 1, 0, "late-secret", true)
	require.NoError(t, err)
	require.False(t, ok, "软删除后不允许重建登录资料")

	// 持有账号行锁，让保存与删除同时排队；无论哪个先获得锁，最终都不能残留密文。
	_, err = isolation.Exec(`INSERT INTO accounts(id,platform,type,parent_account_id,deleted_at,status,schedulable,credentials,extra) VALUES(2,'openai','oauth',NULL,NULL,'active',true,'{"client_id":"codex"}','{"openai_excel_bps":true}')`)
	require.NoError(t, err)
	lockTx, err := isolation.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer lockTx.Rollback()
	var lockedID int64
	require.NoError(t, lockTx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=2 FOR UPDATE`).Scan(&lockedID))
	saveConn, err := isolation.Conn(ctx)
	require.NoError(t, err)
	defer saveConn.Close()
	var savePID int
	require.NoError(t, saveConn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&savePID))
	saveRepo := &accountRepository{sql: saveConn}
	saveResult := make(chan error, 1)
	deleteResult := make(chan error, 1)
	go func() {
		_, e := saveRepo.SaveExcelAuto(ctx, 2, 0, "racing-secret", true)
		saveResult <- e
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waitType sql.NullString
		require.NoError(t, root.QueryRow(`SELECT wait_event_type FROM pg_stat_activity WHERE pid=$1`, savePID).Scan(&waitType))
		if waitType.String == "Lock" {
			break
		}
		require.True(t, time.Now().Before(deadline), "保存语句未等待账号行锁")
		time.Sleep(10 * time.Millisecond)
	}
	go func() { deleteResult <- repo.Delete(ctx, 2) }()
	require.NoError(t, lockTx.Commit())
	require.NoError(t, <-saveResult)
	require.NoError(t, <-deleteResult)
	var count int
	require.NoError(t, isolation.QueryRow(`SELECT count(*) FROM account_excel_auto_authorizations WHERE account_id=2`).Scan(&count))
	require.Zero(t, count, "并发保存不得留下登录密文")

	// 保存语句持有删除前快照并等待账号锁，删除提交后仍须重新检查 deleted_at。
	_, err = isolation.Exec(`INSERT INTO accounts(id,platform,type,parent_account_id,deleted_at,status,schedulable,credentials,extra) VALUES(3,'openai','oauth',NULL,NULL,'active',true,'{"client_id":"codex"}','{"openai_excel_bps":true}')`)
	require.NoError(t, err)
	deleteTx, err := isolation.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer deleteTx.Rollback()
	_, err = deleteTx.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW() WHERE id=3`)
	require.NoError(t, err)
	type saveOutcome struct {
		ok  bool
		err error
	}
	staleSave := make(chan saveOutcome, 1)
	go func() {
		ok, e := saveRepo.SaveExcelAuto(ctx, 3, 0, "stale-secret", true)
		staleSave <- saveOutcome{ok, e}
	}()
	deadline = time.Now().Add(5 * time.Second)
	for {
		var waitType sql.NullString
		require.NoError(t, root.QueryRow(`SELECT wait_event_type FROM pg_stat_activity WHERE pid=$1`, savePID).Scan(&waitType))
		if waitType.String == "Lock" {
			break
		}
		require.True(t, time.Now().Before(deadline), "旧快照保存语句未等待删除事务")
		time.Sleep(10 * time.Millisecond)
	}
	require.NoError(t, deleteTx.Commit())
	result := <-staleSave
	require.NoError(t, result.err)
	require.False(t, result.ok, "删除提交后不得用旧快照重建登录资料")
	require.NoError(t, isolation.QueryRow(`SELECT count(*) FROM account_excel_auto_authorizations WHERE account_id=3`).Scan(&count))
	require.Zero(t, count)
}
