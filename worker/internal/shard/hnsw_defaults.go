package shard

import (
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pavandhadge/vectron/worker/internal/storage"
)

// DefaultHNSWConfig returns the default HNSW configuration used by workers.
func DefaultHNSWConfig(dim int, distance string, durabilityProfile string, writeSpeedMode bool) storage.HNSWConfig {
	quantizeEnabled := distance == "cosine"
	keepFloatVectors := quantizeEnabled

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
	indexQueueSize := 200000
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
	if v := os.Getenv("VECTRON_INDEX_FLUSH_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			indexFlushInterval = time.Duration(n) * time.Millisecond
		}
	}
	if writeSpeedMode {
		indexBatchSize = 8192
		indexQueueSize = 500000
		if durabilityProfile != "relaxed" {
			indexFlushInterval = 500 * time.Millisecond
		}
	}

	hotEnabled := os.Getenv("VECTRON_HOT_INDEX_ENABLED") == "1"
	hotMaxSize := 30000
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
		M:                        16,
		EfConstruction:           200,
		EfSearch:                 100,
		DistanceMetric:           distance,
		NormalizeVectors:         distance == "cosine",
		QuantizeVectors:          quantizeEnabled,
		QuantizeKeepFloatVectors: keepFloatVectors,
		VectorCompressionEnabled: false,
		MultiStageEnabled:        quantizeEnabled,
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
		MmapVectorsEnabled:       false,
		WALBatchEnabled:          true,
		AsyncIndexingEnabled:     true,
		IndexingQueueSize:        indexQueueSize,
		IndexingBatchSize:        indexBatchSize,
		IndexingFlushInterval:    indexFlushInterval,
		WarmupEnabled:            false,
		WarmupMaxVectors:         10000,
		WarmupDelay:              5 * time.Second,
	}
}

// ParseDurabilityProfile reads the durability profile from env.
func ParseDurabilityProfile() string {
	return strings.ToLower(os.Getenv("VECTRON_DURABILITY_PROFILE"))
}
