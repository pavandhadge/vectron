// idxhnsw/insert.go
package idxhnsw

import (
	"errors"
	"math"
	"math/rand"
	"sort"
	"strconv"
)

func (h *HNSW) selectNeighborsHeuristic(query []float32, candidates []candidate, M int) []uint32 {
	if len(candidates) == 0 {
		return nil
	}

	// Sort by distance
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	result := make([]uint32, 0, M)
	used := make(map[uint32]bool, M)

	for _, cand := range candidates {
		if len(result) >= M {
			break
		}
		if used[cand.id] {
			continue
		}

		// Diversity check: reject if too close to any already selected
		tooClose := false
		for _, selectedID := range result {
			selectedNode := h.getNode(selectedID)
			candNode := h.getNode(cand.id)
			if selectedNode == nil || candNode == nil {
				continue
			}
			if h.distance(selectedNode.Vec, candNode.Vec) < cand.dist {
				tooClose = true
				break
			}
		}

		if !tooClose {
			result = append(result, cand.id)
			used[cand.id] = true
		}
	}

	// Fallback: fill with closest if not enough diverse ones
	if len(result) < M {
		for _, cand := range candidates {
			if len(result) >= M {
				break
			}
			if !used[cand.id] {
				result = append(result, cand.id)
				used[cand.id] = true
			}
		}
	}

	return result
}

// Helper: convert []uint32 neighbor list → []candidate (for pruning)
func toCandidates(ids []uint32, query []float32, h *HNSW) []candidate {
	cands := make([]candidate, 0, len(ids))
	for _, id := range ids {
		if n := h.getNode(id); n != nil && n.Vec != nil {
			cands = append(cands, candidate{
				id:   id,
				dist: h.distance(query, n.Vec),
				node: n,
			})
		}
	}
	return cands
}

func (h *HNSW) add(id string, vec []float32) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// --- ID handling ---
	key64, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		key64 = rand.Uint64()
	}
	key := uint32(key64)

	if len(vec) != h.dim {
		return errors.New("invalid vector dimension")
	}

	// --- Random layer (exponential decay) ---
	layer := h.randomLayer()

	// --- Create new node ---
	node := &Node{
		ID:        key,
		Vec:       append([]float32(nil), vec...), // deep copy
		Layer:     layer,
		Neighbors: make([][]uint32, layer+1),
	}
	for i := range node.Neighbors {
		node.Neighbors[i] = make([]uint32, 0, h.config.M) // pre-allocate
	}

	// Persist early (in case of crash)
	if err := h.persistNode(node); err != nil {
		return err
	}
	h.nodes[key] = node

	// --- First node ever? ---
	if h.entry == 0 {
		h.entry = key
		h.maxLayer = layer
		return nil
	}

	// --- 1. Greedy descent from top layer down to layer+1 ---
	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > layer; l-- {
		curr = h.searchLayerSingle(vec, curr, l)
	}

	// --- 2. Insert in each layer from new node's max down to 0 ---
	for l := min(layer, h.maxLayer); l >= 0; l-- {
		// Expanded search with efConstruction
		candidates := h.searchLayer(vec, curr, h.config.EfConstruction, l)

		// Use the REAL heuristic (diversity-aware)
		neighbors := h.selectNeighborsHeuristic(vec, candidates, h.config.M)

		// --- Connect bidirectionally with pruning ---
		for _, nid := range neighbors {
			// New node → neighbor
			node.Neighbors[l] = append(node.Neighbors[l], nid)

			// Neighbor → new node (with pruning!)
			neighbor := h.getNode(nid)
			if neighbor == nil {
				continue
			}

			neighbor.Neighbors[l] = append(neighbor.Neighbors[l], key)

			// CRITICAL: Enforce max M neighbors
			if len(neighbor.Neighbors[l]) > h.config.M {
				neighbor.Neighbors[l] = h.selectNeighborsHeuristic(
					neighbor.Vec,
					toCandidates(neighbor.Neighbors[l], neighbor.Vec, h),
					h.config.M,
				)
				h.persistNode(neighbor) // only if changed (optional optimization)
			}
		}

		// Use closest found neighbor as entry for next lower layer
		if len(neighbors) > 0 {
			closest := neighbors[0]
			if n := h.getNode(closest); n != nil {
				curr = n
			}
		}
	}

	// --- Update entry point if we grew upward ---
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
