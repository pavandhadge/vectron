// This file defines the data structures and configuration options used by the storage package.
// It includes settings for both the underlying PebbleDB instance and the HNSW index.

package storage

// Options defines the configuration for the PebbleDB instance.
type Options struct {
	Path            string     // The directory path for the database files.
	CreateIfMissing bool       // If true, create the database if it doesn't exist.
	MaxOpenFiles    int        // The maximum number of open files for the database.
	WriteBufferSize int        // The size of the in-memory write buffer (memtable).
	CacheSize       int64      // The size of the block cache.
	HNSWConfig      HNSWConfig // Configuration for the HNSW index.
}

// HNSWConfig defines the configuration for the HNSW index.
type HNSWConfig struct {
	Dim            int    // The dimension of the vectors.
	M              int    // The max number of connections per node per layer.
	EfConstruction int    // The size of the dynamic candidate list during index construction.
	EfSearch       int    // The size of the dynamic candidate list during search.
	DistanceMetric string // The distance metric to use (e.g., "euclidean", "cosine").
	WALEnabled     bool   // If true, enable the write-ahead log for the HNSW index.
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
