package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/pavandhadge/vectron/reranker/internal"
)

// RedisCache implements a robust Redis-based cache
type RedisCache struct {
	client    *redis.Client
	config    CacheConfig
	keyPrefix string
	logger    internal.Logger
	stats     CacheStats
}

// RedisConfig holds Redis-specific configuration
type RedisConfig struct {
	Address      string        `json:"address"`
	Password     string        `json:"password"`
	Database     int           `json:"database"`
	PoolSize     int           `json:"pool_size"`
	DialTimeout  time.Duration `json:"dial_timeout"`
	ReadTimeout  time.Duration `json:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout"`
	KeyPrefix    string        `json:"key_prefix"`
}

// DefaultRedisConfig returns sensible defaults for Redis
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Address:      "localhost:6379",
		Password:     "",
		Database:     0,
		PoolSize:     10,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		KeyPrefix:    "reranker:",
	}
}

// NewRedisCache creates a new Redis-based cache
func NewRedisCache(cacheConfig CacheConfig, redisConfig RedisConfig, logger internal.Logger) (*RedisCache, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         redisConfig.Address,
		Password:     redisConfig.Password,
		DB:           redisConfig.Database,
		PoolSize:     redisConfig.PoolSize,
		DialTimeout:  redisConfig.DialTimeout,
		ReadTimeout:  redisConfig.ReadTimeout,
		WriteTimeout: redisConfig.WriteTimeout,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	cache := &RedisCache{
		client:    rdb,
		config:    cacheConfig,
		keyPrefix: redisConfig.KeyPrefix,
		logger:    logger,
		stats:     CacheStats{LastCleanup: time.Now()},
	}

	return cache, nil
}

// Get retrieves a cached result by key
func (c *RedisCache) Get(key string) (*internal.RerankOutput, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Options().ReadTimeout)
	defer cancel()

	fullKey := c.keyPrefix + key
	val, err := c.client.Get(ctx, fullKey).Result()
	if err == redis.Nil {
		if c.config.StatsEnabled {
			c.stats.Misses++
		}
		return nil, false
	}
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Redis GET failed", err, map[string]interface{}{
				"key": key,
			})
		}
		if c.config.StatsEnabled {
			c.stats.Misses++
		}
		return nil, false
	}

	// Deserialize the cached result
	var result internal.RerankOutput
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		if c.logger != nil {
			c.logger.Error("Failed to deserialize cached result", err, map[string]interface{}{
				"key": key,
			})
		}
		if c.config.StatsEnabled {
			c.stats.Misses++
		}
		return nil, false
	}

	if c.config.StatsEnabled {
		c.stats.Hits++
	}

	return &result, true
}

// Set stores a result in cache with optional TTL
func (c *RedisCache) Set(key string, value *internal.RerankOutput, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Options().WriteTimeout)
	defer cancel()

	if ttl <= 0 {
		ttl = c.config.DefaultTTL
	}

	// Serialize the result
	data, err := json.Marshal(value)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Failed to serialize result for caching", err, map[string]interface{}{
				"key": key,
			})
		}
		return
	}

	fullKey := c.keyPrefix + key
	err = c.client.Set(ctx, fullKey, data, ttl).Err()
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Redis SET failed", err, map[string]interface{}{
				"key": key,
				"ttl": ttl.String(),
			})
		}
		return
	}

	if c.config.StatsEnabled {
		c.stats.TotalItems++
	}
}

// Delete removes a specific cache entry
func (c *RedisCache) Delete(key string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Options().WriteTimeout)
	defer cancel()

	fullKey := c.keyPrefix + key
	err := c.client.Del(ctx, fullKey).Err()
	if err != nil && c.logger != nil {
		c.logger.Error("Redis DEL failed", err, map[string]interface{}{
			"key": key,
		})
	}
}

// Clear removes all cache entries (or matching a pattern)
func (c *RedisCache) Clear(pattern string) int {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	searchPattern := c.keyPrefix + "*"
	if pattern != "" {
		searchPattern = c.keyPrefix + pattern
	}

	keys, err := c.client.Keys(ctx, searchPattern).Result()
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Redis KEYS failed", err, map[string]interface{}{
				"pattern": searchPattern,
			})
		}
		return 0
	}

	if len(keys) == 0 {
		return 0
	}

	deleted, err := c.client.Del(ctx, keys...).Result()
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Redis bulk DEL failed", err, map[string]interface{}{
				"keys_count": len(keys),
			})
		}
		return 0
	}

	return int(deleted)
}

// GetStats returns current cache statistics (Redis-specific)
func (c *RedisCache) GetStats() CacheStats {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	info, err := c.client.Info(ctx, "memory", "keyspace").Result()
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Failed to get Redis stats", err, nil)
		}
		stats := c.stats
		stats.CalculateHitRate()
		return stats
	}

	// Parse Redis info for memory usage and key count
	stats := c.stats
	stats.CalculateHitRate()

	// Extract memory usage (simplified parsing)
	lines := strings.Split(info, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "used_memory:") {
			// Parse memory usage - implementation would depend on exact Redis format
			// This is a simplified version
		}
	}

	stats.LastCleanup = time.Now()
	return stats
}

// Cleanup removes expired entries (Redis handles this automatically)
func (c *RedisCache) Cleanup() int {
	// Redis handles TTL expiration automatically
	// We can use this method to collect statistics or perform maintenance
	return 0
}

// Close closes the Redis connection
func (c *RedisCache) Close() error {
	return c.client.Close()
}

// Additional Redis-specific methods

// Exists checks if a key exists in the cache
func (c *RedisCache) Exists(key string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Options().ReadTimeout)
	defer cancel()

	fullKey := c.keyPrefix + key
	exists, err := c.client.Exists(ctx, fullKey).Result()
	if err != nil {
		if c.logger != nil {
			c.logger.Error("Redis EXISTS failed", err, map[string]interface{}{
				"key": key,
			})
		}
		return false
	}

	return exists > 0
}

// SetTTL updates the TTL for an existing key
func (c *RedisCache) SetTTL(key string, ttl time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Options().WriteTimeout)
	defer cancel()

	fullKey := c.keyPrefix + key
	return c.client.Expire(ctx, fullKey, ttl).Err()
}

// GetTTL returns the remaining TTL for a key
func (c *RedisCache) GetTTL(key string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.client.Options().ReadTimeout)
	defer cancel()

	fullKey := c.keyPrefix + key
	return c.client.TTL(ctx, fullKey).Result()
}