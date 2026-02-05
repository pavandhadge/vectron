// This file contains internal data structures and helper functions for the HNSW index.
// It includes the Node struct, distance functions, and logic for node persistence,
// deletion, and background cleanup of deleted nodes.

package idxhnsw

import (
	"bytes"
	"encoding/gob"
	"math"
	"strconv"
	"sync/atomic"
	"time"
)

// Node represents a single vector (or point) in the HNSW graph.
type Node struct {
	ID        uint32
	Vec       []float32
	QVec      []int8
	Norm      float32
	Layer     int
	Neighbors [][]uint32 // A slice of slices, where Neighbors[i] are the neighbors at layer i.
}

// ======================================================================================
// Distance Functions
// ======================================================================================

// distance calculates the distance between two vectors using the configured metric.
func (h *HNSW) distance(a, b []float32) float32 {
	if h.config.Distance == "cosine" {
		return CosineDistance(a, b)
	}
	return EuclideanDistance(a, b)
}

func (h *HNSW) distanceWithNode(a []float32, b []float32, normB float32) float32 {
	if h.config.Distance == "cosine" && h.config.NormalizeVectors {
		return 1 - dotProductSIMD(a, b)
	}
	if h.config.Distance != "cosine" || !h.config.EnableNorms {
		return h.distance(a, b)
	}
	var dot, normA float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
	}
	if normA == 0 || normB == 0 {
		return 2.0
	}
	return 1 - dot/(float32(math.Sqrt(float64(normA)))*normB)
}

func (h *HNSW) distanceToNode(query []float32, qvec []int8, node *Node) float32 {
	if node == nil {
		return float32(math.MaxFloat32)
	}
	if node.QVec != nil {
		if qvec == nil {
			qvec = quantizeVector(query)
		}
		dot := dotProductInt8SIMD(qvec, node.QVec)
		return 1 - float32(dot)/(127*127)
	}
	return h.distanceWithNode(query, node.Vec, node.Norm)
}

func (h *HNSW) distanceExact(query []float32, node *Node) float32 {
	if node == nil {
		return float32(math.MaxFloat32)
	}
	if node.Vec != nil {
		return h.distanceWithNode(query, node.Vec, node.Norm)
	}
	if node.QVec != nil {
		vec := dequantizeVector(node.QVec)
		norm := float32(0)
		if h.config.Distance == "cosine" && h.config.EnableNorms {
			norm = VectorNorm(vec)
		}
		return h.distanceWithNode(query, vec, norm)
	}
	return float32(math.MaxFloat32)
}

func (h *HNSW) maybeQuantizeQuery(vec []float32) []int8 {
	if !h.config.QuantizeVectors {
		return nil
	}
	return quantizeVector(vec)
}

func (h *HNSW) nodeVector(node *Node) []float32 {
	if node == nil {
		return nil
	}
	if node.Vec != nil {
		return node.Vec
	}
	if node.QVec != nil {
		return dequantizeVector(node.QVec)
	}
	return nil
}

// EuclideanDistance calculates the squared Euclidean distance between two vectors.
func EuclideanDistance(a, b []float32) float32 {
	var sum float32
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return sum
}

// CosineDistance calculates the cosine distance between two vectors.
// The result is in the range [0, 2].
func CosineDistance(a, b []float32) float32 {
	var dot, normA, normB float32
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 2.0 // Return max distance if one of the vectors is a zero vector.
	}
	// Cosine similarity is dot / (normA * normB).
	// Cosine distance is 1 - similarity.
	return 1 - dot/(float32(math.Sqrt(float64(normA)))*float32(math.Sqrt(float64(normB))))
}

// VectorNorm computes the L2 norm for a vector.
func VectorNorm(a []float32) float32 {
	sum := sumSquares(a)
	if sum == 0 {
		return 0
	}
	return float32(math.Sqrt(float64(sum)))
}

// NormalizeVector returns a normalized copy of the vector (L2 norm = 1).
func NormalizeVector(a []float32) []float32 {
	norm := float32(math.Sqrt(float64(sumSquares(a))))
	if norm == 0 {
		return append([]float32(nil), a...)
	}
	out := make([]float32, len(a))
	inv := 1.0 / norm
	for i := range a {
		out[i] = a[i] * float32(inv)
	}
	return out
}

func quantizeVector(vec []float32) []int8 {
	q := make([]int8, len(vec))
	for i, v := range vec {
		if v > 1 {
			v = 1
		} else if v < -1 {
			v = -1
		}
		q[i] = int8(v * 127)
	}
	return q
}

func dequantizeVector(q []int8) []float32 {
	out := make([]float32, len(q))
	for i, v := range q {
		out[i] = float32(v) / 127
	}
	return out
}

func dotProductInt8(a, b []int8) int32 {
	n := len(a)
	var sum0, sum1, sum2, sum3 int32
	var sum4, sum5, sum6, sum7 int32
	i := 0
	for ; i+7 < n; i += 8 {
		sum0 += int32(a[i]) * int32(b[i])
		sum1 += int32(a[i+1]) * int32(b[i+1])
		sum2 += int32(a[i+2]) * int32(b[i+2])
		sum3 += int32(a[i+3]) * int32(b[i+3])
		sum4 += int32(a[i+4]) * int32(b[i+4])
		sum5 += int32(a[i+5]) * int32(b[i+5])
		sum6 += int32(a[i+6]) * int32(b[i+6])
		sum7 += int32(a[i+7]) * int32(b[i+7])
	}
	sum := (sum0 + sum1) + (sum2 + sum3) + (sum4 + sum5) + (sum6 + sum7)
	for ; i < n; i++ {
		sum += int32(a[i]) * int32(b[i])
	}
	return sum
}

func dotProduct(a, b []float32) float32 {
	n := len(a)
	var sum0, sum1, sum2, sum3 float32
	var sum4, sum5, sum6, sum7 float32
	i := 0
	for ; i+7 < n; i += 8 {
		sum0 += a[i] * b[i]
		sum1 += a[i+1] * b[i+1]
		sum2 += a[i+2] * b[i+2]
		sum3 += a[i+3] * b[i+3]
		sum4 += a[i+4] * b[i+4]
		sum5 += a[i+5] * b[i+5]
		sum6 += a[i+6] * b[i+6]
		sum7 += a[i+7] * b[i+7]
	}
	sum := (sum0 + sum1) + (sum2 + sum3) + (sum4 + sum5) + (sum6 + sum7)
	for ; i < n; i++ {
		sum += a[i] * b[i]
	}
	return sum
}

func sumSquares(a []float32) float32 {
	n := len(a)
	var sum0, sum1, sum2, sum3 float32
	var sum4, sum5, sum6, sum7 float32
	i := 0
	for ; i+7 < n; i += 8 {
		sum0 += a[i] * a[i]
		sum1 += a[i+1] * a[i+1]
		sum2 += a[i+2] * a[i+2]
		sum3 += a[i+3] * a[i+3]
		sum4 += a[i+4] * a[i+4]
		sum5 += a[i+5] * a[i+5]
		sum6 += a[i+6] * a[i+6]
		sum7 += a[i+7] * a[i+7]
	}
	sum := (sum0 + sum1) + (sum2 + sum3) + (sum4 + sum5) + (sum6 + sum7)
	for ; i < n; i++ {
		sum += a[i] * a[i]
	}
	return sum
}

// ======================================================================================
// Node Management
// ======================================================================================

// getNode retrieves a node by its internal ID, ensuring it's not marked as deleted.
func (h *HNSW) getNode(id uint32) *Node {
	if n, ok := h.nodes[id]; ok && (n.Vec != nil || n.QVec != nil) {
		return n
	}
	return nil // Return nil if the node doesn't exist or is marked as deleted.
}

// persistNode serializes a node and saves it to the underlying key-value store.
func (h *HNSW) persistNode(n *Node) error {
	if !h.config.PersistNodes {
		return nil
	}
	key := []byte("hnsw:" + strconv.FormatUint(uint64(n.ID), 10))
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(n); err != nil {
		return err
	}
	return h.store.Put(key, buf.Bytes())
}

// delete performs a "soft delete" by nil-ing out the vector of the node.
// This is an O(1) operation. The actual cleanup is done by a background process.
func (h *HNSW) delete(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.deleteNoLock(id)
}

func (h *HNSW) deleteNoLock(id string) error {
	internalID, ok := h.idToUint32[id]
	if !ok {
		return nil // ID not found, nothing to delete.
	}

	node, exists := h.nodes[internalID]
	if !exists || (node.Vec == nil && node.QVec == nil) {
		return nil // Node already deleted.
	}

	node.Vec = nil // Mark as deleted.
	node.QVec = nil
	atomic.AddInt64(&h.deletedCount, 1)

	// Persist the deleted marker.
	return h.persistNode(node)
}

// item retrieves the vector for a given external ID.
func (h *HNSW) item(id string) ([]float32, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	internalID, ok := h.idToUint32[id]
	if !ok {
		return nil, false
	}
	node := h.nodes[internalID]
	if node != nil {
		if node.Vec != nil {
			return append([]float32(nil), node.Vec...), true
		}
		if node.QVec != nil {
			return dequantizeVector(node.QVec), true
		}
	}
	return nil, false
}

// ======================================================================================
// Background Cleanup
// ======================================================================================

// CleanupConfig defines the parameters for the background cleanup process.
type CleanupConfig struct {
	Interval   time.Duration // How often to check if cleanup is needed.
	MaxDeleted int64         // The threshold of deleted nodes to trigger a cleanup.
	BatchSize  int           // The max number of nodes to remove in one cleanup cycle.
	Enabled    bool
}

// DefaultCleanupConfig provides sensible defaults for the cleanup process.
var DefaultCleanupConfig = CleanupConfig{
	Interval:   30 * time.Minute,
	MaxDeleted: 10_000,
	BatchSize:  5000,
	Enabled:    true,
}

// StartCleanup launches a goroutine to periodically clean up deleted nodes.
func (h *HNSW) StartCleanup(cfg CleanupConfig) {
	if !cfg.Enabled {
		return
	}
	// Apply defaults for any zero-value fields.
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

// performCleanup removes deleted nodes from the graph and cleans up dangling links.
// This is an efficient approach that avoids a full graph scan by only checking nodes
// that were neighbors of the deleted nodes.
func (h *HNSW) performCleanup(batchSize int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	removed := 0
	affected := make(map[uint32]bool) // Set of nodes that might have links to deleted nodes.

	// Phase 1: Identify deleted nodes and their neighbors.
	for id, node := range h.nodes {
		if removed >= batchSize {
			break
		}
		if node.Vec != nil || node.QVec != nil { // Skip live nodes.
			continue
		}

		// Mark all of the deleted node's neighbors as "affected".
		for _, layerNeighbors := range node.Neighbors {
			for _, nid := range layerNeighbors {
				affected[nid] = true
			}
		}

		// Fully remove the deleted node from the main map.
		delete(h.nodes, id)
		externalID := h.uint32ToID[id]
		delete(h.idToUint32, externalID)
		delete(h.uint32ToID, id)
		atomic.AddInt64(&h.deletedCount, -1)
		removed++
	}

	// Phase 2: Clean the connection lists of only the affected nodes.
	for affectedID := range affected {
		node := h.nodes[affectedID]
		if node == nil || (node.Vec == nil && node.QVec == nil) {
			continue // This node might have been deleted in the same batch.
		}

		changed := false
		for l := range node.Neighbors {
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

// filterDead creates a new slice containing only the live neighbors.
func filterDead(neighbors []uint32, h *HNSW) []uint32 {
	liveNeighbors := make([]uint32, 0, len(neighbors))
	for _, id := range neighbors {
		if h.getNode(id) != nil { // getNode returns nil for dead nodes.
			liveNeighbors = append(liveNeighbors, id)
		}
	}
	return liveNeighbors
}

// maybeUpdateEntryPoint checks if the current entry point was deleted and finds a new one if necessary.
func (h *HNSW) maybeUpdateEntryPoint() {
	if h.entry == 0 || h.getNode(h.entry) != nil {
		return // Entry point is still valid or doesn't exist.
	}

	// Find a new entry point, preferably at the highest layer.
	for l := h.maxLayer; l >= 0; l-- {
		for _, node := range h.nodes {
			if (node.Vec != nil || node.QVec != nil) && node.Layer == l {
				h.entry = node.ID
				h.maxLayer = l
				return
			}
		}
	}
	// If no nodes are left, reset the entry point.
	h.entry = 0
	h.maxLayer = 0
}

// min returns the minimum of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
