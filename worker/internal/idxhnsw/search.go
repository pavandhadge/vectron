// This file contains the search logic for the HNSW index.
// It implements the greedy search algorithm that traverses the graph from the
// entry point at the top layer down to the bottom layer to find the nearest neighbors.

package idxhnsw

import (
	"container/heap"
	"sort"
)

// candidate represents a node being considered during a search.
// It holds the node's ID and its distance to the query vector.
type candidate struct {
	id   uint32
	dist float32
	node *Node
}

// priorityQueue implements the heap.Interface for a min-heap of candidates,
// ordered by distance. This is used to efficiently manage the list of candidates
// during the search process.
type priorityQueue []candidate

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].dist < pq[j].dist }
func (pq priorityQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }

// Push adds a candidate to the priority queue.
func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(candidate))
}

// Pop removes and returns the candidate with the smallest distance from the priority queue.
func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

// maxPriorityQueue implements heap.Interface for a max-heap of candidates.
type maxPriorityQueue []candidate

func (pq maxPriorityQueue) Len() int            { return len(pq) }
func (pq maxPriorityQueue) Less(i, j int) bool  { return pq[i].dist > pq[j].dist } // Note: > for max-heap
func (pq maxPriorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }
func (pq *maxPriorityQueue) Push(x interface{}) { *pq = append(*pq, x.(candidate)) }
func (pq *maxPriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[:n-1]
	return item
}

// search is the public entry point for finding the k-nearest neighbors to a query vector.
func (h *HNSW) search(vec []float32, k int) ([]string, []float32) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == 0 {
		return nil, nil
	}

	// 1. Find the entry point at the base layer by greedily traversing from the top.
	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > 0; l-- {
		curr = h.searchLayerSingle(vec, curr, l)
	}

	// 2. Perform a more thorough search at the base layer (layer 0).
	results := h.searchLayer(vec, curr, h.config.EfSearch, 0)

	// 3. Sort the final results by distance and take the top K.
	sort.Slice(results, func(i, j int) bool {
		return results[i].dist < results[j].dist
	})
	if len(results) > k {
		results = results[:k]
	}

	// 4. Convert internal IDs back to external string IDs and collect scores.
	ids := make([]string, len(results))
	scores := make([]float32, len(results))
	for i, c := range results {
		ids[i] = h.uint32ToID[c.id]
		scores[i] = c.dist
	}
	return ids, scores
}

// searchLayerSingle performs a greedy search for the closest node to the query vector
// within a single layer. It's used to navigate down the layers of the graph.
func (h *HNSW) searchLayerSingle(vec []float32, start *Node, layer int) *Node {
	bestNode := start
	bestDist := h.distance(vec, start.Vec)

	for {
		foundCloser := false
		for _, nid := range bestNode.Neighbors[layer] {
			n := h.getNode(nid)
			if n == nil {
				continue
			}
			d := h.distance(vec, n.Vec)
			if d < bestDist {
				bestDist = d
				bestNode = n
				foundCloser = true
			}
		}
		if !foundCloser {
			break // Local minimum found.
		}
	}
	return bestNode
}

// searchLayer performs an expanded search within a single layer using a priority queue.
// It explores neighbors of neighbors up to `ef` (efConstruction or efSearch) candidates.
func (h *HNSW) searchLayer(vec []float32, start *Node, ef, layer int) []candidate {
	visited := make(map[uint32]bool)

	// Candidate queue (min-heap) to explore promising nodes.
	candidates := &priorityQueue{}
	heap.Init(candidates)
	heap.Push(candidates, candidate{id: start.ID, dist: h.distance(vec, start.Vec), node: start})

	// Results queue (max-heap) to keep track of the best `ef` candidates found so far.
	results := &maxPriorityQueue{}
	heap.Init(results)
	heap.Push(results, candidate{id: start.ID, dist: h.distance(vec, start.Vec), node: start})

	visited[start.ID] = true

	for candidates.Len() > 0 {
		// Get the closest candidate from the min-heap to explore next.
		c := heap.Pop(candidates).(candidate)

		// If this candidate is further than the furthest in our results, we can stop exploring this path.
		if results.Len() >= ef && c.dist > (*results)[0].dist {
			break
		}

		// Explore the neighbors of the current candidate.
		for _, nid := range c.node.Neighbors[layer] {
			if visited[nid] {
				continue
			}
			visited[nid] = true

			n := h.getNode(nid)
			if n == nil {
				continue
			}
			d := h.distance(vec, n.Vec)

			// If we have room in the results max-heap, or if this node is closer than the furthest one in it...
			if results.Len() < ef || d < (*results)[0].dist {
				heap.Push(candidates, candidate{id: nid, dist: d, node: n})
				heap.Push(results, candidate{id: nid, dist: d, node: n})
				// If the results heap is too large, remove the furthest element.
				if results.Len() > ef {
					heap.Pop(results)
				}
			}
		}
	}

	// The result is a max-heap, so we need to convert it to a simple slice.
	finalResults := make([]candidate, len(*results))
	copy(finalResults, *results)
	return finalResults
}

// selectNeighbors is a helper function used during insertion, not search.
// It selects the top M closest candidates from a list.
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
