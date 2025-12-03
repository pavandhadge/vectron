package storage

import "github.com/cockroachdb/pebble"

// Storage is the interface for the PebbleDB storage engine.
type Storage interface {
	// Core Lifecycle
	Init(path string, options *Options) error // Open/create DB
	Close() error                             // Clean shutdown
	Status() (string, error)                  // Check DB health

	// Basic CRUD Operations
	Put(key []byte, value []byte) error // Single insert/update
	Get(key []byte) ([]byte, error)     // Single read
	Delete(key []byte) error            // Single delete
	Exists(key []byte) (bool, error)    // Key existence check

	// Batch Operations (High Performance)
	BatchWrite(operations BatchOperations) error // Atomic batch ops
	BatchPut(puts map[string][]byte) error       // Batch inserts only
	BatchDelete(keys []string) error             // Batch deletes only

	// Iteration & Scanning
	NewIterator(prefix []byte) (Iterator, error)                  // Prefix iterator
	Scan(prefix []byte, limit int) ([]KeyValuePair, error)        // Limited prefix scan
	Iterate(prefix []byte, fn func(key, value []byte) bool) error // Callback iterator

	// Vector-Specific Operations
	StoreVector(id string, vector []float32, metadata []byte) error
	GetVector(id string) (vector []float32, metadata []byte, err error)
	DeleteVector(id string) error

	// Advanced Features
	Compact() error                  // Manual compaction
	Flush() error                    // Force write to disk
	Backup(path string) error        // Create backup
	Restore(backupPath string) error // Restore from backup

	// Metrics & Stats
	Size() (int64, error)                    // DB size on disk
	EntryCount(prefix []byte) (int64, error) // Count entries
}

// PebbleDB is the implementation of the Storage interface using PebbleDB.
type PebbleDB struct {
	iterOpts  *pebble.IterOptions
	db        *pebble.DB
	writeOpts *pebble.WriteOptions
}

// NewPebbleDB creates a new instance of PebbleDB.
func NewPebbleDB() *PebbleDB {
	return &PebbleDB{}
}
