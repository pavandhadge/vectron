// This file implements the core Hierarchical Navigable Small World (HNSW) graph algorithm.
// HNSW is used for efficient approximate nearest neighbor search. This implementation
// manages the graph structure, including nodes and their connections across different layers.

package idxhnsw

import (
	"bufio"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"math"
	"math/rand"
	"sync"
	"time"
)

// vectorPool provides a sync.Pool for reusing float32 slices during vector operations
// This significantly reduces GC pressure during high-throughput indexing
var vectorPool = sync.Pool{
	New: func() interface{} {
		// Pre-allocate for common embedding dimensions (384, 768, 1536)
		return make([]float32, 0, 1536)
	},
}

// getVectorFromPool retrieves a float32 slice from the pool
func getVectorFromPool() []float32 {
	return vectorPool.Get().([]float32)
}

// putVectorToPool returns a float32 slice to the pool
func putVectorToPool(vec []float32) {
	if vec != nil && cap(vec) > 0 {
		// Reset length but keep capacity
		vectorPool.Put(vec[:0])
	}
}

// NodeStore defines the interface for a key-value store used to persist HNSW nodes.
// This allows the index to be larger than memory and to be durable.
type NodeStore interface {
	Put(key, value []byte) error
	Get(key []byte) ([]byte, error)
	Delete(key []byte) error
}

// HNSWConfig holds the configuration parameters for the HNSW index.
type HNSWConfig struct {
	M                 int    // Max number of connections per node per layer.
	EfConstruction    int    // Size of the dynamic candidate list during construction.
	EfSearch          int    // Size of the dynamic candidate list during search.
	MaxLevel          int    // The maximum level to which a node can be promoted.
	Distance          string // The distance metric to use ("euclidean" or "cosine").
	PersistNodes      bool   // Whether to persist each node update to the store.
	EnableNorms       bool   // Whether to store vector norms for cosine distance.
	NormalizeVectors  bool   // Whether to normalize vectors for cosine distance.
	QuantizeVectors   bool   // Whether to store vectors in int8 form (cosine+normalized only).
	SearchParallelism int    // Parallelism for search distance computations.
	HotIndex          bool   // If true, this index is a hot in-memory tier.
	PruneEnabled      bool   // If true, periodically prune redundant edges.
	PruneMaxNodes     int    // Max nodes to prune per maintenance tick.
}

// HNSW represents the HNSW index.
type HNSW struct {
	config          HNSWConfig
	dim             int    // The dimension of the vectors.
	entry           uint32 // The internal ID of the entry point node.
	maxLayer        int    // The highest layer currently in the graph.
	nodes           []*Node
	nodeCount       int64
	store           NodeStore
	mu              sync.RWMutex
	mmapStore       *vectorMmap
	pruneMu         sync.Mutex
	pruneCandidates map[uint32]struct{}

	// Deletion-related fields
	deletedCount int64
	idToUint32   map[string]uint32 // Maps external string IDs to internal uint32 IDs.
	uint32ToID   map[uint32]string // Maps internal uint32 IDs back to external string IDs.
	nextID       uint32
}

// HNSWStats reports high-level index statistics for maintenance decisions.
type HNSWStats struct {
	TotalNodes   int64
	DeletedNodes int64
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
	if config.QuantizeVectors && (config.Distance != "cosine" || !config.NormalizeVectors) {
		config.QuantizeVectors = false
	}

	rand.New(rand.NewSource(time.Now().UnixNano()))

	h := &HNSW{
		config:          config,
		dim:             dim,
		store:           store,
		nodes:           make([]*Node, 1),
		nodeCount:       0,
		entry:           0, // No entry point initially.
		maxLayer:        0,
		deletedCount:    0,
		idToUint32:      make(map[string]uint32),
		uint32ToID:      make(map[uint32]string),
		nextID:          1, // Start internal IDs from 1.
		pruneCandidates: make(map[uint32]struct{}),
	}
	// Start the background cleanup process for marked-for-deletion nodes.
	h.StartCleanup(DefaultCleanupConfig)
	return h
}

// SetSearchParallelism updates runtime search parallelism (not persisted).
func (h *HNSW) SetSearchParallelism(n int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.SearchParallelism = n
}

// SetPruneConfig updates runtime pruning configuration (not persisted).
func (h *HNSW) SetPruneConfig(enabled bool, maxNodes int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.config.PruneEnabled = enabled
	h.config.PruneMaxNodes = maxNodes
}

// Warmup touches vectors and neighbor lists to warm CPU caches and OS page cache.
func (h *HNSW) Warmup(maxNodes int) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	visited := 0
	for _, node := range h.nodes {
		if node == nil {
			continue
		}
		if node.Vec != nil {
			_ = node.Vec[0]
		} else if node.QVec != nil {
			_ = node.QVec[0]
		}
		if len(node.Neighbors) > 0 {
			_ = node.Neighbors[0]
		}
		visited++
		if maxNodes > 0 && visited >= maxNodes {
			break
		}
	}
	return visited
}

// ======================================================================================
// Public API
// ======================================================================================

// Add inserts a new vector with its ID into the index.
func (h *HNSW) Add(id string, vec []float32) error { return h.add(id, vec) }

// Search finds the k-nearest neighbors for a given vector.
func (h *HNSW) Search(vec []float32, k int) ([]string, []float32) { return h.search(vec, k) }

// SearchWithEf finds the k-nearest neighbors with a custom efSearch value.
func (h *HNSW) SearchWithEf(vec []float32, k, ef int) ([]string, []float32) {
	return h.searchWithEf(vec, k, ef)
}

// SearchTwoStage performs a fast candidate search followed by exact reranking.
func (h *HNSW) SearchTwoStage(vec []float32, k, stage1Ef, candidateFactor int) ([]string, []float32) {
	return h.searchTwoStage(vec, k, stage1Ef, candidateFactor)
}

// Delete marks a vector for deletion by its ID.
func (h *HNSW) Delete(id string) error { return h.delete(id) }

// Size returns the total number of nodes currently in the index.
func (h *HNSW) Size() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return int(h.nodeCount) - int(h.deletedCount)
}

// Item retrieves a vector by its ID.
func (h *HNSW) Item(id string) ([]float32, bool) { return h.item(id) }

// Stats returns basic statistics about the index.
func (h *HNSW) Stats() HNSWStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return HNSWStats{
		TotalNodes:   h.nodeCount,
		DeletedNodes: h.deletedCount,
	}
}

// Rebuild compacts the index by removing deleted nodes and rebuilding from live vectors.
func (h *HNSW) Rebuild() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	liveIDs := make([]string, 0, h.nodeCount)
	liveVecs := make([][]float32, 0, h.nodeCount)
	for _, node := range h.nodes {
		if node == nil || (node.Vec == nil && node.QVec == nil) {
			continue
		}
		vec := h.nodeVector(node)
		if vec == nil {
			continue
		}
		liveIDs = append(liveIDs, h.uint32ToID[node.ID])
		liveVecs = append(liveVecs, vec)
	}

	// Reset internal state.
	h.nodes = make([]*Node, 1)
	h.nodeCount = 0
	h.idToUint32 = make(map[string]uint32, len(liveIDs))
	h.uint32ToID = make(map[uint32]string, len(liveIDs))
	h.entry = 0
	h.maxLayer = 0
	h.deletedCount = 0
	h.nextID = 1

	for i, vec := range liveVecs {
		if err := h.addNoLock(liveIDs[i], vec); err != nil {
			return err
		}
	}
	return nil
}

// ======================================================================================
// Serialization
// ======================================================================================

const (
	hnswSnapshotMagic   = "HNSW"
	hnswSnapshotVersion = 2
)

// Save serializes the HNSW index to a writer.
// Note: This saves the in-memory representation and might not be suitable for very large indexes.
func (h *HNSW) Save(w io.Writer) error {
	h.mu.RLock()
	defer h.mu.RUnlock()
	bw := bufio.NewWriter(w)
	if _, err := bw.Write([]byte(hnswSnapshotMagic)); err != nil {
		return err
	}
	if err := bw.WriteByte(hnswSnapshotVersion); err != nil {
		return err
	}

	if err := writeUint32(bw, uint32(h.config.M)); err != nil {
		return err
	}
	if err := writeUint32(bw, uint32(h.config.EfConstruction)); err != nil {
		return err
	}
	if err := writeUint32(bw, uint32(h.config.EfSearch)); err != nil {
		return err
	}
	if err := writeUint32(bw, uint32(h.config.MaxLevel)); err != nil {
		return err
	}
	if err := writeString(bw, h.config.Distance); err != nil {
		return err
	}
	if err := writeBool(bw, h.config.PersistNodes); err != nil {
		return err
	}
	if err := writeBool(bw, h.config.EnableNorms); err != nil {
		return err
	}
	if err := writeBool(bw, h.config.NormalizeVectors); err != nil {
		return err
	}
	if err := writeBool(bw, h.config.QuantizeVectors); err != nil {
		return err
	}

	if err := writeUint32(bw, uint32(h.dim)); err != nil {
		return err
	}
	if err := writeUint32(bw, h.entry); err != nil {
		return err
	}
	if err := writeUint32(bw, uint32(h.maxLayer)); err != nil {
		return err
	}
	if err := writeInt64(bw, h.deletedCount); err != nil {
		return err
	}
	if err := writeUint32(bw, h.nextID); err != nil {
		return err
	}

	var nodeCount uint32
	for _, node := range h.nodes {
		if node != nil {
			nodeCount++
		}
	}
	if err := writeUint32(bw, nodeCount); err != nil {
		return err
	}
	for _, node := range h.nodes {
		if node == nil {
			continue
		}
		if err := writeUint32(bw, node.ID); err != nil {
			return err
		}
		if err := writeUint32(bw, uint32(node.Layer)); err != nil {
			return err
		}
		vecType := byte(0)
		if node.Vec != nil {
			vecType = 1
		} else if node.QVec != nil {
			vecType = 2
		}
		if err := writeByte(bw, vecType); err != nil {
			return err
		}
		switch vecType {
		case 1:
			if err := writeUint32(bw, uint32(len(node.Vec))); err != nil {
				return err
			}
			for _, v := range node.Vec {
				if err := writeFloat32(bw, v); err != nil {
					return err
				}
			}
		case 2:
			if err := writeUint32(bw, uint32(len(node.QVec))); err != nil {
				return err
			}
			buf := make([]byte, len(node.QVec))
			for i, v := range node.QVec {
				buf[i] = byte(v)
			}
			if _, err := bw.Write(buf); err != nil {
				return err
			}
		default:
			if err := writeUint32(bw, 0); err != nil {
				return err
			}
		}
		if err := writeFloat32(bw, node.Norm); err != nil {
			return err
		}
		if err := writeUint32(bw, uint32(len(node.Neighbors))); err != nil {
			return err
		}
		for _, layer := range node.Neighbors {
			if err := writeUint32(bw, uint32(len(layer))); err != nil {
				return err
			}
			for _, neighbor := range layer {
				if err := writeUint32(bw, neighbor); err != nil {
					return err
				}
			}
		}
	}

	if err := writeUint32(bw, uint32(len(h.idToUint32))); err != nil {
		return err
	}
	for id, internalID := range h.idToUint32 {
		if err := writeString(bw, id); err != nil {
			return err
		}
		if err := writeUint32(bw, internalID); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// Load deserializes the HNSW index from a reader.
func (h *HNSW) Load(r io.Reader) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	br := bufio.NewReader(r)
	header, err := br.Peek(len(hnswSnapshotMagic) + 1)
	if err == nil && string(header[:len(hnswSnapshotMagic)]) == hnswSnapshotMagic {
		version := header[len(hnswSnapshotMagic)]
		if version != hnswSnapshotVersion && version != 1 {
			return fmt.Errorf("unsupported HNSW snapshot version: %d", version)
		}
		if _, err := br.Discard(len(hnswSnapshotMagic) + 1); err != nil {
			return err
		}

		cfg := HNSWConfig{}
		if cfg.M, err = readUint32AsInt(br); err != nil {
			return err
		}
		if cfg.EfConstruction, err = readUint32AsInt(br); err != nil {
			return err
		}
		if cfg.EfSearch, err = readUint32AsInt(br); err != nil {
			return err
		}
		if cfg.MaxLevel, err = readUint32AsInt(br); err != nil {
			return err
		}
		if cfg.Distance, err = readString(br); err != nil {
			return err
		}
		if cfg.PersistNodes, err = readBool(br); err != nil {
			return err
		}
		if cfg.EnableNorms, err = readBool(br); err != nil {
			return err
		}
		if cfg.NormalizeVectors, err = readBool(br); err != nil {
			return err
		}
		if version == 1 {
			cfg.QuantizeVectors = false
		} else {
			if cfg.QuantizeVectors, err = readBool(br); err != nil {
				return err
			}
		}

		dim, err := readUint32AsInt(br)
		if err != nil {
			return err
		}
		entry, err := readUint32(br)
		if err != nil {
			return err
		}
		maxLayer, err := readUint32AsInt(br)
		if err != nil {
			return err
		}
		deletedCount, err := readInt64(br)
		if err != nil {
			return err
		}
		nextID, err := readUint32(br)
		if err != nil {
			return err
		}

		nodeCount32, err := readUint32(br)
		if err != nil {
			return err
		}
		nodes := make([]*Node, int(nextID)+1)
		for i := uint32(0); i < nodeCount32; i++ {
			id, err := readUint32(br)
			if err != nil {
				return err
			}
			layer, err := readUint32AsInt(br)
			if err != nil {
				return err
			}
			var vec []float32
			var qvec []int8
			if version == 1 {
				vecLen32, err := readUint32(br)
				if err != nil {
					return err
				}
				if vecLen32 > 0 {
					vec = make([]float32, vecLen32)
					for j := uint32(0); j < vecLen32; j++ {
						val, err := readFloat32(br)
						if err != nil {
							return err
						}
						vec[j] = val
					}
				}
			} else {
				vecType, err := readByte(br)
				if err != nil {
					return err
				}
				vecLen32, err := readUint32(br)
				if err != nil {
					return err
				}
				switch vecType {
				case 1:
					if vecLen32 > 0 {
						vec = make([]float32, vecLen32)
						for j := uint32(0); j < vecLen32; j++ {
							val, err := readFloat32(br)
							if err != nil {
								return err
							}
							vec[j] = val
						}
					}
				case 2:
					if vecLen32 > 0 {
						buf := make([]byte, vecLen32)
						if _, err := io.ReadFull(br, buf); err != nil {
							return err
						}
						qvec = make([]int8, vecLen32)
						for j := range buf {
							qvec[j] = int8(buf[j])
						}
					}
				}
			}
			norm, err := readFloat32(br)
			if err != nil {
				return err
			}
			layerCount32, err := readUint32(br)
			if err != nil {
				return err
			}
			neighbors := make([][]uint32, layerCount32)
			for l := uint32(0); l < layerCount32; l++ {
				neighborCount32, err := readUint32(br)
				if err != nil {
					return err
				}
				layerNeighbors := make([]uint32, neighborCount32)
				for n := uint32(0); n < neighborCount32; n++ {
					neighborID, err := readUint32(br)
					if err != nil {
						return err
					}
					layerNeighbors[n] = neighborID
				}
				neighbors[l] = layerNeighbors
			}
			if int(id) >= len(nodes) {
				newSize := int(id) + 1
				tmp := make([]*Node, newSize)
				copy(tmp, nodes)
				nodes = tmp
			}
			nodes[id] = &Node{
				ID:        id,
				Vec:       vec,
				QVec:      qvec,
				Norm:      norm,
				Layer:     layer,
				Neighbors: neighbors,
			}
		}

		idCount32, err := readUint32(br)
		if err != nil {
			return err
		}
		idToUint32 := make(map[string]uint32, idCount32)
		uint32ToID := make(map[uint32]string, idCount32)
		for i := uint32(0); i < idCount32; i++ {
			id, err := readString(br)
			if err != nil {
				return err
			}
			internalID, err := readUint32(br)
			if err != nil {
				return err
			}
			idToUint32[id] = internalID
			uint32ToID[internalID] = id
		}

		h.config = cfg
		h.dim = dim
		h.entry = entry
		h.maxLayer = maxLayer
		h.nodes = nodes
		h.nodeCount = int64(nodeCount32)
		h.idToUint32 = idToUint32
		h.uint32ToID = uint32ToID
		h.nextID = nextID
		h.deletedCount = deletedCount
		return nil
	}

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
	dec := gob.NewDecoder(br)
	if err := dec.Decode(&data); err != nil {
		return err
	}
	h.config = data.Config
	h.dim = data.Dim
	h.entry = data.Entry
	h.maxLayer = data.MaxLayer
	h.nodes = make([]*Node, int(data.NextID)+1)
	for id, node := range data.Nodes {
		if int(id) >= len(h.nodes) {
			newSize := int(id) + 1
			tmp := make([]*Node, newSize)
			copy(tmp, h.nodes)
			h.nodes = tmp
		}
		h.nodes[id] = node
	}
	h.nodeCount = int64(len(data.Nodes))
	h.idToUint32 = data.IDToUint32
	h.uint32ToID = data.Uint32ToID
	h.nextID = data.NextID
	h.deletedCount = data.DeletedCount
	return nil
}

func writeUint32(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func writeByte(w io.Writer, v byte) error {
	_, err := w.Write([]byte{v})
	return err
}

func writeInt64(w io.Writer, v int64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	_, err := w.Write(buf[:])
	return err
}

func writeFloat32(w io.Writer, v float32) error {
	return writeUint32(w, math.Float32bits(v))
}

func writeBool(w io.Writer, v bool) error {
	b := byte(0)
	if v {
		b = 1
	}
	_, err := w.Write([]byte{b})
	return err
}

func writeString(w io.Writer, s string) error {
	if err := writeUint32(w, uint32(len(s))); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func readUint32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func readByte(r io.Reader) (byte, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return buf[0], nil
}

func readUint32AsInt(r io.Reader) (int, error) {
	v, err := readUint32(r)
	return int(v), err
}

func readInt64(r io.Reader) (int64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf[:])), nil
}

func readFloat32(r io.Reader) (float32, error) {
	v, err := readUint32(r)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(v), nil
}

func readBool(r io.Reader) (bool, error) {
	var buf [1]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return false, err
	}
	return buf[0] != 0, nil
}

func readString(r io.Reader) (string, error) {
	n, err := readUint32(r)
	if err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}
