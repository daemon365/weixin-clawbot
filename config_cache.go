package weixin

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

const (
	configCacheTTL          = 24 * time.Hour
	configCacheInitialRetry = 2 * time.Second
	configCacheMaxRetry     = time.Hour
)

type CachedConfig struct {
	TypingTicket string
}

type configCacheEntry struct {
	config        CachedConfig
	everSucceeded bool
	nextFetchAt   time.Time
	retryDelay    time.Duration
}

type ConfigManager struct {
	api   *APIClient
	now   func() time.Time
	rand  *rand.Rand
	mu    sync.Mutex
	cache map[string]configCacheEntry
}

func NewConfigManager(api *APIClient) *ConfigManager {
	return &ConfigManager{
		api:   api,
		now:   time.Now,
		rand:  rand.New(rand.NewSource(time.Now().UnixNano())),
		cache: make(map[string]configCacheEntry),
	}
}

func (m *ConfigManager) GetForUser(ctx context.Context, userID, contextToken string) (CachedConfig, error) {
	m.mu.Lock()
	entry, ok := m.cache[userID]
	now := m.now()
	shouldFetch := !ok || !now.Before(entry.nextFetchAt)
	m.mu.Unlock()

	if shouldFetch {
		resp, err := m.api.GetConfig(ctx, userID, contextToken, 0)
		if err == nil && resp.Ret == 0 {
			next := configCacheEntry{
				config:        CachedConfig{TypingTicket: resp.TypingTicket},
				everSucceeded: true,
				nextFetchAt:   now.Add(time.Duration(m.rand.Float64() * float64(configCacheTTL))),
				retryDelay:    configCacheInitialRetry,
			}
			m.mu.Lock()
			m.cache[userID] = next
			m.mu.Unlock()
			return next.config, nil
		}

		m.mu.Lock()
		defer m.mu.Unlock()
		if ok {
			entry.retryDelay *= 2
			if entry.retryDelay > configCacheMaxRetry {
				entry.retryDelay = configCacheMaxRetry
			}
			entry.nextFetchAt = now.Add(entry.retryDelay)
			m.cache[userID] = entry
			return entry.config, err
		}

		m.cache[userID] = configCacheEntry{
			config:      CachedConfig{},
			nextFetchAt: now.Add(configCacheInitialRetry),
			retryDelay:  configCacheInitialRetry,
		}
		return CachedConfig{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cache[userID].config, nil
}
