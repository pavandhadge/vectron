// This file implements the core Hierarchical Navigable Small World (HNSW) graph algorithm.
// HNSW is used for efficient approximate nearest neighbor search. This implementation
// manages the graph structure, including nodes and their connections across different layers.

package idxhnsw

import (
	"encoding/gob"
	"io"
	"math/rand"
	"sync"
	"time"
)

// NodeStore defines the interface for a key-value store used to persist HNSW nodes.
// This allows the index to be larger than memory and to be durable.
type NodeStore interface {
	Put(key, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
}

// HNSWConfig holds the configuration parameters for the HNSW index.
type HNSWConfig struct {
	M              int    // Max number of connections per node per layer.
	EfConstruction int    // Size of the dynamic candidate list during construction.
	EfSearch       int    // Size of the dynamic candidate list during search.
	MaxLevel       int    // The maximum level to which a node can be promoted.
	Distance       string // The distance metric to use ("euclidean" or "cosine").
	PersistNodes   bool   // Whether to persist each node update to the store.
	EnableNorms    bool   // Whether to store vector norms for cosine distance.
}

// HNSW represents the HNSW index.
type HNSW struct {
	config   HNSWConfig
	dim      int    // The dimension of the vectors.
	entry    uint32 // The internal ID of the entry point node.
	maxLayer int    // The highest layer currently in the graph.
	nodes    map[uint32]*Node
	store    NodeStore
	mu       sync.RWMutex

	// Deletion-related fields
	deletedCount int64
	idToUint32   map[string]uint32 // Maps external string IDs to internal uint32 IDs.
	uint32ToID   map[uint32]string // Maps internal uint32 IDs back to external string IDs.
	nextID       uint32
}

// NewHNSW creates a new HNSW index with the given configuration.
func NewHNSW(store NodeStore, dim int, config HNSWConfig) *HNSW {
	// Set default values for configuration if they are not provided.
	if config.M == 0 {
		config.M = 16
	}
	if config.EfConstruction == 0 {
		config.EfConstruction = 200
	}
	if config.EfSearch == 0 {
		config.EfSearch = 100
	}
	if config.MaxLevel == 0 {
		config.MaxLevel = 20 // Corresponds to a theoretical max of 4 billion nodes.
	}

	rand.New(rand.NewSource(time.Now().UnixNano()))

	h := &HNSW{
		config:       config,
		dim:          dim,
		store:        store,
		nodes:        make(map[uint32]*Node),
		entry:        0, // No entry point initially.
		maxLayer:     0,
		deletedCount: 0,
		idToUint32:   make(map[string]uint32),
		uint32ToID:   make(map[uint32]string),
		nextID:       1, // Start internal IDs from 1.
	}
	// Start the background cleanup process for marked-for-deletion nodes.
	h.StartCleanup(DefaultCleanupConfig)
	return h
}

// ======================================================================================
// Public API
// ======================================================================================

// Add inserts a new vector with its ID into the index.
func (h *HNSW) Add(id string, vec []float32) error { return h.add(id, vec) }

// Search finds the k-nearest neighbors for a given vector.
func (h *HNSW) Search(vec []float32, k int) ([]string, []float32) { return h.search(vec, k) }

// Delete marks a vector for deletion by its ID.
func (h *HNSW) Delete(id string) error { return h.delete(id) }

// Size returns the total number of nodes currently in the index.
func (h *HNSW) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes) - int(h.deletedCount)
}

// Item retrieves a vector by its ID.
func (h *HNSW) Item(id string) ([]float32, bool) { return h.item(id) }

// ======================================================================================
// Serialization
// ======================================================================================

// Save serializes the HNSW index to a writer.
// Note: This saves the in-memory representation and might not be suitable for very large indexes.
func (h *HNSW) Save(w io.Writer) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	enc := gob.NewEncoder(w)
	// Create a struct for serialization.
	return enc.Encode(struct {
		Config       HNSWConfig
		Dim          int
		Entry        uint32
		MaxLayer     int
		Nodes        map[uint32]*Node
		IDToUint32   map[string]uint32
		Uint32ToID   map[uint32]string
		NextID       uint32
		DeletedCount int64
	}{
		Config:       h.config,
		Dim:          h.dim,
		Entry:        h.entry,
		MaxLayer:     h.maxLayer,
		Nodes:        h.nodes,
		IDToUint32:   h.idToUint32,
		Uint32ToID:   h.uint32ToID,
		NextID:       h.nextID,
		DeletedCount: h.deletedCount,
	})
}

// Load deserializes the HNSW index from a reader.
func (h *HNSW) Load(r io.Reader) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var data struct {
		Config       HNSWConfig
		Dim          int
		Entry        uint32
		MaxLayer     int
		Nodes        map[uint32]*Node
		IDToUint32   map[string]uint32
		Uint32ToID   map[uint32]string
		NextID       uint32
		DeletedCount int64
	}
	if err := gob.NewDecoder(r).Decode(&data); err != nil {
		return err
	}
	// Restore the state from the loaded data.
	h.config = data.Config
	h.dim = data.Dim
	h.entry = data.Entry
	h.maxLayer = data.MaxLayer
	h.nodes = data.Nodes
	h.idToUint32 = data.IDToUint32
	h.uint32ToID = data.Uint32ToID
	h.nextID = data.NextID
	h.deletedCount = data.DeletedCount
	return nil
}
