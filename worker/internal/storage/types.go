package storage

// Options defines the configuration for the PebbleDB instance.
type Options struct {
	CreateIfMissing bool
	MaxOpenFiles    int
	WriteBufferSize int
	CacheSize       int64
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
	Seek(key []byte)
	Next() bool
	Key() []byte
	Value() []byte
	Error() error
	Close() error
}
