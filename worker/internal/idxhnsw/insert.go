// This file contains the logic for inserting new nodes into the HNSW graph.
// It includes the main 'add' method and helper functions for selecting neighbors
// and determining the layer for a new node.

package idxhnsw

import (
	"errors"
	"math/rand"
	"sort"
)

// selectNeighborsHeuristic implements the neighbor selection strategy during insertion.
// It aims to select the M best neighbors for a new node from a set of candidates.
// The heuristic prioritizes closer nodes but also includes a diversity mechanism
// to avoid selecting neighbors that are too close to each other, which improves graph quality.
func (h *HNSW) selectNeighborsHeuristic(query []float32, candidates []candidate, M int) []uint32 {
	if len(candidates) == 0 {
		return nil
	}

	// Sort candidates by distance to the query vector.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})

	result := make([]uint32, 0, M)
	used := make(map[uint32]bool)

	// First pass: Try to select diverse neighbors.
	for _, cand := range candidates {
		if len(result) >= M {
			break
		}
		if used[cand.id] {
			continue
		}

		// Diversity check: reject candidate if it's closer to an already selected neighbor
		// than the selected neighbor is to the query vector. This helps to spread out the connections.
		tooClose := false
		for _, selectedID := range result {
			selectedNode := h.getNode(selectedID)
			candNode := h.getNode(cand.id)
			if selectedNode == nil || candNode == nil {
				continue
			}
			// This is a simplified diversity check. A more robust implementation might be needed.
			selectedVec := h.nodeVector(selectedNode)
			if selectedVec == nil {
				continue
			}
			if h.distanceToNode(selectedVec, nil, candNode) < cand.dist {
				tooClose = true
				break
			}
		}

		if !tooClose {
			result = append(result, cand.id)
			used[cand.id] = true
		}
	}

	// Fallback: If not enough diverse neighbors were found, fill the remaining slots
	// with the closest candidates, regardless of diversity.
	if len(result) < M {
		for _, cand := range candidates {
			if len(result) >= M {
				break
			}
			if !used[cand.id] {
				result = append(result, cand.id)
			}
		}
	}

	return result
}

// toCandidates converts a list of neighbor IDs into a list of candidate structs,
// calculating the distance of each to the query vector.
func toCandidates(ids []uint32, query []float32, h *HNSW) []candidate {
	cands := make([]candidate, 0, len(ids))
	for _, id := range ids {
		if n := h.getNode(id); n != nil {
			cands = append(cands, candidate{
				id:   id,
				dist: h.distanceToNode(query, nil, n),
				node: n,
			})
		}
	}
	return cands
}

// add is the main entry point for inserting a new vector into the HNSW graph.
// OPTIMIZATION: Uses vector pooling to reduce GC allocations
func (h *HNSW) add(id string, vec []float32) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if internalID, ok := h.idToUint32[id]; ok {
		// If the ID already exists, treat as delete+add under the same lock.
		if err := h.deleteNoLock(id); err != nil {
			return err
		}
		if int(internalID) < len(h.nodes) && h.nodes[internalID] != nil {
			h.nodes[internalID] = nil
			h.nodeCount--
		}
		delete(h.idToUint32, id)
		delete(h.uint32ToID, internalID)
	}

	return h.addNoLock(id, vec)
}

// makeVectorCopy creates a deep copy of a vector using either mmap storage or the pool allocator.
// This is used during node creation to reduce GC pressure.
func makeVectorCopy(src []float32, h *HNSW) []float32 {
	if h != nil && h.mmapStore != nil {
		if dst, err := h.mmapStore.allocFloat32(len(src)); err == nil && dst != nil {
			copy(dst, src)
			return dst
		}
	}
	// Get a pooled vector with enough capacity
	pooled := getVectorFromPool()
	if cap(pooled) < len(src) {
		// Pool doesn't have enough capacity, allocate new
		putVectorToPool(pooled)
		return append([]float32(nil), src...)
	}

	// Copy into pooled buffer
	pooled = pooled[:len(src)]
	copy(pooled, src)
	return pooled
}

func (h *HNSW) addNoLock(id string, vec []float32) error {
	internalID := h.nextID
	h.idToUint32[id] = internalID
	h.uint32ToID[internalID] = id
	h.nextID++

	if len(vec) != h.dim {
		return errors.New("invalid vector dimension")
	}

	// 1. Determine the random layer for the new node.
	layer := h.randomLayer()

	// 2. Create the new node.
	// OPTIMIZATION: Use pooled/mmap vector allocation instead of new allocation
	searchVec := vec
	var nodeVec []float32
	node := &Node{
		ID:        internalID,
		Vec:       nil,
		Norm:      0,
		Layer:     layer,
		Neighbors: make([][]uint32, layer+1),
	}
	if h.config.Distance == "cosine" && h.config.NormalizeVectors {
		normalized := NormalizeVector(vec)
		searchVec = normalized
		if h.config.QuantizeVectors {
			node.QVec = quantizeVector(normalized)
			if h.config.KeepFloatVectors {
				nodeVec = makeVectorCopy(normalized, h)
				node.Vec = nodeVec
			}
			node.Norm = 1
		} else {
			nodeVec = makeVectorCopy(normalized, h)
			node.Vec = nodeVec
			node.Norm = 1
		}
	} else {
		nodeVec = makeVectorCopy(vec, h)
		node.Vec = nodeVec
		if h.config.Distance == "cosine" && h.config.EnableNorms {
			node.Norm = VectorNorm(node.Vec)
		}
	}
	for i := range node.Neighbors {
		node.Neighbors[i] = make([]uint32, 0, h.config.M)
	}

	// Persist the node to the store before modifying the graph.
	if err := h.persistNode(node); err != nil {
		return err
	}
	h.ensureNodeCapacity(internalID)
	if h.nodes[internalID] == nil {
		h.nodeCount++
	}
	h.nodes[internalID] = node

	// If this is the first node, set it as the entry point.
	if h.entry == 0 {
		h.entry = internalID
		h.maxLayer = layer
		return nil
	}

	curr := h.getNode(h.entry)
	if curr == nil {
		// This can happen if the entry point was deleted.
		// In this case, we make the new node the entry point.
		h.entry = internalID
		h.maxLayer = layer
		return nil
	}

	// 3. Find the entry point for the insertion layer.
	// Start from the top layer of the graph and greedily search down to the new node's layer.
	qvec := h.maybeQuantizeQuery(searchVec)
	for l := h.maxLayer; l > layer; l-- {
		curr = h.searchLayerSingle(searchVec, qvec, curr, l)
	}

	// 4. Iteratively insert the node into each layer from its layer down to 0.
	for l := min(layer, h.maxLayer); l >= 0; l-- {
		// Find the `efConstruction` nearest neighbors to the new node at this layer.
		candidates := h.searchLayer(searchVec, qvec, curr, h.config.EfConstruction, l)

		// Select the best `M` neighbors from the candidates using the heuristic.
		neighbors := h.selectNeighborsHeuristic(vec, candidates, h.config.M)

		// Connect the new node to its selected neighbors and vice-versa.
		node.Neighbors[l] = neighbors
		for _, nid := range neighbors {
			neighbor := h.getNode(nid)
			if neighbor == nil {
				continue
			}

			// Add the new node to the neighbor's connection list.
			neighbor.Neighbors[l] = append(neighbor.Neighbors[l], internalID)

			// Lazy pruning: defer expensive pruning to the maintenance loop.
			if len(neighbor.Neighbors[l]) > h.config.M {
				h.markForPrune(neighbor.ID)
			}
		}

		// Use the closest neighbor found at this layer as the entry point for the next layer down.
		if len(candidates) > 0 {
			if n := h.getNode(candidates[0].id); n != nil {
				curr = n
			}
		}
	}

	// 5. Update the global entry point if the new node's layer is the highest.
	if layer > h.maxLayer {
		h.maxLayer = layer
		h.entry = internalID
	}

	return nil
}

// randomLayer determines the layer for a new node using an exponentially decaying probability.
// This is a core part of the HNSW algorithm.
func (h *HNSW) randomLayer() int {
	level := 0
	// This is the standard way to generate a random level in HNSW.
	// The probability of increasing the level is 1/M.
	for rand.Float64() < (1.0/float64(h.config.M)) && level < h.config.MaxLevel {
		level++
	}
	return level
}
