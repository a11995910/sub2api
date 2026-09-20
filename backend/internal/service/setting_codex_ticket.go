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

// GetOpenAICodexTicketEnabled 返回后台 292 打票总开关。
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

// GetOpenAICodexTicketHarvestProxyURL 返回后台配置的 292 打票代理。空则调用方回退 yaml/env。
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
}
