// idxhnsw/internal.go
package idxhnsw

import (
	"bytes"
	"encoding/gob"
	"math"
	"strconv"
	"sync/atomic"
	"time"
)

type Node struct {
	ID        uint32
	Vec       []float32
	Layer     int
	Neighbors [][]uint32 // only forward links
}

// --- Distance functions (unchanged) ---
func (h *HNSW) distance(a, b []float32) float32 {
	if h.config.Distance == "cosine" {
		return CosineDistance(a, b)
	}
	return EuclideanDistance(a, b)
}

func EuclideanDistance(a, b []float32) float32 {
	var sum float32
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return sum
}

func CosineDistance(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 2
	}
	return 1 - dot/(float32(math.Sqrt(float64(normA)))*float32(math.Sqrt(float64(normB))))
}

func (h *HNSW) getNode(id uint32) *Node {
	if n, ok := h.nodes[id]; ok && n.Vec != nil {
		return n
	}
	return nil
}

func (h *HNSW) persistNode(n *Node) error {
	key := []byte("hnsw:" + strconv.FormatUint(uint64(n.ID), 10))
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(n); err != nil {
		return err
	}
	return h.store.Put(key, buf.Bytes())
}

// --- INSTANT DELETE (O(1)) ---
func (h *HNSW) delete(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	key, ok := h.idToUint32[id]
	if !ok {
		return nil
	}

	node, exists := h.nodes[key]
	if !exists || node.Vec == nil {
		return nil
	}

	node.Vec = nil
	atomic.AddInt64(&h.deletedCount, 1)
	return h.persistNode(node)
}

func (h *HNSW) item(id string) ([]float32, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	key, ok := h.idToUint32[id]
	if !ok {
		return nil, false
	}
	node := h.nodes[key]
	if node != nil && node.Vec != nil {
		return append([]float32(nil), node.Vec...), true
	}
	return nil, false
}

// --- SUPER EFFICIENT CLEANUP: NO REVERSE LINKS, NO FULL SCAN ---
type CleanupConfig struct {
	Interval   time.Duration
	MaxDeleted int64
	BatchSize  int
	Enabled    bool
}

var DefaultCleanupConfig = CleanupConfig{
	Interval:   30 * time.Minute,
	MaxDeleted: 10_000,
	BatchSize:  5000,
	Enabled:    true,
}

func (h *HNSW) StartCleanup(cfg CleanupConfig) {
	if !cfg.Enabled {
		return
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultCleanupConfig.Interval
	}
	if cfg.MaxDeleted == 0 {
		cfg.MaxDeleted = DefaultCleanupConfig.MaxDeleted
	}
	if cfg.BatchSize == 0 {
		cfg.BatchSize = DefaultCleanupConfig.BatchSize
	}

	go func() {
		ticker := time.NewTicker(cfg.Interval)
		defer ticker.Stop()
		for range ticker.C {
			if atomic.LoadInt64(&h.deletedCount) < cfg.MaxDeleted {
				continue
			}
			h.performCleanup(cfg.BatchSize)
		}
	}()
}

// performCleanup: builds temporary list of affected nodes → no full scan, no reverse links
func (h *HNSW) performCleanup(batchSize int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	removed := 0
	affected := make(map[uint32]bool) // nodes that may have links to deleted nodes

	// Phase 1: Collect all deleted nodes + mark their neighbors as "affected"
	for id, node := range h.nodes {
		if removed >= batchSize {
			break
		}
		if node.Vec != nil {
			continue
		}

		// Mark all its outgoing neighbors as affected
		for l := 0; l < len(node.Neighbors); l++ {
			for _, nid := range node.Neighbors[l] {
				affected[nid] = true
			}
		}

		// Clear its own links
		for l := range node.Neighbors {
			node.Neighbors[l] = node.Neighbors[l][:0]
		}

		delete(h.nodes, id)
		atomic.AddInt64(&h.deletedCount, -1)
		removed++
	}

	// Phase 2: Clean only the affected nodes
	for affectedID := range affected {
		node := h.nodes[affectedID]
		if node == nil || node.Vec == nil {
			continue
		}

		changed := false
		for l := 0; l < len(node.Neighbors); l++ {
			oldLen := len(node.Neighbors[l])
			node.Neighbors[l] = filterDead(node.Neighbors[l], h)
			if len(node.Neighbors[l]) != oldLen {
				changed = true
			}
		}
		if changed {
			_ = h.persistNode(node)
		}
	}

	if removed > 0 {
		h.maybeUpdateEntryPoint()
	}
}

// Helper: remove dead (Vec == nil) nodes from neighbor list
func filterDead(list []uint32, h *HNSW) []uint32 {
	j := 0
	for _, id := range list {
		if h.getNode(id) != nil { // alive
			list[j] = id
			j++
		}
	}
	return list[:j]
}

func (h *HNSW) maybeUpdateEntryPoint() {
	if h.entry == 0 {
		return
	}
	if h.getNode(h.entry) != nil {
		return
	}

	for _, node := range h.nodes {
		if node.Vec != nil && node.Layer == h.maxLayer {
			h.entry = node.ID
			return
		}
	}
	for l := h.maxLayer; l >= 0; l-- {
		for _, node := range h.nodes {
			if node.Vec != nil && node.Layer >= l {
				h.entry = node.ID
				h.maxLayer = l
				return
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
