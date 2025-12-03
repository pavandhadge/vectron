// idxhnsw/insert.go
package idxhnsw

import (
	"errors"
	"math"
	"math/rand"
	"strconv"
)

func (h *HNSW) add(id string, vec []float32) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	key64, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		key64 = rand.Uint64()
	}
	key := uint32(key64)

	if len(vec) != h.dim {
		return errors.New("invalid vector dimension")
	}

	layer := h.randomLayer()
	node := &Node{
		ID:        key,
		Vec:       append([]float32(nil), vec...),
		Layer:     layer,
		Neighbors: make([][]uint32, layer+1),
	}
	for i := range node.Neighbors {
		node.Neighbors[i] = make([]uint32, 0, h.config.M)
	}

	if err := h.persistNode(node); err != nil {
		return err
	}
	h.nodes[key] = node

	if h.entry == 0 {
		h.entry = key
		h.maxLayer = layer
		return nil
	}

	// Insert with HNSW logic
	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > layer; l-- {
		curr = h.searchLayerSingle(vec, curr, l)
	}
	for l := min(layer, h.maxLayer); l >= 0; l-- {
		candidates := h.searchLayer(vec, curr, h.config.EfConstruction, l)
		neighbors := h.selectNeighbors(candidates, h.config.M)
		for _, nid := range neighbors {
			node.Neighbors[l] = append(node.Neighbors[l], nid)
			n := h.getNode(nid)
			n.Neighbors[l] = append(n.Neighbors[l], key)
			h.persistNode(n)
		}
		if len(neighbors) > 0 {
			curr = h.getNode(neighbors[0])
		}
	}

	if layer > h.maxLayer {
		h.maxLayer = layer
		h.entry = key
	}
	return nil
}

func (h *HNSW) randomLayer() int {
	level := 0
	for rand.Float64() < 1/math.Ln2 && level < h.config.MaxLevel {
		level++
	}
	return level
}
