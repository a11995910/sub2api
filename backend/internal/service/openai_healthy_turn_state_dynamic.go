package service

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	healthyDynamicMaxRuns   = 100
	healthyDynamicIdleLimit = 5 * time.Minute
	healthyDynamicRunLimit  = 4 * time.Hour
)

type HealthyTurnStateDynamicRun struct {
	ID             string                             `json:"id"`
	Status         string                             `json:"status"`
	Model          string                             `json:"model"`
	Transport      string                             `json:"transport"`
	Attempts       int                                `json:"attempts"`
	Recorded       int                                `json:"recorded"`
	TargetCount    int                                `json:"target_count"`
	MaxAttempts    int                                `json:"max_attempts"`
	FetchedBatches int                                `json:"fetched_batches"`
	LastResult     *OpenAIHealthyTurnStateProbeResult `json:"last_result"`
	Message        string                             `json:"message"`
}

type healthyTurnStateDynamicService struct {
	settings SettingRepository
	cipher   SecretEncryptor
	mu       sync.Mutex
	runs     map[string]*healthyTurnStateDynamicSession
	// 仅用于离线测试；正式实例使用真实提取与单次探测方法。
	fetch func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error)
	probe func(context.Context, *Account, string, string, string) (*OpenAIHealthyTurnStateProbeResult, error)
	now   func() time.Time
}

type healthyTurnStateDynamicSession struct {
	mu                    sync.Mutex
	stepMu                sync.Mutex
	view                  HealthyTurnStateDynamicRun
	accountID             int64
	account               *Account
	probeModel            string
	config                HealthyTurnStateDynamicConfigInput
	pending               []string
	seen                  map[string]struct{}
	emptyBatches          int
	createdAt, lastUsed   time.Time
	ctx                   context.Context
	cancel                context.CancelFunc
	idleTimer, limitTimer *time.Timer
}

func (d *healthyTurnStateDynamicService) clock() time.Time {
	if d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (r *healthyTurnStateDynamicSession) snapshotLocked() *HealthyTurnStateDynamicRun {
	view := r.view
	if view.LastResult != nil {
		last := *view.LastResult
		view.LastResult = &last
	}
	return &view
}

func (r *healthyTurnStateDynamicSession) finishLocked(status, message string) {
	if r.view.Status != "running" {
		return
	}
	r.view.Status, r.view.Message = status, message
	r.cancel()
	if r.idleTimer != nil {
		r.idleTimer.Stop()
	}
	if r.limitTimer != nil {
		r.limitTimer.Stop()
	}
	r.pending, r.seen, r.account = nil, nil, nil
	r.config.APIURL = ""
}

func (r *healthyTurnStateDynamicSession) expireLocked(now time.Time) {
	if !now.Before(r.createdAt.Add(healthyDynamicRunLimit)) {
		r.finishLocked("stopped", "采集已达到最长运行时间，请重新开始")
	} else if !now.Before(r.lastUsed.Add(healthyDynamicIdleLimit)) {
		r.finishLocked("stopped", "页面长时间未继续采集，已自动停止")
	}
}

func (d *healthyTurnStateDynamicService) cleanupLocked(now time.Time) {
	for id, run := range d.runs {
		run.mu.Lock()
		run.expireLocked(now)
		if run.view.Status != "running" && !now.Before(run.lastUsed.Add(healthyDynamicIdleLimit)) {
			delete(d.runs, id)
		}
		run.mu.Unlock()
	}
	// 保留活跃运行；达到容量时仅回收已结束的最旧记录。
	if len(d.runs) >= healthyDynamicMaxRuns {
		oldestID := ""
		var oldest time.Time
		for id, run := range d.runs {
			run.mu.Lock()
			if run.view.Status != "running" && (oldestID == "" || run.lastUsed.Before(oldest)) {
				oldestID, oldest = id, run.lastUsed
			}
			run.mu.Unlock()
		}
		if oldestID != "" {
			delete(d.runs, oldestID)
		}
	}
}

func validateHealthyDynamicStart(account *Account, input HealthyTurnStateDynamicStartInput) (HealthyTurnStateDynamicStartInput, error) {
	if account == nil || account.ID <= 0 || account.Platform != PlatformOpenAI {
		return input, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_ACCOUNT", "仅支持 OpenAI 账号")
	}
	input.Model = strings.TrimSpace(input.Model)
	if input.Model == "" {
		input.Model = "gpt-6-astra"
	}
	if !account.IsModelSupported(input.Model) || isOpenAIImageModel(input.Model) {
		return input, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请选择账号支持的文本模型")
	}
	if input.Transport == "" {
		input.Transport = "http"
	}
	if input.Transport != "http" && input.Transport != "websocket" {
		return input, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_TRANSPORT", "测试方式仅支持 HTTP 或 WebSocket")
	}
	return input, nil
}

func (s *AccountTestService) StartHealthyTurnStateDynamic(ctx context.Context, account *Account, input HealthyTurnStateDynamicStartInput) (*HealthyTurnStateDynamicRun, error) {
	d, err := s.healthyDynamicService()
	if err != nil {
		return nil, err
	}
	input, err = validateHealthyDynamicStart(account, input)
	if err != nil {
		return nil, err
	}
	if s.openaiGatewayService == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_TURN_STATE_UNAVAILABLE", "状态头测试服务暂不可用")
	}
	config, err := d.loadConfig(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if config.APIURL == "" {
		return nil, infraerrors.BadRequest("HEALTHY_DYNAMIC_NOT_CONFIGURED", "请先保存动态代理采集设置")
	}
	if ctx.Err() != nil {
		return nil, infraerrors.BadRequest("HEALTHY_DYNAMIC_REQUEST_CANCELED", "请求已取消")
	}
	now := d.clock()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.runs == nil {
		d.runs = make(map[string]*healthyTurnStateDynamicSession)
	}
	d.cleanupLocked(now)
	for _, run := range d.runs {
		run.mu.Lock()
		active := run.accountID == account.ID && run.view.Status == "running"
		run.mu.Unlock()
		if active {
			return nil, infraerrors.Conflict("HEALTHY_DYNAMIC_ALREADY_RUNNING", "此账号已有采集运行，请在原页面停止；若原页面已关闭，5 分钟未继续请求后会自动停止")
		}
	}
	if len(d.runs) >= healthyDynamicMaxRuns {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_DYNAMIC_CAPACITY", "当前采集运行已满，请稍后重试")
	}
	copyAccount := *account
	copyAccount.Credentials, copyAccount.Extra = maps.Clone(account.Credentials), maps.Clone(account.Extra)
	model := account.GetMappedModel(input.Model)
	if account.UsesOpenAICodexProtocol() {
		model = normalizeOpenAIModelForUpstream(account, model)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &healthyTurnStateDynamicSession{
		view:      HealthyTurnStateDynamicRun{ID: uuid.NewString(), Status: "running", Model: model, Transport: input.Transport, TargetCount: config.TargetCount, MaxAttempts: config.MaxAttempts, Message: "采集已就绪"},
		accountID: account.ID, account: &copyAccount, probeModel: input.Model, config: config, seen: make(map[string]struct{}), createdAt: now, lastUsed: now, ctx: runCtx, cancel: cancel,
	}
	run.idleTimer = time.AfterFunc(healthyDynamicIdleLimit, func() { run.mu.Lock(); defer run.mu.Unlock(); run.expireLocked(d.clock()) })
	run.limitTimer = time.AfterFunc(healthyDynamicRunLimit, func() {
		run.mu.Lock()
		defer run.mu.Unlock()
		run.finishLocked("stopped", "采集已达到最长运行时间，请重新开始")
	})
	d.runs[run.view.ID] = run
	return run.snapshotLocked(), nil
}

func (d *healthyTurnStateDynamicService) findRun(accountID int64, runID string) (*healthyTurnStateDynamicSession, error) {
	d.mu.Lock()
	run := d.runs[runID]
	d.mu.Unlock()
	if run == nil || run.accountID != accountID {
		return nil, infraerrors.NotFound("HEALTHY_DYNAMIC_RUN_NOT_FOUND", "采集运行不存在或已过期")
	}
	return run, nil
}

func (s *AccountTestService) StopHealthyTurnStateDynamic(ctx context.Context, accountID int64, runID string) (*HealthyTurnStateDynamicRun, error) {
	d, err := s.healthyDynamicService()
	if err != nil {
		return nil, err
	}
	run, err := d.findRun(accountID, runID)
	if err != nil {
		return nil, err
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	run.finishLocked("stopped", "采集已停止")
	run.lastUsed = d.clock()
	return run.snapshotLocked(), nil
}

func (s *AccountTestService) StepHealthyTurnStateDynamic(ctx context.Context, accountID int64, runID string) (*HealthyTurnStateDynamicRun, error) {
	d, err := s.healthyDynamicService()
	if err != nil {
		return nil, err
	}
	run, err := d.findRun(accountID, runID)
	if err != nil {
		return nil, err
	}
	if !run.stepMu.TryLock() {
		return nil, infraerrors.Conflict("HEALTHY_DYNAMIC_RUN_BUSY", "本轮采集尚未结束，请等待结果")
	}
	defer run.stepMu.Unlock()
	run.mu.Lock()
	run.expireLocked(d.clock())
	if ctx.Err() != nil {
		run.finishLocked("stopped", "请求已取消，采集停止")
	}
	if run.view.Status != "running" {
		view := run.snapshotLocked()
		run.mu.Unlock()
		return view, nil
	}
	run.lastUsed = d.clock()
	run.idleTimer.Reset(healthyDynamicIdleLimit)
	stepCtx, cancel := context.WithCancel(run.ctx)
	stopRequestCancellation := context.AfterFunc(ctx, cancel)
	defer func() { stopRequestCancellation(); cancel() }()
	account, config, probeModel, transport := run.account, run.config, run.probeModel, run.view.Transport
	needsBatch := len(run.pending) == 0
	if needsBatch {
		run.view.FetchedBatches++
	}
	run.mu.Unlock()

	if needsBatch {
		fetch := d.fetch
		if fetch == nil {
			fetch = fetchHealthyDynamicProxyBatch
		}
		batch, fetchErr := fetch(stepCtx, config)
		run.mu.Lock()
		if stepCtx.Err() != nil {
			run.finishLocked("stopped", "请求已取消，采集停止")
		}
		if run.view.Status == "running" {
			if fetchErr != nil {
				message := "代理提取失败，请检查接口授权、返回格式与服务器 IP 白名单"
				var publicError *healthyDynamicPublicError
				if errors.As(fetchErr, &publicError) {
					message = publicError.message
				}
				run.finishLocked("failed", message)
			} else {
				for _, proxy := range batch {
					if _, exists := run.seen[proxy]; exists {
						continue
					}
					if len(run.seen) >= run.view.MaxAttempts {
						break
					}
					run.seen[proxy] = struct{}{}
					run.pending = append(run.pending, proxy)
				}
				if len(run.pending) == 0 {
					run.emptyBatches++
					run.view.Message = "本批没有新的代理入口，可继续提取下一批"
					if run.emptyBatches >= 3 {
						run.finishLocked("failed", "连续三批没有新的代理入口，采集已停止")
					}
				} else {
					run.emptyBatches = 0
				}
			}
		}
		if run.view.Status != "running" || len(run.pending) == 0 {
			view := run.snapshotLocked()
			run.mu.Unlock()
			return view, nil
		}
		run.mu.Unlock()
	}

	run.mu.Lock()
	if stepCtx.Err() != nil {
		run.finishLocked("stopped", "请求已取消，采集停止")
	}
	if run.view.Status != "running" {
		view := run.snapshotLocked()
		run.mu.Unlock()
		return view, nil
	}
	proxy := run.pending[0]
	run.pending[0] = ""
	run.pending = run.pending[1:]
	run.view.Attempts++
	run.mu.Unlock()
	probe := d.probe
	if probe == nil {
		probe = s.ProbeOpenAIHealthyTurnStateWithProxy
	}
	result, probeErr := probe(stepCtx, account, probeModel, transport, proxy)
	run.mu.Lock()
	defer run.mu.Unlock()
	if probeErr != nil || result == nil {
		result = &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: run.view.Model, Transport: transport, Message: "本次代理探测失败"}
	}
	run.view.LastResult = result
	if result.Status == "recorded" {
		run.view.Recorded++
	}
	if stepCtx.Err() != nil {
		run.finishLocked("stopped", "请求已取消，采集停止")
	}
	if run.view.Status == "running" {
		run.view.Message = "本轮采集完成"
		if run.view.Recorded >= run.view.TargetCount {
			run.finishLocked("completed", "已达到目标新增记录数")
		} else if run.view.Attempts >= run.view.MaxAttempts {
			run.finishLocked("completed", fmt.Sprintf("已达到尝试上限，新增 %d / 目标 %d", run.view.Recorded, run.view.TargetCount))
		}
	}
	run.lastUsed = d.clock()
	if run.view.Status == "running" {
		run.idleTimer.Reset(healthyDynamicIdleLimit)
	}
	return run.snapshotLocked(), nil
}
