// This file implements the gRPC server for the Worker service.
// It handles incoming RPCs from the API Gateway, and for each request,
// it directs the operation to the correct shard (Raft cluster).
// Write operations are proposed to the shard's Raft log, and read
// operations are performed via linearizable reads on the shard's FSM.

package internal

import (
	"container/heap"
	"context"
	"encoding/binary"
	"hash/maphash"
	"io"
	"log"
	"math"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lni/dragonboat/v3"
	"github.com/pavandhadge/vectron/shared/proto/worker"
	"github.com/pavandhadge/vectron/worker/internal/shard"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// raftTimeout is the default timeout for Raft proposals.
	raftTimeout = 5 * time.Second
)

var (
	debugLogs                    = os.Getenv("VECTRON_DEBUG_LOGS") == "1"
	hotPathLogSampleEvery uint64 = 100
	hotPathLogCounter     uint64
)

func init() {
	if v := os.Getenv("WORKER_LOG_SAMPLE_EVERY"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if n <= 1 {
				hotPathLogSampleEvery = 1
			} else {
				hotPathLogSampleEvery = uint64(n)
			}
		}
	}
}

func shouldLogHotPath() bool {
	if debugLogs {
		return true
	}
	if hotPathLogSampleEvery <= 1 {
		return true
	}
	return atomic.AddUint64(&hotPathLogCounter, 1)%hotPathLogSampleEvery == 0
}

type searchHeapItem struct {
	id    string
	score float32
}

func adaptiveConcurrency(multiplier, maxCap int) int {
	procs := runtime.GOMAXPROCS(0)
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

func envIntDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

type searchMinHeap []searchHeapItem

func (h searchMinHeap) Len() int           { return len(h) }
func (h searchMinHeap) Less(i, j int) bool { return h[i].score < h[j].score }
func (h searchMinHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *searchMinHeap) Push(x interface{}) {
	*h = append(*h, x.(searchHeapItem))
}
func (h *searchMinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

var searchHeapPool = sync.Pool{
	New: func() interface{} {
		h := make(searchMinHeap, 0, 64)
		return &h
	},
}

var searchItemSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]searchHeapItem, 0, 64)
		return &s
	},
}

func getSearchHeap() *searchMinHeap {
	h := searchHeapPool.Get().(*searchMinHeap)
	*h = (*h)[:0]
	return h
}

func putSearchHeap(h *searchMinHeap) {
	if h == nil {
		return
	}
	*h = (*h)[:0]
	searchHeapPool.Put(h)
}

func getSearchItemSlice(n int) *[]searchHeapItem {
	s := searchItemSlicePool.Get().(*[]searchHeapItem)
	if cap(*s) < n {
		*s = make([]searchHeapItem, 0, n)
	}
	*s = (*s)[:0]
	return s
}

func putSearchItemSlice(s *[]searchHeapItem) {
	if s == nil {
		return
	}
	*s = (*s)[:0]
	searchItemSlicePool.Put(s)
}

// GrpcServer implements the gRPC worker.WorkerServiceServer interface.
type GrpcServer struct {
	worker.UnimplementedWorkerServiceServer
	nodeHost       *dragonboat.NodeHost // The Dragonboat node host that manages all shards on this worker.
	shardManager   *shard.Manager       // The manager for all shards hosted on this worker.
	inFlightShards []inFlightShard
	searchCache    *searchCache
}

type inFlightSearch struct {
	done chan struct{}
	resp *worker.SearchResponse
	err  error
}

type inFlightShard struct {
	mu sync.Mutex
	m  map[uint64]*inFlightSearch
}

const inFlightShardCount = 256
const searchCacheShardCount = 128

const (
	defaultSearchCacheTTL     = 200 * time.Millisecond
	defaultSearchCacheMaxSize = 0
)

var searchHashSeed = maphash.MakeSeed()
var searchCacheQuantBits = envInt("VECTRON_WORKER_SEARCH_CACHE_QUANT_BITS", 0)

type searchCacheEntry struct {
	resp      *worker.SearchResponse
	expiresAt int64
}

type searchCacheShard struct {
	mu      sync.Mutex
	entries map[uint64]searchCacheEntry
}

type searchCache struct {
	shards      []searchCacheShard
	ttl         time.Duration
	maxSize     int
	maxPerShard int
}

func newSearchCache(ttl time.Duration, maxSize int) *searchCache {
	if maxSize <= 0 {
		return nil
	}
	shards := make([]searchCacheShard, searchCacheShardCount)
	for i := range shards {
		shards[i].entries = make(map[uint64]searchCacheEntry)
	}
	perShard := maxSize / len(shards)
	if perShard < 1 {
		perShard = 1
	}
	return &searchCache{
		shards:      shards,
		ttl:         ttl,
		maxSize:     maxSize,
		maxPerShard: perShard,
	}
}

func (c *searchCache) Get(key uint64) (*worker.SearchResponse, bool) {
	if c == nil {
		return nil, false
	}
	shard := &c.shards[key%uint64(len(c.shards))]
	now := time.Now().UnixNano()
	shard.mu.Lock()
	entry, ok := shard.entries[key]
	if !ok {
		shard.mu.Unlock()
		return nil, false
	}
	if entry.expiresAt <= now {
		delete(shard.entries, key)
		shard.mu.Unlock()
		return nil, false
	}
	shard.mu.Unlock()
	return entry.resp, true
}

func (c *searchCache) Set(key uint64, resp *worker.SearchResponse) {
	if c == nil || resp == nil {
		return
	}
	shard := &c.shards[key%uint64(len(c.shards))]
	exp := time.Now().Add(c.ttl).UnixNano()
	shard.mu.Lock()
	if _, exists := shard.entries[key]; exists {
		shard.entries[key] = searchCacheEntry{resp: resp, expiresAt: exp}
		shard.mu.Unlock()
		return
	}
	if len(shard.entries) >= c.maxPerShard {
		for k := range shard.entries {
			delete(shard.entries, k)
			break
		}
	}
	shard.entries[key] = searchCacheEntry{resp: resp, expiresAt: exp}
	shard.mu.Unlock()
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// NewGrpcServer creates a new instance of the gRPC server.
func NewGrpcServer(nh *dragonboat.NodeHost, sm *shard.Manager) *GrpcServer {
	cacheTTL := time.Duration(envInt("VECTRON_WORKER_SEARCH_CACHE_TTL_MS", int(defaultSearchCacheTTL.Milliseconds()))) * time.Millisecond
	cacheMax := envInt("VECTRON_WORKER_SEARCH_CACHE_MAX", defaultSearchCacheMaxSize)
	s := &GrpcServer{
		nodeHost:       nh,
		shardManager:   sm,
		inFlightShards: make([]inFlightShard, inFlightShardCount),
		searchCache:    newSearchCache(cacheTTL, cacheMax),
	}
	for i := range s.inFlightShards {
		s.inFlightShards[i].m = make(map[uint64]*inFlightSearch)
	}
	return s
}

func (s *GrpcServer) validateShardLease(shardID uint64, shardEpoch uint64, leaseExpiryUnixMs int64) error {
	if shardID == 0 {
		return nil
	}
	if shardEpoch == 0 {
		return status.Error(codes.FailedPrecondition, "missing shard epoch")
	}
	current := s.shardManager.GetShardEpoch(shardID)
	if current == 0 || shardEpoch != current {
		return status.Errorf(codes.FailedPrecondition, "stale shard epoch (have %d, want %d)", shardEpoch, current)
	}
	if leaseExpiryUnixMs > 0 && time.Now().UnixMilli() > leaseExpiryUnixMs {
		return status.Error(codes.FailedPrecondition, "shard lease expired")
	}
	return nil
}

// StoreVector handles the request to store a vector.
// It marshals the request into a command and proposes it to the target shard's Raft group.
func (s *GrpcServer) StoreVector(ctx context.Context, req *worker.StoreVectorRequest) (*worker.StoreVectorResponse, error) {
	if shouldLogHotPath() {
		log.Printf("Received StoreVector request for ID: %s on shard %d", req.GetVector().GetId(), req.GetShardId())
	}
	start := time.Now()
	if req.GetVector() == nil {
		return nil, status.Error(codes.InvalidArgument, "vector is nil")
	}
	if err := s.validateShardLease(req.GetShardId(), req.GetShardEpoch(), req.GetLeaseExpiryUnixMs()); err != nil {
		return nil, err
	}

	// Before proposing, check if the shard is ready on this node.
	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	// Create the command for the shard's FSM.
	cmd := shard.Command{
		Type:     shard.StoreVector,
		ID:       req.GetVector().GetId(),
		Vector:   req.GetVector().GetVector(),
		Metadata: req.GetVector().GetMetadata(),
	}
	cmdBytes, err := shard.EncodeCommand(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	// Propose the command to the shard's Raft group. This is a blocking call.
	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	_, err = s.nodeHost.SyncPropose(ctx, cs, cmdBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose StoreVector command: %v", err)
	}
	if shouldLogHotPath() {
		log.Printf("Worker StoreVector shard=%d vecDim=%d total=%s err=nil",
			req.GetShardId(), len(req.GetVector().GetVector()), time.Since(start))
	}

	return &worker.StoreVectorResponse{}, nil
}

// BatchStoreVector handles storing multiple vectors in a single Raft proposal.
func (s *GrpcServer) BatchStoreVector(ctx context.Context, req *worker.BatchStoreVectorRequest) (*worker.BatchStoreVectorResponse, error) {
	if req == nil || len(req.GetVectors()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "vectors are empty")
	}
	start := time.Now()
	if err := s.validateShardLease(req.GetShardId(), req.GetShardEpoch(), req.GetLeaseExpiryUnixMs()); err != nil {
		return nil, err
	}

	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	cmdVectors := make([]shard.VectorEntry, 0, len(req.GetVectors()))
	for _, v := range req.GetVectors() {
		if v == nil || v.GetId() == "" || len(v.GetVector()) == 0 {
			return nil, status.Error(codes.InvalidArgument, "vector id or vector data missing")
		}
		cmdVectors = append(cmdVectors, shard.VectorEntry{
			ID:       v.GetId(),
			Vector:   v.GetVector(),
			Metadata: v.GetMetadata(),
		})
	}

	cmd := shard.Command{
		Type:    shard.StoreVectorBatch,
		Vectors: cmdVectors,
	}
	cmdBytes, err := shard.EncodeCommand(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal batch command: %v", err)
	}

	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	if _, err := s.nodeHost.SyncPropose(ctx, cs, cmdBytes); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose batch StoreVector command: %v", err)
	}
	if shouldLogHotPath() {
		vecDim := 0
		if len(req.GetVectors()) > 0 {
			vecDim = len(req.GetVectors()[0].GetVector())
		}
		log.Printf("Worker BatchStoreVector shard=%d batch=%d vecDim=%d total=%s err=nil",
			req.GetShardId(), len(req.GetVectors()), vecDim, time.Since(start))
	}

	return &worker.BatchStoreVectorResponse{Stored: int32(len(cmdVectors))}, nil
}

// StreamBatchStoreVector handles streaming multiple batch requests for large ingests.
// Each incoming batch is proposed as a single Raft command.
func (s *GrpcServer) StreamBatchStoreVector(stream worker.WorkerService_StreamBatchStoreVectorServer) error {
	var totalStored int32
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&worker.BatchStoreVectorResponse{Stored: totalStored})
		}
		if err != nil {
			return status.Errorf(codes.Internal, "stream recv failed: %v", err)
		}
		if req == nil || len(req.GetVectors()) == 0 {
			continue
		}
		if err := s.validateShardLease(req.GetShardId(), req.GetShardEpoch(), req.GetLeaseExpiryUnixMs()); err != nil {
			return err
		}
		if !s.shardManager.IsShardReady(req.GetShardId()) {
			return status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
		}
		cmdVectors := make([]shard.VectorEntry, 0, len(req.GetVectors()))
		for _, v := range req.GetVectors() {
			if v == nil || v.GetId() == "" || len(v.GetVector()) == 0 {
				return status.Error(codes.InvalidArgument, "vector id or vector data missing")
			}
			cmdVectors = append(cmdVectors, shard.VectorEntry{
				ID:       v.GetId(),
				Vector:   v.GetVector(),
				Metadata: v.GetMetadata(),
			})
		}

		cmd := shard.Command{
			Type:    shard.StoreVectorBatch,
			Vectors: cmdVectors,
		}
		cmdBytes, err := shard.EncodeCommand(cmd)
		if err != nil {
			return status.Errorf(codes.Internal, "failed to marshal batch command: %v", err)
		}

		cs := s.nodeHost.GetNoOPSession(req.GetShardId())
		if _, err := s.nodeHost.SyncPropose(stream.Context(), cs, cmdBytes); err != nil {
			return status.Errorf(codes.Internal, "failed to propose batch StoreVector command: %v", err)
		}
		totalStored += int32(len(cmdVectors))
	}
}

// BatchSearch performs multiple searches in a single RPC.
func (s *GrpcServer) BatchSearch(ctx context.Context, req *worker.BatchSearchRequest) (*worker.BatchSearchResponse, error) {
	if req == nil || len(req.GetRequests()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "batch search requests are empty")
	}
	requests := req.GetRequests()
	for _, r := range requests {
		if r == nil || len(r.GetVector()) == 0 {
			return nil, status.Error(codes.InvalidArgument, "search vector is empty")
		}
	}
	if len(requests) == 1 {
		resp, err := s.searchCore(ctx, requests[0])
		if err != nil {
			return nil, err
		}
		return &worker.BatchSearchResponse{Responses: []*worker.SearchResponse{resp}}, nil
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	responses := make([]*worker.SearchResponse, len(requests))
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		sem      = make(chan struct{}, envIntDefault("VECTRON_WORKER_BATCHSEARCH_CONCURRENCY", adaptiveConcurrency(2, 32)))
	)

	for i, r := range requests {
		i := i
		r := r
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := s.validateShardLease(r.GetShardId(), r.GetShardEpoch(), r.GetLeaseExpiryUnixMs()); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
			resp, err := s.searchCore(ctx, r)
			if err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
					cancel()
				}
				mu.Unlock()
				return
			}
			responses[i] = resp
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return &worker.BatchSearchResponse{Responses: responses}, nil
}

// Search performs a similarity search for a given vector.
// It performs a linearizable read on the target shard's FSM to ensure up-to-date results.
func (s *GrpcServer) Search(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	if shouldLogHotPath() {
		log.Printf("Received Search request on shard %d", req.GetShardId())
	}
	start := time.Now()
	if len(req.GetVector()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "search vector is empty")
	}
	if err := s.validateShardLease(req.GetShardId(), req.GetShardEpoch(), req.GetLeaseExpiryUnixMs()); err != nil {
		return nil, err
	}

	key := computeSearchKey(req)
	cacheable := !req.GetLinearizable() && !req.GetBruteForce()
	if cacheable {
		if cached, ok := s.searchCache.Get(key); ok && len(cached.Ids) > 0 {
			return cached, nil
		}
	}
	if resp, err, shared := s.waitForInFlight(ctx, key); shared {
		return resp, err
	}

	resp, err := s.searchCore(ctx, req)
	if err == nil && cacheable && resp != nil && len(resp.Ids) > 0 {
		s.searchCache.Set(key, resp)
	}
	s.finishInFlight(key, resp, err)
	if shouldLogHotPath() {
		log.Printf("Worker Search shard=%d broadcast=%t linearizable=%t vecDim=%d k=%d total=%s err=%v",
			req.GetShardId(),
			req.GetShardId() == 0,
			req.GetLinearizable(),
			len(req.GetVector()),
			req.GetK(),
			time.Since(start),
			err,
		)
	}
	return resp, err
}

func (s *GrpcServer) searchCore(ctx context.Context, req *worker.SearchRequest) (*worker.SearchResponse, error) {
	query := shard.SearchQuery{
		Vector: req.GetVector(),
		K:      int(req.GetK()),
	}
	useLinearizable := req.GetLinearizable()
	cacheable := !useLinearizable && !req.GetBruteForce()

	// If ShardId is 0, it's a broadcast search. Search all shards on this worker.
	if req.GetShardId() == 0 {
		var (
			wg sync.WaitGroup
		)

		searchSem := make(chan struct{}, envIntDefault("VECTRON_WORKER_SEARCH_CONCURRENCY", adaptiveConcurrency(2, 32)))
		var shardIDs []uint64
		if req.GetCollection() != "" {
			shardIDs = s.shardManager.GetShardsForCollection(req.GetCollection())
		} else {
			shardIDs = s.shardManager.GetShards()
		}
		if len(shardIDs) == 1 {
			var res interface{}
			var err error
			if cacheable {
				if cached, ok := s.searchCache.Get(computeSearchKeyWithShard(req, shardIDs[0])); ok && len(cached.Ids) > 0 {
					return &worker.SearchResponse{Ids: cached.Ids, Scores: cached.Scores}, nil
				}
			}
			if useLinearizable {
				res, err = s.nodeHost.SyncRead(ctx, shardIDs[0], query)
			} else {
				res, err = s.nodeHost.StaleRead(shardIDs[0], query)
			}
			if err != nil {
				return nil, err
			}
			searchResult, ok := res.(*shard.SearchResult)
			if !ok {
				return nil, status.Errorf(codes.Internal, "unexpected search result type: %T", res)
			}
			if cacheable && len(searchResult.IDs) > 0 {
				s.searchCache.Set(computeSearchKeyWithShard(req, shardIDs[0]), &worker.SearchResponse{
					Ids:    searchResult.IDs,
					Scores: searchResult.Scores,
				})
			}
			return &worker.SearchResponse{Ids: searchResult.IDs, Scores: searchResult.Scores}, nil
		}
		resultsCh := make(chan *[]searchHeapItem, len(shardIDs))
		for _, shardID := range shardIDs {
			wg.Add(1)
			go func(id uint64) {
				defer wg.Done()
				searchSem <- struct{}{}
				defer func() { <-searchSem }()
				var res interface{}
				var err error
				if cacheable {
					if cached, ok := s.searchCache.Get(computeSearchKeyWithShard(req, id)); ok && len(cached.Ids) > 0 {
						searchResult := &shard.SearchResult{IDs: cached.Ids, Scores: cached.Scores}
						limit := int(req.GetK())
						if limit <= 0 {
							limit = 10
						}
						localHeap := getSearchHeap()
						heap.Init(localHeap)
						for i, resultID := range searchResult.IDs {
							if i >= len(searchResult.Scores) {
								break
							}
							score := searchResult.Scores[i]
							if localHeap.Len() < limit {
								heap.Push(localHeap, searchHeapItem{id: resultID, score: score})
								continue
							}
							if localHeap.Len() > 0 && score > (*localHeap)[0].score {
								(*localHeap)[0] = searchHeapItem{id: resultID, score: score}
								heap.Fix(localHeap, 0)
							}
						}
						if localHeap.Len() > 0 {
							buf := getSearchItemSlice(localHeap.Len())
							*buf = append(*buf, (*localHeap)...)
							resultsCh <- buf
						}
						putSearchHeap(localHeap)
						return
					}
				}
				if useLinearizable {
					res, err = s.nodeHost.SyncRead(ctx, id, query)
				} else {
					res, err = s.nodeHost.StaleRead(id, query)
				}
				if err != nil {
					// Log error but don't fail the whole search for one failed shard.
					log.Printf("Failed to search shard %d: %v", id, err)
					return
				}
				searchResult, ok := res.(*shard.SearchResult)
				if !ok {
					log.Printf("Unexpected search result type from shard %d: %T", id, res)
					return
				}
				if cacheable && len(searchResult.IDs) > 0 {
					s.searchCache.Set(computeSearchKeyWithShard(req, id), &worker.SearchResponse{
						Ids:    searchResult.IDs,
						Scores: searchResult.Scores,
					})
				}

				limit := int(req.GetK())
				if limit <= 0 {
					limit = 10
				}
				localHeap := getSearchHeap()
				heap.Init(localHeap)
				for i, resultID := range searchResult.IDs {
					if i >= len(searchResult.Scores) {
						break
					}
					score := searchResult.Scores[i]
					if localHeap.Len() < limit {
						heap.Push(localHeap, searchHeapItem{id: resultID, score: score})
						continue
					}
					if localHeap.Len() > 0 && score > (*localHeap)[0].score {
						(*localHeap)[0] = searchHeapItem{id: resultID, score: score}
						heap.Fix(localHeap, 0)
					}
				}
				if localHeap.Len() > 0 {
					buf := getSearchItemSlice(localHeap.Len())
					*buf = append(*buf, (*localHeap)...)
					resultsCh <- buf
				}
				putSearchHeap(localHeap)
			}(shardID)
		}
		wg.Wait()

		close(resultsCh)

		limit := int(req.GetK())
		if limit <= 0 {
			limit = 10
		}
		topKHeap := getSearchHeap()
		heap.Init(topKHeap)
		for local := range resultsCh {
			for _, item := range *local {
				if topKHeap.Len() < limit {
					heap.Push(topKHeap, item)
					continue
				}
				if topKHeap.Len() > 0 && item.score > (*topKHeap)[0].score {
					(*topKHeap)[0] = item
					heap.Fix(topKHeap, 0)
				}
			}
			putSearchItemSlice(local)
		}

		resultCount := topKHeap.Len()
		ids := make([]string, resultCount)
		scores := make([]float32, resultCount)
		for i := resultCount - 1; i >= 0; i-- {
			item := heap.Pop(topKHeap).(searchHeapItem)
			ids[i] = item.id
			scores[i] = item.score
		}
		putSearchHeap(topKHeap)
		return &worker.SearchResponse{Ids: ids, Scores: scores}, nil

	} else {
		// Original logic for single-shard search
		var res interface{}
		var err error
		if cacheable {
			if cached, ok := s.searchCache.Get(computeSearchKeyWithShard(req, req.GetShardId())); ok && len(cached.Ids) > 0 {
				return cached, nil
			}
		}
		if useLinearizable {
			res, err = s.nodeHost.SyncRead(ctx, req.GetShardId(), query)
		} else {
			res, err = s.nodeHost.StaleRead(req.GetShardId(), query)
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to perform search: %v", err)
		}

		searchResult, ok := res.(*shard.SearchResult)
		if !ok {
			return nil, status.Errorf(codes.Internal, "unexpected search result type: %T", res)
		}
		if cacheable && len(searchResult.IDs) > 0 {
			s.searchCache.Set(computeSearchKeyWithShard(req, req.GetShardId()), &worker.SearchResponse{
				Ids:    searchResult.IDs,
				Scores: searchResult.Scores,
			})
		}

		return &worker.SearchResponse{Ids: searchResult.IDs, Scores: searchResult.Scores}, nil
	}
}

func computeSearchKey(req *worker.SearchRequest) uint64 {
	var h maphash.Hash
	h.SetSeed(searchHashSeed)
	return computeSearchKeyWithShard(req, req.GetShardId())
}

func computeSearchKeyWithShard(req *worker.SearchRequest, shardID uint64) uint64 {
	var h maphash.Hash
	h.SetSeed(searchHashSeed)
	var buf8 [8]byte
	var buf4 [4]byte
	binary.LittleEndian.PutUint64(buf8[:], shardID)
	h.Write(buf8[:])
	binary.LittleEndian.PutUint32(buf4[:], uint32(req.GetK()))
	h.Write(buf4[:])
	flags := byte(0)
	if req.GetLinearizable() {
		flags |= 1 << 0
	}
	if req.GetBruteForce() {
		flags |= 1 << 1
	}
	h.Write([]byte{flags})
	h.WriteString(req.GetCollection())
	for _, v := range req.GetVector() {
		bits := math.Float32bits(v)
		qb := searchCacheQuantBits
		if qb > 0 && qb < 23 {
			mantissaMask := uint32(1<<(23-qb)) - 1
			bits = (bits & 0xFF800000) | (bits & 0x007FFFFF &^ mantissaMask)
		}
		binary.LittleEndian.PutUint32(buf4[:], bits)
		h.Write(buf4[:])
	}
	return h.Sum64()
}

func (s *GrpcServer) waitForInFlight(ctx context.Context, key uint64) (*worker.SearchResponse, error, bool) {
	shard := &s.inFlightShards[key%uint64(len(s.inFlightShards))]
	shard.mu.Lock()
	if flight, ok := shard.m[key]; ok {
		shard.mu.Unlock()
		select {
		case <-flight.done:
			return flight.resp, flight.err, true
		case <-ctx.Done():
			return nil, ctx.Err(), true
		}
	}
	flight := &inFlightSearch{done: make(chan struct{})}
	shard.m[key] = flight
	shard.mu.Unlock()
	return nil, nil, false
}

func (s *GrpcServer) finishInFlight(key uint64, resp *worker.SearchResponse, err error) {
	shard := &s.inFlightShards[key%uint64(len(s.inFlightShards))]
	shard.mu.Lock()
	flight, ok := shard.m[key]
	if ok {
		delete(shard.m, key)
	}
	shard.mu.Unlock()
	if ok {
		flight.resp = resp
		flight.err = err
		close(flight.done)
	}
}

// GetVector retrieves a vector by its ID.
// This is a read operation and uses a linearizable read from the FSM.
func (s *GrpcServer) GetVector(ctx context.Context, req *worker.GetVectorRequest) (*worker.GetVectorResponse, error) {
	if shouldLogHotPath() {
		log.Printf("Received GetVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())
	}
	if err := s.validateShardLease(req.GetShardId(), req.GetShardEpoch(), req.GetLeaseExpiryUnixMs()); err != nil {
		return nil, err
	}

	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	// Perform a linearizable read on the shard's state machine.
	res, err := s.nodeHost.SyncRead(ctx, req.GetShardId(), shard.GetVectorQuery{ID: req.GetId()})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to perform GetVector: %v", err)
	}
	if res == nil {
		return nil, status.Errorf(codes.NotFound, "vector with id %s not found", req.GetId())
	}

	getResult, ok := res.(*shard.GetVectorQueryResult)
	if !ok || getResult == nil {
		return nil, status.Errorf(codes.Internal, "unexpected GetVector result type or nil result")
	}

	return &worker.GetVectorResponse{
		Vector: &worker.Vector{
			Id:       req.GetId(),
			Vector:   getResult.Vector,
			Metadata: getResult.Metadata,
		},
	}, nil
}

// DeleteVector deletes a vector by its ID.
// This is a write operation and is proposed to the Raft log.
func (s *GrpcServer) DeleteVector(ctx context.Context, req *worker.DeleteVectorRequest) (*worker.DeleteVectorResponse, error) {
	if shouldLogHotPath() {
		log.Printf("Received DeleteVector request for ID: %s on shard %d", req.GetId(), req.GetShardId())
	}
	if err := s.validateShardLease(req.GetShardId(), req.GetShardEpoch(), req.GetLeaseExpiryUnixMs()); err != nil {
		return nil, err
	}
	if !s.shardManager.IsShardReady(req.GetShardId()) {
		return nil, status.Errorf(codes.Unavailable, "shard %d not ready", req.GetShardId())
	}

	cmd := shard.Command{
		Type: shard.DeleteVector,
		ID:   req.GetId(),
	}
	cmdBytes, err := shard.EncodeCommand(cmd)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to marshal command: %v", err)
	}

	cs := s.nodeHost.GetNoOPSession(req.GetShardId())
	// Propose the delete command to the shard's Raft group.
	_, err = s.nodeHost.SyncPropose(ctx, cs, cmdBytes)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to propose DeleteVector command: %v", err)
	}

	return &worker.DeleteVectorResponse{}, nil
}

// --- Unimplemented Methods ---
// The following methods are part of the WorkerService but are not yet implemented.

func (s *GrpcServer) Put(ctx context.Context, req *worker.PutRequest) (*worker.PutResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Put not implemented")
}

func (s *GrpcServer) Get(ctx context.Context, req *worker.GetRequest) (*worker.GetResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Get not implemented")
}

func (s *GrpcServer) Delete(ctx context.Context, req *worker.DeleteRequest) (*worker.DeleteResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Delete not implemented")
}

func (s *GrpcServer) Status(ctx context.Context, req *worker.StatusRequest) (*worker.StatusResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Status not implemented")
}

func (s *GrpcServer) Flush(ctx context.Context, req *worker.FlushRequest) (*worker.FlushResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Flush not implemented")
}
