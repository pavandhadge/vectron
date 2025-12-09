// idxhnsw/hnsw.go
package idxhnsw

import (
	"encoding/gob"
	"io"
	"math/rand"
	"sync"
	"time"
)

type NodeStore interface {
	Put(key, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
}

type HNSWConfig struct {
	M              int
	EfConstruction int
	EfSearch       int
	MaxLevel       int
	Distance       string // "euclidean" or "cosine"
}

type HNSW struct {
	config       HNSWConfig
	dim          int
	entry        uint32
	maxLayer     int
	nodes        map[uint32]*Node
	store        NodeStore
	mu           sync.RWMutex
	deletedCount int64
	idToUint32   map[string]uint32
	uint32ToID   map[uint32]string
	nextID       uint32
}

// NewHNSW creates a new index
func NewHNSW(store NodeStore, dim int, config HNSWConfig) *HNSW {
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
		config.MaxLevel = 20
	}

	rand.New(rand.NewSource(time.Now().UnixNano()))

	h := &HNSW{
		config:       config,
		dim:          dim,
		store:        store,
		nodes:        make(map[uint32]*Node),
		entry:        0,
		maxLayer:     0,
		deletedCount: 0,
		idToUint32:   make(map[string]uint32),
		uint32ToID:   make(map[uint32]string),
		nextID:       1,
	}
	h.StartCleanup(DefaultCleanupConfig) // auto-start
	return h
}

// Public methods
func (h *HNSW) Add(id string, vec []float32) error   { return h.add(id, vec) }
func (h *HNSW) Search(vec []float32, k int) []string { return h.search(vec, k) }
func (h *HNSW) Delete(id string) error               { return h.delete(id) }
func (h *HNSW) Size() int                            { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.nodes) }
func (h *HNSW) Item(id string) ([]float32, bool)     { return h.item(id) }

func (h *HNSW) Save(w io.Writer) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	enc := gob.NewEncoder(w)
	return enc.Encode(struct {
		Config   HNSWConfig
		Entry    uint32
		MaxLayer int
		Nodes    map[uint32]*Node
	}{h.config, h.entry, h.maxLayer, h.nodes})
}

func (h *HNSW) Load(r io.Reader) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var data struct {
		Config   HNSWConfig
		Entry    uint32
		MaxLayer int
		Nodes    map[uint32]*Node
	}
	if err := gob.NewDecoder(r).Decode(&data); err != nil {
		return err
	}
	h.config, h.entry, h.maxLayer, h.nodes = data.Config, data.Entry, data.MaxLayer, data.Nodes
	return nil
}
