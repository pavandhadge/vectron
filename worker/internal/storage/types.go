// This file defines the data structures and configuration options used by the storage package.
// It includes settings for both the underlying PebbleDB instance and the HNSW index.

package storage

import "time"

// Options defines the configuration for the PebbleDB instance.
type Options struct {
	Path                  string     // The directory path for the database files.
	CreateIfMissing       bool       // If true, create the database if it doesn't exist.
	MaxOpenFiles          int        // The maximum number of open files for the database.
	WriteBufferSize       int        // The size of the in-memory write buffer (memtable).
	CacheSize             int64      // The size of the block cache.
	BloomFilterBitsPerKey int        // Bloom filter bits per key (0 disables).
	HNSWConfig            HNSWConfig // Configuration for the HNSW index.
}

// HNSWConfig defines the configuration for the HNSW index.
type HNSWConfig struct {
	Dim                      int           // The dimension of the vectors.
	M                        int           // The max number of connections per node per layer.
	EfConstruction           int           // The size of the dynamic candidate list during index construction.
	EfSearch                 int           // The size of the dynamic candidate list during search.
	DistanceMetric           string        // The distance metric to use (e.g., "euclidean", "cosine").
	WALEnabled               bool          // If true, enable the write-ahead log for the HNSW index.
	PersistNodes             bool          // If true, persist individual HNSW nodes on every update.
	EnableNorms              bool          // If true, store vector norms to speed up cosine distance.
	NormalizeVectors         bool          // If true, normalize vectors for cosine distance.
	QuantizeVectors          bool          // If true, store vectors in int8 form (cosine+normalized only).
	QuantizeKeepFloatVectors bool          // If true, keep float vectors alongside quantized data for exact rerank.
	MultiStageEnabled        bool          // If true, use multi-stage search for latency/recall balance.
	Stage1Ef                 int           // EfSearch for stage 1 (candidate generation).
	Stage1CandidateFactor    int           // Candidate multiplier for stage 2 refinement.
	AdaptiveEfEnabled        bool          // If true, adapt ef based on query k.
	AdaptiveEfMin            int           // Minimum ef when adaptive is enabled.
	AdaptiveEfMax            int           // Maximum ef when adaptive is enabled.
	AdaptiveEfMultiplier     int           // ef = k * multiplier when adaptive is enabled.
	AdaptiveEfDimScale       float64       // Optional scale factor to reduce ef for high dimensions (0 or 1 = disabled).
	SearchParallelism        int           // Parallelism for HNSW search distance computations.
	HotIndexEnabled          bool          // If true, maintain a hot in-memory index for recent vectors.
	HotIndexMaxSize          int           // Max vectors to keep in the hot index.
	HotIndexColdEfScale      float64       // Scale factor for cold ef when hot index is enabled.
	HotIndexEf               int           // EfSearch override for hot index (0 = use ef).
	AsyncIndexingEnabled     bool          // If true, index updates are applied asynchronously.
	IndexingQueueSize        int           // Max queued index operations.
	IndexingBatchSize        int           // Max ops per indexer batch.
	IndexingFlushInterval    time.Duration // Max time to wait before flushing queued ops.
	PruneEnabled             bool          // If true, periodically prune redundant edges.
	PruneMaxNodes            int           // Max nodes to prune per maintenance tick.
	MmapVectorsEnabled       bool          // If true, store vectors in an mmap-backed region.
	MmapInitialMB            int           // Initial mmap file size in MB (0 = auto).
	WarmupEnabled            bool          // If true, warm the index after startup.
	WarmupMaxVectors         int           // Max vectors to touch during warmup.
	WarmupDelay              time.Duration // Delay before warmup starts.
	VectorCompressionEnabled bool          // If true, store vectors in compressed int8 form on disk (cosine+normalized only).
	AdaptiveQualityEnabled   bool          // If true, adjust ef based on query quality.
	LowNormThreshold         float64       // Query norm threshold to consider low quality.
	LowQualityEfScale        float64       // Scale ef for low-quality queries.
	SnapshotInterval         time.Duration // Base interval for HNSW snapshotting.
	SnapshotMaxInterval      time.Duration // Max interval between forced snapshots.
	SnapshotWriteThreshold   uint64        // Writes since last snapshot before deferring.
	BulkLoadEnabled          bool          // If true, allow bulk load path for initial indexing.
	BulkLoadThreshold        int           // Minimum batch size to trigger bulk load.
	MaintenanceEnabled       bool          // If true, run background HNSW maintenance.
	MaintenanceInterval      time.Duration // Interval between maintenance checks.
	RebuildDeletedRatio      float64       // Rebuild when deleted/total ratio exceeds this.
	RebuildMinDeleted        int64         // Minimum deleted nodes before rebuild.
}

// BatchOperations holds a set of write and delete operations to be performed atomically.
type BatchOperations struct {
	Puts    map[string][]byte // A map of keys to values to be inserted or updated.
	Deletes []string          // A slice of keys to be deleted.
}

// KeyValuePair represents a single key-value pair, used for iteration.
type KeyValuePair struct {
	Key   []byte
	Value []byte
}

// Iterator defines the interface for iterating over key-value pairs in the database.
// This allows the storage implementation to be independent of the underlying iterator.
type Iterator interface {
	Valid() bool
	Next() bool
	Seek(key []byte)
	Key() []byte
	Value() []byte
	Close() error
	Error() error
}
