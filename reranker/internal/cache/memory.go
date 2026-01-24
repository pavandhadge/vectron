package cache

import (
	"container/heap"
	"container/list"
	"strings"
	"sync"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

// RobustMemoryCache implements a robust in-memory cache with TTL, eviction policies, and statistics
type RobustMemoryCache struct {
	config    CacheConfig
	data      map[string]*CacheItem
	lruList   *list.List                // For LRU eviction
	lruIndex  map[string]*list.Element  // Quick access to list elements
	lfuHeap   *lfuHeap                  // For LFU eviction
	fifoQueue *list.List                // For FIFO eviction
	stats     CacheStats
	mutex     sync.RWMutex
	stopCh    chan struct{} // For background cleanup
	logger    internal.Logger
}

// NewRobustMemoryCache creates a new robust memory cache
func NewRobustMemoryCache(config CacheConfig, logger internal.Logger) *RobustMemoryCache {
	cache := &RobustMemoryCache{
		config:    config,
		data:      make(map[string]*CacheItem),
		lruList:   list.New(),
		lruIndex:  make(map[string]*list.Element),
		lfuHeap:   &lfuHeap{},
		fifoQueue: list.New(),
		stats:     CacheStats{LastCleanup: time.Now()},
		stopCh:    make(chan struct{}),
		logger:    logger,
	}

	heap.Init(cache.lfuHeap)

	if config.BackgroundCleanup {
		go cache.backgroundCleanup()
	}

	return cache
}

// Get retrieves a cached result by key
func (c *RobustMemoryCache) Get(key string) (*internal.RerankOutput, bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	item, exists := c.data[key]
	if !exists {
		if c.config.StatsEnabled {
			c.stats.Misses++
		}
		return nil, false
	}

	// Check if expired
	if item.IsExpired() {
		c.deleteItemUnsafe(key)
		if c.config.StatsEnabled {
			c.stats.Misses++
			c.stats.ExpiredItems++
		}
		return nil, false
	}

	// Update access patterns for eviction policies
	item.Touch()
	c.updateEvictionPolicy(key, item)

	if c.config.StatsEnabled {
		c.stats.Hits++
	}

	return item.Value, true
}

// Set stores a result in cache with optional TTL
func (c *RobustMemoryCache) Set(key string, value *internal.RerankOutput, ttl time.Duration) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if ttl <= 0 {
		ttl = c.config.DefaultTTL
	}

	now := time.Now()
	item := &CacheItem{
		Value:     value,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
		AccessAt:  now,
		HitCount:  1,
	}

	// Check if we need to evict entries
	if len(c.data) >= c.config.MaxSize {
		c.evictOneEntry()
	}

	// Remove existing entry if it exists
	if _, exists := c.data[key]; exists {
		c.deleteItemUnsafe(key)
	}

	// Add new entry
	c.data[key] = item
	c.addToEvictionPolicy(key, item)

	if c.config.StatsEnabled {
		c.stats.TotalItems = int64(len(c.data))
	}
}

// Delete removes a specific cache entry
func (c *RobustMemoryCache) Delete(key string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.deleteItemUnsafe(key)
}

// Clear removes all cache entries (or matching a pattern)
func (c *RobustMemoryCache) Clear(pattern string) int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if pattern == "" {
		// Clear all
		count := len(c.data)
		c.data = make(map[string]*CacheItem)
		c.lruList = list.New()
		c.lruIndex = make(map[string]*list.Element)
		c.lfuHeap = &lfuHeap{}
		c.fifoQueue = list.New()
		heap.Init(c.lfuHeap)
		c.stats.TotalItems = 0
		return count
	}

	// Pattern matching (simple wildcard support)
	count := 0
	keysToDelete := make([]string, 0)
	for key := range c.data {
		if matchesPattern(key, pattern) {
			keysToDelete = append(keysToDelete, key)
		}
	}

	for _, key := range keysToDelete {
		c.deleteItemUnsafe(key)
		count++
	}

	return count
}

// GetStats returns current cache statistics
func (c *RobustMemoryCache) GetStats() CacheStats {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	stats := c.stats
	stats.CalculateHitRate()
	stats.TotalItems = int64(len(c.data))

	// Calculate average item age
	if len(c.data) > 0 {
		totalAge := time.Duration(0)
		now := time.Now()
		for _, item := range c.data {
			totalAge += now.Sub(item.CreatedAt)
		}
		stats.AverageItemAge = totalAge.Seconds() / float64(len(c.data))
	}

	return stats
}

// Cleanup removes expired entries and optimizes memory usage
func (c *RobustMemoryCache) Cleanup() int {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	return c.cleanupExpiredUnsafe()
}

// Close stops background processes and cleans up resources
func (c *RobustMemoryCache) Close() error {
	if c.stopCh != nil {
		close(c.stopCh)
	}
	return nil
}

// Internal helper methods

func (c *RobustMemoryCache) deleteItemUnsafe(key string) {
	if item, exists := c.data[key]; exists {
		delete(c.data, key)
		c.removeFromEvictionPolicy(key)
		if c.config.StatsEnabled {
			c.stats.TotalItems = int64(len(c.data))
		}
		_ = item // Avoid unused variable warning
	}
}

func (c *RobustMemoryCache) addToEvictionPolicy(key string, item *CacheItem) {
	switch c.config.EvictionPolicy {
	case LRU:
		element := c.lruList.PushFront(key)
		c.lruIndex[key] = element
	case LFU:
		heap.Push(c.lfuHeap, &lfuItem{key: key, frequency: item.HitCount, lastAccess: item.AccessAt})
	case FIFO:
		c.fifoQueue.PushBack(key)
	}
}

func (c *RobustMemoryCache) updateEvictionPolicy(key string, item *CacheItem) {
	switch c.config.EvictionPolicy {
	case LRU:
		if element, exists := c.lruIndex[key]; exists {
			c.lruList.MoveToFront(element)
		}
	case LFU:
		// Update frequency in heap (simplified - in production, use a more efficient heap update)
		c.removeFromEvictionPolicy(key)
		heap.Push(c.lfuHeap, &lfuItem{key: key, frequency: item.HitCount, lastAccess: item.AccessAt})
	}
}

func (c *RobustMemoryCache) removeFromEvictionPolicy(key string) {
	switch c.config.EvictionPolicy {
	case LRU:
		if element, exists := c.lruIndex[key]; exists {
			c.lruList.Remove(element)
			delete(c.lruIndex, key)
		}
	case FIFO:
		// Linear search for FIFO (could be optimized)
		for e := c.fifoQueue.Front(); e != nil; e = e.Next() {
			if e.Value.(string) == key {
				c.fifoQueue.Remove(e)
				break
			}
		}
	}
}

func (c *RobustMemoryCache) evictOneEntry() {
	var keyToEvict string

	switch c.config.EvictionPolicy {
	case LRU:
		if c.lruList.Len() > 0 {
			element := c.lruList.Back()
			keyToEvict = element.Value.(string)
		}
	case LFU:
		if c.lfuHeap.Len() > 0 {
			item := heap.Pop(c.lfuHeap).(*lfuItem)
			keyToEvict = item.key
		}
	case FIFO:
		if c.fifoQueue.Len() > 0 {
			element := c.fifoQueue.Front()
			keyToEvict = element.Value.(string)
		}
	case TTL:
		// Find oldest entry
		var oldestKey string
		var oldestTime time.Time
		for key, item := range c.data {
			if oldestKey == "" || item.CreatedAt.Before(oldestTime) {
				oldestKey = key
				oldestTime = item.CreatedAt
			}
		}
		keyToEvict = oldestKey
	}

	if keyToEvict != "" {
		c.deleteItemUnsafe(keyToEvict)
		if c.config.StatsEnabled {
			c.stats.Evictions++
		}
	}
}

func (c *RobustMemoryCache) cleanupExpiredUnsafe() int {
	now := time.Now()
	expiredKeys := make([]string, 0)

	for key, item := range c.data {
		if now.After(item.ExpiresAt) {
			expiredKeys = append(expiredKeys, key)
		}
	}

	for _, key := range expiredKeys {
		c.deleteItemUnsafe(key)
	}

	if c.config.StatsEnabled {
		c.stats.ExpiredItems += int64(len(expiredKeys))
		c.stats.LastCleanup = now
	}

	return len(expiredKeys)
}

func (c *RobustMemoryCache) backgroundCleanup() {
	ticker := time.NewTicker(c.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			expired := c.Cleanup()
			if expired > 0 && c.logger != nil {
				c.logger.Debug("Cache cleanup completed", map[string]interface{}{
					"expired_items": expired,
				})
			}
		case <-c.stopCh:
			return
		}
	}
}

// Utility functions

func matchesPattern(key, pattern string) bool {
	// Simple wildcard matching (* only)
	if pattern == "*" {
		return true
	}
	if strings.Contains(pattern, "*") {
		parts := strings.Split(pattern, "*")
		if len(parts) == 2 {
			return strings.HasPrefix(key, parts[0]) && strings.HasSuffix(key, parts[1])
		}
	}
	return key == pattern
}

// LFU heap implementation

type lfuItem struct {
	key        string
	frequency  int64
	lastAccess time.Time
	index      int
}

type lfuHeap []*lfuItem

func (h lfuHeap) Len() int { return len(h) }

func (h lfuHeap) Less(i, j int) bool {
	if h[i].frequency == h[j].frequency {
		return h[i].lastAccess.Before(h[j].lastAccess)
	}
	return h[i].frequency < h[j].frequency
}

func (h lfuHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *lfuHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*lfuItem)
	item.index = n
	*h = append(*h, item)
}

func (h *lfuHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}