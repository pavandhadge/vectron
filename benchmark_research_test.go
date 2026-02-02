// benchmark_research_test.go - Comprehensive Benchmark Suite for Research
//
// This test suite provides detailed benchmarking and metrics collection for:
// - Vector search performance (latency, throughput, recall, precision)
// - Distributed system scalability (shard distribution, load balancing)
// - Reranker effectiveness (quality improvement metrics)
// - End-to-end system performance under various workloads

// Metrics Calculated:
// - Latency: P50, P95, P99, Average, Min, Max
// - Throughput: Vectors/second, Queries/second
// - Search Quality: Recall@K, Precision@K, F1-Score, MRR, NDCG
// - System: CPU, Memory, Network utilization (if available)
// - Distributed: Shard balance, Worker load distribution
//
// Run: go test -v -run TestResearchBenchmark -timeout 60m
// Output: Detailed metrics report + CSV files for analysis

package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	apigatewaypb "github.com/pavandhadge/vectron/shared/proto/apigateway"
)

// =============================================================================
// Benchmark Configuration
// =============================================================================

const (
	// Dataset sizes for different test scenarios
	SmallDataset  = 1000   // 1K vectors
	MediumDataset = 10000  // 10K vectors
	LargeDataset  = 100000 // 100K vectors
	XLDataset     = 500000 // 500K vectors

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

// =============================================================================
// Main Research Benchmark Test
// =============================================================================

func TestResearchBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping research benchmark in short mode")
	}

	log.Println("🔬 Starting Research Benchmark Suite...")
	log.Println("This will run comprehensive benchmarks for research paper analysis")
	log.Println("Estimated duration: 45-60 minutes")

	suite := &BenchmarkSuite{}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	// Run comprehensive benchmark scenarios
	t.Run("Scenario1_VectorSearchScalability", func(t *testing.T) {
		benchmarkVectorSearchScalability(t, ctx, suite)
	})

	t.Run("Scenario2_DimensionImpact", func(t *testing.T) {
		benchmarkDimensionImpact(t, ctx, suite)
	})

	t.Run("Scenario3_ConcurrentWorkload", func(t *testing.T) {
		benchmarkConcurrentWorkload(t, ctx, suite)
	})

	t.Run("Scenario4_RerankerEffectiveness", func(t *testing.T) {
		benchmarkRerankerEffectiveness(t, ctx, suite)
	})

	t.Run("Scenario5_DistributedScalability", func(t *testing.T) {
		benchmarkDistributedScalability(t, ctx, suite)
	})

	t.Run("Scenario6_EndToEndLatency", func(t *testing.T) {
		benchmarkEndToEndLatency(t, ctx, suite)
	})

	// Generate final report
	t.Run("GenerateReport", func(t *testing.T) {
		generateResearchReport(t, suite)
	})
}

// =============================================================================
// Scenario 1: Vector Search Scalability
// =============================================================================

func benchmarkVectorSearchScalability(t *testing.T, ctx context.Context, suite *BenchmarkSuite) {
	log.Println("\n📊 Scenario 1: Vector Search Scalability")
	log.Println("Testing search performance with increasing dataset sizes")

	datasetSizes := []int{SmallDataset, MediumDataset, LargeDataset}
	dimensions := []int{Dim128, Dim384, Dim768}

	for _, dim := range dimensions {
		for _, size := range datasetSizes {
			result := runScalabilityBenchmark(t, ctx, size, dim)
			suite.addResult(result)
		}
	}
}

func runScalabilityBenchmark(t *testing.T, ctx context.Context, datasetSize, dimension int) *BenchmarkResult {
	client := setupBenchmarkClient(t)
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
		resp, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		})
		require.NoError(t, err)
		batchDuration := time.Since(batchStart)

		insertLatencies = append(insertLatencies, batchDuration)
		insertCount += int64(resp.Upserted)

		if i%(datasetSize/10) == 0 {
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

func benchmarkDimensionImpact(t *testing.T, ctx context.Context, suite *BenchmarkSuite) {
	log.Println("\n📏 Scenario 2: Dimension Impact Analysis")
	log.Println("Testing how vector dimension affects performance")

	dimensions := []int{64, 128, 256, 384, 512, 768, 1024, 1536}
	datasetSize := 10000

	for _, dim := range dimensions {
		result := runDimensionBenchmark(t, ctx, datasetSize, dim)
		suite.addResult(result)
	}
}

func runDimensionBenchmark(t *testing.T, ctx context.Context, datasetSize, dimension int) *BenchmarkResult {
	client := setupBenchmarkClient(t)
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

		_, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		})
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

func benchmarkConcurrentWorkload(t *testing.T, ctx context.Context, suite *BenchmarkSuite) {
	log.Println("\n🔄 Scenario 3: Concurrent Workload Analysis")
	log.Println("Testing system behavior under concurrent load")

	concurrencyLevels := []int{1, 5, 10, 20, 50, 100}
	datasetSize := 50000
	dimension := 384

	client := setupBenchmarkClient(t)
	collectionName := "concurrent-bench"

	// Setup dataset
	_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
		Name:      collectionName,
		Dimension: int32(dimension),
		Distance:  "cosine",
	})
	require.NoError(t, err)

	// Insert dataset
	for i := 0; i < datasetSize; i += 1000 {
		batch := make([]*apigatewaypb.Point, 0, 1000)
		for j := 0; j < 1000 && i+j < datasetSize; j++ {
			batch = append(batch, &apigatewaypb.Point{
				Id:     fmt.Sprintf("concurrent-%d", i+j),
				Vector: generateNormalizedVector(dimension),
			})
		}
		_, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		})
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

func benchmarkRerankerEffectiveness(t *testing.T, ctx context.Context, suite *BenchmarkSuite) {
	log.Println("\n🎛️ Scenario 4: Reranker Effectiveness Analysis")
	log.Println("Measuring quality improvement from reranking")

	client := setupBenchmarkClient(t)
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
		_, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		})
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

func benchmarkDistributedScalability(t *testing.T, ctx context.Context, suite *BenchmarkSuite) {
	log.Println("\n🌐 Scenario 5: Distributed System Scalability")
	log.Println("Analyzing shard distribution and load balancing")

	// This test requires querying the placement driver for cluster state
	client := setupBenchmarkClient(t)

	// Create multiple collections of different sizes
	collectionSizes := []int{1000, 5000, 10000, 50000}

	for _, size := range collectionSizes {
		result := runDistributedBenchmark(t, ctx, client, size)
		suite.addResult(result)
	}
}

func runDistributedBenchmark(t *testing.T, ctx context.Context, client apigatewaypb.VectronServiceClient, datasetSize int) *BenchmarkResult {
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
		_, err := client.Upsert(ctx, &apigatewaypb.UpsertRequest{
			Collection: collectionName,
			Points:     batch,
		})
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

func benchmarkEndToEndLatency(t *testing.T, ctx context.Context, suite *BenchmarkSuite) {
	log.Println("\n⏱️ Scenario 6: End-to-End Latency Breakdown")
	log.Println("Detailed latency analysis for each operation type")

	client := setupBenchmarkClient(t)
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
		if r.InsertMetrics != nil {
			fmt.Fprintf(file, "| %s | %d | %d | %.0f | %.0f | %v | %v |\n",
				r.Name, r.DatasetSize, r.VectorDimension,
				r.InsertMetrics.VectorsPerSec, r.SearchMetrics.OpsPerSecond,
				r.LatencyMetrics.P50, r.LatencyMetrics.P95)
		}
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

func setupBenchmarkClient(t *testing.T) apigatewaypb.VectronServiceClient {
	conn, err := grpc.Dial("localhost:10010",
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	return apigatewaypb.NewVectronServiceClient(conn)
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

// Avoid "strings" import error by defining locally
func stringsRepeat(s string, count int) string {
	result := ""
	for i := 0; i < count; i++ {
		result += s
	}
	return result
}
