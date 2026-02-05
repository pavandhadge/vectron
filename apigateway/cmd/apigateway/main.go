// This file is the main entry point for the API Gateway service.
// It sets up and runs the gRPC server and the HTTP/JSON gateway,
// which exposes the public Vectron API to clients. It handles request
// forwarding to the appropriate backend services (placement driver or workers).

package main

import (
	"container/heap"
	"context"
	"encoding/binary"
	"fmt"
	runtime "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"hash/maphash"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	stdruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pavandhadge/vectron/apigateway/internal/feedback"
	"github.com/pavandhadge/vectron/apigateway/internal/management"
	"github.com/pavandhadge/vectron/apigateway/internal/middleware"
	"github.com/pavandhadge/vectron/apigateway/internal/translator"
	pb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	placementpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	reranker "github.com/pavandhadge/vectron/shared/proto/reranker"
	workerpb "github.com/pavandhadge/vectron/shared/proto/worker"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var cfg = LoadConfig()
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

const (
	grpcReadBufferSize  = 64 * 1024
	grpcWriteBufferSize = 64 * 1024
	grpcWindowSize      = 1 << 20
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
	Timestamp time.Time
}

type WorkerResolveCache struct {
	mu      sync.RWMutex
	entries map[string]*WorkerResolveCacheEntry
	ttl     time.Duration
	maxSize int
}

func NewWorkerResolveCache(ttl time.Duration, maxSize int) *WorkerResolveCache {
	return &WorkerResolveCache{
		entries: make(map[string]*WorkerResolveCacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func (c *WorkerResolveCache) Get(collection, vectorID string) (string, uint64, bool) {
	key := collection + "|" + vectorID
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return "", 0, false
	}
	if time.Since(entry.Timestamp) > c.ttl {
		return "", 0, false
	}
	return entry.Addr, entry.ShardID, true
}

func (c *WorkerResolveCache) Set(collection, vectorID string, addr string, shardID uint64) {
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
type WorkerConnPool struct {
	mu                     sync.RWMutex
	conns                  map[string]*grpc.ClientConn
	clients                map[string]workerpb.WorkerServiceClient
	grpcCompressionEnabled bool
}

// GetClient returns a cached gRPC client for the given worker address.
// Creates a new connection if one doesn't exist or if the existing one is unhealthy.
func (p *WorkerConnPool) GetClient(addr string) (workerpb.WorkerServiceClient, error) {
	// Fast path: check for existing connection
	p.mu.RLock()
	if client, ok := p.clients[addr]; ok {
		if conn := p.conns[addr]; conn != nil {
			state := conn.GetState()
			if state != connectivity.Shutdown && state != connectivity.TransientFailure {
				p.mu.RUnlock()
				return client, nil
			}
		}
	}
	p.mu.RUnlock()

	// Slow path: create new connection
	p.mu.Lock()
	defer p.mu.Unlock()

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
		}
	}

	// Create new connection with keepalive
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	opts := append(grpcClientOptions(p.grpcCompressionEnabled), grpc.WithBlock())
	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial worker %s: %w", addr, err)
	}

	client := workerpb.NewWorkerServiceClient(conn)
	p.conns[addr] = conn
	p.clients[addr] = client
	return client, nil
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
}

// NewWorkerConnPool creates a new worker connection pool.
func NewWorkerConnPool(enableCompression bool) *WorkerConnPool {
	return &WorkerConnPool{
		conns:                  make(map[string]*grpc.ClientConn),
		clients:                make(map[string]workerpb.WorkerServiceClient),
		grpcCompressionEnabled: enableCompression,
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
	searchCache            *SearchCache    // Cache for search results
	workerListCache        *WorkerListCache
	resolveCache           *WorkerResolveCache
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

func (s *gatewayServer) resolveWorker(ctx context.Context, collection string, vectorID string) (string, uint64, error) {
	if vectorID != "" {
		if addr, shardID, ok := s.resolveCache.Get(collection, vectorID); ok {
			return addr, shardID, nil
		}
	}

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return "", 0, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	resp, err := placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
		VectorId:   vectorID,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return "", 0, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			resp, err = placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
				Collection: collection,
				VectorId:   vectorID,
			})
			if err != nil {
				return "", 0, status.Errorf(codes.Internal, "could not get worker for collection %q after leader update: %v", collection, err)
			}
		} else {
			return "", 0, status.Errorf(codes.Internal, "could not get worker for collection %q: %v", collection, err)
		}
	}
	if resp.GetGrpcAddress() == "" {
		return "", 0, status.Errorf(codes.NotFound, "collection %q not found", collection)
	}
	addr := resp.GetGrpcAddress()
	shardID := uint64(resp.GetShardId())
	if vectorID != "" {
		s.resolveCache.Set(collection, vectorID, addr, shardID)
	}
	return addr, shardID, nil
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

	if s.leader != nil && s.leader.conn != nil {
		s.leader.conn.Close()
	}

	for _, addr := range s.pdAddrs {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		opts := append(grpcClientOptions(s.grpcCompressionEnabled), grpc.WithBlock())
		conn, err := grpc.DialContext(ctx, addr, opts...)
		cancel()
		if err != nil {
			log.Printf("Failed to connect to PD node %s: %v", addr, err)
			continue
		}

		client := placementpb.NewPlacementServiceClient(conn)
		_, err = client.ListCollections(context.Background(), &placementpb.ListCollectionsRequest{})
		if err != nil {
			conn.Close()
			log.Printf("Failed to get leader from PD node %s: %v", addr, err)
			continue
		}

		log.Println("Connected to new PD leader at", conn.Target())
		s.leader = &LeaderInfo{client: client, conn: conn}
		return client, nil
	}

	return nil, status.Error(codes.Unavailable, "no placement driver leader found")
}

func (s *gatewayServer) forwardToWorker(ctx context.Context, collection string, vectorID string, call func(workerpb.WorkerServiceClient, uint64) (interface{}, error)) (interface{}, error) {
	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}

	resp, err := placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
		Collection: collection,
		VectorId:   vectorID,
	})
	if err != nil {
		if st, ok := status.FromError(err); ok && (st.Code() == codes.Unavailable || st.Code() == codes.Internal) {
			placementClient, err = s.updateLeader()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not update placement driver leader: %v", err)
			}
			resp, err = placementClient.GetWorker(ctx, &placementpb.GetWorkerRequest{
				Collection: collection,
				VectorId:   vectorID,
			})
			if err != nil {
				return nil, status.Errorf(codes.Internal, "could not get worker for collection %q after leader update: %v", collection, err)
			}
		} else {
			return nil, status.Errorf(codes.Internal, "could not get worker for collection %q: %v", collection, err)
		}
	}
	if resp.GetGrpcAddress() == "" {
		return nil, status.Errorf(codes.NotFound, "collection %q not found", collection)
	}

	// Use connection pool instead of creating new connection each time
	client, err := s.workerPool.GetClient(resp.GetGrpcAddress())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "worker for collection %q at %s is unreachable: %v", collection, resp.GetGrpcAddress(), err)
	}

	return call(client, uint64(resp.ShardId))
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

	// Resolve worker/shard assignments in parallel, then batch per shard.
	var (
		assignMu    sync.Mutex
		assignErr   error
		assignWg    sync.WaitGroup
		semaphore   = make(chan struct{}, adaptiveConcurrency(4, 64))
		assignments = make(map[string]map[uint64][]*pb.Point)
	)

	for _, point := range req.Points {
		assignWg.Add(1)
		go func(p *pb.Point) {
			defer assignWg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			addr, shardID, err := s.resolveWorker(ctx, req.Collection, p.Id)
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
				shards = make(map[uint64][]*pb.Point)
				assignments[addr] = shards
			}
			shards[shardID] = append(shards[shardID], p)
			assignMu.Unlock()
		}(point)
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
		for shardID, points := range shards {
			sendWg.Add(1)
			go func(workerAddr string, sid uint64, pts []*pb.Point) {
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

				workerReq := translator.ToWorkerBatchStoreVectorRequestFromPoints(pts, sid)
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
				if batchErr != nil {
					sendMu.Lock()
					if sendErr == nil {
						sendErr = status.Errorf(codes.Internal, "failed to upsert batch for shard %d: %v", sid, batchErr)
					}
					sendMu.Unlock()
					return
				}
			}(addr, shardID, points)
		}
	}

	sendWg.Wait()
	if sendErr != nil {
		return nil, sendErr
	}

	return &pb.UpsertResponse{Upserted: int32(len(req.Points))}, nil
}

func (s *gatewayServer) Search(ctx context.Context, req *pb.SearchRequest) (*pb.SearchResponse, error) {
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
			s.searchCache.Set(req, cached)
			return cached, nil
		}
	}

	// Check cache for identical queries to avoid redundant computation
	if cached, found := s.searchCache.Get(req); found {
		return cached, nil
	}

	flightKey := computeSearchCacheKey(req)
	if resp, err, shared := s.waitForInFlight(ctx, flightKey); shared {
		return resp, err
	}
	resp, err := s.searchUncached(ctx, req, warmupKey, warmupKeySet)
	s.finishInFlight(flightKey, resp, err)
	return resp, err
}

func (s *gatewayServer) searchUncached(ctx context.Context, req *pb.SearchRequest, warmupKey uint64, warmupKeySet bool) (*pb.SearchResponse, error) {
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

	placementClient, err := s.getPlacementClient()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "could not get placement driver client: %v", err)
	}
	var workerAddresses []string
	if cachedAddresses, ok := s.workerListCache.Get(req.Collection); ok {
		workerAddresses = cachedAddresses
	} else {
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
		workerAddresses = workerListResp.GetGrpcAddresses()
		s.workerListCache.Set(req.Collection, workerAddresses)
	}
	if len(workerAddresses) == 0 {
		return &pb.SearchResponse{}, nil // No workers, no results
	}

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		topResults = &searchResultHeap{}
		errs       []error
	)
	heap.Init(topResults)

	searchSem := make(chan struct{}, adaptiveConcurrency(2, 32))
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

			linearizable := cfg.SearchLinearizable
			if v, ok := cfg.SearchConsistencyOverrides[req.Collection]; ok {
				linearizable = v
			}
			workerReq := translator.ToWorkerSearchRequest(req, 0, linearizable) // ShardID 0 for broadcast search
			workerReq.K = int32(workerTopK)

			res, err := workerClient.Search(ctx, workerReq)
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

	if len(errs) > 0 {
		// For now, return the first error. In a production system,
		// you might want to log all errors and potentially return partial results.
		return nil, status.Errorf(codes.Internal, "failed to search all workers: %v", errs[0])
	}

	results := searchResultSlicePool.Get().([]*pb.SearchResult)
	results = results[:topResults.Len()]
	for i := len(results) - 1; i >= 0; i-- {
		results[i] = heap.Pop(topResults).(*pb.SearchResult)
	}
	searchResponse := &pb.SearchResponse{Results: results}

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
		s.searchCache.Set(req, searchResponse)
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
		s.searchCache.Set(req, searchResponse)
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
		s.searchCache.Set(req, searchResponse)
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
	s.searchCache.Set(req, finalResponse)
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
	result, err := s.forwardToWorker(ctx, req.Collection, req.Id, func(c workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
		workerReq := translator.ToWorkerGetVectorRequest(req, shardID)
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
	result, err := s.forwardToWorker(ctx, req.Collection, req.Id, func(c workerpb.WorkerServiceClient, shardID uint64) (interface{}, error) {
		workerReq := translator.ToWorkerDeleteVectorRequest(req, shardID)
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

	// Create the gateway server with the list of PD addresses
	server := &gatewayServer{
		pdAddrs:                strings.Split(config.PlacementDriver, ","),
		authClient:             authClient,
		rerankerClient:         rerankerClient,
		feedbackService:        feedbackService,
		workerPool:             NewWorkerConnPool(config.GRPCEnableCompression), // Initialize worker connection pool for reuse
		searchCache:            NewSearchCache(5*time.Second, 1000),             // Cache search results for 5s, max 1000 entries
		workerListCache:        NewWorkerListCache(2 * time.Second),
		resolveCache:           NewWorkerResolveCache(5*time.Second, 10000),
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
	grpcOpts := append(
		grpcServerOptions(),
		grpc.ChainUnaryInterceptor(
			middleware.AuthInterceptor(authClient, config.JWTSecret),
			middleware.LoggingInterceptor,
			middleware.RateLimitInterceptor(config.RateLimitRPS),
		),
	)
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
