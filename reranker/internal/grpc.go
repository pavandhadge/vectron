// Package internal implements the gRPC server for the Reranker service.
package internal

import (
	"context"
	"log"
	"time"

	pb "github.com/pavandhadge/vectron/shared/proto/reranker"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GrpcServer implements the RerankService gRPC interface.
type GrpcServer struct {
	pb.UnimplementedRerankServiceServer
	strategy Strategy
	cache    Cache
	logger   Logger
	metrics  MetricsCollector
}

// NewGrpcServer creates a new gRPC server instance.
func NewGrpcServer(strategy Strategy, cache Cache, logger Logger, metrics MetricsCollector) *GrpcServer {
	if logger == nil {
		logger = noopLogger{}
	}
	if metrics == nil {
		metrics = noopMetrics{}
	}
	return &GrpcServer{
		strategy: strategy,
		cache:    cache,
		logger:   logger,
		metrics:  metrics,
	}
}

// Rerank implements the main reranking RPC.
func (s *GrpcServer) Rerank(ctx context.Context, req *pb.RerankRequest) (*pb.RerankResponse, error) {
	start := time.Now()
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	// Convert proto request to internal format
	input := &RerankInput{
		Query:      req.GetQuery(),
		Candidates: make([]Candidate, len(req.GetCandidates())),
		TopN:       int(req.GetTopN()),
		Options:    req.GetOptions(),
	}

	// Extract candidate IDs for cache key generation
	candidateIDs := make([]string, len(req.GetCandidates()))
	for i, c := range req.GetCandidates() {
		input.Candidates[i] = Candidate{
			ID:       c.GetId(),
			Score:    c.GetScore(),
			Vector:   c.GetVector(),
			Metadata: c.GetMetadata(),
		}
		candidateIDs[i] = c.GetId()
	}

	// Validate input
	if err := ValidateRerankInput(input); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid input: %v", err)
	}

	// Check cache
	cacheKey := CacheKeyWithOptions(req.GetQuery(), candidateIDs, req.GetOptions())
	if s.cache != nil {
		if cached, ok := s.cache.Get(cacheKey); ok {
			s.metrics.RecordCacheHit(s.strategy.Name())
			return s.buildResponse(cached, true, time.Since(start)), nil
		}
		s.metrics.RecordCacheMiss(s.strategy.Name())
	}

	// Execute reranking strategy
	output, err := s.strategy.Rerank(ctx, input)
	if err != nil {
		s.metrics.RecordError(s.strategy.Name(), "rerank_failed")
		s.logger.Error("reranking failed", err, map[string]interface{}{
			"strategy": s.strategy.Name(),
			"query":    req.GetQuery(),
		})
		return nil, status.Errorf(codes.Internal, "reranking failed: %v", err)
	}

	if output == nil {
		s.metrics.RecordError(s.strategy.Name(), "rerank_nil_output")
		return nil, status.Error(codes.Internal, "reranking failed: empty output")
	}

	// Store in cache
	if s.cache != nil {
		s.cache.Set(cacheKey, output, 0)
	}

	// Record metrics
	s.metrics.RecordLatency(s.strategy.Name(), output.Latency)

	return s.buildResponse(output, false, time.Since(start)), nil
}

// GetStrategy returns information about the active reranking strategy.
func (s *GrpcServer) GetStrategy(ctx context.Context, req *pb.GetStrategyRequest) (*pb.GetStrategyResponse, error) {
	return &pb.GetStrategyResponse{
		StrategyName: s.strategy.Name(),
		Version:      s.strategy.Version(),
		Config:       s.strategy.Config(),
	}, nil
}

// InvalidateCache clears cached reranking results.
func (s *GrpcServer) InvalidateCache(ctx context.Context, req *pb.InvalidateCacheRequest) (*pb.InvalidateCacheResponse, error) {
	if s.cache == nil {
		return &pb.InvalidateCacheResponse{EntriesCleared: 0}, nil
	}

	var cleared int32
	if req.GetQuery() != "" && len(req.GetCandidateIds()) > 0 {
		// Invalidate specific query
		key := CacheKey(req.GetQuery(), req.GetCandidateIds())
		s.cache.Delete(key)
		cleared = 1
	} else {
		// Clear all cache entries
		cleared = int32(s.cache.Clear("*"))
	}

	s.logger.Info("cache invalidated", map[string]interface{}{
		"entries_cleared": cleared,
		"query":           req.GetQuery(),
	})

	return &pb.InvalidateCacheResponse{EntriesCleared: cleared}, nil
}

// buildResponse converts internal output to proto response.
func (s *GrpcServer) buildResponse(output *RerankOutput, fromCache bool, totalLatency time.Duration) *pb.RerankResponse {
	results := make([]*pb.ScoredCandidate, len(output.Results))
	for i, r := range output.Results {
		results[i] = &pb.ScoredCandidate{
			Id:            r.ID,
			RerankScore:   r.RerankScore,
			OriginalScore: r.OriginalScore,
			Explanation:   r.Explanation,
		}
	}

	return &pb.RerankResponse{
		Results:      results,
		StrategyUsed: s.strategy.Name(),
		FromCache:    fromCache,
		LatencyMs:    totalLatency.Milliseconds(),
	}
}

// SetStrategy allows hot-swapping the reranking strategy (for A/B testing or upgrades).
func (s *GrpcServer) SetStrategy(strategy Strategy) {
	log.Printf("Switching reranking strategy from %s to %s", s.strategy.Name(), strategy.Name())
	s.strategy = strategy
}
