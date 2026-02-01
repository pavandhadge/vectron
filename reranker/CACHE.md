# Reranker Caching Configuration

The reranker service now supports a robust caching system with multiple backends and advanced features.

## Cache Backends

### Memory Cache (Default)
- **Backend**: `memory`
- **Features**: In-memory storage with multiple eviction policies
- **Use Case**: Single-instance deployments, development

### Redis Cache
- **Backend**: `redis`
- **Features**: Distributed caching, persistence, high availability
- **Use Case**: Multi-instance deployments, production

## Configuration

### Environment Variables

#### General Cache Settings
- `CACHE_BACKEND`: Cache backend (`memory` or `redis`) - Default: `memory`
- `CACHE_MAX_SIZE`: Maximum number of cached items - Default: `1000`
- `CACHE_DEFAULT_TTL`: Default TTL for cached items - Default: `15m`
- `CACHE_CLEANUP_INTERVAL`: Cleanup interval for expired items - Default: `5m`
- `CACHE_EVICTION_POLICY`: Eviction policy (`lru`, `lfu`, `fifo`, `ttl`) - Default: `lru`
- `CACHE_STATS_ENABLED`: Enable cache statistics - Default: `true`
- `CACHE_MAX_MEMORY_MB`: Maximum memory usage in MB - Default: `100`
- `CACHE_BACKGROUND_CLEANUP`: Enable background cleanup - Default: `true`

#### Redis-Specific Settings
- `REDIS_ADDRESS`: Redis server address - Default: `localhost:6379`
- `REDIS_PASSWORD`: Redis password - Default: empty
- `REDIS_DATABASE`: Redis database number - Default: `0`
- `REDIS_POOL_SIZE`: Connection pool size - Default: `10`
- `REDIS_DIAL_TIMEOUT`: Connection timeout - Default: `5s`
- `REDIS_READ_TIMEOUT`: Read timeout - Default: `3s`
- `REDIS_WRITE_TIMEOUT`: Write timeout - Default: `3s`
- `REDIS_KEY_PREFIX`: Key prefix for cache entries - Default: `reranker:`

## Usage Examples

### Development (Memory Cache)
```bash
export CACHE_BACKEND=memory
export CACHE_MAX_SIZE=500
export CACHE_DEFAULT_TTL=10m
./reranker --port=50051
```

### Production (Redis Cache)
```bash
export CACHE_BACKEND=redis
export REDIS_ADDRESS=redis.example.com:6379
export REDIS_PASSWORD=secretpassword
export REDIS_DATABASE=1
export CACHE_DEFAULT_TTL=30m
export CACHE_MAX_SIZE=10000
./reranker --port=50051
```

### High-Performance Configuration
```bash
export CACHE_BACKEND=memory
export CACHE_EVICTION_POLICY=lru
export CACHE_MAX_SIZE=5000
export CACHE_MAX_MEMORY_MB=256
export CACHE_CLEANUP_INTERVAL=1m
export CACHE_DEFAULT_TTL=20m
./reranker --port=50051
```

## Eviction Policies

### LRU (Least Recently Used) - Default
- Evicts the least recently accessed items
- Best for temporal locality patterns
- Good general-purpose policy

### LFU (Least Frequently Used)
- Evicts items with the lowest access frequency
- Best for datasets with consistent access patterns
- Higher memory overhead for frequency tracking

### FIFO (First In, First Out)
- Evicts the oldest items first
- Simple and fast
- Good for streaming/sequential access patterns

### TTL (Time To Live)
- Evicts based on expiration time only
- Items may be evicted before capacity limits
- Good for time-sensitive data

## Monitoring

The cache system provides comprehensive statistics:

- **Hit Rate**: Cache hit percentage
- **Total Items**: Current number of cached items
- **Memory Usage**: Approximate memory usage (memory cache only)
- **Evictions**: Number of items evicted due to capacity/TTL
- **Cache Hits/Misses**: Detailed hit/miss counters

Access statistics via cache logging or implement a metrics endpoint.

## Best Practices

### Memory Cache
1. Set appropriate `CACHE_MAX_SIZE` based on available memory
2. Use `CACHE_MAX_MEMORY_MB` to prevent excessive memory usage
3. Choose eviction policy based on access patterns
4. Enable background cleanup for better performance

### Redis Cache
1. Use a dedicated Redis instance for caching
2. Set appropriate connection pool size
3. Monitor Redis memory usage
4. Use Redis clustering for high availability
5. Configure Redis maxmemory and eviction policies

### General
1. Set TTL based on data freshness requirements
2. Monitor hit rates and adjust cache size accordingly
3. Use cache warming for critical queries
4. Consider cache invalidation strategies for dynamic data

## Troubleshooting

### High Memory Usage
- Reduce `CACHE_MAX_SIZE`
- Set lower `CACHE_MAX_MEMORY_MB`
- Decrease `CACHE_DEFAULT_TTL`
- Use more aggressive eviction policy

### Low Hit Rate
- Increase `CACHE_MAX_SIZE`
- Increase `CACHE_DEFAULT_TTL`
- Check query patterns for cache key variations
- Consider cache warming

### Redis Connection Issues
- Verify `REDIS_ADDRESS` and network connectivity
- Check Redis authentication (`REDIS_PASSWORD`)
- Increase connection timeouts
- Monitor Redis server logs

### Performance Issues
- Enable `CACHE_BACKGROUND_CLEANUP`
- Adjust `CACHE_CLEANUP_INTERVAL`
- Use appropriate eviction policy
- Consider Redis for distributed scenarios