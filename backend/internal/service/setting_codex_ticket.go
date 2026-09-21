package service

import (
	"context"
	"errors"
	"strings"
	"time"
)

type cachedOpenAICodexTicketEnabled struct {
	value     bool
	expiresAt int64
}

const openAICodexTicketEnabledCacheTTL = 5 * time.Second

// GetOpenAICodexTicketEnabled 返回后台 292/332 打票总开关。
// 设置键存在时以后台为准；缺失则回退 yaml/env。
func (s *SettingService) GetOpenAICodexTicketEnabled(ctx context.Context, fallback bool) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return fallback
	}
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if cached, ok := s.openAICodexTicketEnabledCache.Load().(*cachedOpenAICodexTicketEnabled); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.value
		}
	}
	resultCh := s.openAICodexTicketEnabledSF.DoChan(SettingKeyOpenAICodexTicketEnabled, func() (any, error) {
		if cached, ok := s.openAICodexTicketEnabledCache.Load().(*cachedOpenAICodexTicketEnabled); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached.value, nil
			}
		}
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		value, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAICodexTicketEnabled)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			if cached, ok := s.openAICodexTicketEnabledCache.Load().(*cachedOpenAICodexTicketEnabled); ok && cached != nil {
				return cached.value, nil
			}
			return fallback, nil
		}
		enabled := fallback
		if err == nil && strings.TrimSpace(value) != "" {
			enabled = value == "true"
		}
		s.openAICodexTicketEnabledCache.Store(&cachedOpenAICodexTicketEnabled{
			value:     enabled,
			expiresAt: time.Now().Add(openAICodexTicketEnabledCacheTTL).UnixNano(),
		})
		return enabled, nil
	})
	select {
	case <-ctx.Done():
		return fallback
	case result := <-resultCh:
		if v, ok := result.Val.(bool); ok && result.Err == nil {
			return v
		}
		return fallback
	}
}

func (s *SettingService) InvalidateOpenAICodexTicketEnabledCache() {
	if s == nil {
		return
	}
	s.openAICodexTicketEnabledSF.Forget(SettingKeyOpenAICodexTicketEnabled)
	s.openAICodexTicketEnabledCache.Store(&cachedOpenAICodexTicketEnabled{expiresAt: 0})
}

type cachedOpenAICodexTicketHarvestProxy struct {
	value     string
	expiresAt int64
}

const openAICodexTicketHarvestProxyCacheTTL = 5 * time.Second

// GetOpenAICodexTicketHarvestProxyURL 返回后台配置的 292/332 打票代理。空则调用方回退 yaml/env。
func (s *SettingService) GetOpenAICodexTicketHarvestProxyURL(ctx context.Context) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return ""
	}
	if s == nil || s.settingRepo == nil {
		return ""
	}
	if cached, ok := s.openAICodexTicketHarvestProxyCache.Load().(*cachedOpenAICodexTicketHarvestProxy); ok && cached != nil {
		if time.Now().UnixNano() < cached.expiresAt {
			return cached.value
		}
	}
	resultCh := s.openAICodexTicketHarvestProxySF.DoChan(SettingKeyOpenAICodexTicketHarvestProxyURL, func() (any, error) {
		if cached, ok := s.openAICodexTicketHarvestProxyCache.Load().(*cachedOpenAICodexTicketHarvestProxy); ok && cached != nil {
			if time.Now().UnixNano() < cached.expiresAt {
				return cached.value, nil
			}
		}
		dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		value, err := s.settingRepo.GetValue(dbCtx, SettingKeyOpenAICodexTicketHarvestProxyURL)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err != nil && !errors.Is(err, ErrSettingNotFound) {
			// 存储短暂失败时保留最近已知的代理。
			if cached, ok := s.openAICodexTicketHarvestProxyCache.Load().(*cachedOpenAICodexTicketHarvestProxy); ok && cached != nil {
				value = cached.value
			}
			s.openAICodexTicketHarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{
				value:     value,
				expiresAt: time.Now().Add(time.Second).UnixNano(),
			})
			return value, nil
		}
		value = strings.TrimSpace(value)
		s.openAICodexTicketHarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{
			value:     value,
			expiresAt: time.Now().Add(openAICodexTicketHarvestProxyCacheTTL).UnixNano(),
		})
		return value, nil
	})
	select {
	case <-ctx.Done():
		return ""
	case result := <-resultCh:
		if v, ok := result.Val.(string); ok && result.Err == nil {
			return v
		}
		return ""
	}
}

func (s *SettingService) InvalidateOpenAICodexTicketHarvestProxyCache() {
	if s == nil {
		return
	}
	s.openAICodexTicketHarvestProxySF.Forget(SettingKeyOpenAICodexTicketHarvestProxyURL)
	s.openAICodexTicketHarvestProxyCache.Store(&cachedOpenAICodexTicketHarvestProxy{expiresAt: 0})
	s.InvalidateOpenAICodexTicketHarvestSourceCache()
}

type cachedOpenAICodexTicketHarvestSource struct {
	value      OpenAICodexTicketHarvestSource
	expiresAt  int64
	generation uint64
}

func openAICodexTicketHarvestSourceFromSettings(values map[string]string, fallback OpenAICodexTicketHarvestSource) OpenAICodexTicketHarvestSource {
	for key, target := range map[string]*string{
		SettingKeyOpenAICodexTicketHarvestProxyMode:       &fallback.Mode,
		SettingKeyOpenAICodexTicketHarvestProxyURL:        &fallback.ProxyURL,
		SettingKeyOpenAICodexTicketHarvestExtractURL:      &fallback.ExtractURL,
		SettingKeyOpenAICodexTicketHarvestExtractProtocol: &fallback.ExtractProtocol,
	} {
		if value := strings.TrimSpace(values[key]); value != "" {
			*target = value
		}
	}
	return normalizeOpenAICodexTicketHarvestSource(fallback)
}

// GetOpenAICodexTicketHarvestSource 原子读取同一次设置快照，避免切换来源时拼接新旧配置。
func (s *SettingService) GetOpenAICodexTicketHarvestSource(ctx context.Context, fallback OpenAICodexTicketHarvestSource) OpenAICodexTicketHarvestSource {
	fallback = normalizeOpenAICodexTicketHarvestSource(fallback)
	if s == nil || s.settingRepo == nil {
		return fallback
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return fallback
	}
	for attempt := 0; attempt < 2; attempt++ {
		if cached, ok := s.openAICodexTicketHarvestSourceCache.Load().(*cachedOpenAICodexTicketHarvestSource); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
			return cached.value
		}
		resultCh := s.openAICodexTicketHarvestSourceSF.DoChan(SettingKeyOpenAICodexTicketHarvestProxyMode, func() (any, error) {
			s.openAICodexTicketHarvestSourceMu.Lock()
			generation := s.openAICodexTicketHarvestSourceGeneration
			if cached, ok := s.openAICodexTicketHarvestSourceCache.Load().(*cachedOpenAICodexTicketHarvestSource); ok && cached != nil && time.Now().UnixNano() < cached.expiresAt {
				s.openAICodexTicketHarvestSourceMu.Unlock()
				return cached, nil
			}
			s.openAICodexTicketHarvestSourceMu.Unlock()
			dbCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			values, err := s.settingRepo.GetMultiple(dbCtx, []string{
				SettingKeyOpenAICodexTicketHarvestProxyMode,
				SettingKeyOpenAICodexTicketHarvestProxyURL,
				SettingKeyOpenAICodexTicketHarvestExtractURL,
				SettingKeyOpenAICodexTicketHarvestExtractProtocol,
			})
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			s.openAICodexTicketHarvestSourceMu.Lock()
			defer s.openAICodexTicketHarvestSourceMu.Unlock()
			// 保存与代际比较同锁；失效前开始的查询绝不能重新发布旧供应商凭据。
			if generation != s.openAICodexTicketHarvestSourceGeneration {
				return &cachedOpenAICodexTicketHarvestSource{generation: generation}, nil
			}
			if err != nil {
				if cached, ok := s.openAICodexTicketHarvestSourceCache.Load().(*cachedOpenAICodexTicketHarvestSource); ok && cached != nil && cached.value.Mode != "" {
					next := &cachedOpenAICodexTicketHarvestSource{value: cached.value, expiresAt: time.Now().Add(time.Second).UnixNano(), generation: generation}
					s.openAICodexTicketHarvestSourceCache.Store(next)
					return next, nil
				}
				return &cachedOpenAICodexTicketHarvestSource{value: fallback, generation: generation}, nil
			}
			source := openAICodexTicketHarvestSourceFromSettings(values, fallback)
			next := &cachedOpenAICodexTicketHarvestSource{value: source, expiresAt: time.Now().Add(openAICodexTicketHarvestProxyCacheTTL).UnixNano(), generation: generation}
			s.openAICodexTicketHarvestSourceCache.Store(next)
			return next, nil
		})
		select {
		case <-ctx.Done():
			return fallback
		case result := <-resultCh:
			if value, ok := result.Val.(*cachedOpenAICodexTicketHarvestSource); ok && result.Err == nil {
				s.openAICodexTicketHarvestSourceMu.Lock()
				current := value.generation == s.openAICodexTicketHarvestSourceGeneration
				s.openAICodexTicketHarvestSourceMu.Unlock()
				if current {
					return value.value
				}
				continue
			}
			return fallback
		}
	}
	// 连续配置切换时本轮暂停采集，不退回旧来源或启动配置。
	return normalizeOpenAICodexTicketHarvestSource(OpenAICodexTicketHarvestSource{})
}

func (s *SettingService) InvalidateOpenAICodexTicketHarvestSourceCache() {
	if s == nil {
		return
	}
	s.openAICodexTicketHarvestSourceMu.Lock()
	defer s.openAICodexTicketHarvestSourceMu.Unlock()
	s.openAICodexTicketHarvestSourceGeneration++
	s.openAICodexTicketHarvestSourceSF.Forget(SettingKeyOpenAICodexTicketHarvestProxyMode)
	s.openAICodexTicketHarvestSourceCache.Store(&cachedOpenAICodexTicketHarvestSource{expiresAt: 0})
}
