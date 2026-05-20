package handlers

import (
	"sync"
	"time"
)

// responseCache memoizes whole JSON payloads for hot read endpoints. Values
// on the leaderboard and stats only change when the hourly PortfolioSnapshotJob
// fires or a bot trades — both rare relative to page-load rate — so a 30s
// cache is invisible to users and shaves the Tokyo-Turso round-trips out of
// the common path.
type responseCache struct {
	mu  sync.RWMutex
	hit map[string]cacheEntry
	ttl time.Duration
}

type cacheEntry struct {
	payload   interface{}
	expiresAt time.Time
}

func newResponseCache(ttl time.Duration) *responseCache {
	return &responseCache{hit: make(map[string]cacheEntry), ttl: ttl}
}

func (c *responseCache) get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.hit[key]
	if !ok || time.Now().After(e.expiresAt) {
		return nil, false
	}
	return e.payload, true
}

func (c *responseCache) put(key string, v interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hit[key] = cacheEntry{payload: v, expiresAt: time.Now().Add(c.ttl)}
}

var (
	leaderboardCache = newResponseCache(30 * time.Second)
	statsCache       = newResponseCache(30 * time.Second)
)
