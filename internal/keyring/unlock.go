package keyring

import (
	"context"
	"sync"
	"time"
)

// UnlockCache keeps the decrypted master key in memory until its TTL expires.
type UnlockCache struct {
	mu        sync.Mutex
	key       []byte
	expiresAt time.Time
	now       func() time.Time
}

// NewUnlockCache constructs an in-memory unlock cache.
func NewUnlockCache(now func() time.Time) *UnlockCache {
	if now == nil {
		now = time.Now
	}
	return &UnlockCache{now: now}
}

// Set stores a copy of key until ttl elapses.
func (c *UnlockCache) Set(_ context.Context, key []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.key = append(c.key[:0], key...)
	c.expiresAt = c.now().Add(ttl)
}

// Get returns a copy of the cached key when it is still valid.
func (c *UnlockCache) Get(_ context.Context) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.key) == 0 || !c.now().Before(c.expiresAt) {
		c.key = nil
		return nil, false
	}
	return append([]byte(nil), c.key...), true
}

// Clear removes any cached key.
func (c *UnlockCache) Clear(_ context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.key = nil
	c.expiresAt = time.Time{}
}
