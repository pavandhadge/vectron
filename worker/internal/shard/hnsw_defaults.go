package shard

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pavandhadge/vectron/worker/internal/storage"
)

// DefaultHNSWConfig returns the default HNSW configuration used by workers.
func DefaultHNSWConfig(dim int, distance string, durabilityProfile string, writeSpeedMode bool) storage.HNSWConfig {
	quantizeEnabled := distance == "cosine"
	// IMPORTANT: Don't keep both float and int8 - doubles memory!
	// Only store int8 quantized in memory for search, not float vectors
	keepFloatVectors := false // Changed from quantizeEnabled - saves ~40% memory!
	mmapEnabled := true
	mmapInitialMB := 4
	if quantizeEnabled && !keepFloatVectors {
		// The current mmap implementation only backs float32 vectors.
		// For int8-only cosine indexes it is unused and just preallocates disk.
		mmapEnabled = false
	}
	if v := os.Getenv("VECTRON_MMAP_VECTORS_ENABLED"); v != "" {
		mmapEnabled = v == "1"
	}
	if v := os.Getenv("VECTRON_MMAP_INITIAL_MB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mmapInitialMB = n
		}
	}

	// Vector compression is optional - consumes computation
	compressionEnabled := os.Getenv("VECTRON_VECTOR_COMPRESSION") == "1"

	adaptiveScale := 1.0
	if dim >= 1024 {
		adaptiveScale = 0.5
	} else if dim >= 768 {
		adaptiveScale = 0.65
	} else if dim >= 512 {
		adaptiveScale = 0.8
	}

	indexFlushInterval := 200 * time.Millisecond
	if durabilityProfile == "relaxed" {
		indexFlushInterval = 500 * time.Millisecond
	}
	indexBatchSize := 4096
	indexQueueSize := 100000 // Reduced from 200000 to prevent excessive memory usage
	autoTune := os.Getenv("VECTRON_INDEX_AUTO_TUNE") == "1"
	if v := os.Getenv("VECTRON_INDEX_BATCH_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			indexBatchSize = n
		}
	}
	if v := os.Getenv("VECTRON_INDEX_QUEUE_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			indexQueueSize = n
		}
	}
	if autoTune {
		tunedBatch, tunedQueue := autoTuneIndexSizing()
		if os.Getenv("VECTRON_INDEX_BATCH_SIZE") == "" {
			indexBatchSize = tunedBatch
		}
		if os.Getenv("VECTRON_INDEX_QUEUE_SIZE") == "" {
			indexQueueSize = tunedQueue
		}
	}
	if v := os.Getenv("VECTRON_INDEX_FLUSH_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			indexFlushInterval = time.Duration(n) * time.Millisecond
		}
	}
	if writeSpeedMode {
		indexBatchSize = 8192
		indexQueueSize = 200000 // Capped to prevent excessive memory
		if durabilityProfile != "relaxed" {
			indexFlushInterval = 500 * time.Millisecond
		}
	}

	// Hot index is DISABLED by default to save memory (creates duplicate HNSW index)
	// Enable via VECTRON_HOT_INDEX_ENABLED=1 if you need faster searches on recent data
	hotEnabled := false
	hotMaxSize := 30000
	if v := os.Getenv("VECTRON_HOT_INDEX_ENABLED"); v != "" {
		hotEnabled = v == "1"
	}
	if v := os.Getenv("VECTRON_HOT_INDEX_MAX"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			hotMaxSize = n
		}
	}
	hotColdScale := 0.6
	if v := os.Getenv("VECTRON_HOT_INDEX_COLD_EF_SCALE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 && f < 1 {
			hotColdScale = f
		}
	}

	return storage.HNSWConfig{
		Dim:                      dim,
		M:                        16,  // Reduced from 32 to save memory - still good connectivity
		EfConstruction:           128, // Reduced from 200 - faster build, less memory during build
		EfSearch:                 50,  // Reduced from 100 for faster searches
		DistanceMetric:           distance,
		NormalizeVectors:         distance == "cosine",
		QuantizeVectors:          quantizeEnabled,
		QuantizeKeepFloatVectors: keepFloatVectors,
		VectorCompressionEnabled: compressionEnabled, // Optional - set VECTRON_VECTOR_COMPRESSION=1
		MultiStageEnabled:        quantizeEnabled,
		EnableNorms:              quantizeEnabled,
		HotIndexEnabled:          hotEnabled,
		HotIndexMaxSize:          hotMaxSize,
		HotIndexColdEfScale:      hotColdScale,
		BulkLoadEnabled:          true,
		BulkLoadThreshold:        1000,
		AdaptiveEfEnabled:        true,
		AdaptiveEfMultiplier:     2,
		AdaptiveEfDimScale:       adaptiveScale,
		MaintenanceEnabled:       false,
		MaintenanceInterval:      30 * time.Minute,
		PruneEnabled:             false,
		PruneMaxNodes:            2000,
		MmapVectorsEnabled:       mmapEnabled,
		MmapInitialMB:            mmapInitialMB,
		WALBatchEnabled:          true,
		AsyncIndexingEnabled:     true,
		IndexingQueueSize:        indexQueueSize,
		IndexingBatchSize:        indexBatchSize,
		IndexingFlushInterval:    indexFlushInterval,
		WarmupEnabled:            true,
		WarmupMaxVectors:         50000,
		WarmupDelay:              2 * time.Second,
		SearchParallelism:        runtime.GOMAXPROCS(0),
		AdaptiveQualityEnabled:   true,
		LowNormThreshold:         0.5,
		LowQualityEfScale:        1.5,
	}
}

func autoTuneIndexSizing() (int, int) {
	procs := runtime.GOMAXPROCS(0)
	tunedBatch := 1024 * procs
	if tunedBatch < 2048 {
		tunedBatch = 2048
	}
	if tunedBatch > 8192 {
		tunedBatch = 8192 // Cap at 8k to prevent excessive memory
	}
	tunedQueue := 25000 * procs
	if tunedQueue < 50000 {
		tunedQueue = 50000
	}
	if tunedQueue > 200000 {
		tunedQueue = 200000 // Cap at 200k to prevent excessive memory in queue
	}
	return tunedBatch, tunedQueue
}

// ParseDurabilityProfile reads the durability profile from env.
func ParseDurabilityProfile() string {
	return strings.ToLower(os.Getenv("VECTRON_DURABILITY_PROFILE"))
}
