// This file defines the storage interface and its implementation using PebbleDB.
// It abstracts the underlying key-value store and provides methods for basic CRUD,
// batch operations, and vector-specific operations like search. It also integrates
// the HNSW index for approximate nearest neighbor search.

package storage

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

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
	StoreVectorBatch(vectors []VectorEntry) error
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

// VectorEntry represents a vector payload for batch operations.
type VectorEntry struct {
	ID       string
	Vector   []float32
	Metadata []byte
}

// PebbleDB is the implementation of the Storage interface using PebbleDB as the backend.
type PebbleDB struct {
	db                         *pebble.DB
	writeOpts                  *pebble.WriteOptions
	hnsw                       *idxhnsw.HNSW // The HNSW index for approximate nearest neighbor search.
	opts                       *Options
	stop                       chan struct{}
	wg                         sync.WaitGroup
	path                       string // The path to the database directory
	hnswWriteCount             uint64
	hnswSnapshotBaseInterval   time.Duration
	hnswSnapshotMaxInterval    time.Duration
	hnswSnapshotWriteThreshold uint64
	hnswSnapshotMu             sync.Mutex
	hnswLastSnapshot           time.Time
	hnswSnapshotLoaded         bool
	hnswRebuildMu              sync.Mutex
	hnswRebuildInProgress      bool
	hnswHot                    *idxhnsw.HNSW
	hotMu                      sync.Mutex
	hotQueue                   []string
	hotSet                     map[string]struct{}
	indexerCh                  chan indexOp
	indexerStop                chan struct{}
	indexerWg                  sync.WaitGroup
}

type indexOpType int

const (
	indexOpAdd indexOpType = iota
	indexOpDelete
)

type indexOp struct {
	opType indexOpType
	id     string
	vector []float32
}

// NewPebbleDB creates a new, uninitialized instance of PebbleDB.
func NewPebbleDB() *PebbleDB {
	return &PebbleDB{}
}

// Search finds the k-nearest neighbors to a query vector using the HNSW index.
// Optimized to use batch existence checks instead of N+1 queries.
func (r *PebbleDB) Search(query []float32, k int) (ids []string, scores []float32, err error) {
	logEnabled := shouldLogStorageHotPath()
	start := time.Now()
	usedHot := false
	usedTwoStage := false
	efUsed := 0
	defer func() {
		if logEnabled {
			logStoragef("storage search dim=%d k=%d ef=%d hot=%t twoStage=%t quant=%t total=%s err=%v",
				len(query),
				k,
				efUsed,
				usedHot,
				usedTwoStage,
				r.opts != nil && r.opts.HNSWConfig.QuantizeVectors,
				time.Since(start),
				err,
			)
		}
	}()

	if r.hnsw == nil {
		err = errors.New("hnsw index not initialized")
		return nil, nil, err
	}
	if r.opts != nil && r.opts.HNSWConfig.Dim > 0 && len(query) != r.opts.HNSWConfig.Dim {
		err = fmt.Errorf("search vector dimension %d does not match index dimension %d", len(query), r.opts.HNSWConfig.Dim)
		return nil, nil, err
	}

	// The HNSW search returns candidate IDs and their distances.
	searchVec := query
	if r.opts != nil && r.opts.HNSWConfig.DistanceMetric == "cosine" && r.opts.HNSWConfig.NormalizeVectors {
		searchVec = idxhnsw.NormalizeVector(query)
	}
	ef := r.opts.HNSWConfig.EfSearch
	if k > 0 {
		adaptive := k * 2
		if adaptive < k {
			adaptive = k
		}
		if adaptive < ef {
			ef = adaptive
		}
	}
	if r.opts != nil && r.opts.HNSWConfig.AdaptiveEfEnabled {
		mult := r.opts.HNSWConfig.AdaptiveEfMultiplier
		if mult <= 0 {
			mult = 2
		}
		adaptive := k * mult
		minEf := r.opts.HNSWConfig.AdaptiveEfMin
		if minEf <= 0 {
			minEf = k
		}
		maxEf := r.opts.HNSWConfig.AdaptiveEfMax
		if maxEf <= 0 {
			maxEf = r.opts.HNSWConfig.EfSearch
		}
		if adaptive < minEf {
			adaptive = minEf
		}
		if adaptive > maxEf {
			adaptive = maxEf
		}
		if adaptive > ef {
			ef = adaptive
		}
	}
	efUsed = ef
	if r.hnswHot != nil && r.opts != nil && r.opts.HNSWConfig.HotIndexEnabled {
		usedHot = true
		hotEf := r.opts.HNSWConfig.HotIndexEf
		if hotEf <= 0 {
			hotEf = ef
		}
		hotIDs, hotScores := r.hnswHot.SearchWithEf(searchVec, k, hotEf)

		coldEf := ef
		scale := r.opts.HNSWConfig.HotIndexColdEfScale
		if scale > 0 && scale < 1 {
			coldEf = int(float64(ef) * scale)
			if coldEf < k {
				coldEf = k
			}
		}

		var coldIDs []string
		var coldScores []float32
		if r.opts.HNSWConfig.MultiStageEnabled && !r.opts.HNSWConfig.QuantizeVectors {
			usedTwoStage = true
			stage1Ef := r.opts.HNSWConfig.Stage1Ef
			if stage1Ef <= 0 {
				stage1Ef = coldEf / 2
			}
			candidateFactor := r.opts.HNSWConfig.Stage1CandidateFactor
			if candidateFactor <= 0 {
				candidateFactor = 4
			}
			coldIDs, coldScores = r.hnsw.SearchTwoStage(searchVec, k, stage1Ef, candidateFactor)
		} else {
			coldIDs, coldScores = r.hnsw.SearchWithEf(searchVec, k, coldEf)
		}

		ids, scores = mergeSearchResults(hotIDs, hotScores, coldIDs, coldScores, k)
		return ids, scores, nil
	}

	if r.opts != nil && r.opts.HNSWConfig.MultiStageEnabled && !r.opts.HNSWConfig.QuantizeVectors {
		usedTwoStage = true
		stage1Ef := r.opts.HNSWConfig.Stage1Ef
		if stage1Ef <= 0 {
			stage1Ef = ef / 2
		}
		candidateFactor := r.opts.HNSWConfig.Stage1CandidateFactor
		if candidateFactor <= 0 {
			candidateFactor = 4
		}
		ids, scores = r.hnsw.SearchTwoStage(searchVec, k, stage1Ef, candidateFactor)
		return ids, scores, nil
	}
	ids, scores = r.hnsw.SearchWithEf(searchVec, k, ef)
	return ids, scores, nil
}

func mergeSearchResults(idsA []string, scoresA []float32, idsB []string, scoresB []float32, k int) ([]string, []float32) {
	type pair struct {
		id    string
		score float32
	}
	merged := make(map[string]float32, len(idsA)+len(idsB))
	for i, id := range idsA {
		if i >= len(scoresA) {
			break
		}
		merged[id] = scoresA[i]
	}
	for i, id := range idsB {
		if i >= len(scoresB) {
			break
		}
		if prev, ok := merged[id]; !ok || scoresB[i] < prev {
			merged[id] = scoresB[i]
		}
	}
	out := make([]pair, 0, len(merged))
	for id, score := range merged {
		out = append(out, pair{id: id, score: score})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].score < out[j].score
	})
	if k > 0 && len(out) > k {
		out = out[:k]
	}
	ids := make([]string, len(out))
	scores := make([]float32, len(out))
	for i, p := range out {
		ids[i] = p.id
		scores[i] = p.score
	}
	return ids, scores
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
