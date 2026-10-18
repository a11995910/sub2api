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
	// 删除账号时独立凭据和登录资料级联删除。
	_, err = isolated.Exec(`DELETE FROM accounts WHERE id=1`)
	require.NoError(t, err)
	current, err = repo.GetExcelAuto(ctx, 1)
	require.NoError(t, err)
	require.Nil(t, current)
}
