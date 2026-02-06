// This file is the main entry point for the API Gateway service.
// It sets up and runs the gRPC server and the HTTP/JSON gateway,
// which exposes the public Vectron API to clients. It handles request
// forwarding to the appropriate backend services (placement driver or workers).

package main

import (
	"container/heap"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"hash/maphash"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	stdruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	runtime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"

	"github.com/pavandhadge/vectron/apigateway/internal/feedback"
	"github.com/pavandhadge/vectron/apigateway/internal/management"
	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	"github.com/pavandhadge/vectron/apigateway/internal/translator"
	pb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	placementpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	reranker "github.com/pavandhadge/vectron/shared/proto/reranker"
	workerpb "github.com/pavandhadge/vectron/shared/proto/worker"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

var cfg = LoadConfig()

var (
	gatewayLogSampleCounter uint64
)

func shouldLogGatewayHotPath() bool {
	if cfg.GatewayDebugLogs {
		return true
	}
	if cfg.GatewayLogSampleEvery <= 1 {
		return true
	}
	return atomic.AddUint64(&gatewayLogSampleCounter, 1)%uint64(cfg.GatewayLogSampleEvery) == 0
}
var cacheHashSeed = maphash.MakeSeed()
var searchResultSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]*pb.SearchResult, 0, 256)
	},
}

func adaptiveConcurrency(multiplier, maxCap int) int {
	procs := stdruntime.GOMAXPROCS(0)
	if procs < 1 {
		procs = 1
	}
	limit := procs * multiplier
	if maxCap > 0 && limit > maxCap {
		limit = maxCap
	}
	if limit < 1 {
		limit = 1
	}
	return limit
}

func totalMemoryBytes() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				return 0
			}
			kb, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0
			}
			return kb * 1024
		}
	}
	return 0
}

func defaultSearchCacheConfig() (time.Duration, int) {
	if mem := totalMemoryBytes(); mem > 0 {
		switch {
		case mem >= 64<<30:
			return 15 * time.Second, 100000
		case mem >= 32<<30:
			return 12 * time.Second, 50000
		case mem >= 16<<30:
			return 10 * time.Second, 20000
		case mem >= 8<<30:
			return 8 * time.Second, 10000
		case mem >= 4<<30:
			return 6 * time.Second, 5000
		default:
			return 5 * time.Second, 2000
		}
	}
	procs := stdruntime.GOMAXPROCS(0)
	switch {
	case procs >= 32:
		return 12 * time.Second, 50000
	case procs >= 16:
		return 10 * time.Second, 20000
	case procs >= 8:
		return 8 * time.Second, 10000
	case procs >= 4:
		return 6 * time.Second, 5000
	default:
		return 5 * time.Second, 2000
	}
}

func defaultResolveCacheSize() int {
	if mem := totalMemoryBytes(); mem > 0 {
		switch {
		case mem >= 64<<30:
			return 40000
		case mem >= 32<<30:
			return 25000
		case mem >= 16<<30:
			return 15000
		case mem >= 8<<30:
			return 10000
		case mem >= 4<<30:
			return 6000
		default:
			return 3000
		}
	}
	procs := stdruntime.GOMAXPROCS(0)
	switch {
	case procs >= 32:
		return 20000
	case procs >= 16:
		return 15000
	case procs >= 8:
		return 10000
	case procs >= 4:
		return 6000
	default:
		return 3000
	}
}

func defaultDistributedCacheTimeout() time.Duration {
	procs := stdruntime.GOMAXPROCS(0)
	switch {
	case procs >= 32:
		return 12 * time.Millisecond
	case procs >= 16:
		return 10 * time.Millisecond
	case procs >= 8:
		return 8 * time.Millisecond
	case procs >= 4:
		return 6 * time.Millisecond
	default:
		return 5 * time.Millisecond
	}
}

const (
	grpcReadBufferSize  = 64 * 1024
	grpcWriteBufferSize = 64 * 1024
	grpcWindowSize      = 1 << 20
	streamUpsertThreshold = 2000
	streamUpsertChunkSize = 512
	routingLeaseTTL       = 20 * time.Second
)

func grpcClientOptions(enableCompression bool) []grpc.DialOption {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                20 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithInitialWindowSize(grpcWindowSize),
		grpc.WithInitialConnWindowSize(grpcWindowSize),
		grpc.WithReadBufferSize(grpcReadBufferSize),
		grpc.WithWriteBufferSize(grpcWriteBufferSize),
	}
	if enableCompression {
		opts = append(opts, grpc.WithDefaultCallOptions(grpc.UseCompressor(gzip.Name)))
	}
	return opts
}

func grpcServerOptions() []grpc.ServerOption {
	return []grpc.ServerOption{
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ReadBufferSize(grpcReadBufferSize),
		grpc.WriteBufferSize(grpcWriteBufferSize),
		grpc.MaxConcurrentStreams(1024),
	}
}

type LeaderInfo struct {
	client placementpb.PlacementServiceClient
	conn   *grpc.ClientConn
}

// SearchCacheEntry holds cached search results with TTL
type SearchCacheEntry struct {
	Response  *pb.SearchResponse
	Timestamp time.Time
}

// SearchCache provides LRU caching for search results with TTL
type SearchCache struct {
	mu      sync.RWMutex
	entries map[uint64]*SearchCacheEntry
	ttl     time.Duration
	maxSize int
}

type DistributedCache struct {
	client  *redis.Client
	ttl     time.Duration
	timeout time.Duration
}

type routingShardCacheEntry struct {
	ShardID      uint64   `json:"shard_id"`
	Start        uint64   `json:"start"`
	End          uint64   `json:"end"`
	ShardEpoch   uint64   `json:"shard_epoch"`
	LeaderAddr   string   `json:"leader_addr"`
	ReplicaAddrs []string `json:"replica_addrs"`
}

type collectionRoutingCacheEntry struct {
	Shards []routingShardCacheEntry `json:"shards"`
}

type resolveCacheEntry struct {
	Addr      string `json:"addr"`
	ShardID   uint64 `json:"shard_id"`
	ShardEpoch uint64 `json:"shard_epoch"`
}

type RerankWarmupCache struct {
	mu      sync.RWMutex
	entries map[uint64]*SearchCacheEntry
	ttl     time.Duration
	maxSize int
}

type WorkerListCacheEntry struct {
	Addresses []string
	Timestamp time.Time
}

type WorkerListCache struct {
	mu      sync.RWMutex
	entries map[string]*WorkerListCacheEntry
	ttl     time.Duration
}

type WorkerResolveCacheEntry struct {
	Addr      string
	ShardID   uint64
	ShardEpoch uint64
	LeaseExpiryUnixMs int64
	Timestamp time.Time
}

type WorkerResolveCache struct {
	mu      sync.RWMutex
	entries map[string]*WorkerResolveCacheEntry
	ttl     time.Duration
	maxSize int
}

type shardBatch struct {
	points            []*pb.Point
	shardEpoch        uint64
	leaseExpiryUnixMs int64
}

type routingShard struct {
	shardID      uint64
	start        uint64
	end          uint64
	shardEpoch   uint64
	leaderAddr   string
	replicaAddrs []string
}

type collectionRoutingEntry struct {
	shards  []routingShard
	expires time.Time
}

type CollectionRoutingCache struct {
	mu      sync.RWMutex
	entries map[string]*collectionRoutingEntry
	ttl     time.Duration
}

func NewCollectionRoutingCache(ttl time.Duration) *CollectionRoutingCache {
	return &CollectionRoutingCache{
		entries: make(map[string]*collectionRoutingEntry),
		ttl:     ttl,
	}
}

func NewDistributedCache(addr, password string, db int, ttl, timeout time.Duration) *DistributedCache {
	if addr == "" {
		return nil
	}
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
		PoolSize: adaptiveConcurrency(4, 64),
	})
	return &DistributedCache{
		client:  client,
		ttl:     ttl,
		timeout: timeout,
	}
}

func (c *DistributedCache) key(prefix, key string) string {
	return "vectron:" + prefix + ":" + key
}

func (c *DistributedCache) GetBytes(ctx context.Context, prefix, key string) ([]byte, bool) {
	if c == nil || c.client == nil {
		return nil, false
	}
	cacheCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	data, err := c.client.Get(cacheCtx, c.key(prefix, key)).Bytes()
	if err != nil {
		return nil, false
	}
	return data, true
}

func (c *DistributedCache) SetBytes(ctx context.Context, prefix, key string, data []byte, ttl time.Duration) {
	if c == nil || c.client == nil || len(data) == 0 {
		return
	}
	if ttl <= 0 {
		ttl = c.ttl
	}
	cacheCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	_ = c.client.Set(cacheCtx, c.key(prefix, key), data, ttl).Err()
}

func (c *DistributedCache) GetSearch(ctx context.Context, key uint64) (*pb.SearchResponse, bool) {
	data, ok := c.GetBytes(ctx, "search", fmt.Sprintf("%x", key))
	if !ok {
		return nil, false
	}
	resp := &pb.SearchResponse{}
	if err := proto.Unmarshal(data, resp); err != nil {
		return nil, false
	}
	return resp, true
}

func (c *DistributedCache) SetSearch(ctx context.Context, key uint64, resp *pb.SearchResponse, ttl time.Duration) {
	if resp == nil {
		return
	}
	data, err := proto.Marshal(resp)
	if err != nil {
		return
	}
	c.SetBytes(ctx, "search", fmt.Sprintf("%x", key), data, ttl)
}

func (c *DistributedCache) GetWorkerList(ctx context.Context, collection string) ([]string, bool) {
	data, ok := c.GetBytes(ctx, "workers", collection)
	if !ok {
		return nil, false
	}
	var addrs []string
	if err := json.Unmarshal(data, &addrs); err != nil {
		return nil, false
	}
	return addrs, true
}

func (c *DistributedCache) SetWorkerList(ctx context.Context, collection string, addrs []string, ttl time.Duration) {
	if len(addrs) == 0 {
		return
	}
	data, err := json.Marshal(addrs)
	if err != nil {
		return
	}
	c.SetBytes(ctx, "workers", collection, data, ttl)
}

func (c *DistributedCache) GetRouting(ctx context.Context, collection string) (*collectionRoutingEntry, bool) {
	data, ok := c.GetBytes(ctx, "routing", collection)
	if !ok {
		return nil, false
	}
	var cached collectionRoutingCacheEntry
	if err := json.Unmarshal(data, &cached); err != nil {
		return nil, false
	}
	entry := &collectionRoutingEntry{
		shards:  make([]routingShard, 0, len(cached.Shards)),
		expires: time.Now().Add(2 * time.Second),
	}
	for _, shard := range cached.Shards {
		entry.shards = append(entry.shards, routingShard{
			shardID:      shard.ShardID,
			start:        shard.Start,
			end:          shard.End,
			shardEpoch:   shard.ShardEpoch,
			leaderAddr:   shard.LeaderAddr,
			replicaAddrs: shard.ReplicaAddrs,
		})
	}
	return entry, true
}

func (c *DistributedCache) SetRouting(ctx context.Context, collection string, entry *collectionRoutingEntry, ttl time.Duration) {
	if entry == nil {
		return
	}
	cached := collectionRoutingCacheEntry{
		Shards: make([]routingShardCacheEntry, 0, len(entry.shards)),
	}
	for _, shard := range entry.shards {
		cached.Shards = append(cached.Shards, routingShardCacheEntry{
			ShardID:      shard.shardID,
			Start:        shard.start,
			End:          shard.end,
			ShardEpoch:   shard.shardEpoch,
			LeaderAddr:   shard.leaderAddr,
			ReplicaAddrs: shard.replicaAddrs,
		})
	}
	data, err := json.Marshal(cached)
	if err != nil {
		return
	}
	c.SetBytes(ctx, "routing", collection, data, ttl)
}

func (c *DistributedCache) GetResolve(ctx context.Context, key string) (resolveCacheEntry, bool) {
	data, ok := c.GetBytes(ctx, "resolve", key)
	if !ok {
		return resolveCacheEntry{}, false
	}
	var entry resolveCacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return resolveCacheEntry{}, false
	}
	return entry, true
}

func (c *DistributedCache) SetResolve(ctx context.Context, key string, entry resolveCacheEntry, ttl time.Duration) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	c.SetBytes(ctx, "resolve", key, data, ttl)
}

func (c *CollectionRoutingCache) Get(collection string) (*collectionRoutingEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[collection]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		return nil, false
	}
	return entry, true
}

func (c *CollectionRoutingCache) Set(collection string, entry *collectionRoutingEntry) {
	c.mu.Lock()
	c.entries[collection] = entry
	c.mu.Unlock()
}

func NewWorkerResolveCache(ttl time.Duration, maxSize int) *WorkerResolveCache {
	return &WorkerResolveCache{
		entries: make(map[string]*WorkerResolveCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

// TinyLFUCache implements a frequency-based cache admission policy
// OPTIMIZATION: Provides better hit rates than LRU by tracking frequency
// Only admits items that are accessed frequently enough to justify cache space
type TinyLFUCache struct {
	mu      sync.RWMutex
	data    map[uint64]*cacheEntry
	freq    map[uint64]int // Access frequency counter
	ttl     time.Duration
	maxSize int
	// Doorkeeper bloom filter for admission
	doorkeeper map[uint64]bool
	doorReset  int // Counter before resetting doorkeeper
}

type cacheEntry struct {
	value     *pb.SearchResponse
	timestamp time.Time
	freq      int
}

func NewTinyLFUCache(ttl time.Duration, maxSize int) *TinyLFUCache {
	return &TinyLFUCache{
		data:       make(map[uint64]*cacheEntry),
		freq:       make(map[uint64]int),
		ttl:        ttl,
		maxSize:    maxSize,
		doorkeeper: make(map[uint64]bool),
		doorReset:  maxSize * 10, // Reset doorkeeper every 10x capacity
	}
}

func (c *TinyLFUCache) Get(key uint64) (*pb.SearchResponse, bool) {
	c.mu.RLock()
	entry, ok := c.data[key]
	c.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if time.Since(entry.timestamp) > c.ttl {
		c.mu.Lock()
		delete(c.data, key)
		delete(c.freq, key)
		c.mu.Unlock()
		return nil, false
	}

	// Increment frequency
	c.mu.Lock()
	c.freq[key]++
	entry.freq = c.freq[key]
	c.mu.Unlock()

	return entry.value, true
}

func (c *TinyLFUCache) Set(key uint64, value *pb.SearchResponse) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already cached
	if _, ok := c.data[key]; ok {
		c.data[key] = &cacheEntry{
			value:     value,
			timestamp: time.Now(),
			freq:      c.freq[key] + 1,
		}
		c.freq[key]++
		return true
	}

	// Check admission via doorkeeper
	if !c.doorkeeper[key] {
		// First access, add to doorkeeper but don't cache yet
		c.doorkeeper[key] = true
		return false
	}

	// Space available
	if len(c.data) < c.maxSize {
		c.data[key] = &cacheEntry{
			value:     value,
			timestamp: time.Now(),
			freq:      1,
		}
		c.freq[key] = 1
		return true
	}

	// Need to evict - find lowest frequency item
	var minKey uint64
	minFreq := int(^uint(0) >> 1) // MaxInt

	for k := range c.data {
		f := c.freq[k]
		if f < minFreq {
			minFreq = f
			minKey = k
		}
	}

	// Only admit if new item has higher frequency
	if c.freq[key] > minFreq {
		delete(c.data, minKey)
		delete(c.freq, minKey)

		c.data[key] = &cacheEntry{
			value:     value,
			timestamp: time.Now(),
			freq:      c.freq[key],
		}
		return true
	}

	// Reset doorkeeper periodically to prevent saturation
	c.doorReset--
	if c.doorReset <= 0 {
		c.doorkeeper = make(map[uint64]bool)
		c.doorReset = c.maxSize * 10
	}

	return false
}

func (c *TinyLFUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

func (c *WorkerResolveCache) Get(collection, vectorID string) (string, uint64, uint64, int64, bool) {
	key := collection + "|" + vectorID
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return "", 0, 0, 0, false
	}
	if time.Since(entry.Timestamp) > c.ttl {
		return "", 0, 0, 0, false
	}
	if entry.LeaseExpiryUnixMs > 0 && time.Now().UnixMilli() > entry.LeaseExpiryUnixMs {
		return "", 0, 0, 0, false
	}
	return entry.Addr, entry.ShardID, entry.ShardEpoch, entry.LeaseExpiryUnixMs, true
}

func (c *WorkerResolveCache) Set(collection, vectorID string, addr string, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) {
	key := collection + "|" + vectorID
	c.mu.Lock()
	if c.maxSize > 0 && len(c.entries) >= c.maxSize {
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}
	c.entries[key] = &WorkerResolveCacheEntry{
		Addr:      addr,
		ShardID:   shardID,
		ShardEpoch: shardEpoch,
		LeaseExpiryUnixMs: leaseExpiryUnixMs,
		Timestamp: time.Now(),
	}
	c.mu.Unlock()
}

func NewWorkerListCache(ttl time.Duration) *WorkerListCache {
	return &WorkerListCache{
		entries: make(map[string]*WorkerListCacheEntry),
		ttl:     ttl,
	}
}

func (c *WorkerListCache) Get(collection string) ([]string, bool) {
	c.mu.RLock()
	entry, ok := c.entries[collection]
	c.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}
	return entry.Addresses, true
}

func (c *WorkerListCache) Set(collection string, addresses []string) {
	c.mu.Lock()
	c.entries[collection] = &WorkerListCacheEntry{
		Addresses: addresses,
		Timestamp: time.Now(),
	}
	c.mu.Unlock()
}

type searchResultHeap []*pb.SearchResult

func (h searchResultHeap) Len() int { return len(h) }
func (h searchResultHeap) Less(i, j int) bool {
	return h[i].Score < h[j].Score
}
func (h searchResultHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *searchResultHeap) Push(x interface{}) {
	*h = append(*h, x.(*pb.SearchResult))
}
func (h *searchResultHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func snapshotTopResults(topResults *searchResultHeap, limit int) *pb.SearchResponse {
	if topResults.Len() == 0 {
		return &pb.SearchResponse{}
	}
	results := make([]*pb.SearchResult, topResults.Len())
	copy(results, *topResults)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return &pb.SearchResponse{Results: results}
}

func equalResultIDs(a, b *pb.SearchResponse) bool {
	if a == nil || b == nil {
		return false
	}
	if len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Results {
		if a.Results[i].Id != b.Results[i].Id {
			return false
		}
	}
	return true
}

// NewSearchCache creates a new search cache with specified TTL and max size
func NewSearchCache(ttl time.Duration, maxSize int) *SearchCache {
	cache := &SearchCache{
		entries: make(map[uint64]*SearchCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
	// Start cleanup goroutine
	go cache.cleanupLoop()
	return cache
}

func NewRerankWarmupCache(ttl time.Duration, maxSize int) *RerankWarmupCache {
	cache := &RerankWarmupCache{
		entries: make(map[uint64]*SearchCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
	go cache.cleanupLoop()
	return cache
}

// computeSearchCacheKey generates a cache key from search request
func computeSearchCacheKey(req *pb.SearchRequest) uint64 {
	// Use collection + topK + vector hash as key (fast hash)
	var h maphash.Hash
	h.SetSeed(cacheHashSeed)
	var buf [4]byte
	for _, v := range req.Vector {
		binary.LittleEndian.PutUint32(buf[:], math.Float32bits(v))
		h.Write(buf[:])
	}
	h.WriteString(req.Collection)
	h.WriteString(req.Query)
	binary.LittleEndian.PutUint32(buf[:], uint32(req.TopK))
	h.Write(buf[:])
	return h.Sum64()
}

func (s *gatewayServer) getSearchCache(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, bool) {
	if !cfg.SearchCacheEnabled || s.searchCache == nil {
		return nil, false
	}
	if cached, found := s.searchCache.Get(req); found {
		return cached, true
	}
	if s.distributedCache == nil || !cfg.DistributedCacheSearchEnabled {
		return nil, false
	}
	key := computeSearchCacheKey(req)
	if cached, found := s.distributedCache.GetSearch(ctx, key); found {
		s.searchCache.Set(req, cached)
		return cached, true
	}
	return nil, false
}

func (s *gatewayServer) setSearchCache(ctx context.Context, req *pb.SearchRequest, resp *pb.SearchResponse) {
	if resp == nil {
		return
	}
	if !cfg.SearchCacheEnabled || s.searchCache == nil {
		return
	}
	s.searchCache.Set(req, resp)
	if s.distributedCache != nil && cfg.DistributedCacheSearchEnabled {
		key := computeSearchCacheKey(req)
		s.distributedCache.SetSearch(ctx, key, resp, 0)
	}
}

// Get retrieves cached results if available and not expired
func (c *SearchCache) Get(req *pb.SearchRequest) (*pb.SearchResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	key := computeSearchCacheKey(req)
	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	// Check if expired
	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry.Response, true
}

// Set stores search results in cache
func (c *SearchCache) Set(req *pb.SearchRequest, resp *pb.SearchResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest entries if cache is full (simple LRU: random eviction)
	if len(c.entries) >= c.maxSize {
		for k := range c.entries {
			delete(c.entries, k)
			break // Remove just one entry
		}
	}

	key := computeSearchCacheKey(req)
	c.entries[key] = &SearchCacheEntry{
		Response:  resp,
		Timestamp: time.Now(),
	}
}

// cleanupLoop periodically removes expired entries
func (c *SearchCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.Sub(entry.Timestamp) > c.ttl {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

func (c *RerankWarmupCache) Get(key uint64) (*pb.SearchResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.entries[key]
	if !exists {
		return nil, false
	}

	if time.Since(entry.Timestamp) > c.ttl {
		return nil, false
	}

	return entry.Response, true
}

func (c *RerankWarmupCache) Set(key uint64, resp *pb.SearchResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) >= c.maxSize {
		for k := range c.entries {
			delete(c.entries, k)
			break
		}
	}

	c.entries[key] = &SearchCacheEntry{
		Response:  resp,
		Timestamp: time.Now(),
	}
}

func (c *RerankWarmupCache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		c.mu.Lock()
		now := time.Now()
		for key, entry := range c.entries {
			if now.Sub(entry.Timestamp) > c.ttl {
				delete(c.entries, key)
			}
		}
		c.mu.Unlock()
	}
}

func (s *gatewayServer) startRerankWarmup(key uint64, req *pb.SearchRequest, candidates []*reranker.Candidate, metadataByID map[string]map[string]string, rerankTopN int, rerankTimeoutMs int) {
	if s.rerankWarmupCache == nil || s.rerankWarmupSem == nil {
		return
	}

	s.rerankWarmupMu.Lock()
	if _, exists := s.rerankWarmupInFlight[key]; exists {
		s.rerankWarmupMu.Unlock()
		return
	}
	s.rerankWarmupInFlight[key] = struct{}{}
	s.rerankWarmupMu.Unlock()

	select {
	case s.rerankWarmupSem <- struct{}{}:
		// proceed
	default:
		s.rerankWarmupMu.Lock()
		delete(s.rerankWarmupInFlight, key)
		s.rerankWarmupMu.Unlock()
		return
	}

	candidatesCopy := make([]*reranker.Candidate, len(candidates))
	copy(candidatesCopy, candidates)

	go func() {
		defer func() {
			<-s.rerankWarmupSem
			s.rerankWarmupMu.Lock()
			delete(s.rerankWarmupInFlight, key)
			s.rerankWarmupMu.Unlock()
		}()

		rerankReq := &reranker.RerankRequest{
			Query:      req.Query,
			Candidates: candidatesCopy,
			TopN:       int32(rerankTopN),
		}

		rerankCtx := context.Background()
		var cancel context.CancelFunc
		if rerankTimeoutMs > 0 {
			rerankCtx, cancel = context.WithTimeout(rerankCtx, time.Duration(rerankTimeoutMs)*time.Millisecond)
		}
		if cancel != nil {
			defer cancel()
		}

		rerankResp, err := s.rerankerClient.Rerank(rerankCtx, rerankReq)
		if err != nil {
			return
		}

		rerankedResults := make([]*pb.SearchResult, len(rerankResp.Results))
		for i, result := range rerankResp.Results {
			var payload map[string]string
			if metadataByID != nil {
				payload = metadataByID[result.Id]
			}
			rerankedResults[i] = &pb.SearchResult{
				Id:      result.Id,
				Score:   result.RerankScore,
				Payload: payload,
			}
		}

		s.rerankWarmupCache.Set(key, &pb.SearchResponse{Results: rerankedResults})
	}()
}

// WorkerConnPool manages gRPC connections to workers with connection reuse
// to avoid the overhead of creating new connections for each request.
// OPTIMIZATION: Added limits and circuit breaker pattern
const (
	defaultMaxConnsPerWorker       = 10
	defaultCircuitBreakerThreshold = 5
	defaultCircuitBreakerTimeout   = 30 * time.Second
)

type circuitState int

const (
	circuitClosed   circuitState = iota // Normal operation
	circuitOpen                         // Failing, reject requests
	circuitHalfOpen                     // Testing if recovered
)

type circuitBreaker struct {
	failures    int
	lastFailure time.Time
	state       circuitState
	mu          sync.RWMutex
	threshold   int
	timeout     time.Duration
}

func newCircuitBreaker(threshold int, timeout time.Duration) *circuitBreaker {
	return &circuitBreaker{
		threshold: threshold,
		timeout:   timeout,
		state:     circuitClosed,
	}
}

func (cb *circuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures = 0
	if cb.state == circuitHalfOpen {
		cb.state = circuitClosed
	}
}

func (cb *circuitBreaker) recordFailure() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.state == circuitHalfOpen {
		cb.state = circuitOpen
		return true
	}

	if cb.failures >= cb.threshold {
		cb.state = circuitOpen
		return true
	}
	return false
}

func (cb *circuitBreaker) allowRequest() bool {
	cb.mu.RLock()
	state := cb.state
	lastFailure := cb.lastFailure
	cb.mu.RUnlock()

	if state == circuitClosed {
		return true
	}

	if state == circuitOpen {
		if time.Since(lastFailure) > cb.timeout {
			cb.mu.Lock()
			cb.state = circuitHalfOpen
			cb.mu.Unlock()
			return true
		}
		return false
	}

	return true // half-open
}

type WorkerConnPool struct {
	mu                     sync.RWMutex
	conns                  map[string]*grpc.ClientConn
	clients                map[string]workerpb.WorkerServiceClient
	connCount              map[string]int // Track connections per worker
	circuitBreakers        map[string]*circuitBreaker
	grpcCompressionEnabled bool
	maxConnsPerWorker      int
	maxTotalConns          int
}

// PDConnPool manages gRPC connections to placement driver nodes.
type PDConnPool struct {
	mu                     sync.RWMutex
	conns                  map[string]*grpc.ClientConn
	clients                map[string]placementpb.PlacementServiceClient
	grpcCompressionEnabled bool
}

func NewPDConnPool(enableCompression bool) *PDConnPool {
	return &PDConnPool{
		conns:                  make(map[string]*grpc.ClientConn),
		clients:                make(map[string]placementpb.PlacementServiceClient),
		grpcCompressionEnabled: enableCompression,
	}
}

func (p *PDConnPool) GetClient(addr string) (placementpb.PlacementServiceClient, *grpc.ClientConn, error) {
	p.mu.RLock()
	if client, ok := p.clients[addr]; ok {
		if conn := p.conns[addr]; conn != nil {
			state := conn.GetState()
			if state != connectivity.Shutdown && state != connectivity.TransientFailure {
				p.mu.RUnlock()
				return client, conn, nil
			}
		}
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if client, ok := p.clients[addr]; ok {
		if conn := p.conns[addr]; conn != nil {
			state := conn.GetState()
			if state != connectivity.Shutdown && state != connectivity.TransientFailure {
				return client, conn, nil
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	opts := append(grpcClientOptions(p.grpcCompressionEnabled), grpc.WithBlock())
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return nil, nil, err
	}
	client := placementpb.NewPlacementServiceClient(conn)
	p.conns[addr] = conn
	p.clients[addr] = client
	return client, conn, nil
}

// GetClient returns a cached gRPC client for the given worker address.
// Creates a new connection if one doesn't exist or if the existing one is unhealthy.
// OPTIMIZATION: Added circuit breaker pattern and connection limits
func (p *WorkerConnPool) GetClient(addr string) (workerpb.WorkerServiceClient, error) {
	// Check circuit breaker first
	cb := p.getCircuitBreaker(addr)
	if !cb.allowRequest() {
		return nil, fmt.Errorf("circuit breaker open for worker %s", addr)
	}

	// Fast path: check for existing connection
	p.mu.RLock()
	if client, ok := p.clients[addr]; ok {
		if conn := p.conns[addr]; conn != nil {
			state := conn.GetState()
			if state != connectivity.Shutdown && state != connectivity.TransientFailure {
				p.mu.RUnlock()
				cb.recordSuccess()
				return client, nil
			}
		}
	}
	p.mu.RUnlock()

	// Slow path: create new connection
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check connection limits
	totalConns := 0
	for _, count := range p.connCount {
		totalConns += count
	}
	if totalConns >= p.maxTotalConns {
		// Try to find an existing healthy connection first
		if client, ok := p.clients[addr]; ok {
			if conn := p.conns[addr]; conn != nil {
				state := conn.GetState()
				if state != connectivity.Shutdown && state != connectivity.TransientFailure {
					return client, nil
				}
			}
		}
		return nil, fmt.Errorf("max total connections reached (%d)", p.maxTotalConns)
	}

	if p.connCount[addr] >= p.maxConnsPerWorker {
		// Limit reached for this worker, return existing if healthy
		if client, ok := p.clients[addr]; ok {
			if conn := p.conns[addr]; conn != nil {
				state := conn.GetState()
				if state != connectivity.Shutdown && state != connectivity.TransientFailure {
					return client, nil
				}
			}
			// Connection unhealthy, close it
			if conn := p.conns[addr]; conn != nil {
				conn.Close()
				p.connCount[addr]--
			}
		}
	}

	// Double-check after acquiring write lock
	if client, ok := p.clients[addr]; ok {
		if conn := p.conns[addr]; conn != nil {
			state := conn.GetState()
			if state != connectivity.Shutdown && state != connectivity.TransientFailure {
				return client, nil
			}
		}
		// Close unhealthy connection
		if conn := p.conns[addr]; conn != nil {
			conn.Close()
			p.connCount[addr]--
		}
	}

	// Create new connection with keepalive
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := append(grpcClientOptions(p.grpcCompressionEnabled), grpc.WithBlock())
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		cb.recordFailure()
		return nil, fmt.Errorf("failed to dial worker %s: %w", addr, err)
	}

	client := workerpb.NewWorkerServiceClient(conn)
	p.conns[addr] = conn
	p.clients[addr] = client
	p.connCount[addr]++
	cb.recordSuccess()
	return client, nil
}

func (p *WorkerConnPool) getCircuitBreaker(addr string) *circuitBreaker {
	p.mu.RLock()
	cb, ok := p.circuitBreakers[addr]
	p.mu.RUnlock()

	if !ok {
		p.mu.Lock()
		cb, ok = p.circuitBreakers[addr]
		if !ok {
			cb = newCircuitBreaker(defaultCircuitBreakerThreshold, defaultCircuitBreakerTimeout)
			p.circuitBreakers[addr] = cb
		}
		p.mu.Unlock()
	}
	return cb
}

// CloseAll closes all pooled connections. Should be called on shutdown.
func (p *WorkerConnPool) CloseAll() {
	p.mu.Lock()
	defer p.mu.Unlock()

	for addr, conn := range p.conns {
		if err := conn.Close(); err != nil {
			log.Printf("Warning: failed to close connection to %s: %v", addr, err)
		}
	}
	p.conns = make(map[string]*grpc.ClientConn)
	p.clients = make(map[string]workerpb.WorkerServiceClient)
	p.connCount = make(map[string]int)
	p.circuitBreakers = make(map[string]*circuitBreaker)
}

// NewWorkerConnPool creates a new worker connection pool with limits.
// OPTIMIZATION: Added connection limits and circuit breaker pattern
func NewWorkerConnPool(enableCompression bool) *WorkerConnPool {
	return &WorkerConnPool{
		conns:                  make(map[string]*grpc.ClientConn),
		clients:                make(map[string]workerpb.WorkerServiceClient),
		connCount:              make(map[string]int),
		circuitBreakers:        make(map[string]*circuitBreaker),
		grpcCompressionEnabled: enableCompression,
		maxConnsPerWorker:      defaultMaxConnsPerWorker,
		maxTotalConns:          defaultMaxConnsPerWorker * 100, // Support up to 100 workers
	}
}

// NewWorkerConnPoolWithLimits creates a pool with custom limits
func NewWorkerConnPoolWithLimits(enableCompression bool, maxPerWorker, maxTotal int) *WorkerConnPool {
	if maxPerWorker <= 0 {
		maxPerWorker = defaultMaxConnsPerWorker
	}
	if maxTotal <= 0 {
		maxTotal = maxPerWorker * 100
	}
	return &WorkerConnPool{
		conns:                  make(map[string]*grpc.ClientConn),
		clients:                make(map[string]workerpb.WorkerServiceClient),
		connCount:              make(map[string]int),
		circuitBreakers:        make(map[string]*circuitBreaker),
		grpcCompressionEnabled: enableCompression,
		maxConnsPerWorker:      maxPerWorker,
		maxTotalConns:          maxTotal,
	}
}

// gatewayServer implements the public gRPC VectronService. It acts as a facade,
// forwarding requests to the appropriate backend services (placement driver or workers).
type gatewayServer struct {
	pb.UnimplementedVectronServiceServer
	pdAddrs                []string
	leader                 *LeaderInfo
	leaderMu               sync.RWMutex
	authClient             authpb.AuthServiceClient
	rerankerClient         reranker.RerankServiceClient
	feedbackService        *feedback.Service
	managementMgr          *management.Manager
	workerPool             *WorkerConnPool // Connection pool for worker gRPC connections
	workerSearchBatcher    *WorkerSearchBatcher
	pdPool                 *PDConnPool
	searchCache            *SearchCache    // Cache for search results
	workerListCache        *WorkerListCache
	resolveCache           *WorkerResolveCache
	routingCache           *CollectionRoutingCache
	distributedCache       *DistributedCache
	grpcCompressionEnabled bool
	rerankWarmupCache      *RerankWarmupCache
	rerankWarmupSem        chan struct{}
	rerankWarmupMu         sync.Mutex
	rerankWarmupInFlight   map[uint64]struct{}
	inFlightMu             sync.Mutex
	inFlightSearches       map[uint64]*inFlightSearch
}

type inFlightSearch struct {
	done chan struct{}
	resp *pb.SearchResponse
	err  error
}

func (s *gatewayServer) getPlacementClient() (placementpb.PlacementServiceClient, error) {
	s.leaderMu.RLock()
	if s.leader != nil && s.leader.client != nil {
		s.leaderMu.RUnlock()
		return s.leader.client, nil
	}
	s.leaderMu.RUnlock()
	return s.updateLeader()
}

// RequestCoalescer batches similar requests to reduce fan-out overhead
// OPTIMIZATION: Reduces network overhead for high-QPS scenarios
type RequestCoalescer struct {
	mu         sync.Mutex
	pending    map[string]*coalesceGroup
	maxWait    time.Duration
	maxBatch   int
	workerPool *WorkerConnPool
}

type coalesceGroup struct {
	requests []*searchRequest
	done     chan struct{}
	result   *pb.SearchResponse
	err      error
}

type searchRequest struct {
	ctx    context.Context
	req    *pb.SearchRequest
	result chan *pb.SearchResult
	err    chan error
}

// WorkerSearchBatcher batches multiple Search requests destined for the same worker.
// It reduces per-request RPC overhead under high concurrency.
type WorkerSearchBatcher struct {
	mu       sync.Mutex
	pending  map[string]*workerSearchGroup
	maxWait  time.Duration
	maxBatch int
}

type workerSearchGroup struct {
	requests []*workerSearchRequest
	done     chan struct{}
}

type workerSearchRequest struct {
	ctx  context.Context
	req  *workerpb.SearchRequest
	resp chan *workerpb.SearchResponse
	err  chan error
}

func NewWorkerSearchBatcher(maxWait time.Duration, maxBatch int) *WorkerSearchBatcher {
	if maxWait <= 0 {
		maxWait = 2 * time.Millisecond
	}
	if maxBatch <= 0 {
		maxBatch = 16
	}
	return &WorkerSearchBatcher{
		pending:  make(map[string]*workerSearchGroup),
		maxWait:  maxWait,
		maxBatch: maxBatch,
	}
}

func (b *WorkerSearchBatcher) Search(ctx context.Context, workerAddr string, client workerpb.WorkerServiceClient, req *workerpb.SearchRequest) (*workerpb.SearchResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	key := workerAddr
	b.mu.Lock()
	group := b.pending[key]
	if group == nil || len(group.requests) >= b.maxBatch {
		group = &workerSearchGroup{
			requests: make([]*workerSearchRequest, 0, b.maxBatch),
			done:     make(chan struct{}),
		}
		b.pending[key] = group
		go b.processBatch(key, group, client)
	}
	reqEntry := &workerSearchRequest{
		ctx:  ctx,
		req:  req,
		resp: make(chan *workerpb.SearchResponse, 1),
		err:  make(chan error, 1),
	}
	group.requests = append(group.requests, reqEntry)
	b.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-reqEntry.resp:
		return resp, nil
	case err := <-reqEntry.err:
		return nil, err
	}
}

func (b *WorkerSearchBatcher) processBatch(key string, group *workerSearchGroup, client workerpb.WorkerServiceClient) {
	timer := time.NewTimer(b.maxWait)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-group.done:
		return
	}

	b.mu.Lock()
	delete(b.pending, key)
	b.mu.Unlock()

	reqs := make([]*workerpb.SearchRequest, 0, len(group.requests))
	indexMap := make([]int, 0, len(group.requests))
	for i, r := range group.requests {
		if r.ctx.Err() != nil {
			r.err <- r.ctx.Err()
			continue
		}
		reqs = append(reqs, r.req)
		indexMap = append(indexMap, i)
	}
	if len(reqs) == 0 {
		close(group.done)
		return
	}

	var (
		ctx    context.Context = context.Background()
		cancel context.CancelFunc
	)
	var earliest time.Time
	for _, r := range group.requests {
		if deadline, ok := r.ctx.Deadline(); ok {
			if earliest.IsZero() || deadline.Before(earliest) {
				earliest = deadline
			}
		}
	}
	if !earliest.IsZero() {
		ctx, cancel = context.WithDeadline(context.Background(), earliest)
		defer cancel()
	}

	resp, err := client.BatchSearch(ctx, &workerpb.BatchSearchRequest{Requests: reqs})
	if err != nil {
		for _, r := range group.requests {
			if r.ctx.Err() == nil {
				r.err <- err
			}
		}
		close(group.done)
		return
	}
	if len(resp.GetResponses()) != len(reqs) {
		err := fmt.Errorf("batch search response size mismatch: got %d want %d", len(resp.GetResponses()), len(reqs))
		for _, r := range group.requests {
			if r.ctx.Err() == nil {
				r.err <- err
			}
		}
		close(group.done)
		return
	}

	for i, res := range resp.GetResponses() {
		target := group.requests[indexMap[i]]
		target.resp <- res
	}
	close(group.done)
}

func NewRequestCoalescer(workerPool *WorkerConnPool, maxWait time.Duration, maxBatch int) *RequestCoalescer {
	if maxWait <= 0 {
		maxWait = 5 * time.Millisecond
	}
	if maxBatch <= 0 {
		maxBatch = 10
	}
	return &RequestCoalescer{
		pending:    make(map[string]*coalesceGroup),
		maxWait:    maxWait,
		maxBatch:   maxBatch,
		workerPool: workerPool,
	}
}

// CoalesceSearch attempts to coalesce a search request with similar pending requests
func (rc *RequestCoalescer) CoalesceSearch(ctx context.Context, collection string, vector []float32, k int32) (*pb.SearchResult, error) {
	// Create a key for request grouping (collection + approximate vector hash)
	key := fmt.Sprintf("%s|%d", collection, hashVector(vector))

	rc.mu.Lock()
	group, exists := rc.pending[key]

	if !exists || len(group.requests) >= rc.maxBatch {
		// Start a new batch
		group = &coalesceGroup{
			requests: make([]*searchRequest, 0, rc.maxBatch),
			done:     make(chan struct{}),
		}
		rc.pending[key] = group

		// Launch the batch processor
		go rc.processBatch(key, group, collection, vector, k)
	}

	// Add our request to the batch
	req := &searchRequest{
		ctx:    ctx,
		result: make(chan *pb.SearchResult, 1),
		err:    make(chan error, 1),
	}
	group.requests = append(group.requests, req)
	rc.mu.Unlock()

	// Wait for the result
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-req.result:
		return result, nil
	case err := <-req.err:
		return nil, err
	}
}

func (rc *RequestCoalescer) processBatch(key string, group *coalesceGroup, collection string, vector []float32, k int32) {
	// Wait for either max batch size or timeout
	timer := time.NewTimer(rc.maxWait)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Timeout reached, process what we have
	case <-group.done:
		// Already processed
		return
	}

	rc.mu.Lock()
	delete(rc.pending, key)
	rc.mu.Unlock()

	// Execute the batched search
	// TODO: Implement actual batched search logic
	// For now, just notify all waiters that they should proceed individually
	for _, req := range group.requests {
		req.err <- fmt.Errorf("batch processing not yet fully implemented")
	}
	close(group.done)
}

func hashVector(vec []float32) uint64 {
	// Simple hash for vector grouping
	var h uint64 = 14695981039346656037 // FNV offset basis
	for i := 0; i < len(vec) && i < 10; i++ {
		h ^= uint64(int64(vec[i] * 1000)) // Approximate hash
		h *= 1099511628211                // FNV prime
	}
	return h
}

func (s *gatewayServer) getCollectionRouting(ctx context.Context, collection string, forceRefresh bool) (*collectionRoutingEntry, error) {
	if !forceRefresh && s.routingCache != nil {
		if entry, ok := s.routingCache.Get(collection); ok {
			return entry, nil
		}
	}
	if !forceRefresh && s.distributedCache != nil {
		if entry, ok := s.distributedCache.GetRouting(ctx, collection); ok {
			ttl := 2 * time.Second
			if s.routingCache != nil && s.routingCache.ttl > 0 {
				ttl = s.routingCache.ttl
			}
			entry.expires = time.Now().Add(ttl)
			if s.routingCache != nil {
				s.routingCache.Set(collection, entry)
			}
			return entry, nil
		}
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	resp, err := placementClient.GetCollectionStatus(ctx, &placementpb.GetCollectionStatusRequest{
		Name: collection,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			resp, err = placementClient.GetCollectionStatus(ctx, &placementpb.GetCollectionStatusRequest{
				Name: collection,
			})
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not get collection status for %q after leader update: %v", collection, err)
			}
		} else {
			return nil, status.Errorf(codes.Internal, "could not get collection status for %q: %v", collection, err)
		}
	}

	ttl := 2 * time.Second
	if s.routingCache != nil && s.routingCache.ttl > 0 {
		ttl = s.routingCache.ttl
	}
	entry := &collectionRoutingEntry{
		shards:  make([]routingShard, 0, len(resp.Shards)),
		expires: time.Now().Add(ttl),
	}
	for _, shard := range resp.Shards {
		if shard == nil {
			continue
		}
		entry.shards = append(entry.shards, routingShard{
			shardID:      uint64(shard.ShardId),
			start:        shard.KeyRangeStart,
			end:          shard.KeyRangeEnd,
			shardEpoch:   shard.ShardEpoch,
			leaderAddr:   shard.LeaderGrpcAddress,
			replicaAddrs: shard.ReplicaGrpcAddresses,
		})
	}

	if s.routingCache != nil {
		s.routingCache.Set(collection, entry)
	}
	if s.distributedCache != nil {
		ttl := 2 * time.Second
		if s.routingCache != nil && s.routingCache.ttl > 0 {
			ttl = s.routingCache.ttl
		}
		s.distributedCache.SetRouting(ctx, collection, entry, ttl)
	}
	return entry, nil
}

func hashVectorID(id string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(id))
	return h.Sum64()
}

func pickShardFromRouting(entry *collectionRoutingEntry, vectorID string) *routingShard {
	if entry == nil || vectorID == "" {
		return nil
	}
	hashValue := hashVectorID(vectorID)
	for i := range entry.shards {
		shard := &entry.shards[i]
		if hashValue >= shard.start && hashValue <= shard.end {
			return shard
		}
	}
	return nil
}

func pickShardFromRoutingKey(entry *collectionRoutingEntry, key uint64) *routingShard {
	if entry == nil {
		return nil
	}
	for i := range entry.shards {
		shard := &entry.shards[i]
		if key >= shard.start && key <= shard.end {
			return shard
		}
	}
	return nil
}

func pickAddressByKey(addrs []string, key uint64) string {
	if len(addrs) == 0 {
		return ""
	}
	idx := int(key % uint64(len(addrs)))
	return addrs[idx]
}

func pickShardAddress(shard *routingShard) string {
	if shard == nil {
		return ""
	}
	if shard.leaderAddr != "" {
		return shard.leaderAddr
	}
	if len(shard.replicaAddrs) > 0 {
		return shard.replicaAddrs[0]
	}
	return ""
}

func pickShardSearchAddress(shard *routingShard, linearizable bool, key uint64) string {
	if shard == nil {
		return ""
	}
	if linearizable {
		return pickShardAddress(shard)
	}
	candidates := make([]string, 0, len(shard.replicaAddrs)+1)
	for _, addr := range shard.replicaAddrs {
		if addr != "" {
			candidates = append(candidates, addr)
		}
	}
	if shard.leaderAddr != "" {
		candidates = append(candidates, shard.leaderAddr)
	}
	if len(candidates) == 0 {
		return ""
	}
	idx := int((key ^ shard.shardID) % uint64(len(candidates)))
	return candidates[idx]
}

func pickFastSearchTarget(entry *collectionRoutingEntry, linearizable bool, key uint64) (string, uint64, uint64) {
	shard := pickShardFromRoutingKey(entry, key)
	if shard == nil {
		return "", 0, 0
	}
	addr := pickShardSearchAddress(shard, linearizable, key)
	if addr == "" {
		return "", 0, 0
	}
	return addr, shard.shardID, shard.shardEpoch
}

func workerAddressesForSearch(entry *collectionRoutingEntry, linearizable bool, key uint64) []string {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{})
	addrs := make([]string, 0)
	for i := range entry.shards {
		shard := &entry.shards[i]
		addr := pickShardSearchAddress(shard, linearizable, key)
		if addr == "" {
			continue
		}
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		addrs = append(addrs, addr)
	}
	return addrs
}

func workerAddressesFromRouting(entry *collectionRoutingEntry) []string {
	if entry == nil {
		return nil
	}
	seen := make(map[string]struct{})
	addrs := make([]string, 0)
	for _, shard := range entry.shards {
		if shard.leaderAddr != "" {
			if _, ok := seen[shard.leaderAddr]; !ok {
				seen[shard.leaderAddr] = struct{}{}
				addrs = append(addrs, shard.leaderAddr)
			}
		}
		for _, addr := range shard.replicaAddrs {
			if addr == "" {
				continue
			}
			if _, ok := seen[addr]; !ok {
				seen[addr] = struct{}{}
				addrs = append(addrs, addr)
			}
		}
	}
	return addrs
}

func (s *gatewayServer) resolveWorker(ctx context.Context, collection string, vectorID string, forceRefresh bool) (string, uint64, uint64, int64, error) {
	if vectorID != "" && !forceRefresh {
		if addr, shardID, shardEpoch, leaseExpiry, ok := s.resolveCache.Get(collection, vectorID); ok {
			return addr, shardID, shardEpoch, leaseExpiry, nil
		}
		if s.distributedCache != nil {
			if entry, ok := s.distributedCache.GetResolve(ctx, collection+"|"+vectorID); ok {
				leaseExpiry := time.Now().Add(routingLeaseTTL).UnixMilli()
				if entry.Addr != "" && entry.ShardID != 0 {
					s.resolveCache.Set(collection, vectorID, entry.Addr, entry.ShardID, entry.ShardEpoch, leaseExpiry)
					return entry.Addr, entry.ShardID, entry.ShardEpoch, leaseExpiry, nil
				}
			}
		}
	}

	if vectorID != "" {
		entry, err := s.getCollectionRouting(ctx, collection, forceRefresh)
		if err == nil && entry != nil {
			if route := pickShardFromRouting(entry, vectorID); route != nil {
				addr := pickShardAddress(route)
				if addr != "" {
					leaseExpiry := time.Now().Add(routingLeaseTTL).UnixMilli()
					s.resolveCache.Set(collection, vectorID, addr, route.shardID, route.shardEpoch, leaseExpiry)
					if s.distributedCache != nil {
						s.distributedCache.SetResolve(ctx, collection+"|"+vectorID, resolveCacheEntry{
							Addr:      addr,
							ShardID:   route.shardID,
							ShardEpoch: route.shardEpoch,
						}, s.resolveCache.ttl)
					}
					return addr, route.shardID, route.shardEpoch, leaseExpiry, nil
				}
			}
		}
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return "", 0, 0, 0, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	resp, err := placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
		VectorId:   vectorID,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return "", 0, 0, 0, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			resp, err = placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
				Collection: collection,
				VectorId:   vectorID,
			})
			if err != nil {
				return "", 0, 0, 0, status.Errorf(codes.Internal, "could not get worker for collection %q after leader update: %v", collection, err)
			}
		} else {
			return "", 0, 0, 0, status.Errorf(codes.Internal, "could not get worker for collection %q: %v", collection, err)
		}
	}
	if resp.GetGrpcAddress() == "" {
		return "", 0, 0, 0, status.Errorf(codes.NotFound, "collection %q not found", collection)
	}
	addr := resp.GetGrpcAddress()
	shardID := uint64(resp.GetShardId())
	shardEpoch := resp.GetShardEpoch()
	leaseExpiry := resp.GetLeaseExpiryUnixMs()
	if vectorID != "" {
		s.resolveCache.Set(collection, vectorID, addr, shardID, shardEpoch, leaseExpiry)
		if s.distributedCache != nil {
			s.distributedCache.SetResolve(ctx, collection+"|"+vectorID, resolveCacheEntry{
				Addr:      addr,
				ShardID:   shardID,
				ShardEpoch: shardEpoch,
			}, s.resolveCache.ttl)
		}
	}
	return addr, shardID, shardEpoch, leaseExpiry, nil
}

func isStaleShardError(err error) bool {
	if err == nil {
		return false
	}
	if st, ok := status.FromError(err); ok {
		return st.Code() == codes.FailedPrecondition
	}
	return false
}

func (s *gatewayServer) UpdateUserProfile(ctx context.Context, req *pb.UpdateUserProfileRequest) (*pb.UpdateUserProfileResponse, error) {
	authReq := &authpb.UpdateUserProfileRequest{
		Plan: req.Plan,
	}

	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Errorf(codes.Unauthenticated, "missing metadata")
	}
	authHeader := md.Get("authorization")
	if len(authHeader) == 0 {
		return nil, status.Errorf(codes.Unauthenticated, "missing authorization header")
	}

	authCtx := metadata.AppendToOutgoingContext(context.Background(), "authorization", authHeader[0])

	authResp, err := s.authClient.UpdateUserProfile(authCtx, authReq)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update user profile: %v", err)
	}

	return &pb.UpdateUserProfileResponse{
		User: authResp.User,
	}, nil
}

func (s *gatewayServer) SubmitFeedback(ctx context.Context, req *pb.SubmitFeedbackRequest) (*pb.SubmitFeedbackResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.FeedbackItems) == 0 {
		return nil, status.Error(codes.InvalidArgument, "feedback items cannot be empty")
	}

	feedbackItems := make([]feedback.FeedbackItem, len(req.FeedbackItems))
	for i, item := range req.FeedbackItems {
		if item.RelevanceScore < 1 || item.RelevanceScore > 5 {
			return nil, status.Error(codes.InvalidArgument, "relevance score must be between 1 and 5")
		}
		feedbackItems[i] = feedback.FeedbackItem{
			ResultID:       item.ResultId,
			RelevanceScore: item.RelevanceScore,
			Clicked:        item.Clicked,
			Position:       item.Position,
			Comment:        item.Comment,
		}
	}

	session := &feedback.FeedbackSession{
		Collection: req.Collection,
		Query:      req.Query,
		Items:      feedbackItems,
		Context:    req.Context,
	}

	if userID, ok := req.Context["user_id"]; ok {
		session.UserID = userID
	}
	if sessionID, ok := req.Context["session_id"]; ok {
		session.SessionID = sessionID
	}

	feedbackID, err := s.feedbackService.StoreFeedback(ctx, session)
	if err != nil {
		log.Printf("Failed to store feedback: %v", err)
		return nil, status.Error(codes.Internal, "failed to store feedback")
	}

	log.Printf("Stored feedback for collection %s: %s", req.Collection, feedbackID)

	return &pb.SubmitFeedbackResponse{
		Success:    true,
		FeedbackId: feedbackID,
	}, nil
}

func (s *gatewayServer) updateLeader() (placementpb.PlacementServiceClient, error) {
	s.leaderMu.Lock()
	defer s.leaderMu.Unlock()

	if s.pdPool == nil {
		s.pdPool = NewPDConnPool(s.grpcCompressionEnabled)
	}

	for _, addr := range s.pdAddrs {
		client, conn, err := s.pdPool.GetClient(addr)
		if err != nil {
			log.Printf("Failed to connect to PD node %s: %v", addr, err)
			continue
		}

		_, err = client.ListCollections(context.Background(), &placementpb.ListCollectionsRequest{})
		if err != nil {
			log.Printf("Failed to get leader from PD node %s: %v", addr, err)
			continue
		}

		log.Println("Connected to new PD leader at", conn.Target())
		s.leader = &LeaderInfo{client: client, conn: conn}
		return client, nil
	}

	return nil, status.Error(codes.Unavailable, "no placement driver leader found")
}

func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64, uint64, int64) (interface{}, error)) (interface{}, error) {
	// Ensure we always have a deadline for raft proposals on workers.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	addr, shardID, shardEpoch, leaseExpiry, err := s.resolveWorker(ctx, collection, vectorID, false)
	if err != nil {
		return nil, err
	}

	// Use connection pool instead of creating new connection each time
	client, err := s.workerPool.GetClient(addr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "worker for collection %q at %s is unreachable: %v", collection, addr, err)
	}

	result, err := call(client, shardID, shardEpoch, leaseExpiry)
	if err == nil || !isStaleShardError(err) {
		return result, err
	}

	addr, shardID, shardEpoch, leaseExpiry, err = s.resolveWorker(ctx, collection, vectorID, true)
	if err != nil {
		return nil, err
	}
	client, err = s.workerPool.GetClient(addr)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "worker for collection %q at %s is unreachable: %v", collection, addr, err)
	}
	return call(client, shardID, shardEpoch, leaseExpiry)
}

func (s *gatewayServer) CreateCollection(ctx context.Context, req *pb.CreateCollectionRequest) (*pb.CreateCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if req.Dimension <= 0 {
		return nil, status.Error(codes.InvalidArgument, "dimension must be a positive number")
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	pdReq := &placementpb.CreateCollectionRequest{
		Name:      req.Name,
		Dimension: req.Dimension,
		Distance:  req.Distance,
	}
	res, err := placementClient.CreateCollection(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.CreateCollection(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &pb.CreateCollectionResponse{Success: res.Success}, nil
}

func (s *gatewayServer) DeleteCollection(ctx context.Context, req *pb.DeleteCollectionRequest) (*pb.DeleteCollectionResponse, error) {
	if req.Name == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	pdReq := &placementpb.DeleteCollectionRequest{
		Name: req.Name,
	}

	res, err := placementClient.DeleteCollection(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.DeleteCollection(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &pb.DeleteCollectionResponse{Success: res.Success}, nil
}

func (s *gatewayServer) Upsert(ctx context.Context, req *pb.UpsertRequest) (*pb.UpsertResponse, error) {
	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.Points) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one point is required for upsert")
	}

	// Validate all points first
	for _, point := range req.Points {
		if point.Id == "" {
			return nil, status.Error(codes.InvalidArgument, "point ID cannot be empty")
		}
		if len(point.Vector) == 0 {
			return nil, status.Error(codes.InvalidArgument, "point vector cannot be empty")
		}
	}

	// Warm the routing cache once to avoid per-point PD lookups.
	var routingEntry *collectionRoutingEntry
	if s.routingCache != nil {
		if entry, err := s.getCollectionRouting(ctx, req.Collection, false); err != nil {
			log.Printf("Routing cache warm failed for collection %q: %v", req.Collection, err)
		} else {
			routingEntry = entry
		}
	}

	// Resolve worker/shard assignments in parallel, then batch per shard.
	var (
		assignMu    sync.Mutex
		assignErr   error
		assignWg    sync.WaitGroup
		semaphore   = make(chan struct{}, adaptiveConcurrency(4, 64))
		assignments = make(map[string]map[uint64]*shardBatch)
	)

	if routingEntry != nil && len(routingEntry.shards) > 0 {
		for _, p := range req.Points {
			route := pickShardFromRouting(routingEntry, p.Id)
			addr := pickShardAddress(route)
			shardID := uint64(0)
			shardEpoch := uint64(0)
			leaseExpiry := time.Now().Add(routingLeaseTTL).UnixMilli()

			if route != nil {
				shardID = route.shardID
				shardEpoch = route.shardEpoch
			}

			if addr == "" || shardID == 0 {
				var err error
				addr, shardID, shardEpoch, leaseExpiry, err = s.resolveWorker(ctx, req.Collection, p.Id, true)
				if err != nil {
					assignErr = err
					break
				}
			}

			shards, ok := assignments[addr]
			if !ok {
				shards = make(map[uint64]*shardBatch)
				assignments[addr] = shards
			}
			batch := shards[shardID]
			if batch == nil {
				batch = &shardBatch{
					points:            make([]*pb.Point, 0, 8),
					shardEpoch:        shardEpoch,
					leaseExpiryUnixMs: leaseExpiry,
				}
				shards[shardID] = batch
			}
			batch.points = append(batch.points, p)
			if shardEpoch > batch.shardEpoch {
				batch.shardEpoch = shardEpoch
			}
			if leaseExpiry > batch.leaseExpiryUnixMs {
				batch.leaseExpiryUnixMs = leaseExpiry
			}
		}
	} else {
		for _, point := range req.Points {
			assignWg.Add(1)
			go func(p *pb.Point) {
				defer assignWg.Done()

				semaphore <- struct{}{}
				defer func() { <-semaphore }()

				addr, shardID, shardEpoch, leaseExpiry, err := s.resolveWorker(ctx, req.Collection, p.Id, false)
				if err != nil {
					assignMu.Lock()
					if assignErr == nil {
						assignErr = err
					}
					assignMu.Unlock()
					return
				}

				assignMu.Lock()
				shards, ok := assignments[addr]
				if !ok {
					shards = make(map[uint64]*shardBatch)
					assignments[addr] = shards
				}
				batch := shards[shardID]
				if batch == nil {
					batch = &shardBatch{
						points:            make([]*pb.Point, 0, 8),
						shardEpoch:        shardEpoch,
						leaseExpiryUnixMs: leaseExpiry,
					}
					shards[shardID] = batch
				}
				batch.points = append(batch.points, p)
				if shardEpoch > batch.shardEpoch {
					batch.shardEpoch = shardEpoch
				}
				if leaseExpiry > batch.leaseExpiryUnixMs {
					batch.leaseExpiryUnixMs = leaseExpiry
				}
				assignMu.Unlock()
			}(point)
		}
	}

	assignWg.Wait()
	if assignErr != nil {
		return nil, assignErr
	}

	var (
		sendErr error
		sendWg  sync.WaitGroup
		sendMu  sync.Mutex
		sendSem = make(chan struct{}, adaptiveConcurrency(4, 64))
	)

	for addr, shards := range assignments {
		for shardID, batch := range shards {
			sendWg.Add(1)
			go func(workerAddr string, sid uint64, batch *shardBatch) {
				defer sendWg.Done()

				sendSem <- struct{}{}
				defer func() { <-sendSem }()

				client, err := s.workerPool.GetClient(workerAddr)
				if err != nil {
					sendMu.Lock()
					if sendErr == nil {
						sendErr = status.Errorf(codes.Internal, "worker %s unreachable: %v", workerAddr, err)
					}
					sendMu.Unlock()
					return
				}

				if len(batch.points) >= streamUpsertThreshold {
					stream, err := client.StreamBatchStoreVector(ctx)
					if err != nil {
						sendMu.Lock()
						if sendErr == nil {
							sendErr = status.Errorf(codes.Internal, "failed to start stream upsert for shard %d: %v", sid, err)
						}
						sendMu.Unlock()
						return
					}
					for i := 0; i < len(batch.points); i += streamUpsertChunkSize {
						end := i + streamUpsertChunkSize
						if end > len(batch.points) {
							end = len(batch.points)
						}
						workerReq := translator.ToWorkerBatchStoreVectorRequestFromPoints(batch.points[i:end], sid, batch.shardEpoch, batch.leaseExpiryUnixMs)
						if err := stream.Send(workerReq); err != nil {
							if isStaleShardError(err) {
								if retryErr := s.retryUpsertWithFreshRouting(ctx, req.Collection, batch.points); retryErr == nil {
									return
								} else {
									err = retryErr
								}
							}
							sendMu.Lock()
							if sendErr == nil {
								sendErr = status.Errorf(codes.Internal, "failed to stream upsert batch for shard %d: %v", sid, err)
							}
							sendMu.Unlock()
							return
						}
					}
					if _, err := stream.CloseAndRecv(); err != nil {
						if isStaleShardError(err) {
							if retryErr := s.retryUpsertWithFreshRouting(ctx, req.Collection, batch.points); retryErr == nil {
								return
							} else {
								err = retryErr
							}
						}
						sendMu.Lock()
						if sendErr == nil {
							sendErr = status.Errorf(codes.Internal, "failed to finalize stream upsert for shard %d: %v", sid, err)
						}
						sendMu.Unlock()
						return
					}
					return
				}

				workerReq := translator.ToWorkerBatchStoreVectorRequestFromPoints(batch.points, sid, batch.shardEpoch, batch.leaseExpiryUnixMs)
				var batchErr error
				for attempt := 0; attempt < 10; attempt++ {
					_, batchErr = client.BatchStoreVector(ctx, workerReq)
					if batchErr == nil {
						break
					}
					if st, ok := status.FromError(batchErr); ok && st.Code() == codes.Unavailable {
						time.Sleep(200 * time.Millisecond)
						continue
					}
					break
				}
				if batchErr != nil && isStaleShardError(batchErr) {
					if err := s.retryUpsertWithFreshRouting(ctx, req.Collection, batch.points); err == nil {
						return
					} else {
						batchErr = err
					}
				}
				if batchErr != nil {
					sendMu.Lock()
					if sendErr == nil {
						sendErr = status.Errorf(codes.Internal, "failed to upsert batch for shard %d: %v", sid, batchErr)
					}
					sendMu.Unlock()
					return
				}
			}(addr, shardID, batch)
		}
	}

	sendWg.Wait()
	if sendErr != nil {
		return nil, sendErr
	}

	return &pb.UpsertResponse{Upserted: int32(len(req.Points))}, nil
}

func (s *gatewayServer) retryUpsertWithFreshRouting(ctx context.Context, collection string, points []*pb.Point) error {
	if len(points) == 0 {
		return nil
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
	}

	assignments := make(map[string]map[uint64]*shardBatch)
	for _, point := range points {
		if point == nil {
			continue
		}
		addr, shardID, shardEpoch, leaseExpiry, err := s.resolveWorker(ctx, collection, point.Id, true)
		if err != nil {
			return err
		}
		shards, ok := assignments[addr]
		if !ok {
			shards = make(map[uint64]*shardBatch)
			assignments[addr] = shards
		}
		batch := shards[shardID]
		if batch == nil {
			batch = &shardBatch{
				points:            make([]*pb.Point, 0, 8),
				shardEpoch:        shardEpoch,
				leaseExpiryUnixMs: leaseExpiry,
			}
			shards[shardID] = batch
		}
		batch.points = append(batch.points, point)
		if shardEpoch > batch.shardEpoch {
			batch.shardEpoch = shardEpoch
		}
		if leaseExpiry > batch.leaseExpiryUnixMs {
			batch.leaseExpiryUnixMs = leaseExpiry
		}
	}

	for addr, shards := range assignments {
		client, err := s.workerPool.GetClient(addr)
		if err != nil {
			return status.Errorf(codes.Internal, "worker %s unreachable: %v", addr, err)
		}
		for shardID, batch := range shards {
			workerReq := translator.ToWorkerBatchStoreVectorRequestFromPoints(batch.points, shardID, batch.shardEpoch, batch.leaseExpiryUnixMs)
			if _, err := client.BatchStoreVector(ctx, workerReq); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *gatewayServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
	logEnabled := shouldLogGatewayHotPath()
	startTotal := time.Now()

	if req.Collection == "" {
		return nil, status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.Vector) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector cannot be empty")
	}

	if req.TopK == 0 {
		req.TopK = 10
	}

	var warmupKey uint64
	warmupKeySet := false
	if cfg.RerankWarmupEnabled && s.rerankWarmupCache != nil {
		warmupKey = computeSearchCacheKey(req)
		warmupKeySet = true
		if cached, found := s.rerankWarmupCache.Get(warmupKey); found {
			if logEnabled {
				log.Printf("search cache_hit=warmup collection=%s topK=%d vecDim=%d total=%s",
					req.Collection, req.TopK, len(req.Vector), time.Since(startTotal))
			}
			s.setSearchCache(ctx, req, cached)
			return cached, nil
		}
	}

	// Check cache for identical queries to avoid redundant computation
	if cached, found := s.getSearchCache(ctx, req); found {
		if logEnabled {
			log.Printf("search cache_hit=search collection=%s topK=%d vecDim=%d total=%s",
				req.Collection, req.TopK, len(req.Vector), time.Since(startTotal))
		}
		return cached, nil
	}

	flightKey := computeSearchCacheKey(req)
	if resp, err, shared := s.waitForInFlight(ctx, flightKey); shared {
		return resp, err
	}
	resp, err := s.searchUncached(ctx, req, warmupKey, warmupKeySet)
	s.finishInFlight(flightKey, resp, err)
	if logEnabled {
		log.Printf("search cache_hit=miss collection=%s topK=%d vecDim=%d total=%s err=%v",
			req.Collection, req.TopK, len(req.Vector), time.Since(startTotal), err)
	}
	return resp, err
}

func (s *gatewayServer) SearchStream(req *pb.SearchRequest, stream pb.VectronService_SearchStreamServer) error {
	if req.Collection == "" {
		return status.Error(codes.InvalidArgument, "collection name cannot be empty")
	}
	if len(req.Vector) == 0 {
		return status.Error(codes.InvalidArgument, "search vector cannot be empty")
	}
	if req.TopK == 0 {
		req.TopK = 10
	}

	if cached, found := s.getSearchCache(stream.Context(), req); found {
		return stream.Send(cached)
	}

	baseResp, metadataByID, err := s.searchStreamUncached(stream.Context(), req, stream)
	if err != nil {
		return err
	}

	allowAll := len(cfg.RerankCollections) == 0
	enabledForCollection := allowAll || cfg.RerankCollections[req.Collection]
	if req.Query == "" || !cfg.RerankEnabled || !enabledForCollection {
		return nil
	}

	candidates := make([]*reranker.Candidate, len(baseResp.Results))
	for i, result := range baseResp.Results {
		metadata := result.Payload
		if metadataByID != nil {
			if m, ok := metadataByID[result.Id]; ok {
				metadata = m
			}
		}
		candidates[i] = &reranker.Candidate{
			Id:       result.Id,
			Score:    result.Score,
			Metadata: metadata,
		}
	}

	rerankTopN := int(req.TopK)
	if v, ok := cfg.RerankTopNOverrides[req.Collection]; ok && v > 0 {
		rerankTopN = v
	}
	rerankTimeoutMs := cfg.RerankTimeoutMs
	if v, ok := cfg.RerankTimeoutOverrides[req.Collection]; ok && v > 0 {
		rerankTimeoutMs = v
	}

	rerankReq := &reranker.RerankRequest{
		Query:      req.Query,
		Candidates: candidates,
		TopN:       int32(rerankTopN),
	}

	rerankCtx := stream.Context()
	var cancel context.CancelFunc
	if rerankTimeoutMs > 0 {
		rerankCtx, cancel = context.WithTimeout(stream.Context(), time.Duration(rerankTimeoutMs)*time.Millisecond)
		defer cancel()
	}
	rerankResp, err := s.rerankerClient.Rerank(rerankCtx, rerankReq)
	if err != nil {
		// If rerank fails, end after streamed base results.
		return nil
	}

	rerankedResults := make([]*pb.SearchResult, len(rerankResp.Results))
	for i, result := range rerankResp.Results {
		var payload map[string]string
		if metadataByID != nil {
			payload = metadataByID[result.Id]
		}
		rerankedResults[i] = &pb.SearchResult{
			Id:      result.Id,
			Score:   result.RerankScore,
			Payload: payload,
		}
	}

	finalResp := &pb.SearchResponse{Results: rerankedResults}
	s.setSearchCache(stream.Context(), req, finalResp)
	return stream.Send(finalResp)
}

func (s *gatewayServer) searchStreamUncached(ctx context.Context, req *pb.SearchRequest, stream pb.VectronService_SearchStreamServer) (*pb.SearchResponse, map[string]map[string]string, error) {
	logEnabled := shouldLogGatewayHotPath()
	var routeDur time.Duration
	linearizable := cfg.SearchLinearizable

	searchCtx := ctx
	var searchCancel context.CancelFunc
	if req.GetTimeoutMs() > 0 {
		searchCtx, searchCancel = context.WithTimeout(ctx, time.Duration(req.GetTimeoutMs())*time.Millisecond)
		defer searchCancel()
	}
	cancelEarly := func() {}
	if searchCancel == nil {
		searchCtx, cancelEarly = context.WithCancel(ctx)
		defer cancelEarly()
	} else {
		cancelEarly = searchCancel
	}
	allowPartial := req.GetTimeoutMs() > 0

	workerTopK := int(req.TopK * 2)
	if workerTopK < int(req.TopK) {
		workerTopK = int(req.TopK)
	}
	capLimit := 100
	if int(req.TopK) > capLimit {
		capLimit = int(req.TopK)
	}
	if workerTopK > capLimit {
		workerTopK = capLimit
	}
	candidateLimit := int(req.TopK * 2)
	if candidateLimit > workerTopK {
		candidateLimit = workerTopK
	}
	if len(req.Query) > 0 && len(req.Query) < 3 {
		shortQueryLimit := int(req.TopK) + int(req.TopK)/2
		if shortQueryLimit < 1 {
			shortQueryLimit = 1
		}
		if shortQueryLimit < candidateLimit {
			candidateLimit = shortQueryLimit
		}
	}

	if v, ok := cfg.SearchConsistencyOverrides[req.Collection]; ok {
		linearizable = v
	}
	routeKey := computeSearchCacheKey(req)

	var (
		workerAddresses  []string
		targetShardID    uint64
		targetShardEpoch uint64
	)
	routeStart := time.Now()
	if routingEntry, err := s.getCollectionRouting(ctx, req.Collection, false); err == nil {
		if cfg.SearchFanoutEnabled {
			workerAddresses = workerAddressesForSearch(routingEntry, linearizable, routeKey)
		} else {
			if addr, shardID, shardEpoch := pickFastSearchTarget(routingEntry, linearizable, routeKey); addr != "" {
				workerAddresses = []string{addr}
				targetShardID = shardID
				targetShardEpoch = shardEpoch
			}
		}
	}
	if logEnabled {
		routeDur = time.Since(routeStart)
	}
	if cachedAddresses, ok := s.workerListCache.Get(req.Collection); ok {
		if len(workerAddresses) == 0 {
			workerAddresses = cachedAddresses
		}
	}
	if len(workerAddresses) == 0 && s.distributedCache != nil {
		if cachedAddresses, ok := s.distributedCache.GetWorkerList(ctx, req.Collection); ok {
			workerAddresses = cachedAddresses
			s.workerListCache.Set(req.Collection, cachedAddresses)
		}
	}
	if len(workerAddresses) == 0 {
		placementClient, err := s.getPlacementClient()
		if err != nil {
			return nil, nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
		}
		workerListResp, err := placementClient.ListWorkersForCollection(ctx, &placementpb.ListWorkersForCollectionRequest{
			Collection: req.Collection,
		})
		if err != nil {
			if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
				placementClient, err = s.updateLeader()
				if err != nil {
					return nil, nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
				}
				workerListResp, err = placementClient.ListWorkersForCollection(ctx, &placementpb.ListWorkersForCollectionRequest{
					Collection: req.Collection,
				})
				if err != nil {
					return nil, nil, status.Errorf(codes.Internal, "could not list workers for collection %q after leader update: %v", req.Collection, err)
				}
			} else {
				return nil, nil, status.Errorf(codes.Internal, "could not list workers for collection %q: %v", req.Collection, err)
			}
		}
		workerAddresses = workerListResp.GetGrpcAddresses()
		s.workerListCache.Set(req.Collection, workerAddresses)
		if s.distributedCache != nil {
			s.distributedCache.SetWorkerList(ctx, req.Collection, workerAddresses, s.workerListCache.ttl)
		}
	}
	if !cfg.SearchFanoutEnabled && len(workerAddresses) > 1 {
		if addr := pickAddressByKey(workerAddresses, routeKey); addr != "" {
			workerAddresses = []string{addr}
		}
	}
	if len(workerAddresses) == 0 {
		empty := &pb.SearchResponse{}
		if err := stream.Send(empty); err != nil {
			return nil, nil, err
		}
		if logEnabled {
			log.Printf("search stream uncached collection=%s topK=%d vecDim=%d workers=0 linearizable=%t route=%s",
				req.Collection, req.TopK, len(req.Vector), linearizable, routeDur)
		}
		return empty, nil, nil
	}

	if len(workerAddresses) == 1 {
		workerAddr := workerAddresses[0]
		workerClient, err := s.workerPool.GetClient(workerAddr)
		if err != nil {
			return nil, nil, status.Errorf(codes.Internal, "worker %s unreachable: %v", workerAddr, err)
		}

		shardID := uint64(0)
		shardEpoch := uint64(0)
		if !cfg.SearchFanoutEnabled && targetShardID != 0 {
			shardID = targetShardID
			shardEpoch = targetShardEpoch
		}
		workerReq := translator.ToWorkerSearchRequest(req, shardID, shardEpoch, 0, linearizable)
		workerReq.K = int32(workerTopK)

		res, err := workerClient.Search(searchCtx, workerReq)
		if err != nil && !cfg.SearchFanoutEnabled && shardID != 0 && isStaleShardError(err) {
			if routingEntry, rerr := s.getCollectionRouting(ctx, req.Collection, true); rerr == nil {
				if addr, sid, epoch := pickFastSearchTarget(routingEntry, linearizable, routeKey); addr != "" {
					workerAddr = addr
					shardID = sid
					shardEpoch = epoch
					if workerAddr != workerAddresses[0] {
						workerClient, err = s.workerPool.GetClient(workerAddr)
						if err != nil {
							return nil, nil, status.Errorf(codes.Internal, "worker %s unreachable: %v", workerAddr, err)
						}
					}
					workerReq = translator.ToWorkerSearchRequest(req, shardID, shardEpoch, 0, linearizable)
					workerReq.K = int32(workerTopK)
					res, err = workerClient.Search(searchCtx, workerReq)
				}
			}
		}
		if err != nil {
			if allowPartial {
				return nil, nil, status.Errorf(codes.DeadlineExceeded, "search timed out before any results were returned: %v", err)
			}
			return nil, nil, status.Errorf(codes.Internal, "worker %s search failed: %v", workerAddr, err)
		}

		topResults := &searchResultHeap{}
		heap.Init(topResults)
		workerSearchResponse := translator.FromWorkerSearchResponse(res)
		for _, r := range workerSearchResponse.Results {
			if candidateLimit <= 0 {
				continue
			}
			if topResults.Len() < candidateLimit {
				heap.Push(topResults, r)
				continue
			}
			if topResults.Len() > 0 && r.Score > (*topResults)[0].Score {
				(*topResults)[0] = r
				heap.Fix(topResults, 0)
			}
		}

		finalSnapshot := snapshotTopResults(topResults, int(req.TopK))
		if err := stream.Send(finalSnapshot); err != nil {
			return nil, nil, err
		}
		s.setSearchCache(ctx, req, finalSnapshot)

		metadataByID := make(map[string]map[string]string, len(finalSnapshot.Results))
		for _, result := range finalSnapshot.Results {
			if result.Payload != nil {
				metadataByID[result.Id] = result.Payload
			}
		}
		return finalSnapshot, metadataByID, nil
	}

	type workerResult struct {
		resp *workerpb.SearchResponse
		err  error
	}

	resultsCh := make(chan workerResult, len(workerAddresses))
	searchSem := make(chan struct{}, adaptiveConcurrency(2, 32))
	useBatcher := s.workerSearchBatcher != nil && len(workerAddresses) > 1
	var wg sync.WaitGroup

	for _, addr := range workerAddresses {
		wg.Add(1)
		go func(workerAddr string) {
			defer wg.Done()
			searchSem <- struct{}{}
			defer func() { <-searchSem }()

			workerClient, err := s.workerPool.GetClient(workerAddr)
			if err != nil {
				resultsCh <- workerResult{err: fmt.Errorf("worker %s unreachable: %v", workerAddr, err)}
				return
			}

			workerReq := translator.ToWorkerSearchRequest(req, 0, 0, 0, linearizable)
			workerReq.K = int32(workerTopK)

			var res *workerpb.SearchResponse
			if useBatcher {
				res, err = s.workerSearchBatcher.Search(searchCtx, workerAddr, workerClient, workerReq)
			} else {
				res, err = workerClient.Search(searchCtx, workerReq)
			}
			if err != nil {
				resultsCh <- workerResult{err: fmt.Errorf("worker %s search failed: %v", workerAddr, err)}
				return
			}
			resultsCh <- workerResult{resp: res}
		}(addr)
	}

	go func() {
		wg.Wait()
		close(resultsCh)
	}()

	topResults := &searchResultHeap{}
	heap.Init(topResults)
	var errs []error
	received := 0
	stableRounds := 0
	var lastSnapshot *pb.SearchResponse
	const earlyStopStableRounds = 3
	const earlyStopMinWorkers = 2

	for result := range resultsCh {
		if result.err != nil {
			errs = append(errs, result.err)
			continue
		}
		received++
		workerSearchResponse := translator.FromWorkerSearchResponse(result.resp)
		for _, r := range workerSearchResponse.Results {
			if candidateLimit <= 0 {
				continue
			}
			if topResults.Len() < candidateLimit {
				heap.Push(topResults, r)
				continue
			}
			if topResults.Len() > 0 && r.Score > (*topResults)[0].Score {
				(*topResults)[0] = r
				heap.Fix(topResults, 0)
			}
		}

		snapshot := snapshotTopResults(topResults, int(req.TopK))
		if err := stream.Send(snapshot); err != nil {
			return nil, nil, err
		}
		if lastSnapshot != nil && equalResultIDs(lastSnapshot, snapshot) {
			stableRounds++
		} else {
			stableRounds = 0
		}
		lastSnapshot = snapshot
		if topResults.Len() >= int(req.TopK) && received >= earlyStopMinWorkers && stableRounds >= earlyStopStableRounds {
			cancelEarly()
			break
		}
	}

	if topResults.Len() == 0 && len(errs) > 0 {
		if allowPartial {
			return nil, nil, status.Errorf(codes.DeadlineExceeded, "search timed out before any results were returned: %v", errs[0])
		}
		return nil, nil, status.Errorf(codes.Internal, "failed to search all workers: %v", errs[0])
	}

	if topResults.Len() > 0 {
		finalSnapshot := snapshotTopResults(topResults, int(req.TopK))
		s.setSearchCache(ctx, req, finalSnapshot)
	}

	finalSnapshot := snapshotTopResults(topResults, int(req.TopK))
	metadataByID := make(map[string]map[string]string, len(finalSnapshot.Results))
	for _, result := range finalSnapshot.Results {
		if result.Payload != nil {
			metadataByID[result.Id] = result.Payload
		}
	}
	return finalSnapshot, metadataByID, nil
}

func (s *gatewayServer) searchUncached(ctx context.Context, req *pb.SearchRequest, warmupKey uint64, warmupKeySet bool) (resp *pb.SearchResponse, err error) {
	logEnabled := shouldLogGatewayHotPath()
	startTotal := time.Now()
	var routeDur, listDur, workerDur, mergeDur time.Duration
	var workerCount int
	linearizable := false
	if logEnabled {
		defer func() {
			log.Printf("search uncached collection=%s topK=%d vecDim=%d workers=%d linearizable=%t route=%s list=%s worker=%s merge=%s total=%s err=%v",
				req.Collection, req.TopK, len(req.Vector), workerCount, linearizable, routeDur, listDur, workerDur, mergeDur, time.Since(startTotal), err)
		}()
	}
	searchCtx := ctx
	var searchCancel context.CancelFunc
	if req.GetTimeoutMs() > 0 {
		searchCtx, searchCancel = context.WithTimeout(ctx, time.Duration(req.GetTimeoutMs())*time.Millisecond)
		defer searchCancel()
	}
	allowPartial := req.GetTimeoutMs() > 0

	workerTopK := int(req.TopK * 2)
	if workerTopK < int(req.TopK) {
		workerTopK = int(req.TopK)
	}
	capLimit := 100
	if int(req.TopK) > capLimit {
		capLimit = int(req.TopK)
	}
	if workerTopK > capLimit {
		workerTopK = capLimit
	}
	candidateLimit := int(req.TopK * 2)
	if candidateLimit > workerTopK {
		candidateLimit = workerTopK
	}
	if len(req.Query) > 0 && len(req.Query) < 3 {
		shortQueryLimit := int(req.TopK) + int(req.TopK)/2
		if shortQueryLimit < 1 {
			shortQueryLimit = 1
		}
		if shortQueryLimit < candidateLimit {
			candidateLimit = shortQueryLimit
		}
	}
	// If reranking won't run, don't overfetch.
	allowAll := len(cfg.RerankCollections) == 0
	enabledForCollection := allowAll || cfg.RerankCollections[req.Collection]
	if req.Query == "" || !cfg.RerankEnabled || !enabledForCollection {
		workerTopK = int(req.TopK)
		candidateLimit = workerTopK
	}

	linearizable = cfg.SearchLinearizable
	if v, ok := cfg.SearchConsistencyOverrides[req.Collection]; ok {
		linearizable = v
	}
	routeKey := warmupKey
	if !warmupKeySet {
		routeKey = computeSearchCacheKey(req)
	}

	var (
		workerAddresses  []string
		targetShardID    uint64
		targetShardEpoch uint64
	)
	if routingEntry, err := s.getCollectionRouting(ctx, req.Collection, false); err == nil {
		if cfg.SearchFanoutEnabled {
			workerAddresses = workerAddressesForSearch(routingEntry, linearizable, routeKey)
		} else {
			if addr, shardID, shardEpoch := pickFastSearchTarget(routingEntry, linearizable, routeKey); addr != "" {
				workerAddresses = []string{addr}
				targetShardID = shardID
				targetShardEpoch = shardEpoch
			}
		}
	}
	if cachedAddresses, ok := s.workerListCache.Get(req.Collection); ok {
		if len(workerAddresses) == 0 {
			workerAddresses = cachedAddresses
		}
	}
	if len(workerAddresses) == 0 && s.distributedCache != nil {
		if cachedAddresses, ok := s.distributedCache.GetWorkerList(ctx, req.Collection); ok {
			workerAddresses = cachedAddresses
			s.workerListCache.Set(req.Collection, cachedAddresses)
		}
	}
	if len(workerAddresses) == 0 {
		placementClient, err := s.getPlacementClient()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
		}
		listStart := time.Now()
		workerListResp, err := placementClient.ListWorkersForCollection(ctx, &placementpb.ListWorkersForCollectionRequest{
			Collection: req.Collection,
		})
		if err != nil {
			// Attempt to update leader and retry if the placement driver client is stale
			if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
				placementClient, err = s.updateLeader()
				if err != nil {
					return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
				}
				workerListResp, err = placementClient.ListWorkersForCollection(ctx, &placementpb.ListWorkersForCollectionRequest{
					Collection: req.Collection,
				})
				if err != nil {
					return nil, status.Errorf(codes.Internal, "could not list workers for collection %q after leader update: %v", req.Collection, err)
				}
			} else {
				return nil, status.Errorf(codes.Internal, "could not list workers for collection %q: %v", req.Collection, err)
			}
		}
		if logEnabled {
			listDur = time.Since(listStart)
		}
		workerAddresses = workerListResp.GetGrpcAddresses()
		s.workerListCache.Set(req.Collection, workerAddresses)
		if s.distributedCache != nil {
			s.distributedCache.SetWorkerList(ctx, req.Collection, workerAddresses, s.workerListCache.ttl)
		}
	}
	if !cfg.SearchFanoutEnabled && len(workerAddresses) > 1 {
		if addr := pickAddressByKey(workerAddresses, routeKey); addr != "" {
			workerAddresses = []string{addr}
		}
	}
	if len(workerAddresses) == 0 {
		return &pb.SearchResponse{}, nil // No workers, no results
	}
	workerCount = len(workerAddresses)

	var searchResponse *pb.SearchResponse
	var results []*pb.SearchResult

	if len(workerAddresses) == 1 {
		workerAddr := workerAddresses[0]
		workerClient, err := s.workerPool.GetClient(workerAddr)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "worker %s unreachable: %v", workerAddr, err)
		}

		shardID := uint64(0)
		shardEpoch := uint64(0)
		if !cfg.SearchFanoutEnabled && targetShardID != 0 {
			shardID = targetShardID
			shardEpoch = targetShardEpoch
		}
		workerReq := translator.ToWorkerSearchRequest(req, shardID, shardEpoch, 0, linearizable) // ShardID 0 for broadcast search
		workerReq.K = int32(workerTopK)

		workerStart := time.Now()
		res, err := workerClient.Search(searchCtx, workerReq)
		if err != nil && !cfg.SearchFanoutEnabled && shardID != 0 && isStaleShardError(err) {
			if routingEntry, rerr := s.getCollectionRouting(ctx, req.Collection, true); rerr == nil {
				if addr, sid, epoch := pickFastSearchTarget(routingEntry, linearizable, routeKey); addr != "" {
					workerAddr = addr
					shardID = sid
					shardEpoch = epoch
					if workerAddr != workerAddresses[0] {
						workerClient, err = s.workerPool.GetClient(workerAddr)
						if err != nil {
							return nil, status.Errorf(codes.Internal, "worker %s unreachable: %v", workerAddr, err)
						}
					}
					workerReq = translator.ToWorkerSearchRequest(req, shardID, shardEpoch, 0, linearizable)
					workerReq.K = int32(workerTopK)
					res, err = workerClient.Search(searchCtx, workerReq)
				}
			}
		}
		if err != nil {
			if !allowPartial {
				return nil, status.Errorf(codes.Internal, "worker %s search failed: %v", workerAddr, err)
			}
			return nil, status.Errorf(codes.DeadlineExceeded, "search timed out before any results were returned: %v", err)
		}
		if logEnabled {
			workerDur = time.Since(workerStart)
		}

		mergeStart := time.Now()
		topResults := &searchResultHeap{}
		heap.Init(topResults)
		workerSearchResponse := translator.FromWorkerSearchResponse(res)
		for _, result := range workerSearchResponse.Results {
			if candidateLimit <= 0 {
				continue
			}
			if topResults.Len() < candidateLimit {
				heap.Push(topResults, result)
				continue
			}
			if topResults.Len() > 0 && result.Score > (*topResults)[0].Score {
				(*topResults)[0] = result
				heap.Fix(topResults, 0)
			}
		}

		results = searchResultSlicePool.Get().([]*pb.SearchResult)
		results = results[:topResults.Len()]
		for i := len(results) - 1; i >= 0; i-- {
			results[i] = heap.Pop(topResults).(*pb.SearchResult)
		}
		searchResponse = &pb.SearchResponse{Results: results}
		if logEnabled {
			mergeDur = time.Since(mergeStart)
		}

		if len(searchResponse.Results) == 0 {
			searchResultSlicePool.Put(results[:0])
			return searchResponse, nil
		}

		// Inline finalize block to avoid goto across declarations.
		if candidateLimit > 0 && len(searchResponse.Results) > candidateLimit {
			sort.Slice(searchResponse.Results, func(i, j int) bool {
				return searchResponse.Results[i].Score > searchResponse.Results[j].Score
			})
			searchResponse.Results = searchResponse.Results[:candidateLimit]
		}

		allowAll = len(cfg.RerankCollections) == 0
		enabledForCollection = allowAll || cfg.RerankCollections[req.Collection]
		if req.Query == "" || !cfg.RerankEnabled || !enabledForCollection {
			sort.Slice(searchResponse.Results, func(i, j int) bool {
				return searchResponse.Results[i].Score > searchResponse.Results[j].Score
			})
			if len(searchResponse.Results) > int(req.TopK) {
				searchResponse.Results = searchResponse.Results[:req.TopK]
			}
			s.setSearchCache(ctx, req, searchResponse)
			searchResultSlicePool.Put(results[:0])
			return searchResponse, nil
		}

		candidates := make([]*reranker.Candidate, len(searchResponse.Results))
		var metadataByID map[string]map[string]string
		for i, result := range searchResponse.Results {
			metadata := result.Payload
			if metadata != nil {
				if metadataByID == nil {
					metadataByID = make(map[string]map[string]string, len(searchResponse.Results))
				}
				metadataByID[result.Id] = metadata
			}

			candidates[i] = &reranker.Candidate{
				Id:       result.Id,
				Score:    result.Score,
				Metadata: metadata,
			}
		}

		rerankTopN := int(req.TopK)
		if v, ok := cfg.RerankTopNOverrides[req.Collection]; ok && v > 0 {
			rerankTopN = v
		}

		rerankTimeoutMs := cfg.RerankTimeoutMs
		if v, ok := cfg.RerankTimeoutOverrides[req.Collection]; ok && v > 0 {
			rerankTimeoutMs = v
		}

		if cfg.RerankWarmupEnabled && s.rerankWarmupCache != nil {
			if !warmupKeySet {
				warmupKey = computeSearchCacheKey(req)
				warmupKeySet = true
			}
			s.startRerankWarmup(warmupKey, req, candidates, metadataByID, rerankTopN, rerankTimeoutMs)

			sort.Slice(searchResponse.Results, func(i, j int) bool {
				return searchResponse.Results[i].Score > searchResponse.Results[j].Score
			})
			if len(searchResponse.Results) > int(req.TopK) {
				searchResponse.Results = searchResponse.Results[:req.TopK]
			}
			s.setSearchCache(ctx, req, searchResponse)
			searchResultSlicePool.Put(results[:0])
			return searchResponse, nil
		}

		rerankReq := &reranker.RerankRequest{
			Query:      req.Query,
			Candidates: candidates,
			TopN:       int32(rerankTopN),
		}

		rerankCtx := ctx
		var cancel context.CancelFunc
		if rerankTimeoutMs > 0 {
			rerankCtx, cancel = context.WithTimeout(ctx, time.Duration(rerankTimeoutMs)*time.Millisecond)
			defer cancel()
		}
		rerankResp, err := s.rerankerClient.Rerank(rerankCtx, rerankReq)
		if err != nil {
			log.Println("Reranking failed, returning original results:", err)
			sort.Slice(searchResponse.Results, func(i, j int) bool {
				return searchResponse.Results[i].Score > searchResponse.Results[j].Score
			})
			if len(searchResponse.Results) > int(req.TopK) {
				searchResponse.Results = searchResponse.Results[:req.TopK]
			}
			s.setSearchCache(ctx, req, searchResponse)
			searchResultSlicePool.Put(results[:0])
			return searchResponse, nil
		}

		rerankedResults := make([]*pb.SearchResult, len(rerankResp.Results))
		for i, result := range rerankResp.Results {
			var payload map[string]string
			if metadataByID != nil {
				payload = metadataByID[result.Id]
			}

			rerankedResults[i] = &pb.SearchResult{
				Id:      result.Id,
				Score:   result.RerankScore,
				Payload: payload,
			}
		}

		finalResponse := &pb.SearchResponse{
			Results: rerankedResults,
		}
		s.setSearchCache(ctx, req, finalResponse)
		searchResultSlicePool.Put(results[:0])
		return finalResponse, nil
	}

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		topResults = &searchResultHeap{}
		errs       []error
	)
	heap.Init(topResults)

	searchSem := make(chan struct{}, adaptiveConcurrency(2, 32))
	useBatcher := s.workerSearchBatcher != nil && len(workerAddresses) > 1
	workerStart := time.Now()
	for _, addr := range workerAddresses {
		wg.Add(1)
		go func(workerAddr string) {
			defer wg.Done()

			searchSem <- struct{}{}
			defer func() { <-searchSem }()

			workerClient, err := s.workerPool.GetClient(workerAddr)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("worker %s unreachable: %v", workerAddr, err))
				mu.Unlock()
				return
			}

			workerReq := translator.ToWorkerSearchRequest(req, 0, 0, 0, linearizable) // ShardID 0 for broadcast search
			workerReq.K = int32(workerTopK)

			var res *workerpb.SearchResponse
			if useBatcher {
				res, err = s.workerSearchBatcher.Search(searchCtx, workerAddr, workerClient, workerReq)
			} else {
				res, err = workerClient.Search(searchCtx, workerReq)
			}
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("worker %s search failed: %v", workerAddr, err))
				mu.Unlock()
				return
			}

			workerSearchResponse := translator.FromWorkerSearchResponse(res)
			mu.Lock()
			for _, result := range workerSearchResponse.Results {
				if candidateLimit <= 0 {
					continue
				}
				if topResults.Len() < candidateLimit {
					heap.Push(topResults, result)
					continue
				}
				if topResults.Len() > 0 && result.Score > (*topResults)[0].Score {
					(*topResults)[0] = result
					heap.Fix(topResults, 0)
				}
			}
			mu.Unlock()
		}(addr)
	}
	wg.Wait()
	if logEnabled {
		workerDur = time.Since(workerStart)
	}

	if len(errs) > 0 && topResults.Len() == 0 {
		// If partial results aren't allowed or we have nothing, return error.
		if !allowPartial {
			return nil, status.Errorf(codes.Internal, "failed to search all workers: %v", errs[0])
		}
		return nil, status.Errorf(codes.DeadlineExceeded, "search timed out before any results were returned: %v", errs[0])
	}

	if len(errs) > 0 && !allowPartial {
		return nil, status.Errorf(codes.Internal, "failed to search all workers: %v", errs[0])
	}

	mergeStart := time.Now()
	results = searchResultSlicePool.Get().([]*pb.SearchResult)
	results = results[:topResults.Len()]
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(topResults).(*pb.SearchResult)
	}
	searchResponse = &pb.SearchResponse{Results: results}
	if logEnabled {
		mergeDur = time.Since(mergeStart)
	}

	if len(searchResponse.Results) == 0 {
		searchResultSlicePool.Put(results[:0])
		return searchResponse, nil
	}

	// Limit candidates before reranking to reduce latency and CPU usage.
	// Keep 2x TopK (capped by workerTopK) to preserve quality while shrinking work.
	if candidateLimit > 0 && len(searchResponse.Results) > candidateLimit {
		sort.Slice(searchResponse.Results, func(i, j int) bool {
			return searchResponse.Results[i].Score > searchResponse.Results[j].Score
		})
		searchResponse.Results = searchResponse.Results[:candidateLimit]
	}

	// Standard practice: skip reranking when no query text is provided.
	// Also skip unless reranking is explicitly enabled.
	allowAll = len(cfg.RerankCollections) == 0
	enabledForCollection = allowAll || cfg.RerankCollections[req.Collection]
	if req.Query == "" || !cfg.RerankEnabled || !enabledForCollection {
		sort.Slice(searchResponse.Results, func(i, j int) bool {
			return searchResponse.Results[i].Score > searchResponse.Results[j].Score
		})
		if len(searchResponse.Results) > int(req.TopK) {
			searchResponse.Results = searchResponse.Results[:req.TopK]
		}
		s.setSearchCache(ctx, req, searchResponse)
		searchResultSlicePool.Put(results[:0])
		return searchResponse, nil
	}

	candidates := make([]*reranker.Candidate, len(searchResponse.Results))
	var metadataByID map[string]map[string]string
	for i, result := range searchResponse.Results {
		metadata := result.Payload
		if metadata != nil {
			if metadataByID == nil {
				metadataByID = make(map[string]map[string]string, len(searchResponse.Results))
			}
			metadataByID[result.Id] = metadata
		}

		candidates[i] = &reranker.Candidate{
			Id:       result.Id,
			Score:    result.Score,
			Metadata: metadata,
		}
	}

	rerankTopN := int(req.TopK)
	if v, ok := cfg.RerankTopNOverrides[req.Collection]; ok && v > 0 {
		rerankTopN = v
	}

	rerankTimeoutMs := cfg.RerankTimeoutMs
	if v, ok := cfg.RerankTimeoutOverrides[req.Collection]; ok && v > 0 {
		rerankTimeoutMs = v
	}

	if cfg.RerankWarmupEnabled && s.rerankWarmupCache != nil {
		if !warmupKeySet {
			warmupKey = computeSearchCacheKey(req)
			warmupKeySet = true
		}
		s.startRerankWarmup(warmupKey, req, candidates, metadataByID, rerankTopN, rerankTimeoutMs)

		sort.Slice(searchResponse.Results, func(i, j int) bool {
			return searchResponse.Results[i].Score > searchResponse.Results[j].Score
		})
		if len(searchResponse.Results) > int(req.TopK) {
			searchResponse.Results = searchResponse.Results[:req.TopK]
		}
		s.setSearchCache(ctx, req, searchResponse)
		searchResultSlicePool.Put(results[:0])
		return searchResponse, nil
	}

	rerankReq := &reranker.RerankRequest{
		Query:      req.Query, // Pass the query from the API Gateway's SearchRequest
		Candidates: candidates,
		TopN:       int32(rerankTopN),
	}

	rerankCtx := ctx
	var cancel context.CancelFunc
	if rerankTimeoutMs > 0 {
		rerankCtx, cancel = context.WithTimeout(ctx, time.Duration(rerankTimeoutMs)*time.Millisecond)
		defer cancel()
	}
	rerankResp, err := s.rerankerClient.Rerank(rerankCtx, rerankReq)
	if err != nil {
		log.Println("Reranking failed, returning original results:", err)
		// Sort original results by score in descending order before returning
		sort.Slice(searchResponse.Results, func(i, j int) bool {
			return searchResponse.Results[i].Score > searchResponse.Results[j].Score
		})
		// Then truncate to TopK
		if len(searchResponse.Results) > int(req.TopK) {
			searchResponse.Results = searchResponse.Results[:req.TopK]
		}
		// Cache the results before returning
		s.setSearchCache(ctx, req, searchResponse)
		searchResultSlicePool.Put(results[:0])
		return searchResponse, nil
	}

	rerankedResults := make([]*pb.SearchResult, len(rerankResp.Results))
	for i, result := range rerankResp.Results {
		var payload map[string]string
		if metadataByID != nil {
			payload = metadataByID[result.Id]
		}

		rerankedResults[i] = &pb.SearchResult{
			Id:      result.Id,
			Score:   result.RerankScore,
			Payload: payload,
		}
	}

	finalResponse := &pb.SearchResponse{
		Results: rerankedResults,
	}
	// Cache the results before returning
	s.setSearchCache(ctx, req, finalResponse)
	searchResultSlicePool.Put(results[:0])
	return finalResponse, nil
}

func (s *gatewayServer) waitForInFlight(ctx context.Context, key uint64) (*pb.SearchResponse, error, bool) {
	s.inFlightMu.Lock()
	if flight, ok := s.inFlightSearches[key]; ok {
		s.inFlightMu.Unlock()
		select {
		case <-flight.done:
			return flight.resp, flight.err, true
		case <-ctx.Done():
			return nil, ctx.Err(), true
		}
	}
	flight := &inFlightSearch{done: make(chan struct{})}
	s.inFlightSearches[key] = flight
	s.inFlightMu.Unlock()
	return nil, nil, false
}

func (s *gatewayServer) finishInFlight(key uint64, resp *pb.SearchResponse, err error) {
	s.inFlightMu.Lock()
	flight, ok := s.inFlightSearches[key]
	if ok {
		delete(s.inFlightSearches, key)
	}
	s.inFlightMu.Unlock()
	if ok {
		flight.resp = resp
		flight.err = err
		close(flight.done)
	}
}

func (s *gatewayServer) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	result, err := s.forwardToWorker(ctx, req.Collection, req.Id, func(c workerpb.WorkerServiceClient, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) (interface{}, error) {
		workerReq := translator.ToWorkerGetVectorRequest(req, shardID, shardEpoch, leaseExpiryUnixMs)
		res, err := c.GetVector(ctx, workerReq)
		if err != nil {
			return nil, err
		}
		return translator.FromWorkerGetVectorResponse(res), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*pb.GetResponse), nil
}

func (s *gatewayServer) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	result, err := s.forwardToWorker(ctx, req.Collection, req.Id, func(c workerpb.WorkerServiceClient, shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) (interface{}, error) {
		workerReq := translator.ToWorkerDeleteVectorRequest(req, shardID, shardEpoch, leaseExpiryUnixMs)
		res, err := c.DeleteVector(ctx, workerReq)
		if err != nil {
			return nil, err
		}
		return translator.FromWorkerDeleteVectorResponse(res), nil
	})
	if err != nil {
		return nil, err
	}
	return result.(*pb.DeleteResponse), nil
}

func (s *gatewayServer) ListCollections(ctx context.Context, req *pb.ListCollectionsRequest) (*pb.ListCollectionsResponse, error) {
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}
	pdReq := &placementpb.ListCollectionsRequest{}
	res, err := placementClient.ListCollections(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.ListCollections(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return &pb.ListCollectionsResponse{Collections: res.Collections}, nil
}

func (s *gatewayServer) GetCollectionStatus(ctx context.Context, req *pb.GetCollectionStatusRequest) (*pb.GetCollectionStatusResponse, error) {
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}
	pdReq := &placementpb.GetCollectionStatusRequest{
		Name: req.Name,
	}
	res, err := placementClient.GetCollectionStatus(ctx, pdReq)
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			res, err = placementClient.GetCollectionStatus(ctx, pdReq)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	shardStatuses := make([]*pb.ShardStatus, 0, len(res.Shards))
	for _, shard := range res.Shards {
		shardStatuses = append(shardStatuses, &pb.ShardStatus{
			ShardId:  shard.ShardId,
			Replicas: shard.Replicas,
			LeaderId: shard.LeaderId,
			Ready:    shard.Ready,
		})
	}

	return &pb.GetCollectionStatusResponse{
		Name:      res.Name,
		Dimension: res.Dimension,
		Distance:  res.Distance,
		Shards:    shardStatuses,
	}, nil
}

// metricsMiddleware wraps the gRPC gateway mux to track request metrics
func metricsMiddleware(mgr *management.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Record the request
		duration := time.Since(start)
		isError := wrapped.statusCode >= 400
		mgr.RecordRequest(r.URL.Path, r.Method, duration, isError)
	})
}

// responseRecorder wraps http.ResponseWriter to capture status code
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (rr *responseRecorder) WriteHeader(code int) {
	rr.statusCode = code
	rr.ResponseWriter.WriteHeader(code)
}

// managementHandlers returns a handler for management endpoints
func managementHandlers(mgr *management.Manager) http.Handler {
	mux := http.NewServeMux()

	// System health
	mux.HandleFunc("/v1/system/health", mgr.HandleSystemHealth)

	// Admin endpoints
	mux.HandleFunc("/v1/admin/stats", mgr.HandleGatewayStats)
	mux.HandleFunc("/v1/admin/workers", mgr.HandleWorkers)
	mux.HandleFunc("/v1/admin/collections", mgr.HandleCollections)

	// Alerts
	mux.HandleFunc("/v1/alerts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/resolve") {
			mgr.HandleResolveAlert(w, r)
		} else {
			mgr.HandleAlerts(w, r)
		}
	})

	return mux
}

// Start initializes and runs the API Gateway's gRPC and HTTP servers.
func Start(config Config, grpcListener net.Listener) (*grpc.Server, *grpc.ClientConn) {
	// Establish gRPC connection to Auth service
	authConn, err := grpc.Dial(config.AuthServiceAddr, grpcClientOptions(config.GRPCEnableCompression)...)
	if err != nil {
		log.Fatalf("Failed to connect to Auth service: %v", err)
	}
	authClient := authpb.NewAuthServiceClient(authConn)

	// Establish gRPC connection to Reranker service
	rerankerConn, err := grpc.Dial(config.RerankerServiceAddr, grpcClientOptions(config.GRPCEnableCompression)...)
	if err != nil {
		log.Fatalf("Failed to connect to Reranker service: %v", err)
	}
	rerankerClient := reranker.NewRerankServiceClient(rerankerConn)

	// Initialize feedback service with SQLite
	feedbackService, err := feedback.NewService(config.FeedbackDBPath)
	if err != nil {
		log.Fatalf("Failed to initialize feedback service: %v", err)
	}

	searchCacheTTL, searchCacheMax := defaultSearchCacheConfig()
	if config.SearchCacheTTLms > 0 {
		searchCacheTTL = time.Duration(config.SearchCacheTTLms) * time.Millisecond
	}
	if config.SearchCacheMaxSize > 0 {
		searchCacheMax = config.SearchCacheMaxSize
	}

	distCacheTTL := time.Duration(config.DistributedCacheTTLms) * time.Millisecond
	if distCacheTTL <= 0 {
		distCacheTTL = 5 * time.Second
	}
	distCacheTimeout := time.Duration(config.DistributedCacheTimeoutMs) * time.Millisecond
	if distCacheTimeout <= 0 {
		distCacheTimeout = defaultDistributedCacheTimeout()
	}
	var distributedCache *DistributedCache
	if !config.RawSpeedMode {
		distributedCache = NewDistributedCache(config.DistributedCacheAddr, config.DistributedCachePassword, config.DistributedCacheDB, distCacheTTL, distCacheTimeout)
	}

	var searchCache *SearchCache
	if config.SearchCacheEnabled && searchCacheTTL > 0 && searchCacheMax > 0 {
		searchCache = NewSearchCache(searchCacheTTL, searchCacheMax)
	}

	var workerSearchBatcher *WorkerSearchBatcher
	if !config.RawSpeedMode {
		workerSearchBatcher = NewWorkerSearchBatcher(2*time.Millisecond, 16)
	}

	// Create the gateway server with the list of PD addresses
	server := &gatewayServer{
		pdAddrs:                strings.Split(config.PlacementDriver, ","),
		authClient:             authClient,
		rerankerClient:         rerankerClient,
		feedbackService:        feedbackService,
		workerPool:             NewWorkerConnPool(config.GRPCEnableCompression), // Initialize worker connection pool for reuse
		workerSearchBatcher:    workerSearchBatcher,
		pdPool:                 NewPDConnPool(config.GRPCEnableCompression),
		searchCache:            searchCache,
		workerListCache:        NewWorkerListCache(5 * time.Second),
		resolveCache:           NewWorkerResolveCache(5*time.Second, defaultResolveCacheSize()),
		routingCache:           NewCollectionRoutingCache(5 * time.Second),
		distributedCache:       distributedCache,
		grpcCompressionEnabled: config.GRPCEnableCompression,
		rerankWarmupInFlight:   make(map[uint64]struct{}),
		inFlightSearches:       make(map[uint64]*inFlightSearch),
	}
	if config.RerankWarmupEnabled {
		ttl := time.Duration(config.RerankWarmupTTLms) * time.Millisecond
		if ttl <= 0 {
			ttl = 30 * time.Second
		}
		maxSize := config.RerankWarmupMaxSize
		if maxSize <= 0 {
			maxSize = 2000
		}
		concurrency := config.RerankWarmupConcurrency
		if concurrency <= 0 {
			concurrency = 2
		}
		server.rerankWarmupCache = NewRerankWarmupCache(ttl, maxSize)
		server.rerankWarmupSem = make(chan struct{}, concurrency)
	}

	// Initialize the leader connection with retry logic
	maxRetries := 5
	baseBackoff := 1 * time.Second
	for i := 0; i < maxRetries; i++ {
		if _, err := server.updateLeader(); err == nil {
			break
		}
		if i == maxRetries-1 {
			log.Fatalf("Failed to initialize connection with placement driver leader after %d attempts", maxRetries)
		}
		wait := baseBackoff * time.Duration(1<<i)
		log.Printf("Failed to connect to placement driver leader, retrying in %v...", wait)
		time.Sleep(wait)
	}

	// Create management manager with gRPC client
	// Note: We need to pass a pb.VectronServiceClient, but we don't have a direct client.
	// For now, we'll pass nil and the management manager will work without it.
	managementMgr := management.NewManager(
		server.leader.client,
		authClient,
		feedbackService,
		nil, // grpcClient - not available directly
	)
	server.managementMgr = managementMgr

	// Create the gRPC server with a chain of unary interceptors for middleware.
	interceptors := make([]grpc.UnaryServerInterceptor, 0, 3)
	interceptors = append(interceptors, middleware.AuthInterceptor(authClient, config.JWTSecret))
	interceptors = append(interceptors, middleware.RateLimitInterceptor(config.RateLimitRPS))
	if !config.RawSpeedMode {
		interceptors = append(interceptors, middleware.LoggingInterceptor)
	}
	grpcOpts := grpcServerOptions()
	if len(interceptors) > 0 {
		grpcOpts = append(grpcOpts, grpc.ChainUnaryInterceptor(interceptors...))
	}
	grpcServer := grpc.NewServer(grpcOpts...)
	pb.RegisterVectronServiceServer(grpcServer, server)

	// Set up the HTTP/JSON gateway to proxy requests to the gRPC server.
	mux := runtime.NewServeMux()
	opts := grpcClientOptions(config.GRPCEnableCompression)
	if err := pb.RegisterVectronServiceHandlerFromEndpoint(context.Background(), mux, config.GRPCAddr, opts); err != nil {
		log.Fatalf("Failed to register VectronServiceHandlerFromEndpoint: %v", err)
	}

	// Start the gRPC server in a separate goroutine.
	go func() {
		var lis net.Listener
		if grpcListener != nil {
			lis = grpcListener
		} else {
			var err error
			log.Printf("DEBUG: API Gateway config.GRPCAddr: %s, GRPC_ADDR env: %s", config.GRPCAddr, os.Getenv("GRPC_ADDR"))
			lis, err = net.Listen("tcp", config.GRPCAddr)
			if err != nil {
				log.Fatalf("Failed to listen for gRPC server: %v", err)
			}
		}
		log.Printf("Vectron gRPC API (SDKs)     → %s", lis.Addr().String())
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server failed to serve: %v", err)
		}
	}()

	// Create a combined HTTP handler that routes to either management or gRPC gateway
	mainHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if this is a management endpoint
		if strings.HasPrefix(r.URL.Path, "/v1/system/") ||
			strings.HasPrefix(r.URL.Path, "/v1/admin/") ||
			strings.HasPrefix(r.URL.Path, "/v1/alerts") {
			managementHandlers(managementMgr).ServeHTTP(w, r)
		} else {
			mux.ServeHTTP(w, r)
		}
	})

	// Wrap with CORS middleware
	corsHandler := func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, Content-Length, X-CSRF-Token, X-API-Key")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			h.ServeHTTP(w, r)
		})
	}

	// Wrap with metrics middleware
	finalHandler := metricsMiddleware(managementMgr, corsHandler(mainHandler))

	// Start the HTTP server in a separate goroutine.
	go func() {
		log.Printf("Vectron HTTP API (curl)      → %s", config.HTTPAddr)
		log.Printf("Management endpoints         → %s/v1/system/health, %s/v1/admin/...", config.HTTPAddr, config.HTTPAddr)
		log.Printf("Using placement driver           → %s", config.PlacementDriver)
		if err := http.ListenAndServe(config.HTTPAddr, finalHandler); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server failed to serve: %v", err)
		}
	}()

	return grpcServer, authConn
}

// ================ MAIN ================

func main() {
	_, authConn := Start(cfg, nil)
	defer authConn.Close()

	select {}
}
