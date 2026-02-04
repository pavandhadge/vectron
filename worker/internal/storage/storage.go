// This file defines the storage interface and its implementation using PebbleDB.
// It abstracts the underlying key-value store and provides methods for basic CRUD,
// batch operations, and vector-specific operations like search. It also integrates
// the HNSW index for approximate nearest neighbor search.

package storage

import (
	"container/heap"
	"errors"
	"fmt"
	"sync"

	"github.com/cockroachdb/pebble"
	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

// Storage is the primary interface for the storage engine.
// It defines a comprehensive set of methods for data manipulation, iteration,
// vector-specific operations, and administrative tasks.
type Storage interface {
	// Core Lifecycle
	Init(path string, options *Options) error
	Close() error
	Status() (string, error)

	// Basic CRUD Operations
	Put(key []byte, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
	Exists(key []byte) (bool, error)

	// Batch Operations for high performance
	BatchWrite(operations BatchOperations) error
	BatchPut(puts map[string][]byte) error
	BatchDelete(keys []string) error

	// Iteration & Scanning
	NewIterator(prefix []byte) (Iterator, error)
	Scan(prefix []byte, limit int) ([]KeyValuePair, error)
	Iterate(prefix []byte, fn func(key, value []byte) bool) error

	// Vector-Specific Operations
	StoreVector(id string, vector []float32, metadata []byte) error
	GetVector(id string) (vector []float32, metadata []byte, err error)
	DeleteVector(id string) error
	Search(query []float32, k int) ([]string, []float32, error) // Vector search using HNSW
	BruteForceSearch(query []float32, k int) ([]string, error)  // Brute-force scan for comparison

	// Advanced Features
	Compact() error
	Flush() error
	Backup(path string) error
	Restore(backupPath string) error

	// Metrics & Stats
	Size() (int64, error)
	EntryCount(prefix []byte) (int64, error)
}

// PebbleDB is the implementation of the Storage interface using PebbleDB as the backend.
type PebbleDB struct {
	db        *pebble.DB
	writeOpts *pebble.WriteOptions
	hnsw      *idxhnsw.HNSW // The HNSW index for approximate nearest neighbor search.
	opts      *Options
	stop      chan struct{}
	wg        sync.WaitGroup
	path      string // The path to the database directory
}

// NewPebbleDB creates a new, uninitialized instance of PebbleDB.
func NewPebbleDB() *PebbleDB {
	return &PebbleDB{}
}

// Search finds the k-nearest neighbors to a query vector using the HNSW index.
// Optimized to use batch existence checks instead of N+1 queries.
func (r *PebbleDB) Search(query []float32, k int) ([]string, []float32, error) {
	if r.hnsw == nil {
		return nil, nil, errors.New("hnsw index not initialized")
	}

	// The HNSW search returns candidate IDs and their distances.
	ids, scores := r.hnsw.Search(query, k)

	// The HNSW index might contain deleted items, so we need to verify existence.
	// Optimized: Use batch existence check instead of N+1 queries
	keys := make([][]byte, len(ids))
	for i, id := range ids {
		keys[i] = vectorKey(id)
	}

	existenceMap, err := r.ExistsBatch(keys)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to check existence of vectors: %w", err)
	}

	var existingIDs []string
	var existingScores []float32
	for i, id := range ids {
		if existenceMap[string(keys[i])] {
			existingIDs = append(existingIDs, id)
			existingScores = append(existingScores, scores[i])
		}
	}

	return existingIDs, existingScores, nil
}

// result represents a single search result.
type result struct {
	id   string
	dist float32
}

// resultHeap is a min-heap of search results, used for efficiently finding the top K items.
type resultHeap []result

func (h resultHeap) Len() int           { return len(h) }
func (h resultHeap) Less(i, j int) bool { return h[i].dist < h[j].dist } // Use < for min-heap behavior.
func (h resultHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

// Push and Pop use pointers to modify the slice directly.
func (h *resultHeap) Push(x interface{}) { *h = append(*h, x.(result)) }
func (h *resultHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// BruteForceSearch finds the k-nearest neighbors by scanning all vectors in the database.
// This is slow and primarily used for testing and comparison against the HNSW index.
func (r *PebbleDB) BruteForceSearch(query []float32, k int) ([]string, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}

	iter := r.db.NewIter(nil)
	defer iter.Close()

	// Use a min-heap to efficiently keep track of the top K results.
	h := &resultHeap{}
	heap.Init(h)

	prefix := []byte("v_")
	for iter.SeekGE(prefix); iter.Valid() && iter.Key() != nil && len(iter.Key()) > 0 && iter.Key()[0] == 'v'; iter.Next() {
		vec, _, err := decodeVectorWithMeta(iter.Value())
		if err != nil || vec == nil {
			continue // Skip deleted or malformed entries.
		}

		dist := idxhnsw.EuclideanDistance(query, vec)

		heap.Push(h, result{id: string(iter.Key()), dist: dist})
		if h.Len() > k {
			heap.Pop(h)
		}
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	// Pop from the heap to get the sorted results.
	ids := make([]string, h.Len())
	for i := h.Len() - 1; i >= 0; i-- {
		ids[i] = heap.Pop(h).(result).id
	}

	return ids, nil
}