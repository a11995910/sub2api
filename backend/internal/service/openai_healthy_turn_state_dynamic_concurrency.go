package service

import (
	"context"
	"maps"
	"time"
)

const (
	healthyDynamicAccountConcurrency     = 8
	healthyDynamicProbeConcurrency       = 3
	healthyDynamicGlobalProbeConcurrency = 24
	healthyDynamicAccountAttemptSlice    = 15
	healthyDynamicAccountTimeSlice       = 90 * time.Second
)

type healthyDynamicProbeJob struct {
	model healthyDynamicModel
	proxy string
}

type healthyDynamicProbeCompletion struct {
	model  healthyDynamicModel
	result *OpenAIHealthyTurnStateProbeResult
}

// 对实际模型逐轮预留缺口，同一批次不会重复占用最后一个库存位置。
func reserveHealthyDynamicModels(models []healthyDynamicModel, counts map[string]int64, target, limit int) []healthyDynamicModel {
	reserved := make([]healthyDynamicModel, 0, max(0, limit))
	counts = maps.Clone(counts)
	if counts == nil {
		counts = make(map[string]int64)
	}
	for len(reserved) < limit {
		before := len(reserved)
		for _, model := range models {
			if counts[model.upstream] >= int64(target) {
				continue
			}
			reserved = append(reserved, model)
			counts[model.upstream]++
			if len(reserved) == limit {
				break
			}
		}
		if len(reserved) == before {
			break
		}
	}
	return reserved
}

// 跨账号同时采集时也不共用同一个临时代理入口。
func (d *healthyTurnStateDynamicService) reserveHealthyDynamicProxy(proxy string) bool {
	d.probeMu.Lock()
	defer d.probeMu.Unlock()
	if d.activeProxies == nil {
		d.activeProxies = make(map[string]bool)
	}
	if d.activeProxies[proxy] {
		return false
	}
	d.activeProxies[proxy] = true
	return true
}

func (s *AccountTestService) probeHealthyDynamicLimited(ctx context.Context, account *Account, model, transport, proxy string) (*OpenAIHealthyTurnStateProbeResult, error) {
	d := s.healthyTurnStateDynamic
	d.probeMu.Lock()
	if d.probeSlots == nil {
		d.probeSlots = make(chan struct{}, healthyDynamicGlobalProbeConcurrency)
	}
	slots := d.probeSlots
	d.probeMu.Unlock()
	defer func() {
		d.probeMu.Lock()
		delete(d.activeProxies, proxy)
		d.probeMu.Unlock()
	}()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probe := d.probe
	if probe == nil {
		probe = s.ProbeOpenAIHealthyTurnStateWithProxy
	}
	return probe(ctx, account, model, transport, proxy)
}
