package service

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const healthyDynamicMaintenanceInterval = 15 * time.Second

type healthyDynamicModel struct {
	requested string
	upstream  string
}

type healthyDynamicRetry struct {
	failures  int
	after     time.Time
	nextModel int
	message   string
}

// HealthyTurnStateMaintenanceStatus 只提供脱敏维护状态，不暴露提取地址与代理入口。
type HealthyTurnStateMaintenanceStatus struct {
	Status      string     `json:"status"`
	Message     string     `json:"message"`
	NextRetryAt *time.Time `json:"next_retry_at"`
}

func (s *AccountTestService) HealthyTurnStateMaintenanceStatus(ctx context.Context, accountID int64) (*HealthyTurnStateMaintenanceStatus, error) {
	status := &HealthyTurnStateMaintenanceStatus{Status: "unconfigured", Message: "请保存动态 IP 接口并勾选测试模型"}
	d, err := s.healthyDynamicService()
	if err != nil || s.accountRepo == nil {
		return status, nil
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !healthyDynamicMaintenanceEnabled(account) {
		status.Status, status.Message = "disabled", "自动维护已关闭"
		return status, nil
	}
	config, err := d.loadConfig(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if config.APIURL == "" {
		return status, nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var latest *HealthyTurnStateDynamicRun
	var updatedAt time.Time
	for _, run := range d.runs {
		if run.accountID != accountID {
			continue
		}
		run.mu.Lock()
		if latest == nil || run.lastUsed.After(updatedAt) {
			latest, updatedAt = run.snapshotLocked(), run.lastUsed
		}
		run.mu.Unlock()
	}
	status.Status, status.Message = "idle", "定期检查库存，有缺口时自动补齐"
	if len(config.Models) == 0 {
		status.Status, status.Message = "unconfigured", "等待核实上游支持模型并继承上次选择"
	}
	if d.maintenanceActive[accountID] || (latest != nil && latest.Status == "running") {
		status.Status, status.Message = "running", "正在通过动态 IP 代理补充健康头"
	} else if retry := d.maintenanceRetry[accountID]; d.clock().Before(retry.after) {
		status.Status, status.Message = "backoff", "健康头库存尚未补齐，将稍后重试"
		status.NextRetryAt = &retry.after
		if latest != nil && latest.Message != "" {
			status.Message = latest.Message
		}
		if retry.message != "" {
			status.Message = retry.message
		}
	}
	return status, nil
}

func healthyDynamicModels(account *Account, models []string, fallback string) ([]healthyDynamicModel, error) {
	if len(models) == 0 {
		models = []string{fallback}
	}
	result := make([]healthyDynamicModel, 0, len(models))
	seen := make(map[string]bool)
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" || !account.IsModelSupported(model) || isHealthyTurnStateNonTextModel(model) {
			return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请选择账号支持的文本模型")
		}
		upstream := account.GetMappedModel(model)
		if isHealthyTurnStateNonTextModel(upstream) {
			return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请选择账号支持的文本模型")
		}
		if account.UsesOpenAICodexProtocol() {
			upstream = normalizeOpenAIModelForUpstream(account, upstream)
		}
		if isHealthyTurnStateNonTextModel(upstream) || strings.Contains(upstream, "*") {
			return nil, infraerrors.BadRequest("INVALID_HEALTHY_TURN_STATE_MODEL", "请选择账号支持的文本模型")
		}
		// 同一个实际上游模型的多个别名共用库存，避免重复补位。
		if !seen[upstream] {
			seen[upstream] = true
			result = append(result, healthyDynamicModel{requested: model, upstream: upstream})
		}
	}
	return result, nil
}

func healthyDynamicMissingModel(models []healthyDynamicModel, counts map[string]int64, target int) (healthyDynamicModel, bool) {
	for _, model := range models {
		if counts[model.upstream] < int64(target) {
			return model, true
		}
	}
	return healthyDynamicModel{}, false
}

func (s *AccountTestService) healthyDynamicInventory(ctx context.Context, accountID int64) (map[string]int64, error) {
	if d := s.healthyTurnStateDynamic; d != nil && d.inventory != nil {
		return d.inventory(ctx, accountID)
	}
	if s.openaiGatewayService == nil || s.openaiGatewayService.openaiHealthyTurnStates.repo == nil {
		// 无仓储的独立单元测试使用当前运行的记录数；正式维护不启用此分支。
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stats, err := s.openaiGatewayService.openaiHealthyTurnStates.repo.Stats(ctx, accountID)
	if err != nil || stats == nil {
		return nil, errors.New("读取健康头库存失败")
	}
	counts := make(map[string]int64, len(stats.Models))
	for _, model := range stats.Models {
		// 使用中的头仍属于库存，不能因临时租约额外采集。
		counts[model.Model] = model.Available + model.InUse
	}
	return counts, nil
}

func healthyDynamicMaintenanceEnabled(account *Account) bool {
	return account != nil && account.IsOpenAIOAuthLike() && account.IsActive() && account.OpenAIHealthyTurnStateReplaceEnabled()
}

// StartHealthyTurnStateMaintenance 从持久化配置恢复库存维护，不依赖编辑页面轮询。
func (s *AccountTestService) StartHealthyTurnStateMaintenance() {
	d, err := s.healthyDynamicService()
	if err != nil || s.accountRepo == nil || s.openaiGatewayService == nil || s.openaiGatewayService.openaiHealthyTurnStates.repo == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.maintenanceCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	d.maintenanceCancel, d.maintenanceDone = cancel, make(chan struct{})
	d.maintenanceActive = make(map[int64]bool)
	d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
	go func() {
		defer close(d.maintenanceDone)
		ticker := time.NewTicker(healthyDynamicMaintenanceInterval)
		defer ticker.Stop()
		for {
			s.scanHealthyDynamicMaintenance(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// StopHealthyTurnStateMaintenance 取消提取、探测和旧手动运行，再等待后台退出。
func (s *AccountTestService) StopHealthyTurnStateMaintenance() {
	if s == nil || s.healthyTurnStateDynamic == nil {
		return
	}
	d := s.healthyTurnStateDynamic
	d.mu.Lock()
	if d.maintenanceCancel != nil {
		d.maintenanceCancel()
	}
	done := d.maintenanceDone
	for _, run := range d.runs {
		run.mu.Lock()
		run.finishLocked("stopped", "服务关闭，采集已停止")
		run.mu.Unlock()
	}
	d.mu.Unlock()
	if done != nil {
		<-done
	}
	d.maintenanceWorkers.Wait()
}

func (s *AccountTestService) scanHealthyDynamicMaintenance(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	d := s.healthyTurnStateDynamic
	scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	accounts, err := s.accountRepo.ListByPlatform(scanCtx, PlatformOpenAI)
	if err != nil {
		return
	}
	for i := range accounts {
		account := &accounts[i]
		if scanCtx.Err() != nil {
			return
		}
		if !healthyDynamicMaintenanceEnabled(account) {
			d.cancelHealthyDynamicMaintenanceAccount(account.ID)
			continue
		}
		d.mu.Lock()
		active := d.maintenanceActive[account.ID]
		retry := d.maintenanceRetry[account.ID]
		workers := len(d.maintenanceActive)
		d.mu.Unlock()
		if active || d.clock().Before(retry.after) || workers >= 4 {
			continue
		}
		config, err := d.loadConfig(scanCtx, account.ID)
		if err != nil || config.APIURL == "" {
			continue
		}
		if len(config.Models) != 0 {
			models, err := healthyDynamicModels(account, config.Models, "")
			if err != nil {
				continue
			}
			counts, err := s.healthyDynamicInventory(scanCtx, account.ID)
			if err != nil {
				continue
			}
			if _, missing := healthyDynamicMissingModel(models, counts, config.TargetCount); !missing {
				continue
			}
		}
		d.mu.Lock()
		if ctx.Err() != nil {
			d.mu.Unlock()
			return
		}
		sharedRevision, accountRevision := d.sharedConfigRevision, d.accountConfigRevision[account.ID]
		d.maintenanceActive[account.ID] = true
		d.maintenanceWorkers.Add(1)
		d.mu.Unlock()
		go func(accountID int64) {
			defer d.maintenanceWorkers.Done()
			filled, attempted := s.maintainHealthyDynamicAccount(ctx, accountID)
			d.mu.Lock()
			defer d.mu.Unlock()
			delete(d.maintenanceActive, accountID)
			// 保存配置后，旧任务的结束结果不能恢复已清除的退避。
			if sharedRevision != d.sharedConfigRevision || accountRevision != d.accountConfigRevision[accountID] {
				return
			}
			if filled {
				delete(d.maintenanceRetry, accountID)
			} else if attempted && ctx.Err() == nil {
				d.maintenanceRetry[accountID] = nextHealthyDynamicRetry(d.maintenanceRetry[accountID], d.clock())
			}
		}(account.ID)
	}
}

func (d *healthyTurnStateDynamicService) cancelHealthyDynamicMaintenanceAccount(accountID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.maintenanceActive[accountID] {
		return
	}
	for _, run := range d.runs {
		if run.accountID == accountID {
			run.mu.Lock()
			run.finishLocked("stopped", "账号已关闭健康头维护")
			run.mu.Unlock()
		}
	}
}

func nextHealthyDynamicRetry(previous healthyDynamicRetry, now time.Time) healthyDynamicRetry {
	failures := min(previous.failures+1, 6)
	delay := min(30*time.Second*time.Duration(1<<(failures-1)), 10*time.Minute)
	return healthyDynamicRetry{failures: failures, after: now.Add(delay), nextModel: (previous.nextModel + 1) % 100, message: previous.message}
}

func sameHealthyDynamicConfig(a, b HealthyTurnStateDynamicConfigInput) bool {
	return a.APIURL == b.APIURL && a.Protocol == b.Protocol && a.TargetCount == b.TargetCount &&
		a.MaxAttempts == b.MaxAttempts && a.Transport == b.Transport && slices.Equal(a.Models, b.Models)
}

func (s *AccountTestService) maintainHealthyDynamicAccount(ctx context.Context, accountID int64) (filled, attempted bool) {
	d := s.healthyTurnStateDynamic
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil || !healthyDynamicMaintenanceEnabled(account) {
		return false, false
	}
	d.mu.Lock()
	sharedRevision, accountRevision := d.sharedConfigRevision, d.accountConfigRevision[accountID]
	d.mu.Unlock()
	config, err := s.inheritHealthyDynamicConfig(ctx, account)
	if err != nil {
		d.mu.Lock()
		if sharedRevision != d.sharedConfigRevision || accountRevision != d.accountConfigRevision[accountID] {
			d.mu.Unlock()
			return false, false
		}
		if d.maintenanceRetry == nil {
			d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
		}
		retry := d.maintenanceRetry[accountID]
		retry.message = infraerrors.Message(err)
		d.maintenanceRetry[accountID] = retry
		d.mu.Unlock()
		return false, true
	}
	if config.APIURL == "" || len(config.Models) == 0 {
		return false, false
	}
	d.mu.Lock()
	modelOffset := d.maintenanceRetry[accountID].nextModel
	retry := d.maintenanceRetry[accountID]
	retry.message = ""
	if d.maintenanceRetry != nil {
		d.maintenanceRetry[accountID] = retry
	}
	d.mu.Unlock()
	run, err := s.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	if err != nil {
		return false, false
	}
	defer s.StopHealthyTurnStateDynamic(context.Background(), accountID, run.ID)
	for ctx.Err() == nil {
		// 每次网络请求前重读开关、模型与凭据，禁用或修改配置后不继续旧任务。
		current, err := s.accountRepo.GetByID(ctx, accountID)
		if err != nil || !healthyDynamicMaintenanceEnabled(current) {
			return false, attempted
		}
		currentConfig, err := d.loadConfig(ctx, accountID)
		if err != nil || !sameHealthyDynamicConfig(config, currentConfig) {
			return false, false
		}
		currentModels, err := healthyDynamicModels(current, currentConfig.Models, "")
		if err != nil {
			return false, false
		}
		// 某个模型持续失败时，下一轮先尝试其他模型，避免其余缺口永久饥饿。
		if offset := modelOffset % len(currentModels); offset > 0 {
			currentModels = append(currentModels[offset:], currentModels[:offset]...)
		}
		session, err := d.findRun(accountID, run.ID)
		if err != nil {
			return false, attempted
		}
		session.mu.Lock()
		session.account, session.models = current, currentModels
		session.mu.Unlock()
		run, err = s.StepHealthyTurnStateDynamic(ctx, accountID, run.ID)
		if err != nil {
			return false, attempted
		}
		attempted = attempted || run.Attempts > 0 || run.FetchedBatches > 0
		if run.Status != "running" {
			latestConfig, configErr := d.loadConfig(ctx, accountID)
			if configErr == nil && !sameHealthyDynamicConfig(config, latestConfig) {
				return false, false
			}
			counts, err := s.healthyDynamicInventory(ctx, accountID)
			_, missing := healthyDynamicMissingModel(currentModels, counts, config.TargetCount)
			return err == nil && !missing, attempted
		}
		if run.LastResult != nil && run.LastResult.Status != "recorded" && run.LastResult.Status != "already_recorded" {
			session.mu.Lock()
			message := "代理响应未通过健康检查，稍后自动重试"
			if run.LastResult.Message != "" {
				message = run.LastResult.Message
			}
			session.finishLocked("failed", message)
			session.mu.Unlock()
			return false, attempted
		}
		// 限制连续请求速度；供应商失败或无健康结果交给外层指数退避。
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, attempted
		case <-timer.C:
		}
	}
	return false, attempted
}
