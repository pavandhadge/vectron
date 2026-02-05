// This file contains the search logic for the HNSW index.
// It implements the greedy search algorithm that traverses the graph from the
// entry point at the top layer down to the bottom layer to find the nearest neighbors.

package idxhnsw

import (
	"container/heap"
	"sort"
	"sync"
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

var visitedPool = sync.Pool{
	New: func() interface{} {
		return make(map[uint32]struct{}, 1024)
	},
}

var candidateSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]candidate, 0, 256)
	},
}

var resultSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]candidate, 0, 256)
	},
}

// search is the public entry point for finding the k-nearest neighbors to a query vector.
func (h *HNSW) search(vec []float32, k int) ([]string, []float32) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == 0 {
		return nil, nil
	}

	if h.config.Distance == "cosine" && h.config.NormalizeVectors {
		vec = NormalizeVector(vec)
	}
	qvec := h.maybeQuantizeQuery(vec)

	// 1. Find the entry point at the base layer by greedily traversing from the top.
	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > 0; l-- {
		curr = h.searchLayerSingle(vec, qvec, curr, l)
	}

	// 2. Perform a more thorough search at the base layer (layer 0).
	results := h.searchLayer(vec, qvec, curr, h.config.EfSearch, 0)

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

// searchWithEf is like search but allows a custom ef parameter.
func (h *HNSW) searchWithEf(vec []float32, k, ef int) ([]string, []float32) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == 0 {
		return nil, nil
	}

	if h.config.Distance == "cosine" && h.config.NormalizeVectors {
		vec = NormalizeVector(vec)
	}
	qvec := h.maybeQuantizeQuery(vec)

	if ef < k {
		ef = k
	}

	// 1. Find the entry point at the base layer by greedily traversing from the top.
	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > 0; l-- {
		curr = h.searchLayerSingle(vec, qvec, curr, l)
	}

	// 2. Perform a more thorough search at the base layer (layer 0).
	results := h.searchLayer(vec, qvec, curr, ef, 0)

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

// searchTwoStage performs a fast candidate search followed by exact reranking.
func (h *HNSW) searchTwoStage(vec []float32, k, stage1Ef, candidateFactor int) ([]string, []float32) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == 0 {
		return nil, nil
	}
	if k <= 0 {
		return nil, nil
	}
	if stage1Ef < k {
		stage1Ef = k
	}
	if candidateFactor <= 0 {
		candidateFactor = 4
	}

	if h.config.Distance == "cosine" && h.config.NormalizeVectors {
		vec = NormalizeVector(vec)
	}
	qvec := h.maybeQuantizeQuery(vec)

	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > 0; l-- {
		curr = h.searchLayerSingle(vec, qvec, curr, l)
	}

	candidates := h.searchLayer(vec, qvec, curr, stage1Ef, 0)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})
	maxCandidates := k * candidateFactor
	if maxCandidates < k {
		maxCandidates = k
	}
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	for i := range candidates {
		candidates[i].dist = h.distanceExact(vec, candidates[i].node)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].dist < candidates[j].dist
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}

	ids := make([]string, len(candidates))
	scores := make([]float32, len(candidates))
	for i, c := range candidates {
		ids[i] = h.uint32ToID[c.id]
		scores[i] = c.dist
	}
	return ids, scores
}

// searchLayerSingle performs a greedy search for the closest node to the query vector
// within a single layer. It's used to navigate down the layers of the graph.
func (h *HNSW) searchLayerSingle(vec []float32, qvec []int8, start *Node, layer int) *Node {
	bestNode := start
	bestDist := h.distanceToNode(vec, qvec, start)

	for {
		foundCloser := false
		for _, nid := range bestNode.Neighbors[layer] {
			n := h.getNode(nid)
			if n == nil {
				continue
			}
			d := h.distanceToNode(vec, qvec, n)
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
func (h *HNSW) searchLayer(vec []float32, qvec []int8, start *Node, ef, layer int) []candidate {
	visited := visitedPool.Get().(map[uint32]struct{})
	for key := range visited {
		delete(visited, key)
	}
	defer visitedPool.Put(visited)

	// Candidate queue (min-heap) to explore promising nodes.
	candidateSlice := candidateSlicePool.Get().([]candidate)
	candidateSlice = candidateSlice[:0]
	candidates := priorityQueue(candidateSlice)
	heap.Init(&candidates)
	heap.Push(&candidates, candidate{id: start.ID, dist: h.distanceToNode(vec, qvec, start), node: start})

	// Results queue (max-heap) to keep track of the best `ef` candidates found so far.
	resultSlice := resultSlicePool.Get().([]candidate)
	resultSlice = resultSlice[:0]
	results := maxPriorityQueue(resultSlice)
	heap.Init(&results)
	heap.Push(&results, candidate{id: start.ID, dist: h.distanceToNode(vec, qvec, start), node: start})

	visited[start.ID] = struct{}{}

	for candidates.Len() > 0 {
		// Get the closest candidate from the min-heap to explore next.
		c := heap.Pop(&candidates).(candidate)

		// If this candidate is further than the furthest in our results, we can stop exploring this path.
		if results.Len() >= ef && c.dist > results[0].dist {
			break
		}

		// Explore the neighbors of the current candidate.
		for _, nid := range c.node.Neighbors[layer] {
			if _, ok := visited[nid]; ok {
				continue
			}
			visited[nid] = struct{}{}

			n := h.getNode(nid)
			if n == nil {
				continue
			}
			d := h.distanceToNode(vec, qvec, n)

			// If we have room in the results max-heap, or if this node is closer than the furthest one in it...
			if results.Len() < ef || d < results[0].dist {
				heap.Push(&candidates, candidate{id: nid, dist: d, node: n})
				heap.Push(&results, candidate{id: nid, dist: d, node: n})
				// If the results heap is too large, remove the furthest element.
				if results.Len() > ef {
					heap.Pop(&results)
				}
			}
		}
	}

	// The result is a max-heap, so we need to convert it to a simple slice.
	finalResults := make([]candidate, len(results))
	copy(finalResults, results)
	candidateSlicePool.Put([]candidate(candidates)[:0])
	resultSlicePool.Put([]candidate(results)[:0])
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
