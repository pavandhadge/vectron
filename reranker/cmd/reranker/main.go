package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/pavandhadge/vectron/reranker/internal"
	"github.com/pavandhadge/vectron/reranker/internal/cache"
	"github.com/pavandhadge/vectron/reranker/internal/strategies/rule"
	pd "github.com/pavandhadge/vectron/shared/proto/placementdriver"
	pb "github.com/pavandhadge/vectron/shared/proto/reranker"
	"github.com/pavandhadge/vectron/shared/runtimeutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

const (
	defaultPort         = "50051"
	defaultStrategy     = "rule" // "rule", "llm", or "rl"
	defaultCacheBackend = "memory"
	grpcReadBufferSize  = 256 * 1024
	grpcWriteBufferSize = 256 * 1024
	grpcWindowSize      = 4 << 20
)

func main() {
	runtimeutil.LoadServiceEnv("reranker")
	runtimeutil.ConfigureGOMAXPROCS("reranker")
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
		grpc.ReadBufferSize(grpcReadBufferSize),
		grpc.WriteBufferSize(grpcWriteBufferSize),
		grpc.InitialWindowSize(grpcWindowSize),
		grpc.InitialConnWindowSize(grpcWindowSize),
		grpc.MaxConcurrentStreams(4096),
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

	// Register with Placement Driver (best-effort)
	pdAddrs := getPDAddrs()
	if len(pdAddrs) > 0 {
		advertiseAddr := getRerankerAdvertiseAddr(*port)
		go startPDHeartbeat(pdAddrs, advertiseAddr)
	} else {
		log.Println("Placement Driver addresses not configured; skipping reranker registration")
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down gracefully...")
	server.GracefulStop()
	log.Println("Server stopped")
}

func getPDAddrs() []string {
	raw := strings.TrimSpace(os.Getenv("RERANKER_PD_ADDRS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("PD_ADDRS"))
	}
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("PLACEMENT_DRIVER"))
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func getRerankerAdvertiseAddr(port string) string {
	if addr := strings.TrimSpace(os.Getenv("RERANKER_ADVERTISE_ADDR")); addr != "" {
		return addr
	}
	host := strings.TrimSpace(os.Getenv("RERANKER_HOST"))
	if host == "" {
		host = "localhost"
	}
	if strings.Contains(port, ":") {
		return port
	}
	return host + ":" + port
}

func startPDHeartbeat(pdAddrs []string, advertiseAddr string) {
	heartbeatInterval := 5 * time.Second
	var rerankerID string
	var client pd.PlacementServiceClient
	var conn *grpc.ClientConn

	register := func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		for _, addr := range pdAddrs {
			c, err := grpc.DialContext(ctx, addr,
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
			)
			if err != nil {
				log.Printf("Failed to connect to PD at %s: %v", addr, err)
				continue
			}
			cli := pd.NewPlacementServiceClient(c)

			cpuCores, memBytes, diskBytes := collectCapacityMetrics()
			rack, zone, region := collectFailureDomain()
			resp, err := cli.RegisterReranker(ctx, &pd.RegisterRerankerRequest{
				GrpcAddress: advertiseAddr,
				CpuCores:    cpuCores,
				MemoryBytes: memBytes,
				DiskBytes:   diskBytes,
				Rack:        rack,
				Zone:        zone,
				Region:      region,
				Timestamp:   time.Now().Unix(),
			})
			if err != nil || resp.GetRerankerId() == "" {
				log.Printf("Failed to register reranker with PD at %s: %v", addr, err)
				c.Close()
				continue
			}

			if conn != nil {
				conn.Close()
			}
			conn = c
			client = cli
			rerankerID = resp.GetRerankerId()
			log.Printf("Registered reranker with PD at %s as ID %s (addr=%s)", addr, rerankerID, advertiseAddr)
			return true
		}
		return false
	}

	if !register() {
		log.Println("Failed to register reranker with any PD; will retry in background")
	}

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for range ticker.C {
		if client == nil || rerankerID == "" {
			if !register() {
				continue
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := client.RerankerHeartbeat(ctx, &pd.RerankerHeartbeatRequest{
			RerankerId: rerankerID,
			Timestamp:  time.Now().Unix(),
		})
		cancel()
		if err != nil {
			log.Printf("Reranker heartbeat failed: %v", err)
			rerankerID = ""
		}
	}
}

// collectCapacityMetrics gathers system capacity information.
func collectCapacityMetrics() (cpuCores int32, memoryBytes, diskBytes int64) {
	cpuCores = int32(runtime.NumCPU())

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	memoryBytes = int64(m.Sys)

	diskBytes = 500 * 1024 * 1024 * 1024
	return
}

// collectFailureDomain gathers failure domain information from environment.
func collectFailureDomain() (rack, zone, region string) {
	rack = os.Getenv("VECTRON_RACK")
	if rack == "" {
		rack = "default-rack"
	}
	zone = os.Getenv("VECTRON_ZONE")
	if zone == "" {
		zone = "default-zone"
	}
	region = os.Getenv("VECTRON_REGION")
	if region == "" {
		region = "default-region"
	}
	return
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

		log.Printf("Rule strategy config: Original=%.2f, BM25=%.2f, Keyword=%.2f, Coverage=%.2f, Recency=%.2f",
			config.OriginalWeight, config.BM25Weight, config.KeywordWeight, config.CoverageWeight, config.RecencyWeight)

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
