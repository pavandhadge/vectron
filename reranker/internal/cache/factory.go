package cache

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
)

// CacheFactory creates cache instances based on configuration
type CacheFactory struct {
	logger internal.Logger
}

// NewCacheFactory creates a new cache factory
func NewCacheFactory(logger internal.Logger) *CacheFactory {
	return &CacheFactory{logger: logger}
}

// CreateCache creates a cache instance based on backend type and configuration
func (f *CacheFactory) CreateCache(backend string) (internal.Cache, error) {
	switch backend {
	case "memory":
		return f.createMemoryCache()
	case "redis":
		return f.createRedisCache()
	default:
		return nil, fmt.Errorf("unsupported cache backend: %s", backend)
	}
}

// createMemoryCache creates a robust in-memory cache
func (f *CacheFactory) createMemoryCache() (internal.Cache, error) {
	config := f.loadCacheConfig()
	
	if f.logger != nil {
		f.logger.Info("Creating robust memory cache", map[string]interface{}{
			"max_size":         config.MaxSize,
			"default_ttl":      config.DefaultTTL.String(),
			"eviction_policy":  f.evictionPolicyName(config.EvictionPolicy),
			"cleanup_interval": config.CleanupInterval.String(),
		})
	}
	
	return NewRobustMemoryCache(config, f.logger), nil
}

// createRedisCache creates a Redis-based cache
func (f *CacheFactory) createRedisCache() (internal.Cache, error) {
	cacheConfig := f.loadCacheConfig()
	redisConfig := f.loadRedisConfig()
	
	if f.logger != nil {
		f.logger.Info("Creating Redis cache", map[string]interface{}{
			"address":     redisConfig.Address,
			"database":    redisConfig.Database,
			"key_prefix":  redisConfig.KeyPrefix,
			"default_ttl": cacheConfig.DefaultTTL.String(),
		})
	}
	
	return NewRedisCache(cacheConfig, redisConfig, f.logger)
}

// loadCacheConfig loads cache configuration from environment variables
func (f *CacheFactory) loadCacheConfig() CacheConfig {
	config := DefaultConfig()
	
	// Load from environment variables
	if maxSize := getEnvInt("CACHE_MAX_SIZE", 0); maxSize > 0 {
		config.MaxSize = maxSize
	}
	
	if ttl := getEnvDuration("CACHE_DEFAULT_TTL", ""); ttl > 0 {
		config.DefaultTTL = ttl
	}
	
	if cleanupInterval := getEnvDuration("CACHE_CLEANUP_INTERVAL", ""); cleanupInterval > 0 {
		config.CleanupInterval = cleanupInterval
	}
	
	if policy := getEnv("CACHE_EVICTION_POLICY", ""); policy != "" {
		switch policy {
		case "lru":
			config.EvictionPolicy = LRU
		case "lfu":
			config.EvictionPolicy = LFU
		case "fifo":
			config.EvictionPolicy = FIFO
		case "ttl":
			config.EvictionPolicy = TTL
		}
	}
	
	if statsEnabled := getEnv("CACHE_STATS_ENABLED", "true"); statsEnabled == "false" {
		config.StatsEnabled = false
	}
	
	if maxMemoryMB := getEnvInt64("CACHE_MAX_MEMORY_MB", 0); maxMemoryMB > 0 {
		config.MaxMemoryMB = maxMemoryMB
	}
	
	if bgCleanup := getEnv("CACHE_BACKGROUND_CLEANUP", "true"); bgCleanup == "false" {
		config.BackgroundCleanup = false
	}
	
	return config
}

// loadRedisConfig loads Redis configuration from environment variables
func (f *CacheFactory) loadRedisConfig() RedisConfig {
	config := DefaultRedisConfig()
	
	if address := getEnv("REDIS_ADDRESS", ""); address != "" {
		config.Address = address
	}
	
	if password := getEnv("REDIS_PASSWORD", ""); password != "" {
		config.Password = password
	}
	
	if database := getEnvInt("REDIS_DATABASE", -1); database >= 0 {
		config.Database = database
	}
	
	if poolSize := getEnvInt("REDIS_POOL_SIZE", 0); poolSize > 0 {
		config.PoolSize = poolSize
	}
	
	if dialTimeout := getEnvDuration("REDIS_DIAL_TIMEOUT", ""); dialTimeout > 0 {
		config.DialTimeout = dialTimeout
	}
	
	if readTimeout := getEnvDuration("REDIS_READ_TIMEOUT", ""); readTimeout > 0 {
		config.ReadTimeout = readTimeout
	}
	
	if writeTimeout := getEnvDuration("REDIS_WRITE_TIMEOUT", ""); writeTimeout > 0 {
		config.WriteTimeout = writeTimeout
	}
	
	if keyPrefix := getEnv("REDIS_KEY_PREFIX", ""); keyPrefix != "" {
		config.KeyPrefix = keyPrefix
	}
	
	return config
}

// Utility functions

func (f *CacheFactory) evictionPolicyName(policy EvictionPolicy) string {
	switch policy {
	case LRU:
		return "LRU"
	case LFU:
		return "LFU"
	case FIFO:
		return "FIFO"
	case TTL:
		return "TTL"
	default:
		return "UNKNOWN"
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue string) time.Duration {
	value := getEnv(key, defaultValue)
	if value == "" {
		return 0
	}
	if duration, err := time.ParseDuration(value); err == nil {
		return duration
	}
	return 0
}