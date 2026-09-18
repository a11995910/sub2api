//go:build unit

package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 显式指定隔离测试库才运行；每次创建独立 schema，不接触已有表。
func healthyStateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("SUB2API_HEALTHY_STATE_TEST_DSN")
	if dsn == "" {
		t.Skip("未指定健康状态头隔离 PostgreSQL 测试库")
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := "healthy_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = base.Exec("CREATE SCHEMA " + schema)
	require.NoError(t, err)
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	query := u.Query()
	query.Set("search_path", schema)
	u.RawQuery = query.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
		_, err := base.Exec("DROP SCHEMA " + schema + " CASCADE")
		require.NoError(t, err)
		require.NoError(t, base.Close())
	})
	_, err = db.Exec("CREATE TABLE accounts(id BIGINT PRIMARY KEY); INSERT INTO accounts VALUES(1),(2)")
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("239_openai_healthy_turn_state.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	sharedMigration, err := migrations.FS.ReadFile("240_openai_healthy_turn_state_shared_pool.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(sharedMigration))
	require.NoError(t, err)
	return db
}

func TestHealthyTurnStateRepositoryPersistence(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	cipher := &AESEncryptor{key: []byte(strings.Repeat("a", 32))}
	repo := NewHealthyTurnStateRepository(db, cipher)
	scope := service.HealthyTurnStateScope{AccountID: 1, Key: "范围一", Model: "gpt-test", Transport: "http", ProxyID: 12}
	value := service.HealthyTurnStateValue{Value: "测试状态头一", ExpiresAt: time.Now().Add(30 * time.Minute).Truncate(time.Microsecond)}
	stored, err := repo.Save(ctx, scope, value)
	require.NoError(t, err)
	require.True(t, stored)
	var encrypted string
	require.NoError(t, db.QueryRow("SELECT value_encrypted FROM openai_healthy_turn_state_pool").Scan(&encrypted))
	require.NotEqual(t, value.Value, encrypted)

	// 重建仓储后依然能读取，没有依赖进程缓存。
	repo = NewHealthyTurnStateRepository(db, cipher)
	got, err := repo.Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, value.Value, got.Value)
	require.True(t, value.ExpiresAt.Equal(got.ExpiresAt))
	foreign, err := repo.Get(ctx, service.HealthyTurnStateScope{AccountID: 2, Key: "另一个范围", Model: "另一个模型", Transport: "websocket", ProxyID: 88})
	require.NoError(t, err)
	require.Equal(t, value.Value, foreign.Value, "共享池不按账号、模型、传输方式或代理隔离")
	duplicate := value
	duplicate.ExpiresAt = value.ExpiresAt.Add(time.Minute)
	stored, err = repo.Save(ctx, scope, duplicate)
	require.NoError(t, err)
	require.False(t, stored, "同值不续期")
	// 多个仓储、连接并发领取，只允许一个请求持有状态头。
	type result struct {
		value *service.HealthyTurnStateValue
		err   error
	}
	results := make(chan result, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := NewHealthyTurnStateRepository(db, cipher).Claim(ctx, scope, "")
			results <- result{v, err}
		}()
	}
	wg.Wait()
	close(results)
	var claimed *service.HealthyTurnStateValue
	winners := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.value != nil {
			claimed = result.value
			winners++
		}
	}
	require.Equal(t, 1, winners)
	stats, err := repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.InUse)
	require.Zero(t, stats.Attempts)
	require.NoError(t, repo.Release(ctx, scope, *claimed))
	stats, err = repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Available)
	require.Zero(t, stats.Attempts, "发送前归还不计调用")

	claimed, err = repo.Claim(ctx, scope, value.Value)
	require.NoError(t, err)
	require.Nil(t, claimed, "不能替换为同一个头")
	claimed, err = repo.Claim(ctx, scope, "")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.NoError(t, repo.Start(ctx, scope, *claimed, 429))
	require.NoError(t, repo.Complete(ctx, scope, *claimed, true, 200))
	require.NoError(t, repo.Complete(ctx, scope, *claimed, true, 200))
	stats, err = repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Attempts)
	require.EqualValues(t, 1, stats.Successes, "结束写入幂等")
	require.EqualValues(t, 1, stats.Available)

	claimed, err = repo.Claim(ctx, scope, "")
	require.NoError(t, err)
	require.NoError(t, repo.Start(ctx, scope, *claimed, 503))
	require.NoError(t, repo.Complete(ctx, scope, *claimed, false, 503))
	repo = NewHealthyTurnStateRepository(db, cipher)
	got, err = repo.Get(ctx, scope)
	require.NoError(t, err)
	require.Nil(t, got)
	stored, err = repo.Save(ctx, scope, duplicate)
	require.NoError(t, err)
	require.False(t, stored, "失败拒绝摘要跨仓储重建仍生效")
	stats, err = repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.Attempts)
	require.EqualValues(t, 1, stats.Successes)
	require.EqualValues(t, 1, stats.Failures)
	require.Equal(t, "invalid", stats.Records[0].Status)
	var isNull bool
	require.NoError(t, db.QueryRow("SELECT value_encrypted IS NULL FROM openai_healthy_turn_state_pool WHERE value_hash=$1", healthyStateHash(value.Value)).Scan(&isNull))
	require.True(t, isNull, "失败清除密文")

	newValue := service.HealthyTurnStateValue{Value: "测试状态头二", ExpiresAt: value.ExpiresAt.Add(2 * time.Minute)}
	stored, err = repo.Save(ctx, scope, newValue)
	require.NoError(t, err)
	require.True(t, stored)
	require.NoError(t, repo.Complete(ctx, scope, *claimed, false, 429))
	require.Error(t, repo.Start(ctx, scope, *claimed, 429), "旧租约不能再发送")
	got, err = repo.Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, newValue.Value, got.Value, "迟到结果不影响新头")

	_, err = db.Exec("UPDATE openai_healthy_turn_state_pool SET expires_at=NOW()-INTERVAL '1 second'")
	require.NoError(t, err)
	got, err = repo.Claim(ctx, scope, "")
	require.NoError(t, err)
	require.Nil(t, got)
	stats, err = repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, stats.Available)
	require.EqualValues(t, 2, stats.Captures)
	require.Equal(t, "expired", stats.Records[0].Status)
	encoded, err := json.Marshal(stats)
	require.NoError(t, err)
	for _, secret := range []string{value.Value, newValue.Value, encrypted, healthyStateHash(value.Value), "lease_token", "value_encrypted", "value_hash"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestHealthyTurnStateRepositoryIsolationAndProbes(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	repo := NewHealthyTurnStateRepository(db, &AESEncryptor{key: []byte(strings.Repeat("a", 32))})
	for i, scope := range []service.HealthyTurnStateScope{
		{AccountID: 1, Key: "HTTP代理一", Model: "模型一", Transport: "http", ProxyID: 1},
		{AccountID: 1, Key: "WS代理一", Model: "模型一", Transport: "websocket", ProxyID: 1},
		{AccountID: 1, Key: "HTTP代理二", Model: "模型二", Transport: "http", ProxyID: 2},
		{AccountID: 2, Key: "HTTP代理一", Model: "模型一", Transport: "http", ProxyID: 1},
	} {
		stored, err := repo.Save(ctx, scope, service.HealthyTurnStateValue{Value: fmt.Sprint("状态", i), ExpiresAt: time.Now().Add(time.Minute)})
		require.NoError(t, err)
		require.True(t, stored)
		got, err := repo.Claim(ctx, scope, "")
		require.NoError(t, err)
		require.Equal(t, fmt.Sprint("状态", i), got.Value)
		require.NoError(t, repo.Reject(ctx, scope, got.Value))
		require.Error(t, repo.Start(ctx, scope, *got, 429), "领取后已淘汰不能发送")
	}
	for i := range 55 {
		require.NoError(t, repo.RecordProbe(ctx, 1, service.HealthyTurnStateProbeLog{Model: fmt.Sprint("模型", i), Transport: "http", Status: "failed", HTTPStatus: 429}))
	}
	require.NoError(t, repo.RecordProbe(ctx, 1, service.HealthyTurnStateProbeLog{Model: "末次模型", Transport: "http", Status: "no_header", HTTPStatus: 200}))
	stats, err := repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.Len(t, stats.Records, 4)
	require.Len(t, stats.Probes, 50)
	require.Equal(t, "no_header", stats.Probes[0].Status)
	require.Equal(t, "failed", stats.Probes[1].Status)
	_, err = db.Exec("DELETE FROM accounts WHERE id=1")
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM openai_healthy_turn_state_pool").Scan(&count))
	require.Equal(t, 4, count, "共享池不随账号删除")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM openai_healthy_turn_state_probes WHERE account_id=1").Scan(&count))
	require.Zero(t, count)
}

func TestHealthyTurnStateSharedMigrationImportsLegacyRecords(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	cipher := &AESEncryptor{key: []byte(strings.Repeat("a", 32))}
	value := "迁移后的共享状态头"
	encrypted, err := cipher.Encrypt(value)
	require.NoError(t, err)
	expires := time.Now().Add(time.Minute)
	_, err = db.Exec(`INSERT INTO openai_healthy_turn_states(account_id,scope_key,model,transport,proxy_id,value_encrypted,value_hash,expires_at,captures,last_captured_at)
		VALUES(1,'legacy-scope','legacy-model','websocket',77,$1,$2,$3,3,NOW())`, encrypted, healthyStateHash(value), expires)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("240_openai_healthy_turn_state_shared_pool.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	repo := NewHealthyTurnStateRepository(db, cipher)
	claimed, err := repo.Claim(ctx, service.HealthyTurnStateScope{AccountID: 2, Model: "other-model", Transport: "http", ProxyID: 2}, "")
	require.NoError(t, err)
	require.NotNil(t, claimed)
	require.Equal(t, value, claimed.Value, "旧账号记录应进入账号无关的共享池")
	require.NoError(t, repo.Release(ctx, service.HealthyTurnStateScope{AccountID: 2}, *claimed))
}
