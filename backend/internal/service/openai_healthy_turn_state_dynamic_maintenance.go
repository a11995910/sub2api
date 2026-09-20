package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	healthyDynamicMaintenanceInterval = 15 * time.Second
	healthyDynamicFailureLimit        = 10
	healthyDynamicRetryDelay          = 15 * time.Second
)

type healthyDynamicModel struct {
	requested string
	upstream  string
}

type healthyDynamicRetry struct {
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
		status.Status, status.Message = "error", infraerrors.Message(err)
		return status, nil
	}
	if config.APIURL == "" {
		return status, nil
	}
	missing := false
	if len(config.Models) > 0 {
		models, modelErr := healthyDynamicModels(account, config.Models, "")
		if modelErr != nil {
			status.Status, status.Message = "error", "已保存的采集模型不再受账号支持，请重新选择模型"
			return status, nil
		}
		counts, inventoryErr := s.healthyDynamicInventory(ctx, accountID)
		if inventoryErr != nil {
			status.Status, status.Message = "error", "读取健康头库存失败，等待重试"
			return status, nil
		}
		_, missing = healthyDynamicMissingModel(models, counts, config.TargetCount)
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
	status.Status, status.Message = "idle", "所选模型库存已达到目标，定期检查并自动补齐"
	if missing {
		status.Status, status.Message = "queued", "库存存在缺口，等待下一轮并发采集调度"
	}
	if d.maintenanceScanError != "" {
		status.Status, status.Message = "error", d.maintenanceScanError
	}
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
		// 临期空闲头提前补齐，旧头到新头入库前仍可用；占用中的租约不撤销。
		count := model.Available + model.InUse - model.RefreshDue
		if count < 0 {
			count = 0
		}
		counts[model.Model] = count
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
	d.mu.Lock()
	if err != nil {
		d.maintenanceScanError = "读取待采集账号失败，等待下一轮重试"
		d.mu.Unlock()
		return
	}
	d.maintenanceScanError = ""
	nextAccount := d.maintenanceNextAccount
	d.mu.Unlock()
	// 同优先级按账号 ID 稳定排序，从上轮最后派发账号的下一位继续。
	slices.SortFunc(accounts, func(a, b Account) int {
		if order := cmp.Compare(a.Priority, b.Priority); order != 0 {
			return order
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if offset := slices.IndexFunc(accounts, func(a Account) bool { return a.ID == nextAccount }); offset > 0 {
		accounts = append(accounts[offset:], accounts[:offset]...)
	}
	for i := range accounts {
		account := &accounts[i]
		if scanCtx.Err() != nil {
			d.mu.Lock()
			d.maintenanceNextAccount = account.ID
			d.maintenanceScanError = "本轮库存检查超时，下一轮从未检查账号继续"
			d.mu.Unlock()
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
		if active || d.clock().Before(retry.after) || workers >= healthyDynamicAccountConcurrency {
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
		d.maintenanceNextAccount = accounts[(i+1)%len(accounts)].ID
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
	return healthyDynamicRetry{after: now.Add(healthyDynamicRetryDelay), nextModel: previous.nextModel, message: previous.message}
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
	defer func() {
		if !attempted {
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if sharedRevision != d.sharedConfigRevision || accountRevision != d.accountConfigRevision[accountID] {
			return
		}
		if d.maintenanceRetry == nil {
			d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
		}
		retry := d.maintenanceRetry[accountID]
		retry.nextModel = modelOffset
		d.maintenanceRetry[accountID] = retry
	}()
	run, err := s.StartHealthyTurnStateDynamic(ctx, account, HealthyTurnStateDynamicStartInput{})
	if err != nil {
		d.mu.Lock()
		if sharedRevision == d.sharedConfigRevision && accountRevision == d.accountConfigRevision[accountID] {
			retry := d.maintenanceRetry[accountID]
			retry.message = infraerrors.Message(err)
			if d.maintenanceRetry == nil {
				d.maintenanceRetry = make(map[int64]healthyDynamicRetry)
			}
			d.maintenanceRetry[accountID] = retry
		}
		d.mu.Unlock()
		return false, true
	}
	defer s.StopHealthyTurnStateDynamic(context.Background(), accountID, run.ID)
	session, err := d.findRun(accountID, run.ID)
	if err != nil {
		return false, false
	}
	session.mu.Lock()
	session.backgroundMaintenance = true
	session.mu.Unlock()
	consecutiveFailures := 0
	startedAt := time.Now()
	for ctx.Err() == nil {
		// 每次网络请求前重读开关、模型与凭据，禁用或修改配置后不继续旧任务。
		current, err := s.accountRepo.GetByID(ctx, accountID)
		if err != nil || !healthyDynamicMaintenanceEnabled(current) {
			return false, false
		}
		currentConfig, err := d.loadConfig(ctx, accountID)
		if err != nil || !sameHealthyDynamicConfig(config, currentConfig) {
			return false, false
		}
		currentModels, err := healthyDynamicModels(current, currentConfig.Models, "")
		if err != nil {
			return false, false
		}
		// 某个模型失败后，下一次先尝试其他模型，避免其余缺口一直等待。
		modelOffset %= len(currentModels)
		if offset := modelOffset % len(currentModels); offset > 0 {
			currentModels = append(currentModels[offset:], currentModels[:offset]...)
		}
		session.mu.Lock()
		session.account, session.models = current, currentModels
		session.remainingFailures = healthyDynamicFailureLimit - consecutiveFailures
		session.mu.Unlock()
		previousAttempts, previousBatches := run.Attempts, run.FetchedBatches
		run, err = s.stepHealthyTurnStateDynamic(ctx, accountID, run.ID, healthyDynamicProbeConcurrency)
		if err != nil {
			return false, attempted
		}
		attempted = attempted || run.Attempts > 0 || run.FetchedBatches > 0
		if run.Status == "stopped" {
			return false, false
		}
		failed, failureMessage := false, ""
		session.mu.Lock()
		proxyWaiting := session.proxyWaiting
		batch := append([]*OpenAIHealthyTurnStateProbeResult(nil), session.lastBatch...)
		fetchFailures := append([]string(nil), session.lastFetchFailures...)
		session.mu.Unlock()
		// 凑齐并行入口时可能先提取失败再使用已有入口，按发生顺序计入，不能被同批探测遗漏。
		for _, message := range fetchFailures {
			if consecutiveFailures >= healthyDynamicFailureLimit {
				break
			}
			failed, failureMessage = true, message
			consecutiveFailures++
		}
		if run.Attempts > previousAttempts && len(batch) > 0 {
			batchOffset := modelOffset
			for index, model := range currentModels {
				if model.upstream == batch[len(batch)-1].Model {
					modelOffset = (batchOffset + index + 1) % len(currentModels)
					break
				}
			}
			for _, result := range batch {
				// 达到阈值后保留本批已经发出的结果，但成功不能取消本次退避。
				if consecutiveFailures >= healthyDynamicFailureLimit {
					break
				}
				if result.Status == "recorded" || result.Status == "already_recorded" {
					consecutiveFailures = 0
				} else {
					failed, failureMessage = true, result.Message
					consecutiveFailures++
					for index, model := range currentModels {
						if model.upstream == result.Model {
							modelOffset = (batchOffset + index + 1) % len(currentModels)
							break
						}
					}
				}
			}
		} else if !proxyWaiting && len(fetchFailures) == 0 && run.Status == "running" && (run.FetchedBatches > previousBatches || run.Attempts == previousAttempts) {
			failed, failureMessage = true, run.Message
			consecutiveFailures++
		}
		if failed && failureMessage == "" {
			failureMessage = "本次代理采集未获得有效健康头"
		}
		if run.Status != "running" {
			latestConfig, configErr := d.loadConfig(ctx, accountID)
			if configErr == nil && !sameHealthyDynamicConfig(config, latestConfig) {
				return false, false
			}
			counts, err := s.healthyDynamicInventory(ctx, accountID)
			_, missing := healthyDynamicMissingModel(currentModels, counts, config.TargetCount)
			return err == nil && !missing, attempted
		}
		if failed && consecutiveFailures >= healthyDynamicFailureLimit {
			session.mu.Lock()
			session.finishLocked("failed", fmt.Sprintf("连续 %d 次采集失败，暂停 %d 秒后重试；%s", healthyDynamicFailureLimit, int(healthyDynamicRetryDelay/time.Second), failureMessage))
			session.mu.Unlock()
			return false, attempted
		}
		if proxyWaiting {
			message := "代理入口正在被其他账号使用，释放采集槽后等待调度"
			if len(fetchFailures) > 0 {
				message += "；" + failureMessage
			}
			session.mu.Lock()
			session.finishLocked("completed", message)
			session.mu.Unlock()
			// 纯代理争用不记失败；同批真实提取错误仍保留已尝试状态，等待固定退避后重试。
			return false, len(fetchFailures) > 0
		}
		if run.Attempts >= healthyDynamicAccountAttemptSlice || time.Since(startedAt) >= healthyDynamicAccountTimeSlice {
			session.mu.Lock()
			session.finishLocked("completed", "本轮采集时间片已结束，剩余缺口轮转后继续补齐")
			session.mu.Unlock()
			return false, attempted
		}
		// 无论上次成功或失败都保留一秒间隔，未达到失败阈值时继续换入口补齐。
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, false
		case <-session.ctx.Done():
			timer.Stop()
			return false, false
		case <-timer.C:
		}
	}
	return false, attempted
}
