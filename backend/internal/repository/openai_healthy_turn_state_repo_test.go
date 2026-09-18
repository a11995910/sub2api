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
	scopedMigration, err := migrations.FS.ReadFile("242_openai_healthy_turn_state_account_model_pool.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(scopedMigration))
	require.NoError(t, err)
	proxyMigration, err := migrations.FS.ReadFile("243_openai_healthy_turn_state_temporary_proxy.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(proxyMigration))
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
	require.NoError(t, db.QueryRow("SELECT value_encrypted FROM openai_healthy_turn_state_account_model_pool").Scan(&encrypted))
	require.NotEqual(t, value.Value, encrypted)

	// 重建仓储后依然能读取，没有依赖进程缓存。
	repo = NewHealthyTurnStateRepository(db, cipher)
	got, err := repo.Get(ctx, scope)
	require.NoError(t, err)
	require.Equal(t, value.Value, got.Value)
	require.True(t, value.ExpiresAt.Equal(got.ExpiresAt))
	for _, foreignScope := range []service.HealthyTurnStateScope{
		{AccountID: 2, Model: scope.Model},
		{AccountID: scope.AccountID, Model: "另一个模型"},
		{AccountID: 0, Model: scope.Model},
		{AccountID: scope.AccountID, Model: " "},
	} {
		foreign, err := repo.Get(ctx, foreignScope)
		require.NoError(t, err)
		require.Nil(t, foreign, "账号或模型任一不符都不能读取记录")
		foreign, err = repo.Claim(ctx, foreignScope, "")
		require.NoError(t, err)
		require.Nil(t, foreign, "账号或模型任一不符都不能领取记录")
	}
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
	require.NoError(t, db.QueryRow("SELECT value_encrypted IS NULL FROM openai_healthy_turn_state_account_model_pool WHERE value_hash=$1", healthyStateHash(value.Value)).Scan(&isNull))
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

	_, err = db.Exec("UPDATE openai_healthy_turn_state_account_model_pool SET expires_at=NOW()-INTERVAL '1 second'")
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

func TestHealthyTurnStateRepositoryAccountModelIsolation(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	repo := NewHealthyTurnStateRepository(db, &AESEncryptor{key: []byte(strings.Repeat("a", 32))})
	scopes := []service.HealthyTurnStateScope{
		{AccountID: 1, Model: "模型一", Transport: "http", ProxyID: 1},
		{AccountID: 1, Model: "模型二", Transport: "http", ProxyID: 2},
		{AccountID: 2, Model: "模型一", Transport: "http", ProxyID: 1},
	}
	value := service.HealthyTurnStateValue{Value: "各范围内相同的状态头", ExpiresAt: time.Now().Add(time.Minute)}
	claimed := make([]*service.HealthyTurnStateValue, len(scopes))
	for i, scope := range scopes {
		stored, err := repo.Save(ctx, scope, value)
		require.NoError(t, err)
		require.True(t, stored, "相同头在不同账号和模型下独立保存")
		got, err := repo.Get(ctx, scope)
		require.NoError(t, err)
		require.Equal(t, value.Value, got.Value)
		claimed[i], err = repo.Claim(ctx, scope, "")
		require.NoError(t, err)
		require.NotNil(t, claimed[i], "不同范围的租约互不阻塞")
	}
	for _, foreignScope := range scopes[1:] {
		require.NoError(t, repo.Release(ctx, foreignScope, *claimed[0]))
		require.Error(t, repo.Start(ctx, foreignScope, *claimed[0], 429))
		require.NoError(t, repo.Complete(ctx, foreignScope, *claimed[0], false, 503))
	}
	stats, err := repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, stats.InUse, "错误范围不能归还或完成原租约")
	require.Zero(t, stats.Attempts)
	require.Zero(t, stats.Failures)
	require.NoError(t, repo.Start(ctx, scopes[0], *claimed[0], 429))
	require.NoError(t, repo.Complete(ctx, scopes[0], *claimed[0], false, 503))
	stored, err := repo.Save(ctx, scopes[0], value)
	require.NoError(t, err)
	require.False(t, stored)
	for i, scope := range scopes[1:] {
		require.NoError(t, repo.Start(ctx, scope, *claimed[i+1], 429))
		require.NoError(t, repo.Complete(ctx, scope, *claimed[i+1], true, 200))
		got, err := repo.Get(ctx, scope)
		require.NoError(t, err)
		require.NotNil(t, got, "其他范围不受失败拒绝摘要影响")
	}
	require.NoError(t, repo.Reject(ctx, scopes[1], value.Value))
	got, err := repo.Get(ctx, scopes[1])
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = repo.Get(ctx, scopes[2])
	require.NoError(t, err)
	require.NotNil(t, got, "拒绝只影响本账号本模型")
	future := service.HealthyTurnStateValue{Value: "尚未采集的拒绝头", ExpiresAt: value.ExpiresAt}
	require.NoError(t, repo.Reject(ctx, scopes[0], future.Value))
	stored, err = repo.Save(ctx, scopes[0], future)
	require.NoError(t, err)
	require.False(t, stored)
	for _, scope := range scopes[1:] {
		stored, err = repo.Save(ctx, scope, future)
		require.NoError(t, err)
		require.True(t, stored, "其他范围可以保存相同的头")
	}
}

func TestHealthyTurnStateRepositorySharesTransportAndProxyWithinModel(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	repo := NewHealthyTurnStateRepository(db, &AESEncryptor{key: []byte(strings.Repeat("a", 32))})
	scope := service.HealthyTurnStateScope{AccountID: 1, Key: "http范围", Model: " 模型一 ", Transport: "http", ProxyID: 1}
	value := service.HealthyTurnStateValue{Value: "同账号模型的状态头", ExpiresAt: time.Now().Add(time.Minute)}
	stored, err := repo.Save(ctx, scope, value)
	require.NoError(t, err)
	require.True(t, stored)
	shared := service.HealthyTurnStateScope{AccountID: 1, Key: "websocket范围", Model: "模型一", Transport: "websocket", ProxyID: 88}
	stored, err = repo.Save(ctx, shared, value)
	require.NoError(t, err)
	require.False(t, stored, "不按传输或代理重复累计")
	got, err := repo.Get(ctx, shared)
	require.NoError(t, err)
	require.Equal(t, value.Value, got.Value)
	got, err = repo.Claim(ctx, shared, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, repo.Release(ctx, scope, *got))
	got, err = repo.Claim(ctx, scope, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, repo.Reject(ctx, shared, value.Value))
	require.Error(t, repo.Start(ctx, scope, *got, 429), "同账号同模型的拒绝摘要跨传输方式生效")
	stats, err := repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 1, stats.Captures)
	require.Len(t, stats.Models, 1)
	require.Equal(t, "模型一", stats.Models[0].Model)
}

func TestHealthyTurnStateRepositoryStatsAreCompleteAndAccountScoped(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	repo := NewHealthyTurnStateRepository(db, &AESEncryptor{key: []byte(strings.Repeat("a", 32))})
	for _, group := range []struct {
		accountID int64
		model     string
		count     int
		success   bool
	}{{1, "模型一", 55, true}, {1, "模型二", 7, false}, {2, "模型一", 4, true}} {
		scope := service.HealthyTurnStateScope{AccountID: group.accountID, Model: group.model, Transport: "http"}
		for i := range group.count {
			stored, err := repo.Save(ctx, scope, service.HealthyTurnStateValue{Value: fmt.Sprint("状态", i), ExpiresAt: time.Now().Add(time.Minute)})
			require.NoError(t, err)
			require.True(t, stored)
			claimed, err := repo.Claim(ctx, scope, "")
			require.NoError(t, err)
			require.NotNil(t, claimed)
			require.NoError(t, repo.Start(ctx, scope, *claimed, 429))
			require.NoError(t, repo.Complete(ctx, scope, *claimed, group.success, 200))
		}
	}
	for i := range 55 {
		require.NoError(t, repo.RecordProbe(ctx, 1, service.HealthyTurnStateProbeLog{Model: fmt.Sprint("模型", i), Transport: "http", Status: "failed", HTTPStatus: 429}))
	}
	require.NoError(t, repo.RecordProbe(ctx, 1, service.HealthyTurnStateProbeLog{Model: "末次模型", Transport: "http", Status: "no_header", HTTPStatus: 200, TemporaryProxy: true}))
	require.NoError(t, repo.RecordProbe(ctx, 2, service.HealthyTurnStateProbeLog{Model: "其他账号模型", Transport: "http", Status: "failed", HTTPStatus: 503}))
	stats, err := repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.Len(t, stats.Records, 50)
	require.Len(t, stats.Probes, 50)
	require.Equal(t, "no_header", stats.Probes[0].Status)
	require.True(t, stats.Probes[0].TemporaryProxy, "动态代理测试不能被解释为直连")
	require.False(t, stats.Probes[1].TemporaryProxy, "普通测试保留原来源")
	require.Equal(t, "failed", stats.Probes[1].Status)
	require.EqualValues(t, 62, stats.Captures)
	require.EqualValues(t, 62, stats.Attempts)
	require.EqualValues(t, 55, stats.Successes)
	require.EqualValues(t, 7, stats.Failures)
	require.EqualValues(t, 55, stats.Available)
	require.Equal(t, []service.HealthyTurnStateModelStats{
		{Model: "模型一", Available: 55, Captures: 55, Attempts: 55, Successes: 55},
		{Model: "模型二", Captures: 7, Attempts: 7, Failures: 7},
	}, stats.Models, "累计统计不能截断为最近50条")
	_, err = db.Exec("UPDATE openai_healthy_turn_state_account_model_pool SET expires_at=NOW()-INTERVAL '1 second' WHERE account_id=1")
	require.NoError(t, err)
	stats, err = repo.Stats(ctx, 1)
	require.NoError(t, err)
	require.EqualValues(t, 62, stats.Captures, "过期仍保留累计")
	require.Zero(t, stats.Available)
	_, err = db.Exec("DELETE FROM accounts WHERE id=1")
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM openai_healthy_turn_state_account_model_pool").Scan(&count))
	require.Equal(t, 4, count, "账号删除只清理所属记录")
	require.NoError(t, db.QueryRow("SELECT COUNT(*) FROM openai_healthy_turn_state_probes WHERE account_id=1").Scan(&count))
	require.Zero(t, count)
	stats, err = repo.Stats(ctx, 2)
	require.NoError(t, err)
	require.EqualValues(t, 4, stats.Captures)
	require.EqualValues(t, 4, stats.Successes)
	require.Len(t, stats.Probes, 1)
}

func TestHealthyTurnStateAccountModelMigrationPreservesUnattributedHistory(t *testing.T) {
	db := healthyStateTestDB(t)
	ctx := context.Background()
	cipher := &AESEncryptor{key: []byte(strings.Repeat("a", 32))}
	value := "历史全局状态头"
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
	migration, err = migrations.FS.ReadFile("242_openai_healthy_turn_state_account_model_pool.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	repo := NewHealthyTurnStateRepository(db, cipher)
	for _, accountID := range []int64{1, 2} {
		scope := service.HealthyTurnStateScope{AccountID: accountID, Model: "legacy-model", Transport: "http"}
		claimed, err := repo.Claim(ctx, scope, "")
		require.NoError(t, err)
		require.Nil(t, claimed, "旧全局记录不能分配给任意账号")
		stats, err := repo.Stats(ctx, accountID)
		require.NoError(t, err)
		require.Zero(t, stats.Captures)
		require.Empty(t, stats.Models)
	}
	for _, table := range []string{"openai_healthy_turn_states", "openai_healthy_turn_state_pool"} {
		var captures int
		require.NoError(t, db.QueryRow("SELECT captures FROM "+table+" WHERE value_hash=$1", healthyStateHash(value)).Scan(&captures))
		require.Equal(t, 3, captures, "历史表保留原有累计")
	}
}
