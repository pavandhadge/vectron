// idxhnsw/internal.go
package idxhnsw

import (
	"bytes"
	"encoding/gob"
	"math"
	"strconv"
)

type Node struct {
	ID        uint32
	Vec       []float32
	Layer     int
	Neighbors [][]uint32
}

func (h *HNSW) distance(a, b []float32) float32 {
	if h.config.Distance == "cosine" {
		return cosineDistance(a, b)
	}
	return euclideanDistance(a, b)
}

func euclideanDistance(a, b []float32) float32 {
	var sum float32
	for i := range a {
		d := a[i] - b[i]
		sum += d * d
	}
	return sum
}

func cosineDistance(a, b []float32) float32 {
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

func (h *HNSW) delete(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	key64, _ := strconv.ParseUint(id, 10, 64)
	key := uint32(key64)
	if node := h.nodes[key]; node != nil {
		node.Vec = nil
		for _, n := range h.nodes {
			for l := range n.Neighbors {
				n.Neighbors[l] = removeUint32(n.Neighbors[l], key)
			}
			h.persistNode(n)
		}
		delete(h.nodes, key)
	}
	return nil
}

func (h *HNSW) item(id string) ([]float32, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	key64, _ := strconv.ParseUint(id, 10, 64)
	if node := h.nodes[uint32(key64)]; node != nil && node.Vec != nil {
		return append([]float32(nil), node.Vec...), true
	}
	return nil, false
}

func removeUint32(s []uint32, val uint32) []uint32 {
	for i, v := range s {
		if v == val {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
