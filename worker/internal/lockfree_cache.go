// lockfree_cache.go - Lock-free search cache using sync.Map
//
// This file provides a lock-free search cache that uses sync.Map
// for concurrent read-heavy access without blocking.

package internal

import (
	"sync"
	"time"

	"github.com/pavandhadge/vectron/shared/proto/worker"
)

// LockFreeSearchCache provides lock-free search result caching
type LockFreeSearchCache struct {
	cache sync.Map // map[uint64]*lockFreeCacheEntry
	ttl   time.Duration
}

type lockFreeCacheEntry struct {
	resp      *worker.SearchResponse
	expiresAt int64 // Unix nanoseconds
}

// NewLockFreeSearchCache creates a new lock-free search cache
func NewLockFreeSearchCache(ttl time.Duration, maxSize int) *LockFreeSearchCache {
	if ttl <= 0 {
		ttl = 200 * time.Millisecond
	}
	c := &LockFreeSearchCache{
		ttl: ttl,
	}
	// Start cleanup goroutine
	go c.cleanup()
	return c
}

// Get retrieves a cached search result (lock-free)
func (c *LockFreeSearchCache) Get(key uint64) (*worker.SearchResponse, bool) {
	val, ok := c.cache.Load(key)
	if !ok {
		return nil, false
	}
	entry := val.(*lockFreeCacheEntry)
	if entry.expiresAt <= time.Now().UnixNano() {
		// Expired, remove it
		c.cache.Delete(key)
		return nil, false
	}
	return entry.resp, true
}

// Set adds or updates a cached search result (lock-free)
func (c *LockFreeSearchCache) Set(key uint64, resp *worker.SearchResponse) {
	if resp == nil {
		return
	}
	entry := &lockFreeCacheEntry{
		resp:      resp,
		expiresAt: time.Now().Add(c.ttl).UnixNano(),
	}
	c.cache.Store(key, entry)
}

// Delete removes a cached result (lock-free)
func (c *LockFreeSearchCache) Delete(key uint64) {
	c.cache.Delete(key)
}

// Clear removes all cached results
func (c *LockFreeSearchCache) Clear() {
	c.cache.Range(func(key, value interface{}) bool {
		c.cache.Delete(key)
		return true
	})
}

// Size returns the approximate number of cached entries
func (c *LockFreeSearchCache) Size() int {
	count := 0
	c.cache.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// cleanup periodically removes expired entries
func (c *LockFreeSearchCache) cleanup() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().UnixNano()
		c.cache.Range(func(key, value interface{}) bool {
			entry := value.(*lockFreeCacheEntry)
			if entry.expiresAt <= now {
				c.cache.Delete(key)
			}
			return true
		})
	}
}

// LockFreeRoutingCache provides lock-free routing cache for placement driver
type LockFreeRoutingCache struct {
	cache sync.Map // map[string]*routingCacheEntry
	ttl   time.Duration
}

type routingCacheEntry struct {
	response  interface{}
	expiresAt int64
}

// NewLockFreeRoutingCache creates a new lock-free routing cache
func NewLockFreeRoutingCache(ttl time.Duration) *LockFreeRoutingCache {
	if ttl <= 0 {
		ttl = 100 * time.Millisecond
	}
	c := &LockFreeRoutingCache{
		ttl: ttl,
	}
	go c.cleanup()
	return c
}

// Get retrieves a cached routing result (lock-free)
func (c *LockFreeRoutingCache) Get(key string) (interface{}, bool) {
	val, ok := c.cache.Load(key)
	if !ok {
		return nil, false
	}
	entry := val.(*routingCacheEntry)
	if entry.expiresAt <= time.Now().UnixNano() {
		c.cache.Delete(key)
		return nil, false
	}
	return entry.response, true
}

// Set adds or updates a cached routing result (lock-free)
func (c *LockFreeRoutingCache) Set(key string, response interface{}) {
	entry := &routingCacheEntry{
		response:  response,
		expiresAt: time.Now().Add(c.ttl).UnixNano(),
	}
	c.cache.Store(key, entry)
}

// Delete removes a cached result (lock-free)
func (c *LockFreeRoutingCache) Delete(key string) {
	c.cache.Delete(key)
}

// cleanup periodically removes expired entries
func (c *LockFreeRoutingCache) cleanup() {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now().UnixNano()
		c.cache.Range(func(key, value interface{}) bool {
			entry := value.(*routingCacheEntry)
			if entry.expiresAt <= now {
				c.cache.Delete(key)
			}
			return true
		})
	}
}

// LockFreeWorkerCache provides lock-free worker info caching
type LockFreeWorkerCache struct {
	cache sync.Map // map[uint64]*workerCacheEntry
	mu    sync.RWMutex
}

type workerCacheEntry struct {
	info      interface{}
	expiresAt int64
}

// NewLockFreeWorkerCache creates a new lock-free worker cache
func NewLockFreeWorkerCache() *LockFreeWorkerCache {
	return &LockFreeWorkerCache{}
}

// Get retrieves cached worker info (lock-free)
func (c *LockFreeWorkerCache) Get(workerID uint64) (interface{}, bool) {
	val, ok := c.cache.Load(workerID)
	if !ok {
		return nil, false
	}
	entry := val.(*workerCacheEntry)
	if entry.expiresAt <= time.Now().UnixNano() {
		c.cache.Delete(workerID)
		return nil, false
	}
	return entry.info, true
}

// Set adds or updates cached worker info (lock-free)
func (c *LockFreeWorkerCache) Set(workerID uint64, info interface{}, ttl time.Duration) {
	entry := &workerCacheEntry{
		info:      info,
		expiresAt: time.Now().Add(ttl).UnixNano(),
	}
	c.cache.Store(workerID, entry)
}

// Delete removes cached worker info (lock-free)
func (c *LockFreeWorkerCache) Delete(workerID uint64) {
	c.cache.Delete(workerID)
}
