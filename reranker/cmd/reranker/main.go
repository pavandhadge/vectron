package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
	"github.com/pavandhadge/vectron/reranker/internal/cache"
	"github.com/pavandhadge/vectron/reranker/internal/strategies/rule"
	pb "github.com/pavandhadge/vectron/shared/proto/reranker"
	"google.golang.org/grpc"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

const (
	defaultPort         = "50051"
	defaultStrategy     = "rule" // "rule", "llm", or "rl"
	defaultCacheBackend = "memory"
)

func main() {
	// Parse command-line flags
	port := flag.String("port", getEnv("RERANKER_PORT", defaultPort), "gRPC server port")
	strategyType := flag.String("strategy", getEnv("RERANKER_STRATEGY", defaultStrategy), "Reranking strategy (rule/llm/rl)")
	cacheBackend := flag.String("cache", getEnv("CACHE_BACKEND", defaultCacheBackend), "Cache backend (memory/redis)")
	flag.Parse()

	log.Printf("Starting Reranker Service on port %s", *port)
	log.Printf("Strategy: %s, Cache: %s", *strategyType, *cacheBackend)

	// Initialize components
	logger := createLogger()
	cacheInstance, err := createCache(*cacheBackend, logger)
	if err != nil {
		log.Fatalf("Failed to create cache: %v", err)
	}
	metrics := createMetrics()
	strategy := createStrategy(*strategyType, logger)

	// Create gRPC server
	grpcServer := internal.NewGrpcServer(strategy, cacheInstance, logger, metrics)

	// Start gRPC listener
	// Handle both port-only (e.g., "50051") and full address (e.g., "127.0.0.1:50051")
	listenAddr := *port
	if !strings.Contains(listenAddr, ":") {
		listenAddr = ":" + listenAddr
	}
	lis, err := net.Listen("tcp", listenAddr)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}

	server := grpc.NewServer(
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.ReadBufferSize(64*1024),
		grpc.WriteBufferSize(64*1024),
		grpc.MaxConcurrentStreams(1024),
	)
	pb.RegisterRerankServiceServer(server, grpcServer)

	// Enable reflection for grpcurl/debugging
	reflection.Register(server)

	log.Printf("Reranker gRPC server listening on :%s", *port)

	// Graceful shutdown
	go func() {
		if err := server.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
	server.GracefulStop()
	log.Println("Server stopped")
}

// createCache initializes the cache implementation based on config.
func createCache(backend string, logger internal.Logger) (internal.Cache, error) {
	factory := cache.NewCacheFactory(logger)
	return factory.CreateCache(backend)
}

// createLogger initializes the logger implementation.
func createLogger() internal.Logger {
	return &SimpleLogger{}
}

// createMetrics initializes the metrics collector.
func createMetrics() internal.MetricsCollector {
	return &SimpleMetrics{}
}

// createStrategy initializes the reranking strategy.
func createStrategy(strategyType string, logger internal.Logger) internal.Strategy {
	switch strategyType {
	case "rule":
		log.Println("Initializing rule-based reranking strategy")

		// Try to load config from file, fall back to env vars
		var config rule.Config
		configPath := getEnv("RULE_CONFIG_PATH", "")

		if configPath != "" {
			log.Printf("Loading rule config from: %s", configPath)
			var err error
			config, err = rule.LoadConfigFromJSON(configPath)
			if err != nil {
				log.Printf("Failed to load config file, using env vars: %v", err)
				config = rule.LoadConfigFromEnv()
			}
		} else {
			config = rule.LoadConfigFromEnv()
		}

		// Validate config
		if err := rule.ValidateConfig(config); err != nil {
			log.Printf("Warning: config validation failed: %v", err)
		}

		log.Printf("Rule strategy config: ExactBoost=%.2f, TitleBoost=%.2f, TFIDF=%.2f, Original=%.2f",
			config.ExactMatchBoost, config.TitleBoost, config.TFIDFWeight, config.OriginalWeight)

		return rule.NewStrategy(config)

	case "llm":
		// TODO: Implement LLM strategy
		log.Println("LLM strategy not yet implemented, falling back to stub")
		return &StubStrategy{name: "llm-stub", version: "0.1.0"}
	case "rl":
		// TODO: Implement RL strategy
		log.Println("RL strategy not yet implemented, falling back to stub")
		return &StubStrategy{name: "rl-stub", version: "0.1.0"}
	default:
		log.Printf("Unknown strategy '%s', using rule-based as default", strategyType)
		return rule.NewStrategy(rule.DefaultConfig())
	}
}

// getEnv retrieves an environment variable with a fallback default.
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// ==============================
// Stub Implementations (Temporary)
// ==============================

// SimpleLogger is a basic logger implementation.
type SimpleLogger struct{}

func (l *SimpleLogger) Info(msg string, fields map[string]interface{}) {
	log.Printf("[INFO] %s %v", msg, fields)
}

func (l *SimpleLogger) Error(msg string, err error, fields map[string]interface{}) {
	log.Printf("[ERROR] %s: %v %v", msg, err, fields)
}

func (l *SimpleLogger) Debug(msg string, fields map[string]interface{}) {
	log.Printf("[DEBUG] %s %v", msg, fields)
}

// SimpleMetrics is a basic metrics collector.
type SimpleMetrics struct{}

func (m *SimpleMetrics) RecordLatency(strategy string, latency time.Duration) {
	log.Printf("[METRIC] Strategy: %s, Latency: %v", strategy, latency)
}

func (m *SimpleMetrics) RecordCacheHit(strategy string) {
	log.Printf("[METRIC] Cache hit for strategy: %s", strategy)
}

func (m *SimpleMetrics) RecordCacheMiss(strategy string) {
	log.Printf("[METRIC] Cache miss for strategy: %s", strategy)
}

func (m *SimpleMetrics) RecordError(strategy string, errType string) {
	log.Printf("[METRIC] Error in strategy: %s, type: %s", strategy, errType)
}

// StubStrategy is a placeholder strategy implementation.
type StubStrategy struct {
	name    string
	version string
}

func (s *StubStrategy) Name() string    { return s.name }
func (s *StubStrategy) Version() string { return s.version }

func (s *StubStrategy) Rerank(ctx context.Context, req *internal.RerankInput) (*internal.RerankOutput, error) {
	start := time.Now()

	// Stub: Just return candidates in original order with unchanged scores
	results := make([]internal.ScoredCandidate, len(req.Candidates))
	for i, c := range req.Candidates {
		results[i] = internal.ScoredCandidate{
			ID:            c.ID,
			RerankScore:   c.Score,
			OriginalScore: c.Score,
			Explanation:   "stub implementation - no reranking applied",
		}
	}

	// Limit to TopN
	if req.TopN < len(results) {
		results = results[:req.TopN]
	}

	return &internal.RerankOutput{
		Results:  results,
		Latency:  time.Since(start),
		Metadata: map[string]string{"strategy": s.name},
	}, nil
}

func (s *StubStrategy) Config() map[string]string {
	return map[string]string{
		"type":        "stub",
		"description": "Placeholder strategy for testing",
	}
}
