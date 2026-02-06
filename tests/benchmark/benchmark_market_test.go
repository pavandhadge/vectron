// Run: go test -v ./tests/benchmark -run TestMarketTrafficBenchmark -timeout 30m

package main_test

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apigatewaypb "github.com/pavandhadge/vectron/shared/proto/apigateway"
)

// Market-traffic style benchmark: mixed reads/writes, hot queries, repeats, and multi-dimension sweep.
func TestMarketTrafficBenchmark(t *testing.T) {
	// Resolve temp paths relative to repo root (so child processes use correct paths).
	resolveBenchmarkPaths(t)

	// Mirror research benchmark setup to ensure context and temp dirs exist.
	_ = os.RemoveAll(benchmarkDataTempDir)
	if err := os.MkdirAll(benchmarkDataTempDir, 0755); err != nil {
		t.Fatalf("Failed to create data temp dir %s: %v", benchmarkDataTempDir, err)
	}
	if err := os.MkdirAll(benchmarkLogTempDir, 0755); err != nil {
		t.Fatalf("Failed to create log temp dir %s: %v", benchmarkLogTempDir, err)
	}

	suite := &BenchmarkTest{t: t}
	suite.ctx, suite.cancel = context.WithTimeout(context.Background(), 60*time.Minute)
	defer suite.cleanup()
	defer suite.cancel()
	t.Run("SystemStartup", suite.TestSystemStartup)
	t.Run("InitializeAndAuthenticate", suite.TestInitializeAndAuthenticate)

	ctx := suite.getAuthenticatedContext()
	client := suite.apigwClient

	dimList := []int{256, 384, 768, 1024, 1536}
	if v := os.Getenv("MARKET_DIMS"); v != "" {
		dimList = parseDimList(v, dimList)
	}
	baseDataset := envIntDefault("MARKET_DATASET", 50000)
	duration := time.Duration(envIntDefault("MARKET_DURATION_SEC", 180)) * time.Second
	concurrency := envIntDefault("MARKET_CONCURRENCY", 32)
	readRatio := envFloatDefault("MARKET_READ_RATIO", 0.90)
	hotRatio := envFloatDefault("MARKET_HOT_RATIO", 0.10)
	hotQueryRatio := envFloatDefault("MARKET_HOT_QUERY_RATIO", 0.80)
	writeBatch := envIntDefault("MARKET_WRITE_BATCH", 200)
	topK := uint32(envIntDefault("MARKET_TOPK", 10))

	if concurrency < 1 {
		concurrency = 1
	}
	if readRatio < 0 || readRatio > 1 {
		readRatio = 0.90
	}
	if hotRatio < 0.01 || hotRatio > 0.5 {
		hotRatio = 0.10
	}
	if hotQueryRatio < 0 || hotQueryRatio > 1 {
		hotQueryRatio = 0.80
	}
	if writeBatch < 1 {
		writeBatch = 1
	}

	type dimResult struct {
		Dim         int
		Dataset     int
		ReadQPS     float64
		WriteQPS    float64
		VecPerSec   float64
		ReadLat     *LatencyMetrics
		WriteLat    *LatencyMetrics
		ReadErrors  int64
		WriteErrors int64
	}
	results := make([]dimResult, 0, len(dimList))

	log.Printf("\n🧪 Market Traffic Benchmark")
	log.Printf("Dims=%v baseDataset=%d duration=%s concurrency=%d", dimList, baseDataset, duration, concurrency)
	log.Printf("Reads=%.0f%% Writes=%.0f%% HotSet=%.0f%% HotQueries=%.0f%% BatchWrite=%d TopK=%d", readRatio*100, (1-readRatio)*100, hotRatio*100, hotQueryRatio*100, writeBatch, topK)

	for _, dim := range dimList {
		datasetSize := scaleDatasetForDim(baseDataset, dim)
		collection := fmt.Sprintf("market_bench_%d_%d", dim, time.Now().UnixNano())
		_, err := client.CreateCollection(ctx, &apigatewaypb.CreateCollectionRequest{
			Name:      collection,
			Dimension: int32(dim),
			Distance:  "cosine",
		})
		if err != nil {
			t.Fatalf("failed to create collection: %v", err)
		}
		if err := waitForCollectionReady(ctx, client, collection, 120*time.Second); err != nil {
			t.Fatalf("collection not ready: %v", err)
		}

		log.Printf("\n▶️  Dim=%d Dataset=%d Collection=%s", dim, datasetSize, collection)

		// Preload dataset
		vectors := make([][]float32, 0, datasetSize)
		batchSize := 500
		if datasetSize < batchSize {
			batchSize = datasetSize
		}
		startLoad := time.Now()
		for i := 0; i < datasetSize; i += batchSize {
			end := i + batchSize
			if end > datasetSize {
				end = datasetSize
			}
			points := make([]*apigatewaypb.Point, 0, end-i)
			for j := i; j < end; j++ {
				vec := generateNormalizedVector(dim)
				id := fmt.Sprintf("vec_%d", j)
				vectors = append(vectors, vec)
				points = append(points, &apigatewaypb.Point{Id: id, Vector: vec})
			}
			_, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{Collection: collection, Points: points}, 3)
			if err != nil {
				t.Fatalf("failed to preload vectors: %v", err)
			}
		}
		log.Printf("Preload: %d vectors in %s", datasetSize, time.Since(startLoad))

		hotCount := int(math.Max(1, float64(datasetSize)*hotRatio))
		hotVectors := vectors[:hotCount]

		// Workload
		var (
			readOps         int64
			writeOps        int64
			writeVectors    int64
			readErrors      int64
			writeErrors     int64
			searchLatMu     sync.Mutex
			writeLatMu      sync.Mutex
			searchLatencies []time.Duration
			writeLatencies  []time.Duration
		)

		maxSamples := 30000
		start := time.Now()
		endTime := start.Add(duration)
		wg := sync.WaitGroup{}

		for w := 0; w < concurrency; w++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()
				rnd := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)*101))
				for time.Now().Before(endTime) {
					if rnd.Float64() < readRatio {
						// Search
						var qvec []float32
						if rnd.Float64() < hotQueryRatio {
							qvec = hotVectors[rnd.Intn(len(hotVectors))]
						} else {
							qvec = vectors[rnd.Intn(len(vectors))]
						}
						req := &apigatewaypb.SearchRequest{
							Collection: collection,
							Vector:     qvec,
							TopK:       topK,
						}
						t0 := time.Now()
						_, err := client.Search(ctx, req)
						if err != nil {
							atomic.AddInt64(&readErrors, 1)
							continue
						}
						atomic.AddInt64(&readOps, 1)
						if len(searchLatencies) < maxSamples {
							searchLatMu.Lock()
							if len(searchLatencies) < maxSamples {
								searchLatencies = append(searchLatencies, time.Since(t0))
							}
							searchLatMu.Unlock()
						}
					} else {
						// Bulk write
						points := make([]*apigatewaypb.Point, 0, writeBatch)
						base := rnd.Int63()
						for i := 0; i < writeBatch; i++ {
							vec := generateNormalizedVector(dim)
							id := fmt.Sprintf("w_%d_%d", workerID, base+int64(i))
							points = append(points, &apigatewaypb.Point{Id: id, Vector: vec})
						}
						t0 := time.Now()
						_, err := upsertWithRetry(ctx, client, &apigatewaypb.UpsertRequest{Collection: collection, Points: points}, 2)
						if err != nil {
							atomic.AddInt64(&writeErrors, 1)
							continue
						}
						atomic.AddInt64(&writeOps, 1)
						atomic.AddInt64(&writeVectors, int64(writeBatch))
						if len(writeLatencies) < maxSamples {
							writeLatMu.Lock()
							if len(writeLatencies) < maxSamples {
								writeLatencies = append(writeLatencies, time.Since(t0))
							}
							writeLatMu.Unlock()
						}
					}
				}
			}(w)
		}
		wg.Wait()

		elapsed := time.Since(start)
		readQPS := float64(readOps) / elapsed.Seconds()
		writeQPS := float64(writeOps) / elapsed.Seconds()
		vecPerSec := float64(writeVectors) / elapsed.Seconds()

		var readLat, writeLat *LatencyMetrics
		if len(searchLatencies) > 0 {
			readLat = calculateLatencyMetrics(searchLatencies)
		}
		if len(writeLatencies) > 0 {
			writeLat = calculateLatencyMetrics(writeLatencies)
		}

		log.Printf("✅ Dim=%d Results: Reads=%d (%.0f q/s) Writes=%d (%.1f batches/s) Vec/s=%.0f ErrR=%d ErrW=%d",
			dim, readOps, readQPS, writeOps, writeQPS, vecPerSec, readErrors, writeErrors)
		if readLat != nil {
			log.Printf("   Search Latency: Avg=%v P50=%v P95=%v P99=%v", readLat.Average, readLat.P50, readLat.P95, readLat.P99)
		}
		if writeLat != nil {
			log.Printf("   Write Latency: Avg=%v P50=%v P95=%v P99=%v", writeLat.Average, writeLat.P50, writeLat.P95, writeLat.P99)
		}

		results = append(results, dimResult{
			Dim:         dim,
			Dataset:     datasetSize,
			ReadQPS:     readQPS,
			WriteQPS:    writeQPS,
			VecPerSec:   vecPerSec,
			ReadLat:     readLat,
			WriteLat:    writeLat,
			ReadErrors:  readErrors,
			WriteErrors: writeErrors,
		})
	}

	log.Printf("\n📈 Market Benchmark Summary")
	for _, r := range results {
		p50 := time.Duration(0)
		p95 := time.Duration(0)
		if r.ReadLat != nil {
			p50 = r.ReadLat.P50
			p95 = r.ReadLat.P95
		}
		log.Printf("Dim=%d Dataset=%d ReadQPS=%.0f WriteQPS=%.1f Vec/s=%.0f P50=%v P95=%v ErrR=%d ErrW=%d",
			r.Dim, r.Dataset, r.ReadQPS, r.WriteQPS, r.VecPerSec, p50, p95, r.ReadErrors, r.WriteErrors)
	}
}

func envIntDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloatDefault(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

func parseDimList(raw string, fallback []int) []int {
	parts := strings.Split(raw, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func scaleDatasetForDim(base, dim int) int {
	switch {
	case dim >= 1536:
		return int(float64(base) * 0.6)
	case dim >= 1024:
		return int(float64(base) * 0.75)
	case dim >= 768:
		return int(float64(base) * 0.85)
	default:
		return base
	}
}
