package service

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type codexTicketProbeResult struct {
	state, proxy, result string
	status               int
}

func (s *OpenAIGatewayService) collectOpenAICodexTicket(ctx context.Context, account *Account, model string, loadProxies func(context.Context) ([]string, OpenAICodexTicketHarvestSource, error)) bool {
	if s == nil || ctx.Err() != nil || !isOpenAICodexTicketAccount(account) || s.httpUpstream == nil || !s.openAICodexTicketEnabledContext(ctx) {
		return false
	}
	model = normalizeOpenAICodexTicketModel(model)
	if !s.openAICodexTicketGatedModel(model) {
		return false
	}
	s.initOpenAICodexTicketLimits()
	outcome, _, _ := s.openaiCodexTicketFlight.Do(openAICodexTicketKey(account.ID, model), func() (any, error) {
		cfg := s.openAICodexTicketConfig()
		if ctx.Err() != nil {
			return false, nil
		}
		if s.lookupOpenAICodexTicket(account, model).valid(time.Now(), cfg.TargetLength) {
			return true, nil
		}
		s.openaiCodexTicketHarvest.beginAttempt(account.ID, model, 0, 0)
		result, status := "canceled", 0
		defer func() {
			if ctx.Err() != nil {
				result, status = "canceled", 0
			}
			s.openaiCodexTicketHarvest.finishAttempt(account.ID, model, result, status)
			logger.L().Info("门票独立采集完成", zap.Int64("account_id", account.ID), zap.String("model", model), zap.String("result", result), zap.Int("http", status))
		}()
		if !s.openAICodexTicketProbeStillEligible(ctx, account) {
			return nil, nil
		}
		token, _, err := s.GetAccessToken(ctx, account)
		if err != nil || strings.TrimSpace(token) == "" {
			result = "auth_failed"
			return nil, nil
		}
		proxies, source, err := loadProxies(ctx)
		if err != nil {
			result = "proxy_extract_failed"
			return nil, nil
		}
		if len(proxies) == 0 || source != s.openAICodexTicketHarvestSource(ctx) || ctx.Err() != nil {
			return nil, nil
		}
		winner, attempts := s.raceOpenAICodexTicketProxies(ctx, account, token, model, proxies, source)
		result, status = winner.result, winner.status
		if result != "success" {
			return nil, nil
		}
		// 仅协调者保存一张票；竞速失败者、迟到结果和取消结果均不得写入。
		if !s.openAICodexTicketProbeStillEligible(ctx, account) || source != s.openAICodexTicketHarvestSource(ctx) || ctx.Err() != nil {
			result, status = "canceled", 0
			return nil, nil
		}
		now := time.Now()
		s.storeOpenAICodexTicket(ctx, account, &openAICodexTicket{
			AccountID: account.ID, Model: model, State: winner.state, Length: len(winner.state),
			CapturedAt: now, ExpiresAt: now.Add(time.Duration(cfg.TTLSeconds) * time.Second),
			Attempts: attempts, ProxyURL: winner.proxy,
		})
		return true, nil
	})
	return outcome == true
}

// 抢的是第一张完成校验的票，响应头先到但模型偏移或响应失败的候选不能胜出。
func (s *OpenAIGatewayService) raceOpenAICodexTicketProxies(ctx context.Context, account *Account, token, model string, proxies []string, source OpenAICodexTicketHarvestSource) (codexTicketProbeResult, int) {
	cfg := s.openAICodexTicketConfig()
	parallel := min(cfg.HarvestProxyConcurrency, len(proxies))
	if source.Mode != OpenAICodexTicketHarvestExtractMode {
		parallel = 1
	}
	raceCtx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	results := make(chan codexTicketProbeResult, parallel)
	var next, started atomic.Int64
	for i := 0; i < parallel; i++ {
		workers.Add(1)
		go func(account *Account) {
			defer workers.Done()
			for raceCtx.Err() == nil {
				index := int(next.Add(1)) - 1
				if index >= len(proxies) {
					return
				}
				select {
				case <-raceCtx.Done():
					return
				case s.openaiCodexTicketProbeSlots <- struct{}{}:
				}
				out := func() codexTicketProbeResult {
					defer func() { <-s.openaiCodexTicketProbeSlots }()
					if !s.openAICodexTicketProbeStillEligible(raceCtx, account) || source != s.openAICodexTicketHarvestSource(raceCtx) || raceCtx.Err() != nil {
						return codexTicketProbeResult{result: "canceled"}
					}
					started.Add(1)
					s.openaiCodexTicketHarvest.startProbe(account.ID, model, len(proxies))
					state, status, err := s.fireOpenAICodexTicketProbe(raceCtx, account, token, model, proxies[index], time.Duration(cfg.HarvestAttemptTimeoutSeconds)*time.Second)
					out := codexTicketProbeResult{state: state, status: status, proxy: proxies[index]}
					switch {
					case err != nil:
						out.result = classifyOpenAICodexTicketProbeError(err)
					case status != http.StatusOK || !validOpenAICodexTicketState(state, cfg.TargetLength):
						out.result = classifyOpenAICodexTicketProbeResponse(status)
					default:
						out.result = "success"
					}
					if out.result != "success" && raceCtx.Err() == nil {
						logger.L().Info("门票探测未命中", zap.Int64("account_id", account.ID), zap.String("model", model), zap.String("reason", out.result), zap.Int("http", status), zap.Int("len", len(state)))
					}
					return out
				}()
				select {
				case results <- out:
				case <-raceCtx.Done():
					return
				}
				if out.result == "success" || out.result == "canceled" {
					return
				}
			}
		}(cloneCodexTicketAccount(account))
	}
	go func() { workers.Wait(); close(results) }()
	last := codexTicketProbeResult{result: "canceled"}
	for {
		select {
		case <-ctx.Done():
			return codexTicketProbeResult{result: "canceled"}, int(started.Load())
		case out, ok := <-results:
			if !ok {
				return last, int(started.Load())
			}
			last = out
			if out.result == "success" {
				cancel()
				return out, int(started.Load())
			}
		}
	}
}
