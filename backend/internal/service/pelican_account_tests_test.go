package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pelicanAccountRepoFake struct {
	PelicanAccountTestRepository
	count     int
	completed chan PelicanAccountResult
}

func (r *pelicanAccountRepoFake) Claim(_ context.Context, _ PelicanAccountStart, run string, now, until time.Time) ([]*PelicanAccountResult, error) {
	out := []*PelicanAccountResult{}
	for i := 1; i <= r.count; i++ {
		out = append(out, &PelicanAccountResult{AccountID: int64(i), RunID: run, Status: "running", StartedAt: now, RunningUntil: until})
	}
	return out, nil
}
func (r *pelicanAccountRepoFake) Complete(_ context.Context, result *PelicanAccountResult) error {
	r.completed <- *result
	return nil
}
func (r *pelicanAccountRepoFake) Stop(_ context.Context, runs []PelicanAccountRun) ([]PelicanAccountRun, error) {
	return runs, nil
}

func TestPelicanAccountTestsStartAllIndependentlyAndSurviveRequestCancellation(t *testing.T) {
	repo := &pelicanAccountRepoFake{count: 8, completed: make(chan PelicanAccountResult, 8)}
	svc := NewPelicanAccountTestService(repo, nil)
	started := make(chan int64, 8)
	release := make(chan struct{})
	svc.runAccount = func(ctx context.Context, id int64, _ string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		started <- id
		select {
		case <-release:
			return &ScheduledTestResult{Status: "success", ResponseText: "<svg></svg>"}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results, err := svc.Start(ctx, PelicanAccountStart{All: true})
	require.NoError(t, err)
	require.Len(t, results, 8)
	cancel()
	// 所有八个任务都必须先启动，不能等前三个任务完成。
	for i := 0; i < 8; i++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("账号没有同时启动")
		}
	}
	close(release)
	for i := 0; i < 8; i++ {
		select {
		case result := <-repo.completed:
			require.Equal(t, "success", result.Status)
		case <-time.After(2 * time.Second):
			t.Fatal("结果未保存")
		}
	}
}
func TestPelicanAccountTestsIsolateFailureAndPanic(t *testing.T) {
	repo := &pelicanAccountRepoFake{count: 3, completed: make(chan PelicanAccountResult, 3)}
	svc := NewPelicanAccountTestService(repo, nil)
	svc.runAccount = func(_ context.Context, id int64, _ string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		if id == 1 {
			panic("模拟单账号异常")
		}
		if id == 2 {
			return &ScheduledTestResult{Status: "failed", ResponseText: "截断内容", ErrorMessage: "输出截断"}, nil
		}
		return &ScheduledTestResult{Status: "success", ResponseText: "<svg></svg>"}, nil
	}
	_, err := svc.Start(context.Background(), PelicanAccountStart{All: true})
	require.NoError(t, err)
	saved := map[int64]PelicanAccountResult{}
	for i := 0; i < 3; i++ {
		select {
		case result := <-repo.completed:
			saved[result.AccountID] = result
		case <-time.After(2 * time.Second):
			t.Fatal("结果未保存")
		}
	}
	require.Equal(t, "failed", saved[1].Status)
	require.Empty(t, saved[2].ResponseText)
	require.Equal(t, "success", saved[3].Status)
}
func TestPelicanAccountTestsStopCancelsUpstream(t *testing.T) {
	repo := &pelicanAccountRepoFake{count: 1, completed: make(chan PelicanAccountResult, 1)}
	svc := NewPelicanAccountTestService(repo, nil)
	started := make(chan struct{})
	svc.runAccount = func(ctx context.Context, _ int64, _ string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	results, err := svc.Start(context.Background(), PelicanAccountStart{All: true})
	require.NoError(t, err)
	<-started
	require.NoError(t, svc.Stop(context.Background(), []PelicanAccountRun{{AccountID: 1, RunID: results[0].RunID}}))
	select {
	case <-repo.completed:
	case <-time.After(2 * time.Second):
		t.Fatal("上游请求未取消")
	}
}
func TestPelicanAccountTestsRejectInvalidSelection(t *testing.T) {
	svc := NewPelicanAccountTestService(nil, nil)
	for _, input := range []PelicanAccountStart{{AccountType: "setup-token", All: true}, {}, {All: true, AccountIDs: []int64{1}}, {AccountIDs: []int64{-1}}, {All: true, ReasoningEffort: "错误值"}} {
		_, err := svc.Start(context.Background(), input)
		require.Error(t, err)
	}
}
