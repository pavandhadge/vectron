package storage

// Options defines the configuration for the PebbleDB instance.
type Options struct {
	CreateIfMissing bool
	MaxOpenFiles    int
	WriteBufferSize int
	CacheSize       int64
	HNSWConfig      HNSWConfig
}

// HNSWConfig defines the configuration for the HNSW index.
type HNSWConfig struct {
	Dim            int
	M              int
	EfConstruction int
	EfSearch       int
	DistanceMetric string
	WALEnabled     bool
}

// BatchOperations holds operations to be performed in a batch.
type BatchOperations struct {
	Puts    map[string][]byte
	Deletes []string
}

// KeyValuePair represents a single key-value pair.
type KeyValuePair struct {
	Key   []byte
	Value []byte
}

// Iterator is the interface for a PebbleDB iterator.
type Iterator interface {
	Valid() bool
	Next() bool
	Seek([]byte)
	Key() []byte
	Value() []byte
	Close() error
	Error() error
}
