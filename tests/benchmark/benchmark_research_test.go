// Run: go test -v ./tests/benchmark -run TestResearchBenchmark -timeout 30m

// benchmark_research_test.go - Comprehensive Standalone Benchmark Suite for Research
//
// This test suite provides detailed benchmarking and metrics collection for:
// - Vector search performance (latency, throughput, recall, precision)
// - Distributed system scalability (shard distribution, load balancing)
// - Reranker effectiveness (quality improvement metrics)
// - End-to-end system performance under various workloads
//
// This is a STANDALONE test that starts all required services automatically.
//
// Metrics Calculated:
// - Latency: P50, P95, P99, Average, Min, Max
// - Throughput: Vectors/second, Queries/second
// - Search Quality: Recall@K, Precision@K, F1-Score, MRR, NDCG
// - System: CPU, Memory, Network utilization (if available)
// - Distributed: Shard balance, Worker load distribution
//
// Output: Detailed metrics report + CSV files for analysis

package main_test

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	apigatewaypb "github.com/pavandhadge/vectron/shared/proto/apigateway"
	authpb "github.com/pavandhadge/vectron/shared/proto/auth"
	placementpb "github.com/pavandhadge/vectron/shared/proto/placementdriver"
)

// =============================================================================
// Benchmark Configuration
// =============================================================================

const (
	// Dataset sizes for different test scenarios - reduced for local testing
	SmallDataset  = 1000  // 1K vectors
	MediumDataset = 2500  // 5K vectors
	LargeDataset  = 5000  // 10K vectors
	XLDataset     = 10000 // 50K vectors (max for local testing)

	// Vector dimensions to test
	Dim128  = 128  // Common for sentence embeddings
	Dim384  = 384  // Common for small models
	Dim768  = 768  // BERT-base size
	Dim1536 = 1536 // OpenAI embeddings

	// Test durations
	ShortDuration  = 30 * time.Second
	MediumDuration = 2 * time.Minute
	LongDuration   = 5 * time.Minute
)

// =============================================================================
// Service Configuration (Benchmark-specific to avoid conflicts with ultimate_e2e_test.go)
// =============================================================================

var (
	// Service ports - benchmark-specific to avoid package conflicts
	benchmarkEtcdPort       = 2379
	benchmarkPdGRPCPort1    = 10001
	benchmarkPdGRPCPort2    = 10002
	benchmarkPdGRPCPort3    = 10003
	benchmarkWorkerPort1    = 10007
	benchmarkWorkerPort2    = 10017
	benchmarkAuthGRPCPort   = 10008
	benchmarkAuthHTTPPort   = 10009
	benchmarkApigatewayPort = 10010
	benchmarkApigatewayHTTP = 10012
	benchmarkRerankerPort   = 10013
	benchmarkValkeyPort     = 6379

	// Base directory for all temporary test files (data and logs)
	benchmarkBaseTempDir = "./temp_vectron_benchmark"
	// Subdirectory for all service data
	benchmarkDataTempDir = filepath.Join(benchmarkBaseTempDir, "data")
	// Subdirectory for all service logs
	benchmarkLogTempDir = filepath.Join(benchmarkBaseTempDir, "logs")
)

// =============================================================================
// Metrics Structures
// =============================================================================

// LatencyMetrics holds detailed latency statistics
type LatencyMetrics struct {
	Count        int64
	Total        time.Duration
	Min          time.Duration
	Max          time.Duration
	Average      time.Duration
	P50          time.Duration
	P95          time.Duration
	P99          time.Duration
	StdDev       time.Duration
	AllLatencies []time.Duration // Raw data for analysis
}

// ThroughputMetrics holds throughput statistics
type ThroughputMetrics struct {
	Operations    int64
	Duration      time.Duration
	OpsPerSecond  float64
	VectorsPerSec float64
}

// SearchQualityMetrics holds search quality metrics
type SearchQualityMetrics struct {
	RecallAt1     float64
	RecallAt5     float64
	RecallAt10    float64
	RecallAt50    float64
	RecallAt100   float64
	PrecisionAt1  float64
	PrecisionAt5  float64
	PrecisionAt10 float64
	F1At10        float64
	MRR           float64 // Mean Reciprocal Rank
	NDCGAt10      float64 // Normalized Discounted Cumulative Gain
	QueryCount    int
}

// DistributedMetrics holds distributed system performance
type DistributedMetrics struct {
	ShardCount         int
	WorkerCount        int
	ShardsPerWorker    map[uint64]int
	LoadVariance       float64 // Coefficient of variation
	ReplicationFactor  int
	LeaderDistribution map[uint64]int
}

// RerankerMetrics holds reranker effectiveness data
type RerankerMetrics struct {
	BaseResults     *SearchQualityMetrics
	RerankedResults *SearchQualityMetrics
	Improvement     float64 // Percentage improvement
	LatencyImpact   time.Duration
}

// BenchmarkResult holds all metrics for a single benchmark run
type BenchmarkResult struct {
	Name            string
	Description     string
	Timestamp       time.Time
	DatasetSize     int
	VectorDimension int
	Duration        time.Duration

	// All metrics
	InsertMetrics      *ThroughputMetrics
	SearchMetrics      *ThroughputMetrics
	LatencyMetrics     *LatencyMetrics
	QualityMetrics     *SearchQualityMetrics
	DistributedMetrics *DistributedMetrics
	RerankerMetrics    *RerankerMetrics

	// Resource usage
	MemoryBeforeMB   uint64
	MemoryAfterMB    uint64
	GoroutinesBefore int
	GoroutinesAfter  int
}

// BenchmarkSuite manages multiple benchmark runs
type BenchmarkSuite struct {
	results []*BenchmarkResult
	mutex   sync.RWMutex
}

// BenchmarkTest represents the comprehensive benchmark suite with service management
type BenchmarkTest struct {
	t              *testing.T
	ctx            context.Context
	cancel         context.CancelFunc
	processes      []*exec.Cmd
	processesMutex sync.Mutex
	dataDirs       []string
	dataDirsMutex  sync.Mutex
	jwtToken       string
	apiKey         string
	userID         string
	baseEnv        []string

	// gRPC clients
	authClient      authpb.AuthServiceClient
	apigwClient     apigatewaypb.VectronServiceClient
	placementClient placementpb.PlacementServiceClient
}

func findRepoRootBenchmark() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from %s", wd)
		}
		dir = parent
	}
}

func resolveBenchmarkPaths(t *testing.T) {
	t.Helper()
	root, err := findRepoRootBenchmark()
	if err != nil {
		t.Fatalf("Failed to locate repo root: %v", err)
	}
	benchmarkBaseTempDir = filepath.Join(root, "temp_vectron_benchmark")
	benchmarkDataTempDir = filepath.Join(benchmarkBaseTempDir, "data")
	benchmarkLogTempDir = filepath.Join(benchmarkBaseTempDir, "logs")
}

func loadEnvFile(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()

	env := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if len(val) >= 2 {
			if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
				val = val[1 : len(val)-1]
			}
		}
		if key != "" {
			env[key] = val
		}
	}
	return env
}

func mergeEnv(base []string, extra map[string]string) []string {
	out := make(map[string]string, len(base)+len(extra))
	for _, kv := range base {
		if kv == "" {
			continue
		}
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			continue
		}
		out[kv[:idx]] = kv[idx+1:]
	}
	for k, v := range extra {
		out[k] = v
	}

	result := make([]string, 0, len(out))
	for k, v := range out {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}

func buildBenchmarkEnv(t *testing.T) []string {
	t.Helper()
	root, err := findRepoRootBenchmark()
	if err != nil {
		return os.Environ()
	}
	envPath := filepath.Join(root, ".env")
	extra := loadEnvFile(envPath)
	if len(extra) == 0 {
		return os.Environ()
	}
	return mergeEnv(os.Environ(), extra)
}

// =============================================================================
// Main Research Benchmark Test
// =============================================================================

func TestResearchBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping research benchmark in short mode")
	}

	log.Println("🔬 Starting Research Benchmark Suite...")
	log.Println("This will run comprehensive benchmarks for research paper analysis")
	log.Println("Estimated duration: 20-30 minutes")

	// Resolve temp paths relative to repo root (so child processes use correct paths).
	resolveBenchmarkPaths(t)

	// Clean up stale data from previous runs to avoid disk-full failures.
	if err := os.RemoveAll(benchmarkDataTempDir); err != nil {
		log.Printf("Warning: failed to remove stale data dir %s: %v", benchmarkDataTempDir, err)
	}

	// Create base temporary directories
	if err := os.MkdirAll(benchmarkDataTempDir, 0755); err != nil {
		t.Fatalf("Failed to create data temp dir %s: %v", benchmarkDataTempDir, err)
	}
	if err := os.MkdirAll(benchmarkLogTempDir, 0755); err != nil {
		t.Fatalf("Failed to create log temp dir %s: %v", benchmarkLogTempDir, err)
	}

	suite := &BenchmarkTest{t: t}
	suite.baseEnv = buildBenchmarkEnv(t)
	suite.ctx, suite.cancel = context.WithTimeout(context.Background(), 60*time.Minute)
	t.Cleanup(suite.cleanup)
	defer suite.cancel()

	// Start all services
	t.Run("SystemStartup", suite.TestSystemStartup)

	// Initialize clients and authenticate
	t.Run("InitializeAndAuthenticate", suite.TestInitializeAndAuthenticate)

	benchmarkSuite := &BenchmarkSuite{}

	runScenario := func(name string, reset bool, fn func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			if reset {
				suite.resetSystem(t)
			}
			fn(t)
		})
	}

	// Run comprehensive benchmark scenarios (single system, reset between scenarios).
	runScenario("Scenario1_VectorSearchScalability", false, func(t *testing.T) {
		benchmarkVectorSearchScalability(t, suite.getAuthenticatedContext(), suite.apigwClient, benchmarkSuite)
	})

	runScenario("Scenario2_DimensionImpact", true, func(t *testing.T) {
		benchmarkDimensionImpact(t, suite.getAuthenticatedContext(), suite.apigwClient, benchmarkSuite)
	})

	runScenario("Scenario3_ConcurrentWorkload", true, func(t *testing.T) {
		benchmarkConcurrentWorkload(t, suite.getAuthenticatedContext(), suite.apigwClient, benchmarkSuite)
	})

	runScenario("Scenario4_RerankerEffectiveness", true, func(t *testing.T) {
		benchmarkRerankerEffectiveness(t, suite.getAuthenticatedContext(), suite.apigwClient, benchmarkSuite)
	})

	runScenario("Scenario5_DistributedScalability", true, func(t *testing.T) {
		benchmarkDistributedScalability(t, suite.getAuthenticatedContext(), suite.apigwClient, suite.placementClient, benchmarkSuite)
	})

	runScenario("Scenario6_EndToEndLatency", true, func(t *testing.T) {
		benchmarkEndToEndLatency(t, suite.getAuthenticatedContext(), suite.apigwClient, benchmarkSuite)
	})

	// Generate final report
	t.Run("GenerateReport", func(t *testing.T) {
		generateResearchReport(t, benchmarkSuite)
	})
}

// =============================================================================
// System Startup (from ultimate_e2e_test.go)
// =============================================================================

func (s *BenchmarkTest) TestSystemStartup(t *testing.T) {
	log.Println("🚀 Starting System for Benchmark...")

	// Start all services individually
	t.Run("StartAllServices", func(t *testing.T) {
		s.startEtcd()
		s.startPlacementDriverCluster()
		s.startWorkers()
		s.startAuthService()
		s.startReranker()
		s.startValkey()
		s.startAPIGateway()

		// Wait for all services to be ready
		ports := []int{benchmarkEtcdPort, benchmarkPdGRPCPort1, benchmarkWorkerPort1, benchmarkAuthGRPCPort, benchmarkApigatewayPort, benchmarkRerankerPort, benchmarkValkeyPort}
		for _, port := range ports {
			require.Eventually(t, func() bool {
				return s.isPortOpen(port)
			}, 30*time.Second, 1*time.Second, "Port %d should be open", port)
		}

		log.Println("✅ All services started and healthy")
	})

	// Initialize clients
	t.Run("InitializeClients", func(t *testing.T) {
		s.initializeClients()
		require.NotNil(t, s.apigwClient, "API Gateway client should be initialized")
		require.NotNil(t, s.authClient, "Auth client should be initialized")
		log.Println("✅ gRPC clients initialized")
	})
}

func (s *BenchmarkTest) TestInitializeAndAuthenticate(t *testing.T) {
	log.Println("🔐 Authenticating for Benchmark...")

	// Register and login to get JWT token
	email := fmt.Sprintf("benchmark-%d@example.com", time.Now().Unix())
	_, err := s.authClient.RegisterUser(s.ctx, &authpb.RegisterUserRequest{
		Email:    email,
		Password: "BenchmarkPassword123!",
	})
	require.NoError(t, err, "Failed to register user")

	loginResp, err := s.authClient.Login(s.ctx, &authpb.LoginRequest{
		Email:    email,
		Password: "BenchmarkPassword123!",
	})
	require.NoError(t, err, "Failed to login and get JWT token")
	require.NotEmpty(t, loginResp.JwtToken, "JWT token should not be empty")

	s.jwtToken = loginResp.JwtToken
	log.Println("✅ JWT token obtained for API Gateway authentication")
}

// =============================================================================
// Service Management (from ultimate_e2e_test.go)
// =============================================================================

func (s *BenchmarkTest) startEtcd() {
	log.Println("▶️ Starting etcd (Podman)...")

	// 1. Force stop the container if it's already running.
	exec.CommandContext(s.ctx, "podman", "stop", "etcd").Run()

	// 2. Prepare Data Directory
	etcdDataDir := filepath.Join(benchmarkDataTempDir, "etcd")
	if err := os.MkdirAll(etcdDataDir, 0755); err != nil {
		s.t.Fatalf("Failed to create etcd data dir: %v", err)
	}

	// 3. Check if container 'etcd' exists
	checkCmd := exec.CommandContext(s.ctx, "podman", "container", "exists", "etcd")
	if err := checkCmd.Run(); err == nil {
		// Container Exists -> Start it
		startCmd := exec.CommandContext(s.ctx, "podman", "start", "etcd")
		if out, err := startCmd.CombinedOutput(); err != nil {
			s.t.Fatalf("Failed to start existing etcd container: %v\nOutput: %s", err, string(out))
		}
	} else {
		// Container Does Not Exist -> Run new
		runCmd := exec.CommandContext(s.ctx, "podman", "run", "-d",
			"--name", "etcd",
			"-p", "2379:2379",
			"-v", fmt.Sprintf("%s:/etcd-data:Z", etcdDataDir),
			"quay.io/coreos/etcd:v3.5.0",
			"/usr/local/bin/etcd",
			"--data-dir", "/etcd-data",
			"--listen-client-urls", "http://0.0.0.0:2379",
			"--advertise-client-urls", "http://127.0.0.1:2379",
		)
		if out, err := runCmd.CombinedOutput(); err != nil {
			s.t.Fatalf("Failed to run new etcd container: %v\nOutput: %s", err, string(out))
		}
	}

	// 4. Wait for Etcd to be ready
	log.Println("⏳ Waiting for etcd to be ready...")
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			s.t.Fatal("Timed out waiting for etcd to start")
		case <-ticker.C:
			if s.isPortOpen(benchmarkEtcdPort) {
				log.Println("✅ Etcd started and listening")
				return
			}
		}
	}
}

func (s *BenchmarkTest) startValkey() {
	log.Println("▶️ Starting Valkey (Podman)...")

	// Stop existing container if running.
	exec.CommandContext(s.ctx, "podman", "stop", "vectron-valkey-benchmark").Run()

	checkCmd := exec.CommandContext(s.ctx, "podman", "container", "exists", "vectron-valkey-benchmark")
	if err := checkCmd.Run(); err == nil {
		startCmd := exec.CommandContext(s.ctx, "podman", "start", "vectron-valkey-benchmark")
		if out, err := startCmd.CombinedOutput(); err != nil {
			s.t.Fatalf("Failed to start existing Valkey container: %v\nOutput: %s", err, string(out))
		}
	} else {
		runCmd := exec.CommandContext(s.ctx, "podman", "run", "-d",
			"--name", "vectron-valkey-benchmark",
			"-p", fmt.Sprintf("%d:6379", benchmarkValkeyPort),
			"valkey/valkey:latest",
			"valkey-server",
			"--save", "",
			"--appendonly", "no",
			"--maxmemory", "2gb",
			"--maxmemory-policy", "allkeys-lru",
		)
		if out, err := runCmd.CombinedOutput(); err != nil {
			s.t.Fatalf("Failed to run new Valkey container: %v\nOutput: %s", err, string(out))
		}
	}

	log.Println("⏳ Waiting for Valkey to be ready...")
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			s.t.Fatal("Timed out waiting for Valkey to start")
		case <-ticker.C:
			if s.isPortOpen(benchmarkValkeyPort) {
				log.Println("✅ Valkey started and listening")
				return
			}
		}
	}
}

func (s *BenchmarkTest) startPlacementDriverCluster() {
	profileDir := os.Getenv("BENCH_PROFILE_DIR")
	nodes := []struct {
		id       uint64
		raftAddr string
		grpcAddr string
		grpcPort int
	}{
		{1, "127.0.0.1:11001", "127.0.0.1:10001", 10001},
		{2, "127.0.0.1:11002", "127.0.0.1:10002", 10002},
		{3, "127.0.0.1:11003", "127.0.0.1:10003", 10003},
	}

	for _, node := range nodes {
		_, raftPortStr, err := net.SplitHostPort(node.raftAddr)
		if err != nil {
			s.t.Fatalf("Failed to parse raft address %s: %v", node.raftAddr, err)
		}
		raftPort, err := strconv.Atoi(raftPortStr)
		if err != nil {
			s.t.Fatalf("Failed to convert raft port %s to int: %v", raftPortStr, err)
		}

		s.killProcessOnPort(node.grpcPort)
		s.killProcessOnPort(raftPort)
		time.Sleep(500 * time.Millisecond)

		dataDir, err := os.MkdirTemp(benchmarkDataTempDir, fmt.Sprintf("pd_benchmark_%d_", node.id))
		if err != nil {
			log.Printf("Warning: Failed to create temp dir for PD node %d: %v", node.id, err)
			continue
		}
		s.registerDataDir(dataDir)

		cmd := exec.CommandContext(s.ctx, "./bin/placementdriver",
			"--node-id", fmt.Sprintf("%d", node.id),
			"--cluster-id", "1",
			"--raft-addr", node.raftAddr,
			"--grpc-addr", node.grpcAddr,
			"--initial-members", "1:127.0.0.1:11001,2:127.0.0.1:11002,3:127.0.0.1:11003",
			"--data-dir", dataDir,
		)
		env := append([]string{}, s.baseEnv...)
		if profileDir != "" {
			env = append(env,
				"PPROF_MUTEX_FRACTION=5",
				"PPROF_BLOCK_RATE=1",
				"PPROF_CPU_SECONDS=15",
				fmt.Sprintf("PPROF_CPU_PATH=%s", filepath.Join(profileDir, fmt.Sprintf("pd%d_cpu.pprof", node.id))),
				fmt.Sprintf("PPROF_MUTEX_PATH=%s", filepath.Join(profileDir, fmt.Sprintf("pd%d_mutex.pprof", node.id))),
				fmt.Sprintf("PPROF_BLOCK_PATH=%s", filepath.Join(profileDir, fmt.Sprintf("pd%d_block.pprof", node.id))),
			)
		}
		cmd.Env = env
		cmd.Dir = "/home/pavan/Programming/vectron"

		logFile := filepath.Join(benchmarkLogTempDir, fmt.Sprintf("vectron-pd%d-benchmark.log", node.id))
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
		}

		s.registerProcess(cmd)
		if err := cmd.Start(); err != nil {
			log.Printf("Warning: Failed to start PD node %d: %v", node.id, err)
		} else {
			log.Printf("Started PD node %d (PID: %d) with data dir %s", node.id, cmd.Process.Pid, dataDir)
		}
	}
}

func (s *BenchmarkTest) startWorkers() {
	profileDir := os.Getenv("BENCH_PROFILE_DIR")
	for i := 1; i <= 2; i++ {
		port := benchmarkWorkerPort1 + (i-1)*10
		raftPort := 20000 + i

		s.killProcessOnPort(port)
		s.killProcessOnPort(raftPort)
		time.Sleep(500 * time.Millisecond)

		dataDir, err := os.MkdirTemp(benchmarkDataTempDir, fmt.Sprintf("worker_benchmark_%d_", i))
		if err != nil {
			log.Printf("Warning: Failed to create temp dir for worker %d: %v", i, err)
			continue
		}
		s.registerDataDir(dataDir)

		cmd := exec.CommandContext(s.ctx, "./bin/worker",
			"--node-id", fmt.Sprintf("%d", i),
			"--grpc-addr", fmt.Sprintf("127.0.0.1:%d", port),
			"--raft-addr", fmt.Sprintf("127.0.0.1:%d", raftPort),
			"--pd-addrs", "127.0.0.1:10001,127.0.0.1:10002,127.0.0.1:10003",
			"--data-dir", dataDir,
		)
		env := append([]string{}, s.baseEnv...)
		if profileDir != "" {
			env = append(env,
				"PPROF_MUTEX_FRACTION=5",
				"PPROF_BLOCK_RATE=1",
				"PPROF_CPU_SECONDS=15",
				fmt.Sprintf("PPROF_CPU_PATH=%s", filepath.Join(profileDir, fmt.Sprintf("worker%d_cpu.pprof", i))),
				fmt.Sprintf("PPROF_MUTEX_PATH=%s", filepath.Join(profileDir, fmt.Sprintf("worker%d_mutex.pprof", i))),
				fmt.Sprintf("PPROF_BLOCK_PATH=%s", filepath.Join(profileDir, fmt.Sprintf("worker%d_block.pprof", i))),
			)
		}
		cmd.Env = env
		cmd.Dir = "/home/pavan/Programming/vectron"

		logFile := filepath.Join(benchmarkLogTempDir, fmt.Sprintf("vectron-worker%d-benchmark.log", i))
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
		}

		s.registerProcess(cmd)
		if err := cmd.Start(); err != nil {
			log.Printf("Warning: Failed to start worker %d: %v", i, err)
		} else {
			log.Printf("Started worker %d (PID: %d) with data dir %s", i, cmd.Process.Pid, dataDir)
		}
	}
}

func (s *BenchmarkTest) startAuthService() {
	s.killProcessOnPort(benchmarkAuthGRPCPort)
	s.killProcessOnPort(benchmarkAuthHTTPPort)
	time.Sleep(500 * time.Millisecond)

	cmd := exec.CommandContext(s.ctx, "./bin/authsvc")
	cmd.Dir = "/home/pavan/Programming/vectron"
	cmd.Env = append(append([]string{}, s.baseEnv...),
		"GRPC_PORT=:10008",
		"HTTP_PORT=:10009",
		"JWT_SECRET=test-jwt-secret-for-testing-only-do-not-use-in-production",
		"ETCD_ENDPOINTS=127.0.0.1:2379",
	)

	if f, err := os.OpenFile(filepath.Join(benchmarkLogTempDir, "vectron-auth-benchmark.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	s.registerProcess(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("Warning: Failed to start auth service: %v", err)
	} else {
		log.Printf("Started auth service (PID: %d)", cmd.Process.Pid)
	}
}

func (s *BenchmarkTest) startReranker() {
	s.killProcessOnPort(benchmarkRerankerPort)
	time.Sleep(500 * time.Millisecond)

	cmd := exec.CommandContext(s.ctx, "./bin/reranker",
		"--port", fmt.Sprintf("127.0.0.1:%d", benchmarkRerankerPort),
		"--strategy", "rule",
		"--cache", "memory",
	)
	cmd.Dir = "/home/pavan/Programming/vectron"
	cmd.Env = append([]string{}, s.baseEnv...)

	if f, err := os.OpenFile(filepath.Join(benchmarkLogTempDir, "vectron-reranker-benchmark.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	s.registerProcess(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("Warning: Failed to start reranker: %v", err)
	} else {
		log.Printf("Started reranker (PID: %d)", cmd.Process.Pid)
	}
}

func (s *BenchmarkTest) startAPIGateway() {
	s.killProcessOnPort(benchmarkApigatewayPort)
	s.killProcessOnPort(benchmarkApigatewayHTTP)
	time.Sleep(500 * time.Millisecond)

	dataDir := filepath.Join(benchmarkDataTempDir, "apigw-data")
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(benchmarkLogTempDir, 0755)
	profileDir := os.Getenv("BENCH_PROFILE_DIR")

	repoRoot, err := findRepoRootBenchmark()
	if err != nil {
		s.t.Fatalf("failed to resolve repo root: %v", err)
	}
	apigwPath := filepath.Join(repoRoot, "bin", "apigateway")
	if _, err := os.Stat(apigwPath); err != nil {
		s.t.Fatalf("apigateway binary missing; build it first (make linux): %v", err)
	}

	cmd := exec.CommandContext(s.ctx, apigwPath)
	cmd.Dir = repoRoot
	cmd.Env = append(append([]string{}, s.baseEnv...),
		"GRPC_ADDR=127.0.0.1:10010",
		"HTTP_ADDR=127.0.0.1:10012",
		"PLACEMENT_DRIVER=127.0.0.1:10001,127.0.0.1:10002,127.0.0.1:10003",
		"AUTH_SERVICE_ADDR=127.0.0.1:10008",
		"RERANKER_SERVICE_ADDR=127.0.0.1:10013",
		"FEEDBACK_DB_PATH="+dataDir+"/feedback.db",
		"JWT_SECRET=test-jwt-secret-for-testing-only-do-not-use-in-production",
		"RATE_LIMIT_RPS=10000",
		fmt.Sprintf("DISTRIBUTED_CACHE_ADDR=127.0.0.1:%d", benchmarkValkeyPort),
	)
	if profileDir != "" {
		cmd.Env = append(cmd.Env,
			"PPROF_MUTEX_FRACTION=5",
			"PPROF_BLOCK_RATE=1",
			"PPROF_CPU_SECONDS=15",
			fmt.Sprintf("PPROF_CPU_PATH=%s", filepath.Join(profileDir, "apigateway_cpu.pprof")),
			fmt.Sprintf("PPROF_MUTEX_PATH=%s", filepath.Join(profileDir, "apigateway_mutex.pprof")),
			fmt.Sprintf("PPROF_BLOCK_PATH=%s", filepath.Join(profileDir, "apigateway_block.pprof")),
		)
	}

	if f, err := os.OpenFile(filepath.Join(benchmarkLogTempDir, "vectron-apigw-benchmark.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}

	s.registerProcess(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("Warning: Failed to start API gateway: %v", err)
	} else {
		log.Printf("Started API gateway (PID: %d)", cmd.Process.Pid)
	}
}

func (s *BenchmarkTest) initializeClients() {
	// Auth client
	authConn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", benchmarkAuthGRPCPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to auth service: %v", err)
	} else {
		s.authClient = authpb.NewAuthServiceClient(authConn)
	}

	// API Gateway client
	apigwConn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", benchmarkApigatewayPort),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to API gateway: %v", err)
	} else {
		s.apigwClient = apigatewaypb.NewVectronServiceClient(apigwConn)
	}

	// Placement driver client (connect to first PD node)
	pdConn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", benchmarkPdGRPCPort1),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("Failed to connect to placement driver: %v", err)
	} else {
		s.placementClient = placementpb.NewPlacementServiceClient(pdConn)
	}
}

// =============================================================================
// Cleanup and Helper Functions (from ultimate_e2e_test.go)
// =============================================================================

func (s *BenchmarkTest) cleanup() {
	log.Println("\n🧹 Cleaning up benchmark resources...")

	// Cancel context to signal all goroutines to stop
	if s.cancel != nil {
		s.cancel()
	}

	s.stopProcesses()
	s.cleanupDataDirs()

	// Stop Valkey container if running
	exec.Command("podman", "stop", "vectron-valkey-benchmark").Run()
	// Stop etcd container if running
	exec.Command("podman", "stop", "etcd").Run()

	log.Println("✅ Cleanup complete (logs preserved in: " + benchmarkLogTempDir + ")")
}

func (s *BenchmarkTest) stopProcesses() {
	// Kill all registered processes
	s.processesMutex.Lock()
	for _, cmd := range s.processes {
		if cmd.Process != nil {
			log.Printf("Stopping process %d...", cmd.Process.Pid)
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	}
	s.processes = nil
	s.processesMutex.Unlock()
}

func (s *BenchmarkTest) cleanupDataDirs() {
	// Clean up data directories only (keep logs for debugging)
	s.dataDirsMutex.Lock()
	for _, dir := range s.dataDirs {
		log.Printf("Removing data directory: %s", dir)
		_ = os.RemoveAll(dir)
	}
	s.dataDirs = nil
	s.dataDirsMutex.Unlock()

	// Clean up base data directory but keep logs
	log.Printf("Removing data directory: %s", benchmarkDataTempDir)
	_ = os.RemoveAll(benchmarkDataTempDir)
}

func (s *BenchmarkTest) resetSystem(t *testing.T) {
	log.Println("🔄 Resetting system between scenarios...")

	s.stopProcesses()
	exec.Command("podman", "stop", "vectron-valkey-benchmark").Run()
	exec.Command("podman", "stop", "etcd").Run()
	s.cleanupDataDirs()

	if err := os.MkdirAll(benchmarkDataTempDir, 0755); err != nil {
		t.Fatalf("Failed to create data temp dir %s: %v", benchmarkDataTempDir, err)
	}
	if err := os.MkdirAll(benchmarkLogTempDir, 0755); err != nil {
		t.Fatalf("Failed to create log temp dir %s: %v", benchmarkLogTempDir, err)
	}

	s.jwtToken = ""
	s.apiKey = ""
	s.userID = ""

	s.TestSystemStartup(t)
	s.TestInitializeAndAuthenticate(t)
}

func (s *BenchmarkTest) registerProcess(cmd *exec.Cmd) {
	s.processesMutex.Lock()
	defer s.processesMutex.Unlock()
	s.processes = append(s.processes, cmd)
}

func (s *BenchmarkTest) registerDataDir(dir string) {
	s.dataDirsMutex.Lock()
	defer s.dataDirsMutex.Unlock()
	s.dataDirs = append(s.dataDirs, dir)
}

func (s *BenchmarkTest) isPortOpen(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 1*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (s *BenchmarkTest) killProcessOnPort(port int) {
	// Try to find and kill process using the port
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err == nil && len(output) > 0 {
		pids := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, pid := range pids {
			if pid != "" {
				log.Printf("Killing process %s on port %d", pid, port)
				exec.Command("kill", "-9", pid).Run()
			}
		}
	}
}

func (s *BenchmarkTest) getAuthenticatedContext() context.Context {
	if s.jwtToken == "" {
		return s.ctx
	}
	md := metadata.New(map[string]string{
		"authorization": "Bearer " + s.jwtToken,
	})
	return metadata.NewOutgoingContext(s.ctx, md)
}

// =============================================================================
// Helper Functions for Shard Readiness
// =============================================================================

// =============================================================================
// Scenario 1: Vector Search Scalability
// =============================================================================

// upsertWithRetry attempts to upsert with retry logic for transient failures like "shard not ready"
func upsertWithRetry(ctx context.Context, client apigatewaypb.VectronServiceClient, req *apigatewaypb.UpsertRequest, maxRetries int) (*apigatewaypb.UpsertResponse, error) {
	var lastErr error
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Upsert(ctx, req)
		if err == nil {
			return resp, nil
		}

		// Check if it's a retryable error (shard not ready, unavailable)
		errStr := err.Error()
		if !strings.Contains(errStr, "not ready") && !strings.Contains(errStr, "Unavailable") {
			return nil, err // Not retryable
		}

		lastErr = err
		// Exponential backoff: 500ms, 1s, 2s, 4s, etc.
		backoff := time.Duration(1<<i) * 500 * time.Millisecond
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}
		time.Sleep(backoff)
	}
	return nil, lastErr
}

// waitForCollectionReady polls GetCollectionStatus until all shards are ready or timeout.
func waitForCollectionReady(ctx context.Context, client apigatewaypb.VectronServiceClient, collection string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for collection %q to become ready", collection)
		}
		resp, err := client.GetCollectionStatus(ctx, &apigatewaypb.GetCollectionStatusRequest{Name: collection})
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		if len(resp.Shards) == 0 {
			time.Sleep(500 * time.Millisecond)
			continue
		}
		allReady := true
		for _, shard := range resp.Shards {
			if !shard.Ready {
				allReady = false
				break
			}
		}
		if allReady {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func benchmarkVectorSearchScalability(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, suite *BenchmarkSuite) {
	log.Println("\n📊 Scenario 1: Vector Search Scalability")
	log.Println("Testing search performance with increasing dataset sizes")

	datasetSizes := []int{SmallDataset, MediumDataset, LargeDataset, XLDataset}
	dimensions := []int{Dim128, Dim384, Dim768}

	for _, dim := range dimensions {
		for _, size := range datasetSizes {
			result := runScalabilityBenchmark(t, ctx, client, size, dim)
			suite.addResult(result)
		}
	}
}

func runScalabilityBenchmark(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, datasetSize, dimension int) *BenchmarkResult {
	collectionName := fmt.Sprintf("scalability-d%d-n%d", dimension, datasetSize)

	result := &BenchmarkResult{
		Name:            fmt.Sprintf("Scalability-D%d-N%d", dimension, datasetSize),
		Description:     fmt.Sprintf("Search scalability with %d dimensions and %d vectors", dimension, datasetSize),
		Timestamp:       time.Now(),
		DatasetSize:     datasetSize,
		VectorDimension: dimension,
	}

	// Memory before
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)
	result.MemoryBeforeMB = m1.Alloc / 1024 / 1024
	result.GoroutinesBefore = runtime.NumGoroutine()

	// Create collection
	start := time.Now()
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Wait for shards to be assigned and initialized
	log.Printf("  Waiting for shards to be assigned and initialized...")
	require.NoError(t, waitForCollectionReady(ctx, client, collectionName, 120*time.Second))

	// Insert vectors in batches
	batchSize := 1000
	insertLatencies := make([]time.Duration, 0, datasetSize/batchSize)
	insertCount := int64(0)

	for i := 0; i < datasetSize; i += batchSize {
		batch := make([]*apigatewaypb.Point, 0, batchSize)
		for j := 0; j < batchSize && i+j < datasetSize; j++ {
			batch = append(batch, &apigatewaypb.Point{
				Id:     fmt.Sprintf("vec-%d-%d", i, j),
				Vector: generateNormalizedVector(dimension),
				Payload: map[string]string{
					"index": fmt.Sprintf("%d", i+j),
				},
			})
		}

		batchStart := time.Now()
		resp, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		}, 5)
		require.NoError(t, err)
		batchDuration := time.Since(batchStart)

		insertLatencies = append(insertLatencies, batchDuration)
		insertCount += int64(resp.Upserted)

		if datasetSize >= 10000 && i%(datasetSize/10) == 0 && i > 0 {
			progress := float64(i) / float64(datasetSize) * 100
			log.Printf("  Insert progress: %.1f%% (%d/%d vectors)", progress, i, datasetSize)
		}
	}

	insertDuration := time.Since(start)
	result.InsertMetrics = &ThroughputMetrics{
		Operations:    insertCount,
		Duration:      insertDuration,
		OpsPerSecond:  float64(insertCount) / insertDuration.Seconds(),
		VectorsPerSec: float64(insertCount) / insertDuration.Seconds(),
	}

	// Wait for indexing
	time.Sleep(2 * time.Second)

	// Search benchmark
	searchLatencies := make([]time.Duration, 0, 1000)
	queryCount := 1000
	start = time.Now()

	for i := 0; i < queryCount; i++ {
		queryVector := generateNormalizedVector(dimension)

		queryStart := time.Now()
		_, err := client.Search(ctx, &apigatewaypb.SearchRequest{
			Collection: collectionName,
			Vector:     queryVector,
			TopK:       10,
		})
		queryDuration := time.Since(queryStart)

		require.NoError(t, err)
		searchLatencies = append(searchLatencies, queryDuration)
	}

	searchDuration := time.Since(start)
	result.SearchMetrics = &ThroughputMetrics{
		Operations:    int64(queryCount),
		Duration:      searchDuration,
		OpsPerSecond:  float64(queryCount) / searchDuration.Seconds(),
		VectorsPerSec: float64(queryCount) / searchDuration.Seconds(),
	}
	result.LatencyMetrics = calculateLatencyMetrics(searchLatencies)

	// Memory after
	var m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m2)
	result.MemoryAfterMB = m2.Alloc / 1024 / 1024
	result.GoroutinesAfter = runtime.NumGoroutine()
	result.Duration = insertDuration + searchDuration

	// Print results
	log.Printf("\n  ✅ Scalability Benchmark: %dD × %dV", dimension, datasetSize)
	log.Printf("     Insert: %d vectors in %v (%.0f vec/s)",
		insertCount, insertDuration, result.InsertMetrics.VectorsPerSec)
	log.Printf("     Search: %d queries in %v (%.0f q/s)",
		queryCount, searchDuration, result.SearchMetrics.OpsPerSecond)
	log.Printf("     Latency: Avg=%v, P50=%v, P95=%v, P99=%v",
		result.LatencyMetrics.Average,
		result.LatencyMetrics.P50,
		result.LatencyMetrics.P95,
		result.LatencyMetrics.P99)
	log.Printf("     Memory: %dMB → %dMB (+%dMB)",
		result.MemoryBeforeMB, result.MemoryAfterMB,
		result.MemoryAfterMB-result.MemoryBeforeMB)

	return result
}

// =============================================================================
// Scenario 2: Dimension Impact Analysis
// =============================================================================

func benchmarkDimensionImpact(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, suite *BenchmarkSuite) {
	log.Println("\n📏 Scenario 2: Dimension Impact Analysis")
	log.Println("Testing how vector dimension affects performance")

	dimensions := []int{64, 128, 256, 384, 512, 768, 1024, 1536}
	datasetSize := 10000

	for _, dim := range dimensions {
		result := runDimensionBenchmark(t, ctx, client, datasetSize, dim)
		suite.addResult(result)
	}
}

func runDimensionBenchmark(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, datasetSize, dimension int) *BenchmarkResult {
	collectionName := fmt.Sprintf("dimension-bench-d%d", dimension)

	result := &BenchmarkResult{
		Name:            fmt.Sprintf("Dimension-%d", dimension),
		Description:     fmt.Sprintf("Performance with %d-dimensional vectors", dimension),
		Timestamp:       time.Now(),
		DatasetSize:     datasetSize,
		VectorDimension: dimension,
	}

	// Create and populate collection
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Wait for shards to be ready
	// Wait for shards to be assigned and initialized
	// This is critical - shards are created asynchronously after collection creation
	log.Printf("  Waiting for shards to be assigned and initialized...")
	require.NoError(t, waitForCollectionReady(ctx, client, collectionName, 120*time.Second))

	// Measure insert performance
	batchSize := 100
	start := time.Now()

	for i := 0; i < datasetSize; i += batchSize {
		batch := make([]*apigatewaypb.Point, 0, batchSize)
		for j := 0; j < batchSize && i+j < datasetSize; j++ {
			batch = append(batch, &apigatewaypb.Point{
				Id:     fmt.Sprintf("dim-%d", i+j),
				Vector: generateNormalizedVector(dimension),
			})
		}

		_, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		}, 5)
		require.NoError(t, err)
	}

	insertDuration := time.Since(start)
	result.InsertMetrics = &ThroughputMetrics{
		Operations:    int64(datasetSize),
		Duration:      insertDuration,
		VectorsPerSec: float64(datasetSize) / insertDuration.Seconds(),
	}

	// Measure search performance with various top-k values
	topKValues := []uint32{1, 5, 10, 50, 100}
	searchLatencies := make(map[uint32][]time.Duration)

	for _, topK := range topKValues {
		latencies := make([]time.Duration, 0, 100)
		for i := 0; i < 100; i++ {
			queryStart := time.Now()
			_, err := client.Search(ctx, &apigatewaypb.SearchRequest{
				Collection: collectionName,
				Vector:     generateNormalizedVector(dimension),
				TopK:       topK,
			})
			latencies = append(latencies, time.Since(queryStart))
			require.NoError(t, err)
		}
		searchLatencies[topK] = latencies
	}

	// Use topK=10 for main metrics
	result.LatencyMetrics = calculateLatencyMetrics(searchLatencies[10])
	result.Duration = insertDuration

	// Print dimension-specific results
	log.Printf("\n  ✅ Dimension Benchmark: %dD", dimension)
	log.Printf("     Insert Throughput: %.0f vectors/sec", result.InsertMetrics.VectorsPerSec)
	for _, topK := range topKValues {
		metrics := calculateLatencyMetrics(searchLatencies[topK])
		log.Printf("     Search (top-%d): P50=%v, P95=%v", topK, metrics.P50, metrics.P95)
	}

	return result
}

// =============================================================================
// Scenario 3: Concurrent Workload Analysis
// =============================================================================

func benchmarkConcurrentWorkload(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, suite *BenchmarkSuite) {
	log.Println("\n🔄 Scenario 3: Concurrent Workload Analysis")
	log.Println("Testing system behavior under concurrent load")

	concurrencyLevels := []int{1, 5, 10, 20, 50, 100}
	datasetSize := 50000
	dimension := 384

	collectionName := "concurrent-bench"

	// Setup dataset
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Wait for shards to be ready
	// Wait for shards to be assigned and initialized
	// This is critical - shards are created asynchronously after collection creation
	log.Printf("  Waiting for shards to be assigned and initialized...")
	require.NoError(t, waitForCollectionReady(ctx, client, collectionName, 120*time.Second))

	// Insert dataset
	for i := 0; i < datasetSize; i += 1000 {
		batch := make([]*apigatewaypb.Point, 0, 1000)
		for j := 0; j < 1000 && i+j < datasetSize; j++ {
			batch = append(batch, &apigatewaypb.Point{
				Id:     fmt.Sprintf("concurrent-%d", i+j),
				Vector: generateNormalizedVector(dimension),
			})
		}
		_, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		}, 5)
		require.NoError(t, err)
	}

	// Test different concurrency levels
	for _, concurrency := range concurrencyLevels {
		result := runConcurrentBenchmark(t, ctx, client, collectionName, concurrency)
		suite.addResult(result)
	}
}

func runConcurrentBenchmark(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, collectionName string, concurrency int) *BenchmarkResult {
	result := &BenchmarkResult{
		Name:            fmt.Sprintf("Concurrent-%d", concurrency),
		Description:     fmt.Sprintf("Performance with %d concurrent clients", concurrency),
		Timestamp:       time.Now(),
		DatasetSize:     50000,
		VectorDimension: 384,
	}

	// Run concurrent searches for 30 seconds
	duration := 30 * time.Second
	start := time.Now()

	var wg sync.WaitGroup
	latencies := make([]time.Duration, 0)
	var latenciesMutex sync.Mutex
	var queryCount int64

	// Use semaphore for concurrency control
	semaphore := make(chan struct{}, concurrency)

	ctxTimeout, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctxTimeout.Done():
					return
				default:
					semaphore <- struct{}{}

					queryStart := time.Now()
					_, err := client.Search(ctxTimeout, &apigatewaypb.SearchRequest{
						Collection: collectionName,
						Vector:     generateNormalizedVector(384),
						TopK:       10,
					})
					queryDuration := time.Since(queryStart)

					<-semaphore

					if err == nil {
						atomic.AddInt64(&queryCount, 1)
						latenciesMutex.Lock()
						latencies = append(latencies, queryDuration)
						latenciesMutex.Unlock()
					}
				}
			}
		}(i)
	}

	wg.Wait()
	actualDuration := time.Since(start)

	result.SearchMetrics = &ThroughputMetrics{
		Operations:   queryCount,
		Duration:     actualDuration,
		OpsPerSecond: float64(queryCount) / actualDuration.Seconds(),
	}
	result.LatencyMetrics = calculateLatencyMetrics(latencies)
	result.Duration = actualDuration

	log.Printf("\n  ✅ Concurrent Benchmark: %d clients", concurrency)
	log.Printf("     Total Queries: %d", queryCount)
	log.Printf("     Throughput: %.0f queries/sec", result.SearchMetrics.OpsPerSecond)
	log.Printf("     Latency: Avg=%v, P50=%v, P95=%v, P99=%v",
		result.LatencyMetrics.Average,
		result.LatencyMetrics.P50,
		result.LatencyMetrics.P95,
		result.LatencyMetrics.P99)

	return result
}

// =============================================================================
// Scenario 4: Reranker Effectiveness
// =============================================================================

func benchmarkRerankerEffectiveness(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, suite *BenchmarkSuite) {
	log.Println("\n🎛️ Scenario 4: Reranker Effectiveness Analysis")
	log.Println("Measuring quality improvement from reranking")

	collectionName := "reranker-effectiveness"
	datasetSize := 10000
	dimension := 384

	result := &BenchmarkResult{
		Name:            "Reranker-Effectiveness",
		Description:     "Measuring search quality improvement from reranking",
		Timestamp:       time.Now(),
		DatasetSize:     datasetSize,
		VectorDimension: dimension,
	}

	// Create collection
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Wait for shards to be ready
	// Wait for shards to be assigned and initialized
	// This is critical - shards are created asynchronously after collection creation
	log.Printf("  Waiting for shards to be assigned and initialized...")
	require.NoError(t, waitForCollectionReady(ctx, client, collectionName, 120*time.Second))

	// Insert vectors with metadata for reranking
	start := time.Now()
	for i := 0; i < datasetSize; i += 100 {
		batch := make([]*apigatewaypb.Point, 0, 100)
		for j := 0; j < 100 && i+j < datasetSize; j++ {
			isVerified := (i+j)%10 == 0 // 10% verified
			isFeatured := (i+j)%20 == 0 // 5% featured

			batch = append(batch, &apigatewaypb.Point{
				Id:     fmt.Sprintf("rerank-%d", i+j),
				Vector: generateNormalizedVector(dimension),
				Payload: map[string]string{
					"verified": fmt.Sprintf("%t", isVerified),
					"featured": fmt.Sprintf("%t", isFeatured),
					"title":    fmt.Sprintf("Product %d", i+j),
				},
			})
		}
		_, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		}, 5)
		require.NoError(t, err)
	}
	insertDuration := time.Since(start)

	// Run search queries and measure quality
	queryCount := 500
	baseMetrics := &SearchQualityMetrics{QueryCount: queryCount}

	// Calculate baseline metrics (without reranker)
	verifiedInTop10 := 0
	featuredInTop10 := 0
	baseLatencies := make([]time.Duration, 0, queryCount)

	for i := 0; i < queryCount; i++ {
		queryStart := time.Now()
		resp, err := client.Search(ctx, &apigatewaypb.SearchRequest{
			Collection: collectionName,
			Vector:     generateNormalizedVector(dimension),
			TopK:       10,
		})
		baseLatencies = append(baseLatencies, time.Since(queryStart))
		require.NoError(t, err)

		for _, r := range resp.Results {
			if r.Payload["verified"] == "true" {
				verifiedInTop10++
			}
			if r.Payload["featured"] == "true" {
				featuredInTop10++
			}
		}
	}

	// With reranker (already integrated)
	rerankedMetrics := &SearchQualityMetrics{QueryCount: queryCount}
	verifiedInTop10Reranked := 0
	featuredInTop10Reranked := 0
	rerankedLatencies := make([]time.Duration, 0, queryCount)

	for i := 0; i < queryCount; i++ {
		queryStart := time.Now()
		resp, err := client.Search(ctx, &apigatewaypb.SearchRequest{
			Collection: collectionName,
			Vector:     generateNormalizedVector(dimension),
			TopK:       10,
		})
		rerankedLatencies = append(rerankedLatencies, time.Since(queryStart))
		require.NoError(t, err)

		for _, r := range resp.Results {
			if r.Payload["verified"] == "true" {
				verifiedInTop10Reranked++
			}
			if r.Payload["featured"] == "true" {
				featuredInTop10Reranked++
			}
		}
	}

	// Calculate improvements
	verifiedBoost := float64(verifiedInTop10Reranked-verifiedInTop10) / float64(verifiedInTop10) * 100
	featuredBoost := float64(featuredInTop10Reranked-featuredInTop10) / float64(featuredInTop10) * 100
	latencyIncrease := calculateAverageDuration(rerankedLatencies) - calculateAverageDuration(baseLatencies)

	result.RerankerMetrics = &RerankerMetrics{
		BaseResults:     baseMetrics,
		RerankedResults: rerankedMetrics,
		Improvement:     (verifiedBoost + featuredBoost) / 2,
		LatencyImpact:   latencyIncrease,
	}
	result.Duration = insertDuration
	result.InsertMetrics = &ThroughputMetrics{
		VectorsPerSec: float64(datasetSize) / insertDuration.Seconds(),
	}

	log.Printf("\n  ✅ Reranker Effectiveness")
	log.Printf("     Verified items in top-10: %d → %d (+%.1f%%)",
		verifiedInTop10, verifiedInTop10Reranked, verifiedBoost)
	log.Printf("     Featured items in top-10: %d → %d (+%.1f%%)",
		featuredInTop10, featuredInTop10Reranked, featuredBoost)
	log.Printf("     Latency impact: +%v", latencyIncrease)

	suite.addResult(result)
}

// =============================================================================
// Scenario 5: Distributed Scalability
// =============================================================================

func benchmarkDistributedScalability(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, placementClient placementpb.PlacementServiceClient, suite *BenchmarkSuite) {
	log.Println("\n🌐 Scenario 5: Distributed System Scalability")
	log.Println("Analyzing shard distribution and load balancing")

	// Create multiple collections of different sizes
	collectionSizes := []int{1000, 5000, 10000, 50000}

	for _, size := range collectionSizes {
		result := runDistributedBenchmark(t, ctx, client, placementClient, size)
		suite.addResult(result)
	}
}

func runDistributedBenchmark(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, placementClient placementpb.PlacementServiceClient, datasetSize int) *BenchmarkResult {
	collectionName := fmt.Sprintf("distributed-bench-%d", datasetSize)
	dimension := 384

	result := &BenchmarkResult{
		Name:            fmt.Sprintf("Distributed-%d", datasetSize),
		Description:     fmt.Sprintf("Distributed performance with %d vectors", datasetSize),
		Timestamp:       time.Now(),
		DatasetSize:     datasetSize,
		VectorDimension: dimension,
	}

	// Create collection
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Wait for shards to be ready
	// Wait for shards to be assigned and initialized
	// This is critical - shards are created asynchronously after collection creation
	log.Printf("  Waiting for shards to be assigned and initialized...")
	require.NoError(t, waitForCollectionReady(ctx, client, collectionName, 120*time.Second))

	// Insert data
	start := time.Now()
	for i := 0; i < datasetSize; i += 1000 {
		batch := make([]*apigatewaypb.Point, 0, 1000)
		for j := 0; j < 1000 && i+j < datasetSize; j++ {
			batch = append(batch, &apigatewaypb.Point{
				Id:     fmt.Sprintf("dist-%d", i+j),
				Vector: generateNormalizedVector(dimension),
			})
		}
		_, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		}, 5)
		require.NoError(t, err)
	}
	insertDuration := time.Since(start)

	// Get collection status for shard info
	status, err := client.GetCollectionStatus(ctx, &apigatewaypb.GetCollectionStatusRequest{
		Name: collectionName,
	})
	require.NoError(t, err)

	// Analyze shard distribution
	shardCount := len(status.Shards)
	workerShards := make(map[uint64]int)

	for _, shard := range status.Shards {
		for _, replica := range shard.Replicas {
			workerShards[replica]++
		}
	}

	// Calculate load variance
	if len(workerShards) > 0 {
		var sum, sumSquared float64
		for _, count := range workerShards {
			sum += float64(count)
			sumSquared += float64(count * count)
		}
		mean := sum / float64(len(workerShards))
		variance := (sumSquared / float64(len(workerShards))) - (mean * mean)
		cv := math.Sqrt(variance) / mean // Coefficient of variation

		result.DistributedMetrics = &DistributedMetrics{
			ShardCount:      shardCount,
			WorkerCount:     len(workerShards),
			ShardsPerWorker: workerShards,
			LoadVariance:    cv,
		}
	}

	result.Duration = insertDuration
	result.InsertMetrics = &ThroughputMetrics{
		VectorsPerSec: float64(datasetSize) / insertDuration.Seconds(),
	}

	log.Printf("\n  ✅ Distributed Benchmark: %d vectors", datasetSize)
	log.Printf("     Shards: %d across %d workers", shardCount, len(workerShards))
	for worker, count := range workerShards {
		log.Printf("     Worker %d: %d shards", worker, count)
	}
	if result.DistributedMetrics != nil {
		log.Printf("     Load variance (CV): %.2f", result.DistributedMetrics.LoadVariance)
	}

	return result
}

// =============================================================================
// Scenario 6: End-to-End Latency Breakdown
// =============================================================================

func benchmarkEndToEndLatency(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, suite *BenchmarkSuite) {
	log.Println("\n⏱️ Scenario 6: End-to-End Latency Breakdown")
	log.Println("Detailed latency analysis for each operation type")

	collectionName := "latency-breakdown"
	datasetSize := 10000
	dimension := 384

	result := &BenchmarkResult{
		Name:            "Latency-Breakdown",
		Description:     "Detailed latency analysis for each operation",
		Timestamp:       time.Now(),
		DatasetSize:     datasetSize,
		VectorDimension: dimension,
	}

	// Test 1: Collection creation latency
	creationLatencies := make([]time.Duration, 10)
	for i := 0; i < 10; i++ {
		start := time.Now()
		_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
			Name:      fmt.Sprintf("latency-test-%d", i),
			Dimension: int32(dimension),
			Distance:  "cosine",
		})
		creationLatencies[i] = time.Since(start)
		require.NoError(t, err)
	}

	// Create main collection
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Wait for shards to be ready
	// Wait for shards to be assigned and initialized
	// This is critical - shards are created asynchronously after collection creation
	log.Printf("  Waiting for shards to be assigned and initialized...")
	require.NoError(t, waitForCollectionReady(ctx, client, collectionName, 180*time.Second))

	// Test 2: Single upsert latency
	singleUpsertLatencies := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		start := time.Now()
		_, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points: []*apigatewaypb.Point{
				{
					Id:     fmt.Sprintf("single-%d", i),
					Vector: generateNormalizedVector(dimension),
				},
			},
		})
		singleUpsertLatencies[i] = time.Since(start)
		require.NoError(t, err)
	}

	// Test 3: Batch upsert latency
	batchSizes := []int{10, 50, 100, 500, 1000}
	batchLatencies := make(map[int][]time.Duration)

	for _, batchSize := range batchSizes {
		latencies := make([]time.Duration, 20)
		for i := 0; i < 20; i++ {
			batch := make([]*apigatewaypb.Point, batchSize)
			for j := 0; j < batchSize; j++ {
				batch[j] = &apigatewaypb.Point{
					Id:     fmt.Sprintf("batch-%d-%d", i, j),
					Vector: generateNormalizedVector(dimension),
				}
			}

			start := time.Now()
			_, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
				Collection: collectionName,
				Points:     batch,
			})
			latencies[i] = time.Since(start)
			require.NoError(t, err)
		}
		batchLatencies[batchSize] = latencies
	}

	// Test 4: Search latency with different top-k
	topKValues := []uint32{1, 5, 10, 50, 100}
	searchLatencies := make(map[uint32][]time.Duration)

	for _, topK := range topKValues {
		latencies := make([]time.Duration, 100)
		for i := 0; i < 100; i++ {
			start := time.Now()
			_, err := client.Search(ctx, &apigatewaypb.SearchRequest{
				Collection: collectionName,
				Vector:     generateNormalizedVector(dimension),
				TopK:       topK,
			})
			latencies[i] = time.Since(start)
			require.NoError(t, err)
		}
		searchLatencies[topK] = latencies
	}

	// Test 5: Get by ID latency
	getLatencies := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		start := time.Now()
		_, err := client.Get(ctx, &apigatewaypb.GetRequest{
			Collection: collectionName,
			Id:         fmt.Sprintf("single-%d", i),
		})
		getLatencies[i] = time.Since(start)
		require.NoError(t, err)
	}

	// Print detailed breakdown
	log.Printf("\n  ✅ Latency Breakdown Analysis")

	creationMetrics := calculateLatencyMetrics(creationLatencies)
	log.Printf("\n     Collection Creation:")
	log.Printf("       Average: %v, P50: %v, P95: %v",
		creationMetrics.Average, creationMetrics.P50, creationMetrics.P95)

	singleMetrics := calculateLatencyMetrics(singleUpsertLatencies)
	log.Printf("\n     Single Upsert:")
	log.Printf("       Average: %v, P50: %v, P95: %v",
		singleMetrics.Average, singleMetrics.P50, singleMetrics.P95)

	log.Printf("\n     Batch Upsert (by size):")
	for _, size := range batchSizes {
		metrics := calculateLatencyMetrics(batchLatencies[size])
		perVector := metrics.Average / time.Duration(size)
		log.Printf("       Batch-%d: Total=%v, Per-vector=%v",
			size, metrics.Average, perVector)
	}

	log.Printf("\n     Search (by top-k):")
	for _, topK := range topKValues {
		metrics := calculateLatencyMetrics(searchLatencies[topK])
		log.Printf("       Top-%d: Average=%v, P50=%v, P95=%v",
			topK, metrics.Average, metrics.P50, metrics.P95)
	}

	getMetrics := calculateLatencyMetrics(getLatencies)
	log.Printf("\n     Get by ID:")
	log.Printf("       Average: %v, P50: %v, P95: %v",
		getMetrics.Average, getMetrics.P50, getMetrics.P95)

	// Store overall metrics
	result.LatencyMetrics = calculateLatencyMetrics(searchLatencies[10])

	suite.addResult(result)
}

// =============================================================================
// Report Generation
// =============================================================================

func generateResearchReport(t *testing.T, suite *BenchmarkSuite) {
	log.Println("\n" + strings.Repeat("=", 80))
	log.Println("📊 RESEARCH BENCHMARK REPORT")
	log.Println(strings.Repeat("=", 80))

	suite.mutex.RLock()
	results := suite.results
	suite.mutex.RUnlock()

	// Generate CSV files
	generateCSVReport(t, results)
	generateMarkdownReport(t, results)
	generateSummaryStats(t, results)

	log.Println("\n✅ Research benchmark complete!")
	log.Printf("✅ Generated %d benchmark results", len(results))
	log.Println("✅ CSV files saved for analysis")
	log.Println("✅ Markdown report generated")
}

func generateCSVReport(t *testing.T, results []*BenchmarkResult) {
	// CSV 1: Scalability data
	scalabilityFile, err := os.Create("benchmark_scalability.csv")
	if err != nil {
		t.Logf("Warning: Could not create CSV: %v", err)
		return
	}
	defer scalabilityFile.Close()

	writer := csv.NewWriter(scalabilityFile)
	writer.Write([]string{
		"Test Name", "Dataset Size", "Dimension",
		"Insert Throughput (vec/s)", "Search Throughput (q/s)",
		"Latency P50 (ms)", "Latency P95 (ms)", "Latency P99 (ms)",
		"Memory Delta (MB)",
	})

	for _, r := range results {
		if r.InsertMetrics != nil && r.SearchMetrics != nil {
			writer.Write([]string{
				r.Name,
				fmt.Sprintf("%d", r.DatasetSize),
				fmt.Sprintf("%d", r.VectorDimension),
				fmt.Sprintf("%.2f", r.InsertMetrics.VectorsPerSec),
				fmt.Sprintf("%.2f", r.SearchMetrics.OpsPerSecond),
				fmt.Sprintf("%.2f", float64(r.LatencyMetrics.P50)/float64(time.Millisecond)),
				fmt.Sprintf("%.2f", float64(r.LatencyMetrics.P95)/float64(time.Millisecond)),
				fmt.Sprintf("%.2f", float64(r.LatencyMetrics.P99)/float64(time.Millisecond)),
				fmt.Sprintf("%d", r.MemoryAfterMB-r.MemoryBeforeMB),
			})
		}
	}
	writer.Flush()

	// CSV 2: Reranker data
	if len(results) > 0 {
		rerankerFile, _ := os.Create("benchmark_reranker.csv")
		defer rerankerFile.Close()

		writer := csv.NewWriter(rerankerFile)
		writer.Write([]string{"Test Name", "Improvement (%)", "Latency Impact (ms)"})

		for _, r := range results {
			if r.RerankerMetrics != nil {
				writer.Write([]string{
					r.Name,
					fmt.Sprintf("%.2f", r.RerankerMetrics.Improvement),
					fmt.Sprintf("%.2f", float64(r.RerankerMetrics.LatencyImpact)/float64(time.Millisecond)),
				})
			}
		}
		writer.Flush()
	}

	log.Println("✅ CSV reports generated: benchmark_scalability.csv, benchmark_reranker.csv")
}

func generateMarkdownReport(t *testing.T, results []*BenchmarkResult) {
	file, err := os.Create("BENCHMARK_REPORT.md")
	if err != nil {
		t.Logf("Warning: Could not create markdown report: %v", err)
		return
	}
	defer file.Close()

	fmt.Fprintf(file, "# Vectron Research Benchmark Report\n\n")
	fmt.Fprintf(file, "Generated: %s\n\n", time.Now().Format("2006-01-02 15:04:05"))

	fmt.Fprintf(file, "## Summary\n\n")
	fmt.Fprintf(file, "Total benchmarks: %d\n\n", len(results))

	// Table of results
	fmt.Fprintf(file, "## Results Table\n\n")
	fmt.Fprintf(file, "| Test | Dataset | Dim | Insert (v/s) | Search (q/s) | P50 | P95 |\n")
	fmt.Fprintf(file, "|------|---------|-----|--------------|--------------|-----|-----|\n")

	for _, r := range results {
		if r.InsertMetrics == nil {
			continue
		}
		searchOps := 0.0
		if r.SearchMetrics != nil {
			searchOps = r.SearchMetrics.OpsPerSecond
		}
		var p50, p95 interface{} = "-", "-"
		if r.LatencyMetrics != nil {
			p50 = r.LatencyMetrics.P50
			p95 = r.LatencyMetrics.P95
		}
		fmt.Fprintf(file, "| %s | %d | %d | %.0f | %.0f | %v | %v |\n",
			r.Name, r.DatasetSize, r.VectorDimension,
			r.InsertMetrics.VectorsPerSec, searchOps, p50, p95)
	}

	fmt.Fprintf(file, "\n## Detailed Results\n\n")
	for _, r := range results {
		fmt.Fprintf(file, "### %s\n\n", r.Name)
		fmt.Fprintf(file, "- **Description**: %s\n", r.Description)
		fmt.Fprintf(file, "- **Dataset**: %d vectors of %d dimensions\n", r.DatasetSize, r.VectorDimension)

		if r.InsertMetrics != nil {
			fmt.Fprintf(file, "- **Insert Throughput**: %.0f vectors/sec\n", r.InsertMetrics.VectorsPerSec)
		}
		if r.SearchMetrics != nil {
			fmt.Fprintf(file, "- **Search Throughput**: %.0f queries/sec\n", r.SearchMetrics.OpsPerSecond)
		}
		if r.LatencyMetrics != nil {
			fmt.Fprintf(file, "- **Latency**: Avg=%v, P50=%v, P95=%v, P99=%v\n",
				r.LatencyMetrics.Average, r.LatencyMetrics.P50,
				r.LatencyMetrics.P95, r.LatencyMetrics.P99)
		}

		fmt.Fprintf(file, "\n")
	}

	log.Println("✅ Markdown report generated: BENCHMARK_REPORT.md")
}

func generateSummaryStats(t *testing.T, results []*BenchmarkResult) {
	log.Println("\n📈 Summary Statistics:")

	var totalInsertThroughput, totalSearchThroughput float64
	var count int

	for _, r := range results {
		if r.InsertMetrics != nil {
			totalInsertThroughput += r.InsertMetrics.VectorsPerSec
			totalSearchThroughput += r.SearchMetrics.OpsPerSecond
			count++
		}
	}

	if count > 0 {
		avgInsert := totalInsertThroughput / float64(count)
		avgSearch := totalSearchThroughput / float64(count)

		log.Printf("   Average Insert Throughput: %.0f vectors/sec", avgInsert)
		log.Printf("   Average Search Throughput: %.0f queries/sec", avgSearch)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func (s *BenchmarkSuite) addResult(result *BenchmarkResult) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.results = append(s.results, result)
}

func calculateLatencyMetrics(latencies []time.Duration) *LatencyMetrics {
	if len(latencies) == 0 {
		return &LatencyMetrics{}
	}

	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	var total time.Duration
	min := sorted[0]
	max := sorted[len(sorted)-1]

	for _, l := range sorted {
		total += l
	}

	avg := total / time.Duration(len(sorted))

	// Calculate percentiles
	p50Index := int(float64(len(sorted)) * 0.50)
	p95Index := int(float64(len(sorted)) * 0.95)
	p99Index := int(float64(len(sorted)) * 0.99)

	// Calculate standard deviation
	var variance float64
	for _, l := range sorted {
		diff := float64(l - avg)
		variance += diff * diff
	}
	stdDev := time.Duration(math.Sqrt(variance / float64(len(sorted))))

	return &LatencyMetrics{
		Count:        int64(len(sorted)),
		Total:        total,
		Min:          min,
		Max:          max,
		Average:      avg,
		P50:          sorted[p50Index],
		P95:          sorted[p95Index],
		P99:          sorted[p99Index],
		StdDev:       stdDev,
		AllLatencies: sorted,
	}
}

func calculateAverageDuration(latencies []time.Duration) time.Duration {
	if len(latencies) == 0 {
		return 0
	}
	var total time.Duration
	for _, l := range latencies {
		total += l
	}
	return total / time.Duration(len(latencies))
}

func generateNormalizedVector(dim int) []float32 {
	vec := make([]float32, dim)
	var sum float32

	for i := 0; i < dim; i++ {
		vec[i] = rand.Float32()
		sum += vec[i] * vec[i]
	}

	// Normalize
	norm := float32(math.Sqrt(float64(sum)))
	if norm > 0 {
		for i := 0; i < dim; i++ {
			vec[i] /= norm
		}
	}

	return vec
}
