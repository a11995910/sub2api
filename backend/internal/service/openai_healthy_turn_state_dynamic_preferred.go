package service

import (
	"crypto/sha256"
	"sync"
	"time"
)

const healthyDynamicPreferredTTL = 10 * time.Minute
const healthyDynamicPreferredLimit = 1024

// 只保存临时入口，不代表供应商背后的实际出口 IP；凭据不进入数据库或管理接口。
type healthyDynamicPreferredKey struct {
	accountID        int64
	model, transport string
	source           [32]byte
}

type healthyDynamicPreferredEntry struct {
	proxy     string
	expiresAt time.Time
}

type healthyDynamicPreferredCache struct {
	mu      sync.Mutex
	entries map[healthyDynamicPreferredKey]healthyDynamicPreferredEntry
}

func healthyDynamicPreferredScope(accountID int64, model, transport string, config HealthyTurnStateDynamicConfigInput) healthyDynamicPreferredKey {
	return healthyDynamicPreferredKey{accountID: accountID, model: model, transport: transport,
		source: sha256.Sum256([]byte(config.APIURL + "\x00" + config.Protocol))}
}

func (c *healthyDynamicPreferredCache) get(key healthyDynamicPreferredKey, now time.Time) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for scope, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, scope)
		}
	}
	return c.entries[key].proxy
}

func (c *healthyDynamicPreferredCache) complete(key healthyDynamicPreferredKey, proxy string, success bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !success {
		if c.entries[key].proxy == proxy {
			delete(c.entries, key)
		}
		return
	}
	if c.entries == nil {
		c.entries = make(map[healthyDynamicPreferredKey]healthyDynamicPreferredEntry)
	}
	if _, exists := c.entries[key]; !exists && len(c.entries) >= healthyDynamicPreferredLimit {
		var oldest healthyDynamicPreferredKey
		var expiration time.Time
		for scope, entry := range c.entries {
			if expiration.IsZero() || entry.expiresAt.Before(expiration) {
				oldest, expiration = scope, entry.expiresAt
			}
		}
		delete(c.entries, oldest)
	}
	c.entries[key] = healthyDynamicPreferredEntry{proxy: proxy, expiresAt: now.Add(healthyDynamicPreferredTTL)}
}

func (c *healthyDynamicPreferredCache) invalidate(accountID int64, all bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.entries {
		if all || key.accountID == accountID {
			delete(c.entries, key)
		}
	}
}
