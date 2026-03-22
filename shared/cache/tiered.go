// tiered.go - Tiered caching implementation
//
// This file provides a two-level cache:
// - L1: In-memory cache (fast, limited size)
// - L2: Distributed cache (Redis, optional)
//
// This reduces cache misses and improves throughput.

package cache

import (
	"sync"
	"time"
)

// L1Cache is a simple in-memory cache with TTL
type L1Cache struct {
	mu      sync.RWMutex
	entries map[string]*l1Entry
	maxSize int
	ttl     time.Duration
}

type l1Entry struct {
	value     interface{}
	expiresAt time.Time
}

// NewL1Cache creates a new L1 cache
func NewL1Cache(maxSize int, ttl time.Duration) *L1Cache {
	if maxSize <= 0 {
		maxSize = 10000
	}
	if ttl <= 0 {
		ttl = 1 * time.Second
	}

	c := &L1Cache{
		entries: make(map[string]*l1Entry),
		maxSize: maxSize,
		ttl:     ttl,
	}

	// Start cleanup goroutine
	go c.cleanup()

	return c
}

// Get retrieves a value from the cache
func (c *L1Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		// Expired, remove it
		c.mu.Lock()
		delete(c.entries, key)
		c.mu.Unlock()
		return nil, false
	}

	return entry.value, true
}

// Set adds or updates a value in the cache
func (c *L1Cache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict if at capacity
	if len(c.entries) >= c.maxSize {
		// Simple eviction: remove oldest
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}

	c.entries[key] = &l1Entry{
		value:     value,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// Delete removes a value from the cache
func (c *L1Cache) Delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

// Clear removes all entries from the cache
func (c *L1Cache) Clear() {
	c.mu.Lock()
	c.entries = make(map[string]*l1Entry)
	c.mu.Unlock()
}

// Size returns the number of entries in the cache
func (c *L1Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// cleanup periodically removes expired entries
func (c *L1Cache) cleanup() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.expiresAt) {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

// L2Cache is an interface for distributed cache backends
type L2Cache interface {
	Get(key string) (interface{}, bool, error)
	Set(key string, value interface{}, ttl time.Duration) error
	Delete(key string) error
}

// TieredCache combines L1 and L2 caches
type TieredCache struct {
	l1 *L1Cache
	l2 L2Cache // Optional, can be nil
}

// NewTieredCache creates a new tiered cache
func NewTieredCache(l1 *L1Cache, l2 L2Cache) *TieredCache {
	return &TieredCache{
		l1: l1,
		l2: l2,
	}
}

// Get retrieves a value, checking L1 first, then L2
func (c *TieredCache) Get(key string) (interface{}, bool) {
	// Check L1 first
	if value, ok := c.l1.Get(key); ok {
		return value, true
	}

	// Check L2 if available
	if c.l2 != nil {
		if value, ok, err := c.l2.Get(key); err == nil && ok {
			// Promote to L1
			c.l1.Set(key, value)
			return value, true
		}
	}

	return nil, false
}

// Set adds a value to both L1 and L2
func (c *TieredCache) Set(key string, value interface{}, ttl time.Duration) {
	c.l1.Set(key, value)

	if c.l2 != nil {
		// Best effort, ignore errors
		_ = c.l2.Set(key, value, ttl)
	}
}

// Delete removes a value from both L1 and L2
func (c *TieredCache) Delete(key string) {
	c.l1.Delete(key)

	if c.l2 != nil {
		// Best effort, ignore errors
		_ = c.l2.Delete(key)
	}
}
