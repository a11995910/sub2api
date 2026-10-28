//go:build integration || pelicanpostgres

package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func pelicanAccountFixtures(t *testing.T) []int64 {
	t.Helper()
	ids := []int64{}
	for _, kind := range []string{"oauth", "oauth", "apikey", "oauth"} {
		var id int64
		require.NoError(t, integrationDB.QueryRow(`INSERT INTO accounts(name,platform,type) VALUES ('测试账号','openai',$1) RETURNING id`, kind).Scan(&id))
		ids = append(ids, id)
	}
	_, err := integrationDB.Exec(`UPDATE accounts SET deleted_at=NOW() WHERE id=$1`, ids[3])
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = integrationDB.Exec(`DELETE FROM accounts WHERE id=ANY($1)`, pq.Array(ids)) })
	return ids
}
func TestPelicanAccountRepoLatestResultAndStaleWrites(t *testing.T) {
	ctx := context.Background()
	ids := pelicanAccountFixtures(t)
	repo := NewPelicanAccountTestRepository(integrationDB)
	now := time.Now().Truncate(time.Microsecond)
	input := service.PelicanAccountStart{AccountIDs: ids, Models: map[string]string{"openai": "测试模型"}}
	jobs, err := repo.Claim(ctx, input, "第一轮", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, jobs, 2)
	require.Equal(t, "测试模型", jobs[0].ModelID)
	duplicate, err := repo.Claim(ctx, input, "重复请求", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Empty(t, duplicate)
	first := jobs[0]
	first.Status = "success"
	first.ResponseText = "<svg>旧结果</svg>"
	first.FinishedAt = &now
	require.NoError(t, repo.Complete(ctx, first))
	// 新仓储实例仍能读取正文，证明不依赖服务内存。
	restored, err := NewPelicanAccountTestRepository(integrationDB).Get(ctx, first.AccountID)
	require.NoError(t, err)
	require.Equal(t, first.ResponseText, restored.ResponseText)
	summaries, err := repo.List(ctx)
	require.NoError(t, err)
	for _, summary := range summaries {
		require.Empty(t, summary.ResponseText)
	}
	next, err := repo.Claim(ctx, service.PelicanAccountStart{AccountIDs: []int64{first.AccountID}}, "第二轮", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, next, 1)
	require.NoError(t, repo.Complete(ctx, first))
	restored, err = repo.Get(ctx, first.AccountID)
	require.NoError(t, err)
	require.Equal(t, "第二轮", restored.RunID)
	require.Equal(t, "running", restored.Status)
	require.Empty(t, restored.ResponseText)
	stopped, err := repo.Stop(ctx, []service.PelicanAccountRun{{AccountID: first.AccountID, RunID: "第一轮"}})
	require.NoError(t, err)
	require.Empty(t, stopped)
	stopped, err = repo.Stop(ctx, []service.PelicanAccountRun{{AccountID: first.AccountID, RunID: "第二轮"}})
	require.NoError(t, err)
	require.Len(t, stopped, 1)
	next[0].Status = "success"
	next[0].ResponseText = "<svg>迟到结果</svg>"
	require.NoError(t, repo.Complete(ctx, next[0]))
	restored, err = repo.Get(ctx, first.AccountID)
	require.NoError(t, err)
	require.Equal(t, "stopped", restored.Status)
	require.Empty(t, restored.ResponseText)
	require.NoError(t, repo.Expire(ctx, now.Add(2*time.Minute)))
	other, err := repo.Get(ctx, jobs[1].AccountID)
	require.NoError(t, err)
	require.Equal(t, "failed", other.Status)
	latest, err := repo.Claim(ctx, service.PelicanAccountStart{AccountIDs: []int64{first.AccountID}}, "第三轮", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, latest, 1)
	latest[0].Status = "success"
	latest[0].ResponseText = "<svg>最新作品</svg>"
	latest[0].FinishedAt = &now
	require.NoError(t, repo.Complete(ctx, latest[0]))
	restored, err = NewPelicanAccountTestRepository(integrationDB).Get(ctx, first.AccountID)
	require.NoError(t, err)
	require.Equal(t, "<svg>最新作品</svg>", restored.ResponseText)
	var count int
	require.NoError(t, integrationDB.QueryRow(`SELECT count(*) FROM pelican_account_test_results WHERE account_id=$1`, first.AccountID).Scan(&count))
	require.Equal(t, 1, count)
}
func TestPelicanAccountRepoConcurrentClaim(t *testing.T) {
	ids := pelicanAccountFixtures(t)
	repo := NewPelicanAccountTestRepository(integrationDB)
	input := service.PelicanAccountStart{AccountIDs: ids}
	now := time.Now()
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	errs := make(chan error, 2)
	for _, run := range []string{"管理员一", "管理员二"} {
		wg.Add(1)
		go func(run string) {
			defer wg.Done()
			jobs, err := repo.Claim(context.Background(), input, run, now, now.Add(time.Minute))
			counts <- len(jobs)
			errs <- err
		}(run)
	}
	wg.Wait()
	close(counts)
	close(errs)
	total := 0
	for count := range counts {
		total += count
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 2, total)
}

func TestPelicanAccountRepoAllOnlyClaimsOAuth(t *testing.T) {
	ids := pelicanAccountFixtures(t)
	repo := NewPelicanAccountTestRepository(integrationDB)
	now := time.Now()
	jobs, err := repo.Claim(context.Background(), service.PelicanAccountStart{All: true}, "全部测试", now, now.Add(time.Minute))
	require.NoError(t, err)
	claimed := map[int64]bool{}
	for _, job := range jobs {
		claimed[job.AccountID] = true
	}
	require.True(t, claimed[ids[0]])
	require.True(t, claimed[ids[1]])
	require.False(t, claimed[ids[2]])
	require.False(t, claimed[ids[3]])
}

func TestPelicanAccountRepoSelectedAPIKeysAndTypeIsolation(t *testing.T) {
	ctx := context.Background()
	ids := pelicanAccountFixtures(t)
	repo := NewPelicanAccountTestRepository(integrationDB)
	now := time.Now()
	jobs, err := repo.Claim(ctx, service.PelicanAccountStart{AccountType: "apikey", AccountIDs: []int64{ids[0], ids[2], ids[3]}}, "密钥测试", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, ids[2], jobs[0].AccountID)
	jobs[0].Status = "success"
	jobs[0].ResponseText = "<svg>密钥作品</svg>"
	require.NoError(t, repo.Complete(ctx, jobs[0]))
	saved, err := repo.Get(ctx, ids[2])
	require.NoError(t, err)
	require.Equal(t, "<svg>密钥作品</svg>", saved.ResponseText)
	list, err := repo.List(ctx)
	require.NoError(t, err)
	found := false
	for _, r := range list {
		if r.AccountID == ids[2] {
			found = true
		}
	}
	require.True(t, found)
	oauth, err := repo.Claim(ctx, service.PelicanAccountStart{AccountType: "oauth", AccountIDs: []int64{ids[1], ids[2]}}, "所选OAuth", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, oauth, 1)
	require.Equal(t, ids[1], oauth[0].AccountID)
	missing, err := repo.Get(ctx, ids[0])
	require.NoError(t, err)
	require.Nil(t, missing)
}
