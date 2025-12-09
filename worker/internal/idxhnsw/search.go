// idxhnsw/search.go
package idxhnsw

import (
	"container/heap"
	"sort"
)

// candidate for priority queue
type candidate struct {
	id   uint32
	dist float32
	node *Node
}

// priorityQueue implements heap.Interface (min-heap by distance)
type priorityQueue []candidate

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].dist < pq[j].dist }
func (pq priorityQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(candidate))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

// Search returns k nearest neighbor IDs as strings
func (h *HNSW) search(vec []float32, k int) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == 0 {
		return nil
	}

	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > 0; l-- {
		curr = h.searchLayerSingle(vec, curr, l)
	}

	results := h.searchLayer(vec, curr, h.config.EfSearch, 0)

	// Sort final results and take top-k
	sort.Slice(results, func(i, j int) bool {
		return results[i].dist < results[j].dist
	})
	if len(results) > k {
		results = results[:k]
	}

	ids := make([]string, len(results))
	for i, c := range results {
		ids[i] = h.uint32ToID[c.id]
	}
	return ids
}

// Greedy search in one layer (for descending)
func (h *HNSW) searchLayerSingle(vec []float32, start *Node, layer int) *Node {
	best, bestDist := start, h.distance(vec, start.Vec)
	for _, nid := range start.Neighbors[layer] {
		n := h.getNode(nid)
		if n == nil || n.Vec == nil {
			continue
		}
		d := h.distance(vec, n.Vec)
		if d < bestDist {
			best, bestDist = n, d
		}
	}
	return best
}

// Main layer search with efSearch
func (h *HNSW) searchLayer(vec []float32, start *Node, ef, layer int) []candidate {
	visited := make(map[uint32]bool)

	// Candidate queue (min-heap)
	cq := &priorityQueue{}
	heap.Init(cq)
	heap.Push(cq, candidate{id: start.ID, dist: h.distance(vec, start.Vec), node: start})

	// Result queue (also min-heap, keeps best ef)
	results := &priorityQueue{}
	heap.Init(results)
	heap.Push(results, candidate{id: start.ID, dist: h.distance(vec, start.Vec), node: start})

	visited[start.ID] = true

	for cq.Len() > 0 {
		c := heap.Pop(cq).(candidate)

		// Early exit if current is worse than farthest in results
		if results.Len() >= ef && c.dist >= (*results)[0].dist {
			break
		}

		for _, nid := range c.node.Neighbors[layer] {
			if visited[nid] {
				continue
			}
			visited[nid] = true

			n := h.getNode(nid)
			if n == nil || n.Vec == nil {
				continue
			}
			d := h.distance(vec, n.Vec)

			if results.Len() < ef || d < (*results)[0].dist {
				heap.Push(cq, candidate{id: nid, dist: d, node: n})
				heap.Push(results, candidate{id: nid, dist: d, node: n})
				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}

	return *results
}

// Select top m closest from candidates
func (h *HNSW) selectNeighbors(cands []candidate, m int) []uint32 {
	sort.Slice(cands, func(i, j int) bool { return cands[i].dist < cands[j].dist })
	if len(cands) > m {
		cands = cands[:m]
	}
	ids := make([]uint32, len(cands))
	for i, c := range cands {
		ids[i] = c.id
	}
	return ids
}
