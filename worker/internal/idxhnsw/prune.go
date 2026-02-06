package idxhnsw

// PruneNeighbors removes redundant edges and drops references to deleted nodes.
// It prunes up to maxNodes nodes (0 = all).
func (h *HNSW) PruneNeighbors(maxNodes int) int {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.config.M <= 0 {
		return 0
	}

	processed := 0
	candidates := h.takePruneCandidates(maxNodes)
	for _, id := range candidates {
		if h.pruneNodeLocked(id) {
			processed++
			if maxNodes > 0 && processed >= maxNodes {
				return processed
			}
		}
	}

	for id := range h.nodes {
		if h.pruneNodeLocked(id) {
			processed++
			if maxNodes > 0 && processed >= maxNodes {
				break
			}
		}
	}
	return processed
}

func (h *HNSW) markForPrune(id uint32) {
	if !h.config.PruneEnabled {
		return
	}
	h.pruneMu.Lock()
	h.pruneCandidates[id] = struct{}{}
	h.pruneMu.Unlock()
}

func (h *HNSW) takePruneCandidates(maxNodes int) []uint32 {
	h.pruneMu.Lock()
	defer h.pruneMu.Unlock()
	if len(h.pruneCandidates) == 0 {
		return nil
	}
	limit := len(h.pruneCandidates)
	if maxNodes > 0 && maxNodes < limit {
		limit = maxNodes
	}
	out := make([]uint32, 0, limit)
	for id := range h.pruneCandidates {
		out = append(out, id)
		delete(h.pruneCandidates, id)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (h *HNSW) pruneNodeLocked(id uint32) bool {
	node := h.nodes[id]
	if node == nil || (node.Vec == nil && node.QVec == nil) {
		return false
	}
	vec := h.nodeVector(node)
	if vec == nil {
		return false
	}
	changed := false
	for l := range node.Neighbors {
		if len(node.Neighbors[l]) == 0 {
			continue
		}
		pruned := h.selectNeighborsHeuristic(vec, toCandidates(node.Neighbors[l], vec, h), h.config.M)
		if len(pruned) != len(node.Neighbors[l]) {
			node.Neighbors[l] = pruned
			changed = true
		}
	}
	if changed {
		_ = h.persistNode(node)
	}
	return changed
}
