package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCandyAnswerParsing(t *testing.T) {
	for _, tc := range []struct {
		output, answer string
		valid          bool
	}{
		{"21", "21", true}, {" +0021 \n", "21", true}, {"-21", "-21", true},
		{`{"answer":21}`, "21", true}, {`{"answer":"21"}`, "21", true},
		{"先考虑 21，但它不能保证。\n最终答案：22 个糖果。", "22", true},
		{"先考虑 21，但答案不是 21", "", false}, {"121", "121", true},
		{`{"answer":21,"answer":22}`, "", false}, {`{"answer":21,"reason":"21"}`, "", false},
		{`{"answer":21} 多余解释`, "", false}, {`{"answer":21}{"answer":22}`, "", false},
		{`{"answer":21.0}`, "", false}, {`{"answer":2.1e1}`, "", false}, {`{"answer":null}`, "", false},
		{`{"answer":true}`, "", false}, {`{"answer":[]}`, "", false}, {`{"result":21}`, "", false},
		{`{"answer":21`, "", false}, {"21 或 22", "", false}, {"21.5", "", false}, {"21\n22", "", false},
		{"答案：21\n还有补充", "", false}, {"```json\n{\"answer\":21}\n```", "", false},
		{"", "", false}, {strings.Repeat("9", 102), "", false},
	} {
		t.Run(tc.output, func(t *testing.T) {
			answer, valid := parseCandyAnswer(tc.output)
			require.Equal(t, tc.valid, valid)
			require.Equal(t, tc.answer, answer)
		})
	}
}

func TestCandyDailyScheduleUsesBeijing(t *testing.T) {
	now := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	next, err := NextCandyTestRun("08:00", now)
	require.NoError(t, err)
	require.Equal(t, now.Add(24*time.Hour), next.UTC())
	next, err = NextCandyTestRun("08:01", now.In(time.FixedZone("测试时区", -7*60*60)))
	require.NoError(t, err)
	require.Equal(t, now.Add(time.Minute), next.UTC())
	for _, value := range []string{"8:00", "24:00", "12:60", "08:00:00", ""} {
		_, err := NextCandyTestRun(value, now)
		require.Error(t, err)
	}
	require.Equal(t, 11*time.Minute, CandyTestLeaseDuration(10))
	require.Equal(t, 21*time.Minute, CandyTestLeaseDuration(11))
}

type candyServiceRepo struct {
	CandyTestRepository
	mu               sync.Mutex
	settings         CandyTestSettingsInput
	runID            string
	count            int
	completed        chan CandyTestResult
	finished         chan struct{}
	claimedSelection CandyTestStart
	claimedScheduled bool
	claimErr         error
}

func newCandyServiceRepo(count int) *candyServiceRepo {
	return &candyServiceRepo{settings: DefaultCandyTestSettings(), count: count, completed: make(chan CandyTestResult, count), finished: make(chan struct{}, 1)}
}

func (r *candyServiceRepo) Claim(_ context.Context, selection CandyTestStart, runID string, now, _ time.Time, scheduled bool) (*CandyTestBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claimedSelection, r.claimedScheduled = selection, scheduled
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	r.runID = runID
	batch := &CandyTestBatch{RunID: runID, Settings: r.settings, Results: []*CandyTestResult{}}
	for i := 1; i <= r.count; i++ {
		snapshot := r.settings
		batch.Results = append(batch.Results, &CandyTestResult{ID: int64(i), AccountID: int64(i), RunID: runID, Status: "running", StartedAt: now, RunningUntil: now.Add(CandyTestLeaseDuration(r.count)), SettingsSnapshot: &snapshot})
	}
	return batch, nil
}
func (r *candyServiceRepo) GetSettings(context.Context) (*CandyTestSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return &CandyTestSettings{CandyTestSettingsInput: r.settings, RunningRunID: r.runID}, nil
}
func (r *candyServiceRepo) UpdateSettings(_ context.Context, settings *CandyTestSettingsInput, _ time.Time) (*CandyTestSettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.settings = *settings
	return &CandyTestSettings{CandyTestSettingsInput: *settings, RunningRunID: r.runID}, nil
}
func (r *candyServiceRepo) Complete(_ context.Context, result *CandyTestResult) error {
	r.completed <- *result
	return nil
}
func (r *candyServiceRepo) Finish(context.Context, string, time.Time) error {
	r.finished <- struct{}{}
	return nil
}
func (r *candyServiceRepo) Stop(context.Context, string, time.Time) error {
	r.mu.Lock()
	r.runID = ""
	r.mu.Unlock()
	return nil
}
func (r *candyServiceRepo) Expire(context.Context, time.Time) error { return nil }

type candyServiceGroups struct {
	GroupRepository
	groups map[int64]*Group
}

func (g candyServiceGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	return g.groups[id], nil
}

func TestCandySettingsValidationAllowsDisablingStaleGroups(t *testing.T) {
	repo := newCandyServiceRepo(0)
	svc := NewCandyTestService(repo, candyServiceGroups{groups: map[int64]*Group{1: {ID: 1, Platform: PlatformOpenAI, Status: StatusActive}, 2: {ID: 2, Platform: PlatformAnthropic, Status: StatusActive}, 3: {ID: 3, Platform: PlatformOpenAI, Status: StatusDisabled}}}, nil)
	input := DefaultCandyTestSettings()
	input.ExpectedAnswer = "+0021"
	input.ModelID = " "
	input.NormalAddGroupIDs = []int64{999}
	settings, err := svc.UpdateSettings(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, "21", settings.ExpectedAnswer)
	require.Equal(t, "gpt-6-astra", settings.ModelID)
	input.AutoGroupEnabled = true
	for _, id := range []int64{999, 2, 3} {
		input.NormalAddGroupIDs = []int64{id}
		_, err = svc.UpdateSettings(context.Background(), input)
		require.Error(t, err)
	}
	input.NormalAddGroupIDs = []int64{1, 1}
	settings, err = svc.UpdateSettings(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, settings.NormalAddGroupIDs)
	input.NormalRemoveGroupIDs = []int64{1}
	_, err = svc.UpdateSettings(context.Background(), input)
	require.Error(t, err)
	input = DefaultCandyTestSettings()
	input.ModelID = "gpt-image-2"
	_, err = svc.UpdateSettings(context.Background(), input)
	require.Error(t, err)
	input = DefaultCandyTestSettings()
	input.ExpectedAnswer = "21或22"
	_, err = svc.UpdateSettings(context.Background(), input)
	require.Error(t, err)
}

func TestCandyWorkersBoundConcurrencyAndKeepSnapshot(t *testing.T) {
	repo := newCandyServiceRepo(23)
	svc := NewCandyTestService(repo, nil, nil)
	started := make(chan struct{}, 23)
	release := make(chan struct{})
	var active, maximum atomic.Int64
	svc.runAccount = func(_ context.Context, _ int64, _ string, cfg *PelicanTestConfig) (*ScheduledTestResult, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for old := maximum.Load(); current > old && !maximum.CompareAndSwap(old, current); old = maximum.Load() {
		}
		started <- struct{}{}
		<-release
		if cfg.Prompt != CandyTestDefaultPrompt {
			return nil, errors.New("运行快照被覆盖")
		}
		return &ScheduledTestResult{Status: "success", ResponseText: `{"answer":21}`}, nil
	}
	requestCtx, cancel := context.WithCancel(context.Background())
	batch, err := svc.Start(requestCtx, CandyTestStart{All: true})
	require.NoError(t, err)
	require.Len(t, batch.Results, 23)
	cancel()
	for i := 0; i < CandyTestMaxWorkers; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("工作池未启动")
		}
	}
	input := DefaultCandyTestSettings()
	input.Prompt = "已修改题目"
	input.ExpectedAnswer = "22"
	_, err = svc.UpdateSettings(context.Background(), input)
	require.NoError(t, err)
	close(release)
	for i := 0; i < 23; i++ {
		select {
		case result := <-repo.completed:
			require.Equal(t, "normal", result.Status)
		case <-time.After(2 * time.Second):
			t.Fatal("排队账号未得到结果")
		}
	}
	require.LessOrEqual(t, maximum.Load(), int64(CandyTestMaxWorkers))
	select {
	case <-repo.finished:
	case <-time.After(time.Second):
		t.Fatal("批次未释放")
	}
}

func TestCandyScoresOnlyCompleteAnswersAndIsolatesPanics(t *testing.T) {
	repo := newCandyServiceRepo(6)
	svc := NewCandyTestService(repo, nil, nil)
	svc.runAccount = func(_ context.Context, id int64, _ string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		switch id {
		case 1:
			return &ScheduledTestResult{Status: "success", ResponseText: "21"}, nil
		case 2:
			return &ScheduledTestResult{Status: "success", ResponseText: "考虑21\n答案：22"}, nil
		case 3:
			return &ScheduledTestResult{Status: "failed", ResponseText: "21", ErrorMessage: "流截断"}, nil
		case 4:
			return &ScheduledTestResult{Status: "success", ResponseText: "答案不是21"}, nil
		case 5:
			panic("模拟错误")
		default:
			return nil, errors.New("网络失败")
		}
	}
	_, err := svc.Start(context.Background(), CandyTestStart{All: true})
	require.NoError(t, err)
	saved := map[int64]CandyTestResult{}
	for i := 0; i < 6; i++ {
		select {
		case result := <-repo.completed:
			saved[result.AccountID] = result
		case <-time.After(time.Second):
			t.Fatal("结果未保存")
		}
	}
	require.Equal(t, "normal", saved[1].Status)
	require.Equal(t, "degraded", saved[2].Status)
	require.Equal(t, "22", saved[2].Answer)
	for _, id := range []int64{3, 4, 5, 6} {
		require.Equal(t, "error", saved[id].Status)
		require.Empty(t, saved[id].Answer)
	}
}

func TestCandyStopCancelsUpstreamAndQueuedAccounts(t *testing.T) {
	repo := newCandyServiceRepo(12)
	svc := NewCandyTestService(repo, nil, nil)
	started := make(chan struct{}, 12)
	var called atomic.Int64
	svc.runAccount = func(ctx context.Context, _ int64, _ string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		called.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	batch, err := svc.Start(context.Background(), CandyTestStart{All: true})
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("工作池未启动")
		}
	}
	require.NoError(t, svc.Stop(context.Background(), batch.RunID))
	for i := 0; i < 12; i++ {
		select {
		case result := <-repo.completed:
			require.Equal(t, "error", result.Status)
		case <-time.After(time.Second):
			t.Fatal("取消没有完成")
		}
	}
	require.Equal(t, int64(10), called.Load())
}

func TestCandyScheduledSelectionAlwaysAll(t *testing.T) {
	repo := newCandyServiceRepo(0)
	repo.claimErr = ErrCandyTestRunning
	svc := NewCandyTestService(repo, nil, nil)
	svc.RunDue(context.Background(), time.Now())
	require.True(t, repo.claimedScheduled)
	require.True(t, repo.claimedSelection.All)
	require.Empty(t, repo.claimedSelection.AccountIDs)
}
