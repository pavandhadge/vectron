// Package cache provides robust caching implementations for the reranker service.
package cache

import (
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

// CacheItem represents a single cache entry with TTL and metadata
type CacheItem struct {
	Value     *internal.RerankOutput
	ExpiresAt time.Time
	CreatedAt time.Time
	AccessAt  time.Time
	HitCount  int64
}

// IsExpired checks if the cache item has expired
func (item *CacheItem) IsExpired() bool {
	return time.Now().After(item.ExpiresAt)
}

// Touch updates the access time and hit count
func (item *CacheItem) Touch() {
	item.AccessAt = time.Now()
	item.HitCount++
}

// CacheStats provides cache performance statistics
type CacheStats struct {
	Hits            int64     `json:"hits"`
	Misses          int64     `json:"misses"`
	Evictions       int64     `json:"evictions"`
	ExpiredItems    int64     `json:"expired_items"`
	TotalItems      int64     `json:"total_items"`
	HitRate         float64   `json:"hit_rate"`
	MemoryUsage     int64     `json:"memory_usage_bytes"`
	LastCleanup     time.Time `json:"last_cleanup"`
	AverageItemAge  float64   `json:"average_item_age_seconds"`
}

// CalculateHitRate computes the cache hit rate
func (s *CacheStats) CalculateHitRate() {
	total := s.Hits + s.Misses
	if total > 0 {
		s.HitRate = float64(s.Hits) / float64(total)
	}
}

// EvictionPolicy defines how cache entries should be evicted
type EvictionPolicy int

const (
	LRU EvictionPolicy = iota // Least Recently Used
	LFU                       // Least Frequently Used
	FIFO                      // First In, First Out
	TTL                       // Time To Live only
)

// CacheConfig holds configuration for robust cache
type CacheConfig struct {
	MaxSize           int                `json:"max_size"`           // Maximum number of entries
	DefaultTTL        time.Duration      `json:"default_ttl"`        // Default TTL for entries
	CleanupInterval   time.Duration      `json:"cleanup_interval"`   // How often to clean expired entries
	EvictionPolicy    EvictionPolicy     `json:"eviction_policy"`    // Eviction strategy
	StatsEnabled      bool               `json:"stats_enabled"`      // Enable statistics collection
	MaxMemoryMB       int64              `json:"max_memory_mb"`      // Maximum memory usage in MB
	BackgroundCleanup bool               `json:"background_cleanup"` // Enable background cleanup goroutine
}

// DefaultConfig returns a sensible default cache configuration
func DefaultConfig() CacheConfig {
	return CacheConfig{
		MaxSize:           10000,
		DefaultTTL:        24 * time.Hour,
		CleanupInterval:   5 * time.Minute,
		EvictionPolicy:    LRU,
		StatsEnabled:      true,
		MaxMemoryMB:       100,
		BackgroundCleanup: true,
	}
}