package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
)

const pelicanAccountTimeout = 10 * time.Minute

type PelicanAccountResult struct {
	AccountID       int64      `json:"account_id"`
	RunID           string     `json:"run_id"`
	Status          string     `json:"status"`
	ModelID         string     `json:"model_id"`
	ReasoningEffort string     `json:"reasoning_effort"`
	ResponseText    string     `json:"response_text,omitempty"`
	ErrorMessage    string     `json:"error_message"`
	StartedAt       time.Time  `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	RunningUntil    time.Time  `json:"running_until"`
}
type PelicanAccountRun struct {
	AccountID int64  `json:"account_id"`
	RunID     string `json:"run_id"`
}
type PelicanAccountStart struct {
	All             bool              `json:"all"`
	AccountIDs      []int64           `json:"account_ids"`
	Models          map[string]string `json:"models"`
	ReasoningEffort string            `json:"reasoning_effort"`
}
type PelicanAccountTestRepository interface {
	Claim(context.Context, PelicanAccountStart, string, time.Time, time.Time) ([]*PelicanAccountResult, error)
	List(context.Context) ([]*PelicanAccountResult, error)
	Get(context.Context, int64) (*PelicanAccountResult, error)
	Complete(context.Context, *PelicanAccountResult) error
	Stop(context.Context, []PelicanAccountRun) ([]PelicanAccountRun, error)
	Expire(context.Context, time.Time) error
}
type pelicanAccountCancel struct {
	runID  string
	cancel context.CancelFunc
}
type PelicanAccountTestService struct {
	repo       PelicanAccountTestRepository
	runAccount func(context.Context, int64, string, *PelicanTestConfig) (*ScheduledTestResult, error)
	mu         sync.Mutex
	active     map[int64]pelicanAccountCancel
}

func NewPelicanAccountTestService(repo PelicanAccountTestRepository, tests *AccountTestService) *PelicanAccountTestService {
	return &PelicanAccountTestService{repo: repo, runAccount: tests.RunPelicanBackground, active: make(map[int64]pelicanAccountCancel)}
}
func (s *PelicanAccountTestService) Start(ctx context.Context, input PelicanAccountStart) ([]*PelicanAccountResult, error) {
	if (!input.All && len(input.AccountIDs) == 0) || (input.All && len(input.AccountIDs) > 0) {
		return nil, infraerrors.BadRequest("PELICAN_ACCOUNT_SELECTION", "请选择全部账号或指定账号")
	}
	for _, id := range input.AccountIDs {
		if id <= 0 {
			return nil, infraerrors.BadRequest("PELICAN_ACCOUNT_SELECTION", "账号编号无效")
		}
	}
	for platform, model := range input.Models {
		model = strings.TrimSpace(model)
		if len(model) > 200 {
			return nil, infraerrors.BadRequest("PELICAN_ACCOUNT_MODEL", "模型名称过长")
		}
		input.Models[platform] = model
	}
	input.ReasoningEffort = strings.TrimSpace(input.ReasoningEffort)
	if input.ReasoningEffort != "" && normalizePelicanReasoningEffort(input.ReasoningEffort) == "" {
		return nil, infraerrors.BadRequest("PELICAN_ACCOUNT_EFFORT", "推理强度无效")
	}
	input.ReasoningEffort = normalizePelicanReasoningEffort(input.ReasoningEffort)
	// 注册取消函数与数据库认领在同一临界区，避免停止请求错过刚启动的任务。
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	results, err := s.repo.Claim(ctx, input, uuid.NewString(), now, now.Add(pelicanAccountTimeout+time.Minute))
	if err != nil {
		return nil, err
	}
	for _, result := range results {
		jobCtx, cancel := context.WithTimeout(context.Background(), pelicanAccountTimeout)
		s.active[result.AccountID] = pelicanAccountCancel{result.RunID, cancel}
		// 每个账号独立启动，不设置跨账号的工作池或并发上限。
		job := *result
		go s.execute(jobCtx, cancel, &job)
	}
	return results, nil
}
func (s *PelicanAccountTestService) execute(ctx context.Context, cancel context.CancelFunc, job *PelicanAccountResult) {
	defer cancel()
	defer func() {
		s.mu.Lock()
		if active, ok := s.active[job.AccountID]; ok && active.runID == job.RunID {
			delete(s.active, job.AccountID)
		}
		s.mu.Unlock()
	}()
	// 单账号异常不能使其他账号或整个应用退出。
	defer func() {
		if recovered := recover(); recovered != nil {
			job.Status = "failed"
			job.ResponseText = ""
			job.ErrorMessage = "测试执行异常，请重试"
			s.complete(job)
		}
	}()
	cfg := &PelicanTestConfig{Prompt: "创建一个 HTML，内容是 SVG 绘制一个鹈鹕骑自行车的 2D 动画。", ReasoningEffort: job.ReasoningEffort, ParallelCount: 1, QuestionKind: "pelican"}
	result, err := s.runAccount(ctx, job.AccountID, job.ModelID, cfg)
	job.Status = "failed"
	switch {
	case ctx.Err() != nil:
		job.ErrorMessage = "测试已取消或超过 10 分钟，请重试"
	case err != nil:
		job.ErrorMessage = err.Error()
	case result == nil:
		job.ErrorMessage = "测试未返回结果"
	default:
		job.Status = result.Status
		job.ResponseText = result.ResponseText
		job.ErrorMessage = result.ErrorMessage
		if result.PelicanConfig != nil {
			job.ModelID = result.PelicanConfig.ModelID
		}
	}
	if job.Status != "success" {
		job.Status = "failed"
		job.ResponseText = ""
	}
	s.complete(job)
}
func (s *PelicanAccountTestService) complete(job *PelicanAccountResult) {
	now := time.Now()
	job.FinishedAt = &now
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.repo.Complete(ctx, job); err != nil {
		logger.LegacyPrintf("service.pelican_account_tests", "保存账号测试结果失败：account_id=%d error=%v", job.AccountID, err)
	}
}
func (s *PelicanAccountTestService) List(ctx context.Context) ([]*PelicanAccountResult, error) {
	if err := s.repo.Expire(ctx, time.Now()); err != nil {
		return nil, err
	}
	return s.repo.List(ctx)
}
func (s *PelicanAccountTestService) Get(ctx context.Context, id int64) (*PelicanAccountResult, error) {
	result, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, infraerrors.NotFound("PELICAN_ACCOUNT_RESULT_NOT_FOUND", "账号尚无测试结果")
	}
	return result, nil
}
func (s *PelicanAccountTestService) Stop(ctx context.Context, runs []PelicanAccountRun) error {
	for _, run := range runs {
		if run.AccountID <= 0 || run.RunID == "" {
			return infraerrors.BadRequest("PELICAN_ACCOUNT_RUN", fmt.Sprintf("账号 %d 的运行标识无效", run.AccountID))
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stopped, err := s.repo.Stop(ctx, runs)
	if err != nil {
		return err
	}
	for _, run := range stopped {
		if active, ok := s.active[run.AccountID]; ok && active.runID == run.RunID {
			active.cancel()
		}
	}
	return nil
}
