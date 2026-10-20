package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/google/uuid"
)

const CandyTestDefaultPrompt = `在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）
苹果味 桃子味 西瓜味
圆形 7 9 8
五角星形 7 6 4`

const (
	CandyTestMaxWorkers     = 10
	CandyTestAccountTimeout = 10 * time.Minute
)

func CandyTestLeaseDuration(accountCount int) time.Duration {
	waves := max(1, (accountCount+CandyTestMaxWorkers-1)/CandyTestMaxWorkers)
	return time.Duration(waves)*CandyTestAccountTimeout + time.Minute
}

var (
	ErrCandyTestRunning        = infraerrors.Conflict("CANDY_TEST_RUNNING", "已有糖果测试正在运行")
	ErrCandyTestResultNotFound = infraerrors.NotFound("CANDY_TEST_RESULT_NOT_FOUND", "糖果测试结果不存在")
	ErrCandyTestNoAccounts     = infraerrors.BadRequest("CANDY_TEST_NO_ACCOUNTS", "没有可测试的 GPT OAuth 主账号")
	candyIntegerPattern        = regexp.MustCompile(`^[+-]?[0-9]+$`)
	candyFinalAnswerPattern    = regexp.MustCompile(`(?i)^(?:最终答案|答案|final answer|answer)\s*[:：=]\s*([+-]?[0-9]+)\s*(?:个|颗|枚)?\s*(?:糖果)?[。.!！]?$`)
	candyDailyTimePattern      = regexp.MustCompile(`^(?:[01][0-9]|2[0-3]):[0-5][0-9]$`)
	candyBeijingLocation       = time.FixedZone("Asia/Shanghai", 8*60*60)
)

type CandyTestSettingsInput struct {
	Prompt                 string  `json:"prompt"`
	ExpectedAnswer         string  `json:"expected_answer"`
	ModelID                string  `json:"model_id"`
	ReasoningEffort        string  `json:"reasoning_effort"`
	ScheduleEnabled        bool    `json:"schedule_enabled"`
	DailyTime              string  `json:"daily_time"`
	AutoGroupEnabled       bool    `json:"auto_group_enabled"`
	NormalAddGroupIDs      []int64 `json:"normal_add_group_ids"`
	NormalRemoveGroupIDs   []int64 `json:"normal_remove_group_ids"`
	DegradedAddGroupIDs    []int64 `json:"degraded_add_group_ids"`
	DegradedRemoveGroupIDs []int64 `json:"degraded_remove_group_ids"`
}

type CandyTestSettings struct {
	CandyTestSettingsInput
	LastRunAt    *time.Time `json:"last_run_at"`
	NextRunAt    *time.Time `json:"next_run_at"`
	RunningUntil *time.Time `json:"running_until"`
	RunningRunID string     `json:"running_run_id"`
}

type CandyTestGroupAction struct {
	AddedGroupIDs   []int64 `json:"added_group_ids"`
	RemovedGroupIDs []int64 `json:"removed_group_ids"`
}

type CandyTestResult struct {
	ID               int64                   `json:"id"`
	AccountID        int64                   `json:"account_id"`
	AccountName      string                  `json:"account_name"`
	RunID            string                  `json:"run_id"`
	Status           string                  `json:"status"`
	Answer           string                  `json:"answer"`
	ResponseText     string                  `json:"response_text,omitempty"`
	ErrorMessage     string                  `json:"error_message"`
	ModelID          string                  `json:"model_id"`
	ReasoningEffort  string                  `json:"reasoning_effort"`
	StartedAt        time.Time               `json:"started_at"`
	FinishedAt       *time.Time              `json:"finished_at"`
	RunningUntil     time.Time               `json:"running_until"`
	SettingsSnapshot *CandyTestSettingsInput `json:"settings_snapshot,omitempty"`
	GroupAction      *CandyTestGroupAction   `json:"group_action"`
	GroupError       string                  `json:"group_error"`
}

type CandyTestStart struct {
	All        bool    `json:"all"`
	AccountIDs []int64 `json:"account_ids"`
}

type CandyTestBatch struct {
	RunID    string                 `json:"run_id"`
	Settings CandyTestSettingsInput `json:"settings"`
	Results  []*CandyTestResult     `json:"results"`
}

type CandyTestRepository interface {
	GetSettings(context.Context) (*CandyTestSettings, error)
	UpdateSettings(context.Context, *CandyTestSettingsInput, time.Time) (*CandyTestSettings, error)
	Claim(context.Context, CandyTestStart, string, time.Time, time.Time, bool) (*CandyTestBatch, error)
	Complete(context.Context, *CandyTestResult) error
	Finish(context.Context, string, time.Time) error
	Stop(context.Context, string, time.Time) error
	Expire(context.Context, time.Time) error
	ListResults(context.Context, int, int) ([]*CandyTestResult, int64, error)
	GetResult(context.Context, int64) (*CandyTestResult, error)
}

type CandyTestService struct {
	repo       CandyTestRepository
	groups     pelicanGroupTestGroups
	runAccount func(context.Context, int64, string, *PelicanTestConfig) (*ScheduledTestResult, error)
	now        func() time.Time
	mu         sync.Mutex
	active     map[string]context.CancelFunc
}

func NewCandyTestService(repo CandyTestRepository, groups GroupRepository, tests *AccountTestService) *CandyTestService {
	return &CandyTestService{repo: repo, groups: groups, runAccount: tests.RunCandyBackground, now: time.Now, active: make(map[string]context.CancelFunc)}
}

func DefaultCandyTestSettings() CandyTestSettingsInput {
	return CandyTestSettingsInput{Prompt: CandyTestDefaultPrompt, ExpectedAnswer: "21", ReasoningEffort: "high", DailyTime: "08:00", NormalAddGroupIDs: []int64{}, NormalRemoveGroupIDs: []int64{}, DegradedAddGroupIDs: []int64{}, DegradedRemoveGroupIDs: []int64{}}
}

// 每天按北京时间计算下一次执行，不依赖宿主机时区，也不追补停机期间的每一天。
func NextCandyTestRun(dailyTime string, now time.Time) (time.Time, error) {
	if !candyDailyTimePattern.MatchString(dailyTime) {
		return time.Time{}, infraerrors.BadRequest("CANDY_TEST_TIME", "测试时间须为北京时间 HH:mm")
	}
	clock, _ := time.Parse("15:04", dailyTime)
	local := now.In(candyBeijingLocation)
	next := time.Date(local.Year(), local.Month(), local.Day(), clock.Hour(), clock.Minute(), 0, 0, candyBeijingLocation)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

func (s *CandyTestService) GetSettings(ctx context.Context) (*CandyTestSettings, error) {
	if err := s.repo.Expire(ctx, s.now()); err != nil {
		return nil, err
	}
	return s.repo.GetSettings(ctx)
}

func (s *CandyTestService) UpdateSettings(ctx context.Context, input CandyTestSettingsInput) (*CandyTestSettings, error) {
	input.Prompt = strings.TrimSpace(input.Prompt)
	if input.Prompt == "" || len(input.Prompt) > 32000 {
		return nil, infraerrors.BadRequest("CANDY_TEST_PROMPT", "题目不能为空且不得超过 32000 字节")
	}
	answer, ok := normalizeCandyInteger(strings.TrimSpace(input.ExpectedAnswer))
	if !ok {
		return nil, infraerrors.BadRequest("CANDY_TEST_ANSWER", "标准答案必须是完整整数，最多 100 位")
	}
	input.ExpectedAnswer = answer
	input.ModelID = strings.TrimSpace(input.ModelID)
	if len(input.ModelID) > 200 {
		return nil, infraerrors.BadRequest("CANDY_TEST_MODEL", "模型名称不得超过 200 字节")
	}
	if isOpenAIImageModel(input.ModelID) {
		return nil, infraerrors.BadRequest("CANDY_TEST_MODEL", "糖果测试需要文本模型，不能使用图像模型")
	}
	input.ReasoningEffort = normalizePelicanReasoningEffort(input.ReasoningEffort)
	if input.ReasoningEffort == "" {
		return nil, infraerrors.BadRequest("CANDY_TEST_EFFORT", "推理强度无效")
	}
	input.DailyTime = strings.TrimSpace(input.DailyTime)
	if _, err := NextCandyTestRun(input.DailyTime, s.now()); err != nil {
		return nil, err
	}
	seen := make(map[int64]bool)
	for _, pair := range [][2]*[]int64{{&input.NormalAddGroupIDs, &input.NormalRemoveGroupIDs}, {&input.DegradedAddGroupIDs, &input.DegradedRemoveGroupIDs}} {
		for _, ids := range pair {
			*ids = slices.Clone(*ids)
			slices.Sort(*ids)
			*ids = slices.Compact(*ids)
			if *ids == nil {
				*ids = []int64{}
			}
			for _, id := range *ids {
				if id <= 0 {
					return nil, infraerrors.BadRequest("CANDY_TEST_GROUP", "分组编号无效")
				}
				if !input.AutoGroupEnabled {
					continue
				}
				if seen[id] {
					continue
				}
				group, err := s.groups.GetByID(ctx, id)
				if err != nil && !errors.Is(err, ErrGroupNotFound) {
					return nil, err
				}
				if group == nil || err != nil || group.Platform != PlatformOpenAI || group.Status != StatusActive {
					return nil, infraerrors.BadRequest("CANDY_TEST_GROUP", fmt.Sprintf("分组 %d 必须是启用中的 OpenAI 分组", id))
				}
				seen[id] = true
			}
		}
		for _, id := range *pair[0] {
			if slices.Contains(*pair[1], id) {
				return nil, infraerrors.BadRequest("CANDY_TEST_GROUP", "同一测试结果不能同时加入和移除同一分组")
			}
		}
	}
	return s.repo.UpdateSettings(ctx, &input, s.now())
}

func (s *CandyTestService) Start(ctx context.Context, input CandyTestStart) (*CandyTestBatch, error) {
	if input.All == (len(input.AccountIDs) > 0) {
		return nil, infraerrors.BadRequest("CANDY_TEST_SELECTION", "请选择全部账号或指定账号")
	}
	for _, id := range input.AccountIDs {
		if id <= 0 {
			return nil, infraerrors.BadRequest("CANDY_TEST_SELECTION", "账号编号无效")
		}
	}
	return s.claimAndStart(ctx, input, s.now(), false)
}

func (s *CandyTestService) RunDue(ctx context.Context, now time.Time) {
	if s == nil {
		return
	}
	if err := s.repo.Expire(ctx, now); err != nil {
		logger.LegacyPrintf("service.candy_tests", "清理过期糖果测试失败：%v", err)
		return
	}
	if _, err := s.claimAndStart(ctx, CandyTestStart{All: true}, now, true); err != nil && !errors.Is(err, ErrCandyTestRunning) {
		logger.LegacyPrintf("service.candy_tests", "启动定时糖果测试失败：%v", err)
	}
}

func (s *CandyTestService) claimAndStart(ctx context.Context, input CandyTestStart, now time.Time, scheduled bool) (*CandyTestBatch, error) {
	// 数据库认领与本实例取消函数注册保持同一临界区，停止操作不会漏掉刚认领的任务。
	s.mu.Lock()
	defer s.mu.Unlock()
	batch, err := s.repo.Claim(ctx, input, uuid.NewString(), now, now.Add(CandyTestLeaseDuration(1)), scheduled)
	if err != nil || batch == nil {
		return batch, err
	}
	deadline := now.Add(CandyTestLeaseDuration(len(batch.Results)))
	if len(batch.Results) > 0 {
		deadline = batch.Results[0].RunningUntil
	}
	jobCtx, cancel := context.WithDeadline(context.Background(), deadline.Add(-30*time.Second))
	s.active[batch.RunID] = cancel
	go s.executeBatch(jobCtx, cancel, batch)
	return batch, nil
}

func (s *CandyTestService) executeBatch(ctx context.Context, cancel context.CancelFunc, batch *CandyTestBatch) {
	defer cancel()
	defer func() {
		s.mu.Lock()
		delete(s.active, batch.RunID)
		s.mu.Unlock()
		saveCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if err := s.repo.Finish(saveCtx, batch.RunID, s.now()); err != nil {
			logger.LegacyPrintf("service.candy_tests", "结束糖果测试失败：run_id=%s error=%v", batch.RunID, err)
		}
	}()
	go s.watchCancellation(ctx, cancel, batch.RunID)
	var wg sync.WaitGroup
	queue := make(chan *CandyTestResult)
	for i := 0; i < min(CandyTestMaxWorkers, len(batch.Results)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range queue {
				job := *item
				accountCtx, stop := context.WithTimeout(ctx, CandyTestAccountTimeout)
				s.executeAccount(accountCtx, &job, batch.Settings)
				stop()
			}
		}()
	}
	for _, item := range batch.Results {
		queue <- item
	}
	close(queue)
	wg.Wait()
}

// 取消可能由另一应用实例发起；轮询数据库运行标识，让排队账号和在途请求尽快停止。
func (s *CandyTestService) watchCancellation(ctx context.Context, cancel context.CancelFunc, runID string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pollCtx, stop := context.WithTimeout(ctx, 5*time.Second)
			settings, err := s.repo.GetSettings(pollCtx)
			stop()
			if err == nil && (settings == nil || settings.RunningRunID != runID) {
				cancel()
				return
			}
		}
	}
}

func (s *CandyTestService) executeAccount(ctx context.Context, job *CandyTestResult, settings CandyTestSettingsInput) {
	defer func() {
		if recover() != nil {
			job.Status, job.ErrorMessage = "error", "测试执行异常，请重试"
		}
		now := s.now()
		job.FinishedAt = &now
		saveCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.repo.Complete(saveCtx, job); err != nil {
			logger.LegacyPrintf("service.candy_tests", "保存糖果测试失败：account_id=%d error=%v", job.AccountID, err)
		}
	}()
	job.Status = "error"
	if ctx.Err() != nil {
		job.ErrorMessage = "测试已取消或超过执行期限"
		return
	}
	result, err := s.runAccount(ctx, job.AccountID, settings.ModelID, &PelicanTestConfig{Prompt: settings.Prompt, ModelID: settings.ModelID, ReasoningEffort: settings.ReasoningEffort, QuestionKind: "candy", ParallelCount: 1})
	switch {
	case ctx.Err() != nil:
		job.ErrorMessage = "测试已取消或超过 10 分钟，请重试"
		return
	case err != nil:
		job.ErrorMessage = err.Error()
		return
	case result == nil:
		job.ErrorMessage = "测试未返回结果"
		return
	}
	job.ResponseText = result.ResponseText
	if result.PelicanConfig != nil {
		job.ModelID = result.PelicanConfig.ModelID
	}
	if result.Status != "success" {
		job.ErrorMessage = result.ErrorMessage
		if job.ErrorMessage == "" {
			job.ErrorMessage = "模型未完整返回答案"
		}
		return
	}
	answer, ok := parseCandyAnswer(result.ResponseText)
	if !ok {
		job.ErrorMessage = "无法从完整响应中识别最终整数答案"
		return
	}
	job.Answer = answer
	job.Status = "degraded"
	if answer == settings.ExpectedAnswer {
		job.Status = "normal"
	}
	job.ErrorMessage = ""
}

func normalizeCandyInteger(value string) (string, bool) {
	if len(value) > 101 || !candyIntegerPattern.MatchString(value) {
		return "", false
	}
	number, ok := new(big.Int).SetString(value, 10)
	if !ok || len(strings.TrimPrefix(number.String(), "-")) > 100 {
		return "", false
	}
	return number.String(), true
}

func parseCandyAnswer(output string) (string, bool) {
	output = strings.TrimSpace(output)
	if answer, ok := normalizeCandyInteger(output); ok {
		return answer, true
	}
	if strings.HasPrefix(output, "{") {
		// 逐个读取标记，拒绝重复 answer、额外字段和 JSON 后追加的解释或第二个对象。
		decoder := json.NewDecoder(strings.NewReader(output))
		decoder.UseNumber()
		if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
			return "", false
		}
		if token, err := decoder.Token(); err != nil || token != "answer" {
			return "", false
		}
		var value any
		if err := decoder.Decode(&value); err != nil {
			return "", false
		}
		if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
			return "", false
		}
		if _, err := decoder.Token(); err != io.EOF {
			return "", false
		}
		switch value := value.(type) {
		case json.Number:
			return normalizeCandyInteger(string(value))
		case string:
			return normalizeCandyInteger(value)
		default:
			return "", false
		}
	}
	// 允许解释后的明确答案行；普通推理中的数字、否定句、范围和小数都不能命中。
	lines := strings.Split(output, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if match := candyFinalAnswerPattern.FindStringSubmatch(last); match != nil {
		return normalizeCandyInteger(match[1])
	}
	return "", false
}

func candyResponseCompletionError(event map[string]any) string {
	response, ok := event["response"].(map[string]any)
	if !ok || response["status"] != "completed" {
		return "糖果测试响应未确认完整结束"
	}
	if response["incomplete_details"] != nil || response["error"] != nil {
		return "糖果测试响应被截断或执行失败"
	}
	output, _ := response["output"].([]any)
	for _, raw := range output {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if status, ok := item["status"].(string); ok && status != "completed" {
			return "糖果测试输出未完整结束"
		}
		content, _ := item["content"].([]any)
		for _, rawPart := range content {
			part, ok := rawPart.(map[string]any)
			if ok && part["type"] == "refusal" {
				return "模型拒绝回答糖果题"
			}
		}
	}
	return ""
}

func (s *CandyTestService) Stop(ctx context.Context, runID string) error {
	if _, err := uuid.Parse(runID); err != nil {
		return infraerrors.BadRequest("CANDY_TEST_RUN", "运行标识无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.repo.Stop(ctx, runID, s.now()); err != nil {
		return err
	}
	if cancel := s.active[runID]; cancel != nil {
		cancel()
	}
	return nil
}

func (s *CandyTestService) ListResults(ctx context.Context, page, pageSize int) ([]*CandyTestResult, int64, error) {
	if err := s.repo.Expire(ctx, s.now()); err != nil {
		return nil, 0, err
	}
	params := pagination.PaginationParams{Page: page, PageSize: pageSize}
	return s.repo.ListResults(ctx, params.Offset(), params.Limit())
}

func (s *CandyTestService) GetResult(ctx context.Context, id int64) (*CandyTestResult, error) {
	if err := s.repo.Expire(ctx, s.now()); err != nil {
		return nil, err
	}
	result, err := s.repo.GetResult(ctx, id)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrCandyTestResultNotFound
	}
	return result, nil
}
