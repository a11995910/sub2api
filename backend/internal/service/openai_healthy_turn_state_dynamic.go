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
	Models         []string                           `json:"models"`
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
	// configMu 保证共享配置迁移、保存与运行注册使用同一版本。
	configMu              sync.Mutex
	legacyProxyChecked    bool
	legacyProxyConflict   bool
	sharedConfigRevision  uint64
	accountConfigRevision map[int64]uint64
	settings              SettingRepository
	cipher                SecretEncryptor
	mu                    sync.Mutex
	runs                  map[string]*healthyTurnStateDynamicSession
	// 仅用于离线测试；正式实例使用真实提取与单次探测方法。
	fetch                  func(context.Context, HealthyTurnStateDynamicConfigInput) ([]string, error)
	probe                  func(context.Context, *Account, string, string, string) (*OpenAIHealthyTurnStateProbeResult, error)
	now                    func() time.Time
	inventory              func(context.Context, int64) (map[string]int64, error)
	maintenanceCancel      context.CancelFunc
	maintenanceDone        chan struct{}
	maintenanceWorkers     sync.WaitGroup
	maintenanceActive      map[int64]bool
	maintenanceRetry       map[int64]healthyDynamicRetry
	maintenanceNextAccount int64
	maintenanceScanError   string
	probeMu                sync.Mutex
	probeSlots             chan struct{}
	activeProxies          map[string]bool
}

type healthyTurnStateDynamicSession struct {
	mu              sync.Mutex
	stepMu          sync.Mutex
	view            HealthyTurnStateDynamicRun
	accountID       int64
	account         *Account
	probeModel      string
	models          []healthyDynamicModel
	recordedByModel map[string]int64
	config          HealthyTurnStateDynamicConfigInput
	pending         []string
	seen            map[string]struct{}
	// 后台由维护循环统一计算连续失败，手动步骤保留单次提取失败即结束的行为。
	backgroundMaintenance bool
	lastBatch             []*OpenAIHealthyTurnStateProbeResult
	lastFetchFailures     []string
	proxyWaiting          bool
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
	view.Models = append([]string{}, view.Models...)
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
	if account == nil || account.ID <= 0 || account.Platform != PlatformOpenAI {
		return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_ACCOUNT", "仅支持 OpenAI 账号")
	}
	if s.openaiGatewayService == nil {
		return nil, infraerrors.ServiceUnavailable("HEALTHY_TURN_STATE_UNAVAILABLE", "状态头测试服务暂不可用")
	}
	d.configMu.Lock()
	defer d.configMu.Unlock()
	config, err := d.loadConfigLocked(ctx, account.ID)
	if err != nil {
		return nil, err
	}
	if config.APIURL == "" {
		return nil, infraerrors.BadRequest("HEALTHY_DYNAMIC_NOT_CONFIGURED", "请先保存动态代理采集设置")
	}
	if input.Model == "" && len(config.Models) > 0 {
		input.Model = config.Models[0]
	}
	if input.Transport == "" {
		input.Transport = config.Transport
	}
	input, err = validateHealthyDynamicStart(account, input)
	if err != nil {
		return nil, err
	}
	models, err := healthyDynamicModels(account, config.Models, input.Model)
	if err != nil {
		return nil, err
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
	model := models[0].upstream
	selectedModels := make([]string, 0, len(models))
	for _, selected := range models {
		selectedModels = append(selectedModels, selected.requested)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	run := &healthyTurnStateDynamicSession{
		view:      HealthyTurnStateDynamicRun{ID: uuid.NewString(), Status: "running", Model: model, Models: selectedModels, Transport: input.Transport, TargetCount: config.TargetCount, MaxAttempts: config.MaxAttempts, Message: "采集已就绪"},
		accountID: account.ID, account: &copyAccount, probeModel: input.Model, config: config, seen: make(map[string]struct{}), createdAt: now, lastUsed: now, ctx: runCtx, cancel: cancel,
		models: models, recordedByModel: make(map[string]int64),
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
	return s.stepHealthyTurnStateDynamic(ctx, accountID, runID, 1)
}

// 后台批次与手动步骤复用库存、代理提取、预算和结果写回逻辑。
func (s *AccountTestService) stepHealthyTurnStateDynamic(ctx context.Context, accountID int64, runID string, parallelism int) (*HealthyTurnStateDynamicRun, error) {
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
	run.lastBatch = nil
	run.lastFetchFailures = nil
	run.proxyWaiting = false
	run.idleTimer.Reset(healthyDynamicIdleLimit)
	stepCtx, cancel := context.WithCancel(run.ctx)
	stopRequestCancellation := context.AfterFunc(ctx, cancel)
	defer func() { stopRequestCancellation(); cancel() }()
	account, config, transport := run.account, run.config, run.view.Transport
	run.mu.Unlock()
	counts, inventoryErr := s.healthyDynamicInventory(stepCtx, accountID)
	run.mu.Lock()
	if inventoryErr != nil {
		run.finishLocked("failed", "无法读取健康头库存，已暂停采集")
	}
	if counts == nil {
		counts = maps.Clone(run.recordedByModel)
	}
	selected, missing := healthyDynamicMissingModel(run.models, counts, config.TargetCount)
	if !missing {
		run.finishLocked("completed", "所选模型的健康头库存已达到目标")
	}
	if run.view.Status != "running" {
		view := run.snapshotLocked()
		run.mu.Unlock()
		return view, nil
	}
	run.view.Model = selected.upstream
	limit := min(parallelism, run.view.MaxAttempts-run.view.Attempts)
	if run.backgroundMaintenance {
		limit = min(limit, healthyDynamicAccountAttemptSlice-run.view.Attempts)
	}
	reserved := reserveHealthyDynamicModels(run.models, counts, config.TargetCount, limit)
	run.mu.Unlock()

	// 供应商每批只有一个入口时，最多提取三批以组成并行批次。
	for batchIndex := 0; batchIndex < parallelism; batchIndex++ {
		run.mu.Lock()
		needsBatch := len(run.pending) < len(reserved) && run.view.Status == "running"
		if needsBatch {
			run.view.FetchedBatches++
		}
		run.mu.Unlock()
		if !needsBatch {
			break
		}
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
				if run.backgroundMaintenance {
					run.lastFetchFailures = append(run.lastFetchFailures, message)
					run.view.Message = message
				} else {
					run.finishLocked("failed", message)
				}
			} else {
				previousPending := len(run.pending)
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
				if run.backgroundMaintenance && len(run.pending) == previousPending {
					run.lastFetchFailures = append(run.lastFetchFailures, "本批没有新的代理入口，可继续提取下一批")
				}
				if len(run.pending) == 0 {
					run.emptyBatches++
					run.view.Message = "本批没有新的代理入口，可继续提取下一批"
					if run.emptyBatches >= 3 && !run.backgroundMaintenance {
						run.finishLocked("failed", "连续三批没有新的代理入口，采集已停止")
					}
				} else {
					run.emptyBatches = 0
				}
			}
		}
		if run.view.Status != "running" || len(run.pending) == 0 {
			if run.view.Status == "running" {
				run.lastUsed = d.clock()
				run.idleTimer.Reset(healthyDynamicIdleLimit)
			}
			view := run.snapshotLocked()
			run.mu.Unlock()
			return view, nil
		}
		run.mu.Unlock()
		if fetchErr != nil {
			break
		}
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
	jobs := make([]healthyDynamicProbeJob, 0, len(reserved))
	for remaining := len(run.pending); remaining > 0 && len(jobs) < len(reserved); remaining-- {
		proxy := run.pending[0]
		run.pending[0] = ""
		run.pending = run.pending[1:]
		if !d.reserveHealthyDynamicProxy(proxy) {
			run.pending = append(run.pending, proxy)
			continue
		}
		jobs = append(jobs, healthyDynamicProbeJob{model: reserved[len(jobs)], proxy: proxy})
	}
	run.view.Attempts += len(jobs)
	if len(jobs) == 0 {
		run.proxyWaiting = true
		run.view.Message = "本批代理入口正在被其他账号采集使用，稍后重试"
		view := run.snapshotLocked()
		run.mu.Unlock()
		return view, nil
	}
	run.mu.Unlock()
	results := make(chan healthyDynamicProbeCompletion, len(jobs))
	for _, job := range jobs {
		go func(job healthyDynamicProbeJob) {
			// Account 的模型缓存可变，每次并行探测使用独立账号副本。
			copyAccount := *account
			copyAccount.Credentials, copyAccount.Extra = maps.Clone(account.Credentials), maps.Clone(account.Extra)
			result, err := s.probeHealthyDynamicLimited(stepCtx, &copyAccount, job.model.requested, transport, job.proxy)
			if err != nil || result == nil {
				result = &OpenAIHealthyTurnStateProbeResult{Status: "failed", Model: job.model.upstream, Transport: transport, Message: "本次代理探测失败"}
			}
			results <- healthyDynamicProbeCompletion{model: job.model, result: result}
		}(job)
	}
	for range jobs {
		completed := <-results
		run.mu.Lock()
		run.lastBatch = append(run.lastBatch, completed.result)
		run.view.LastResult = completed.result
		if completed.result.Status == "recorded" {
			run.view.Recorded++
			run.recordedByModel[completed.model.upstream]++
			counts[completed.model.upstream]++
		}
		run.mu.Unlock()
	}
	run.mu.Lock()
	defer run.mu.Unlock()
	if stepCtx.Err() != nil {
		run.finishLocked("stopped", "请求已取消，采集停止")
	}
	if run.view.Status == "running" {
		run.view.Message = "本轮采集完成"
		if _, missing := healthyDynamicMissingModel(run.models, counts, config.TargetCount); !missing {
			run.finishLocked("completed", "所选模型的健康头库存已达到目标")
		} else if run.view.Attempts >= run.view.MaxAttempts {
			run.finishLocked("completed", fmt.Sprintf("本轮已达到尝试上限，新增 %d 个健康头，剩余缺口稍后继续补齐", run.view.Recorded))
		}
	}
	run.lastUsed = d.clock()
	if run.view.Status == "running" {
		run.idleTimer.Reset(healthyDynamicIdleLimit)
	}
	return run.snapshotLocked(), nil
}
