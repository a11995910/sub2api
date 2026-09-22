package service

import (
	"context"
	"maps"
	"time"
)

type openAICodexTicketJob struct {
	cancel context.CancelFunc
	wake   chan struct{}
}

func cloneCodexTicketAccount(account *Account) *Account {
	copy := *account
	copy.Extra = maps.Clone(account.Extra)
	copy.Credentials = maps.Clone(account.Credentials)
	return &copy
}

func (s *OpenAIGatewayService) initOpenAICodexTicketLimits() {
	s.openaiCodexTicketLimitsOnce.Do(func() {
		s.openaiCodexTicketTargetSlots = make(chan struct{}, 4)
		s.openaiCodexTicketProbeSlots = make(chan struct{}, s.openAICodexTicketConfig().HarvestGlobalConcurrency)
	})
}

// 调度只传递唤醒信号；业务请求不等待数据库、IP 提取或采票。
func (s *OpenAIGatewayService) requestOpenAICodexTicketHarvest(account *Account, model string) {
	if s == nil || !isOpenAICodexTicketAccount(account) {
		return
	}
	model = normalizeOpenAICodexTicketModel(model)
	s.openaiCodexTicketLifecycleMu.Lock()
	defer s.openaiCodexTicketLifecycleMu.Unlock()
	if s.openaiCodexTicketStopped || s.openaiCodexTicketContext == nil || s.openaiCodexTicketContext.Err() != nil {
		return
	}
	configured := false
	for _, item := range s.openAICodexTicketConfig().Models {
		if model != "" && model == normalizeOpenAICodexTicketModel(item) {
			configured = true
			break
		}
	}
	if !configured {
		return
	}
	key := openAICodexTicketKey(account.ID, model)
	if job := s.openaiCodexTicketJobs[key]; job != nil {
		select {
		case job.wake <- struct{}{}:
		default:
		}
		return
	}
	ctx, cancel := context.WithCancel(s.openaiCodexTicketContext)
	job := &openAICodexTicketJob{cancel: cancel, wake: make(chan struct{}, 1)}
	s.openaiCodexTicketJobs[key] = job
	// Add 与停止标记同锁，退出时不会再有新的 worker 加入。
	s.openaiCodexTicketWorkers.Add(1)
	go func(account *Account) {
		defer s.openaiCodexTicketWorkers.Done()
		defer cancel()
		defer func() {
			s.openaiCodexTicketLifecycleMu.Lock()
			s.openaiCodexTicketHarvest.scheduleAttempt(account.ID, model, time.Time{})
			delete(s.openaiCodexTicketJobs, key)
			s.openaiCodexTicketLifecycleMu.Unlock()
		}()
		s.runOpenAICodexTicketJob(ctx, job, account, model)
	}(cloneCodexTicketAccount(account))
}

func (s *OpenAIGatewayService) reconcileOpenAICodexTicketJobs(targets []openAICodexTicketHarvestTarget) {
	wanted := make(map[string]bool, len(targets))
	for _, target := range targets {
		wanted[openAICodexTicketKey(target.account.ID, target.model)] = true
		s.requestOpenAICodexTicketHarvest(&target.account, target.model)
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	defer s.openaiCodexTicketLifecycleMu.Unlock()
	for key, job := range s.openaiCodexTicketJobs {
		if !wanted[key] {
			job.cancel()
		}
	}
}

func (s *OpenAIGatewayService) runOpenAICodexTicketJob(ctx context.Context, job *openAICodexTicketJob, account *Account, model string) {
	s.initOpenAICodexTicketLimits()
	nextAttempt := time.Time{}
	for ctx.Err() == nil {
		ticket := s.lookupOpenAICodexTicket(account, model)
		valid := ticket.valid(time.Now(), s.openAICodexTicketConfig().TargetLength)
		wakeAt := nextAttempt
		if valid {
			wakeAt = ticket.ExpiresAt
		}
		if delay := time.Until(wakeAt); delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
			case <-job.wake:
			case <-timer.C:
			}
			timer.Stop()
			continue
		}
		// 全局最多四个目标同时提取或采集；排队不会拖住其他目标的独立计时。
		select {
		case <-ctx.Done():
			return
		case s.openaiCodexTicketTargetSlots <- struct{}{}:
		}
		harvested := false
		func() {
			defer func() { <-s.openaiCodexTicketTargetSlots }()
			s.openaiCodexTicketHarvest.scheduleAttempt(account.ID, model, time.Time{})
			if s.accountRepo != nil {
				readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				latest, err := s.accountRepo.GetByID(readCtx, account.ID)
				cancel()
				if err != nil || latest == nil || latest.Status != StatusActive || !isOpenAICodexTicketAccount(latest) {
					return
				}
				account = cloneCodexTicketAccount(latest)
			}
			harvested = s.probeOnceOpenAICodexTicket(ctx, account, model)
		}()
		nextAttempt = time.Now().Add(time.Duration(s.openAICodexTicketConfig().HarvestProbeIntervalSeconds) * time.Second)
		// 新票刚保存就被业务弃用时，也应立即补采，不能误套失败退避。
		if harvested || s.lookupOpenAICodexTicket(account, model).valid(time.Now(), s.openAICodexTicketConfig().TargetLength) {
			nextAttempt = time.Time{}
		}
		s.openaiCodexTicketHarvest.scheduleAttempt(account.ID, model, nextAttempt)
	}
}
