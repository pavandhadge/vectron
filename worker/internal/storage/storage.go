package storage

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

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
	Search(query []float32, k int) ([]string, error) // Vector search
	BruteForceSearch(query []float32, k int) ([]string, error)

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
	hnsw      *idxhnsw.HNSW
	opts      *Options
	stop      chan struct{}
	wg        sync.WaitGroup
}

// NewPebbleDB creates a new instance of PebbleDB.
func NewPebbleDB() *PebbleDB {
	return &PebbleDB{}
}

// Search finds the k-nearest neighbors to a query vector.
func (r *PebbleDB) Search(query []float32, k int) ([]string, error) {
	if r.hnsw == nil {
		return nil, errors.New("hnsw index not initialized")
	}

	candidateIDs := r.hnsw.Search(query, k)
	var existingIDs []string

	for _, id := range candidateIDs {
		exists, err := r.Exists([]byte(id))
		if err != nil {
			return nil, fmt.Errorf("failed to check existence of vector %s: %w", id, err)
		}
		if exists {
			existingIDs = append(existingIDs, id)
		}
	}

	return existingIDs, nil
}

// resultHeap is a min-heap of search results.
type resultHeap []result

type result struct {
	id   string
	dist float32
}

func (h resultHeap) Len() int           { return len(h) }
func (h resultHeap) Less(i, j int) bool { return h[i].dist > h[j].dist } // Min-heap
func (h resultHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

// Push and Pop use pointers to modify the slice directly.
func (h *resultHeap) Push(x interface{}) {
	*h = append(*h, x.(result))
}

func (h *resultHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// BruteForceSearch finds the k-nearest neighbors to a query vector using a brute-force scan.
func (r *PebbleDB) BruteForceSearch(query []float32, k int) ([]string, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}

	iter, err := r.db.NewIter(nil)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	h := &resultHeap{}
	heap.Init(h)

	for iter.First(); iter.Valid(); iter.Next() {
		// Assuming keys are vector IDs and values are encoded vectors + metadata
		vec, _, err := decodeVectorWithMeta(iter.Value())
		if err != nil {
			// Skip if not a valid vector
			continue
		}

		dist := idxhnsw.EuclideanDistance(query, vec) // Assuming Euclidean, can be made configurable

		if h.Len() < k {
			heap.Push(h, result{id: string(iter.Key()), dist: dist})
		} else if dist < (*h)[0].dist {
			heap.Pop(h)
			heap.Push(h, result{id: string(iter.Key()), dist: dist})
		}
	}

	if iter.Error() != nil {
		return nil, iter.Error()
	}

	ids := make([]string, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		ids[i] = heap.Pop(h).(result).id
	}

	return ids, nil
}
