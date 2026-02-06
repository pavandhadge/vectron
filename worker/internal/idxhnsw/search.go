// This file contains the search logic for the HNSW index.
// It implements the greedy search algorithm that traverses the graph from the
// entry point at the top layer down to the bottom layer to find the nearest neighbors.

package idxhnsw


import (
	"container/heap"
	"math"
	"os"
	"runtime"
	"sort"
	"strconv"
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

type visitTracker struct {
	marks []uint32
	epoch uint32
}

var visitedPool = sync.Pool{
	New: func() interface{} {
		return &visitTracker{marks: make([]uint32, 0)}
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

var distanceSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]float32, 0, 256)
	},
}

var tempCandidateSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]candidate, 0, 256)
	},
}

var (
	qnodeSlicePool = sync.Pool{
		New: func() interface{} { return make([][]int8, 0, 256) },
	}
	qidxSlicePool = sync.Pool{
		New: func() interface{} { return make([]int, 0, 256) },
	}
	qtmpSlicePool = sync.Pool{
		New: func() interface{} { return make([]int32, 0, 256) },
	}
	fnodeSlicePool = sync.Pool{
		New: func() interface{} { return make([][]float32, 0, 256) },
	}
	fidxSlicePool = sync.Pool{
		New: func() interface{} { return make([]int, 0, 256) },
	}
	ftmpSlicePool = sync.Pool{
		New: func() interface{} { return make([]float32, 0, 256) },
	}
)

var neighborNodeSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]*Node, 0, 256)
	},
}

var neighborIDSlicePool = sync.Pool{
	New: func() interface{} {
		return make([]uint32, 0, 256)
	},
}

const smallEfThreshold = 32

var neighborCap = func() int {
	v := os.Getenv("VECTRON_HNSW_NEIGHBOR_CAP")
	if v == "" {
		return 0
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}()

func getDistanceSlice(n int) []float32 {
	buf := distanceSlicePool.Get().([]float32)
	if cap(buf) < n {
		return make([]float32, n)
	}
	return buf[:n]
}

func putDistanceSlice(buf []float32) {
	distanceSlicePool.Put(buf[:0])
}

func getTempCandidateSlice(n int) []candidate {
	buf := tempCandidateSlicePool.Get().([]candidate)
	if cap(buf) < n {
		return make([]candidate, 0, n)
	}
	return buf[:0]
}

func putTempCandidateSlice(buf []candidate) {
	tempCandidateSlicePool.Put(buf[:0])
}

func selectTopKByDist(cands []candidate, k int) []candidate {
	if len(cands) <= k || k <= 0 {
		return cands
	}
	lo, hi := 0, len(cands)-1
	for lo <= hi {
		pivot := cands[(lo+hi)/2].dist
		i, j := lo, hi
		for i <= j {
			for cands[i].dist < pivot {
				i++
			}
			for cands[j].dist > pivot {
				j--
			}
			if i <= j {
				cands[i], cands[j] = cands[j], cands[i]
				i++
				j--
			}
		}
		if k-1 <= j {
			hi = j
		} else if k-1 >= i {
			lo = i
		} else {
			break
		}
	}
	return cands[:k]
}

// search is the public entry point for finding the k-nearest neighbors to a query vector.
func (h *HNSW) search(vec []float32, k int) ([]string, []float32) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.entry == 0 {
		return nil, nil
	}

	vec, releaseVec := h.normalizedQuery(vec)
	qvec, releaseQ := h.quantizedQuery(vec)
	defer releaseVec()
	defer releaseQ()

	// 1. Find the entry point at the base layer by greedily traversing from the top.
	curr := h.getNode(h.entry)
	for l := h.maxLayer; l > 0; l-- {
		curr = h.searchLayerSingle(vec, qvec, curr, l)
	}

	// 2. Perform a more thorough search at the base layer (layer 0).
	results := h.searchLayer(vec, qvec, curr, h.config.EfSearch, 0)

	// 3. Sort the final results by distance and take the top K.
	if len(results) > k {
		results = selectTopKByDist(results, k)
	}
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

	vec, releaseVec := h.normalizedQuery(vec)
	qvec, releaseQ := h.quantizedQuery(vec)
	defer releaseVec()
	defer releaseQ()

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
	if len(results) > k {
		results = selectTopKByDist(results, k)
	}
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

	vec, releaseVec := h.normalizedQuery(vec)
	qvec, releaseQ := h.quantizedQuery(vec)
	defer releaseVec()
	defer releaseQ()

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
	if ef <= smallEfThreshold {
		return h.searchLayerSmall(vec, qvec, start, ef, layer)
	}
	tracker := visitedPool.Get().(*visitTracker)
	maxID := int(h.nextID)
	if maxID < 0 {
		maxID = 0
	}
	if len(tracker.marks) <= maxID {
		tracker.marks = make([]uint32, maxID+1)
	}
	tracker.epoch++
	if tracker.epoch == 0 {
		for i := range tracker.marks {
			tracker.marks[i] = 0
		}
		tracker.epoch = 1
	}
	defer visitedPool.Put(tracker)

	// Candidate queue (min-heap) to explore promising nodes.
	candidateSlice := candidateSlicePool.Get().([]candidate)
	candidateSlice = candidateSlice[:0]
	candidates := priorityQueue(candidateSlice)
	heap.Init(&candidates)
	startDist := h.distanceToNode(vec, qvec, start)
	heap.Push(&candidates, candidate{id: start.ID, dist: startDist, node: start})

	// Results queue (max-heap) to keep track of the best `ef` candidates found so far.
	resultSlice := resultSlicePool.Get().([]candidate)
	resultSlice = resultSlice[:0]
	results := maxPriorityQueue(resultSlice)
	heap.Init(&results)
	heap.Push(&results, candidate{id: start.ID, dist: startDist, node: start})

	if int(start.ID) < len(tracker.marks) {
		tracker.marks[start.ID] = tracker.epoch
	}

	for candidates.Len() > 0 {
		// Get the closest candidate from the min-heap to explore next.
		c := heap.Pop(&candidates).(candidate)

		// If this candidate is further than the furthest in our results, we can stop exploring this path.
		if results.Len() >= ef && c.dist > results[0].dist {
			break
		}

		// Explore the neighbors of the current candidate.
		neighbors := c.node.Neighbors[layer]
		if neighborCap > 0 && len(neighbors) > neighborCap {
			neighbors = neighbors[:neighborCap]
		}
		if len(neighbors) == 0 {
			continue
		}

		nodes := neighborNodeSlicePool.Get().([]*Node)
		ids := neighborIDSlicePool.Get().([]uint32)
		if cap(nodes) < len(neighbors) {
			nodes = make([]*Node, 0, len(neighbors))
		} else {
			nodes = nodes[:0]
		}
		if cap(ids) < len(neighbors) {
			ids = make([]uint32, 0, len(neighbors))
		} else {
			ids = ids[:0]
		}
		for _, nid := range neighbors {
			if int(nid) < len(tracker.marks) && tracker.marks[nid] == tracker.epoch {
				continue
			}
			if int(nid) < len(tracker.marks) {
				tracker.marks[nid] = tracker.epoch
			}
			n := h.getNode(nid)
			if n == nil {
				continue
			}
			nodes = append(nodes, n)
			ids = append(ids, nid)
		}
		if len(nodes) == 0 {
			neighborNodeSlicePool.Put(nodes[:0])
			neighborIDSlicePool.Put(ids[:0])
			continue
		}

		distances := getDistanceSlice(len(nodes))
		h.computeDistancesInto(vec, qvec, nodes, distances)
		defer putDistanceSlice(distances)
		if results.Len() >= ef && len(nodes) >= 256 {
			worst := results[0].dist
			parallelism := h.config.SearchParallelism
			if parallelism <= 0 {
				parallelism = runtime.GOMAXPROCS(0)
				if parallelism < 1 {
					parallelism = 1
				}
			}
			if parallelism > 1 {
				chunk := (len(nodes) + parallelism - 1) / parallelism
				locals := make([][]candidate, parallelism)
				var wg sync.WaitGroup
				for w := 0; w < parallelism; w++ {
					start := w * chunk
					if start >= len(nodes) {
						break
					}
					end := start + chunk
					if end > len(nodes) {
						end = len(nodes)
					}
					wg.Add(1)
					go func(idx, s, e int) {
						defer wg.Done()
						local := getTempCandidateSlice(e - s)
						for i := s; i < e; i++ {
							d := distances[i]
							if d < worst {
								local = append(local, candidate{id: ids[i], dist: d, node: nodes[i]})
							}
						}
						locals[idx] = local
					}(w, start, end)
				}
				wg.Wait()
				for _, local := range locals {
					if len(local) == 0 {
						if local != nil {
							putTempCandidateSlice(local)
						}
						continue
					}
					if len(local) > 0 && results.Len() > 0 {
						local = selectTopKByDist(local, results.Len())
					}
					for _, cand := range local {
						if results.Len() < ef || cand.dist < results[0].dist {
							heap.Push(&candidates, cand)
							heap.Push(&results, cand)
							if results.Len() > ef {
								heap.Pop(&results)
							}
						}
					}
					putTempCandidateSlice(local)
				}
				neighborNodeSlicePool.Put(nodes[:0])
				neighborIDSlicePool.Put(ids[:0])
				continue
			}
		}
		if len(nodes) > ef*2 {
			tmp := getTempCandidateSlice(len(nodes))
			for i, n := range nodes {
				d := distances[i]
				if results.Len() < ef || d < results[0].dist {
					tmp = append(tmp, candidate{id: ids[i], dist: d, node: n})
				}
			}
			if len(tmp) > 0 {
				tmp = selectTopKByDist(tmp, ef)
				for _, cand := range tmp {
					if results.Len() < ef || cand.dist < results[0].dist {
						heap.Push(&candidates, cand)
						heap.Push(&results, cand)
						if results.Len() > ef {
							heap.Pop(&results)
						}
					}
				}
			}
			putTempCandidateSlice(tmp)
		} else {
			for i, n := range nodes {
				d := distances[i]
				// If we have room in the results max-heap, or if this node is closer than the furthest one in it...
				if results.Len() < ef || d < results[0].dist {
					heap.Push(&candidates, candidate{id: ids[i], dist: d, node: n})
					heap.Push(&results, candidate{id: ids[i], dist: d, node: n})
					// If the results heap is too large, remove the furthest element.
					if results.Len() > ef {
						heap.Pop(&results)
					}
				}
			}
		}
		neighborNodeSlicePool.Put(nodes[:0])
		neighborIDSlicePool.Put(ids[:0])
	}

	// The result is a max-heap, so we need to convert it to a simple slice.
	finalResults := make([]candidate, len(results))
	copy(finalResults, results)
	candidateSlicePool.Put([]candidate(candidates)[:0])
	resultSlicePool.Put([]candidate(results)[:0])
	return finalResults
}

func (h *HNSW) searchLayerSmall(vec []float32, qvec []int8, start *Node, ef, layer int) []candidate {
	tracker := visitedPool.Get().(*visitTracker)
	maxID := int(h.nextID)
	if maxID < 0 {
		maxID = 0
	}
	if len(tracker.marks) <= maxID {
		tracker.marks = make([]uint32, maxID+1)
	}
	tracker.epoch++
	if tracker.epoch == 0 {
		for i := range tracker.marks {
			tracker.marks[i] = 0
		}
		tracker.epoch = 1
	}
	defer visitedPool.Put(tracker)

	candidateSlice := candidateSlicePool.Get().([]candidate)
	candidateSlice = candidateSlice[:0]
	startDist := h.distanceToNode(vec, qvec, start)
	candidates := append(candidateSlice, candidate{id: start.ID, dist: startDist, node: start})

	resultSlice := resultSlicePool.Get().([]candidate)
	resultSlice = resultSlice[:0]
	results := append(resultSlice, candidate{id: start.ID, dist: startDist, node: start})
	worstDist := float32(math.MaxFloat32)
	worstIdx := 0
	if ef <= 0 {
		ef = 1
	}
	if int(start.ID) < len(tracker.marks) {
		tracker.marks[start.ID] = tracker.epoch
	}

	for len(candidates) > 0 {
		minIdx := 0
		for i := 1; i < len(candidates); i++ {
			if candidates[i].dist < candidates[minIdx].dist {
				minIdx = i
			}
		}
		c := candidates[minIdx]
		last := len(candidates) - 1
		candidates[minIdx] = candidates[last]
		candidates = candidates[:last]

		if len(results) >= ef && c.dist > worstDist {
			break
		}

		neighbors := c.node.Neighbors[layer]
		if neighborCap > 0 && len(neighbors) > neighborCap {
			neighbors = neighbors[:neighborCap]
		}
		if len(neighbors) == 0 {
			continue
		}

		nodes := neighborNodeSlicePool.Get().([]*Node)
		ids := neighborIDSlicePool.Get().([]uint32)
		if cap(nodes) < len(neighbors) {
			nodes = make([]*Node, 0, len(neighbors))
		} else {
			nodes = nodes[:0]
		}
		if cap(ids) < len(neighbors) {
			ids = make([]uint32, 0, len(neighbors))
		} else {
			ids = ids[:0]
		}

		for _, nid := range neighbors {
			if int(nid) < len(tracker.marks) && tracker.marks[nid] == tracker.epoch {
				continue
			}
			if int(nid) < len(tracker.marks) {
				tracker.marks[nid] = tracker.epoch
			}
			n := h.getNode(nid)
			if n == nil {
				continue
			}
			nodes = append(nodes, n)
			ids = append(ids, nid)
		}
		if len(nodes) == 0 {
			neighborNodeSlicePool.Put(nodes[:0])
			neighborIDSlicePool.Put(ids[:0])
			continue
		}

		distances := getDistanceSlice(len(nodes))
		h.computeDistancesInto(vec, qvec, nodes, distances)
		for i, n := range nodes {
			d := distances[i]
			if len(results) < ef || d < worstDist {
				candidates = append(candidates, candidate{id: ids[i], dist: d, node: n})
				if len(results) < ef {
					results = append(results, candidate{id: ids[i], dist: d, node: n})
					if len(results) == ef {
						worstDist = results[0].dist
						worstIdx = 0
						for j := 1; j < len(results); j++ {
							if results[j].dist > worstDist {
								worstDist = results[j].dist
								worstIdx = j
							}
						}
					}
					continue
				}
				if d < worstDist {
					results[worstIdx] = candidate{id: ids[i], dist: d, node: n}
					worstDist = results[0].dist
					worstIdx = 0
					for j := 1; j < len(results); j++ {
						if results[j].dist > worstDist {
							worstDist = results[j].dist
							worstIdx = j
						}
					}
				}
			}
		}
		putDistanceSlice(distances)
		neighborNodeSlicePool.Put(nodes[:0])
		neighborIDSlicePool.Put(ids[:0])
	}

	finalResults := make([]candidate, len(results))
	copy(finalResults, results)
	candidateSlicePool.Put(candidateSlice[:0])
	resultSlicePool.Put(resultSlice[:0])
	return finalResults
}

func (h *HNSW) computeDistancesInto(vec []float32, qvec []int8, nodes []*Node, distances []float32) {
	if len(nodes) == 1 {
		distances[0] = h.distanceToNode(vec, qvec, nodes[0])
		return
	}

	if qvec != nil && dotCgoBatchEnabled && len(nodes) >= dotCgoBatchMin {
		qnodes := qnodeSlicePool.Get().([][]int8)
		if cap(qnodes) < len(nodes) {
			qnodes = make([][]int8, 0, len(nodes))
		} else {
			qnodes = qnodes[:0]
		}
		qidx := qidxSlicePool.Get().([]int)
		if cap(qidx) < len(nodes) {
			qidx = make([]int, 0, len(nodes))
		} else {
			qidx = qidx[:0]
		}
		for i, n := range nodes {
			if n != nil && n.QVec != nil {
				qnodes = append(qnodes, n.QVec)
				qidx = append(qidx, i)
			}
		}
		if len(qnodes) >= dotCgoBatchMin {
			tmp := qtmpSlicePool.Get().([]int32)
			if cap(tmp) < len(qnodes) {
				tmp = make([]int32, len(qnodes))
			} else {
				tmp = tmp[:len(qnodes)]
			}
			if dotProductInt8BatchSIMD(qvec, qnodes, tmp) {
				scale := float32(127 * 127)
				for i, idx := range qidx {
					distances[idx] = 1 - float32(tmp[i])/scale
				}
				for i, n := range nodes {
					if n == nil || n.QVec != nil {
						continue
					}
					distances[i] = h.distanceWithNode(vec, n.Vec, n.Norm)
				}
				qnodeSlicePool.Put(qnodes[:0])
				qidxSlicePool.Put(qidx[:0])
				qtmpSlicePool.Put(tmp[:0])
				return
			}
			qtmpSlicePool.Put(tmp[:0])
		}
		qnodeSlicePool.Put(qnodes[:0])
		qidxSlicePool.Put(qidx[:0])
	}

	if qvec == nil && h.config.Distance == "cosine" && h.config.NormalizeVectors &&
		dotCgoBatchEnabled && len(nodes) >= dotCgoBatchMin {
		fnodes := fnodeSlicePool.Get().([][]float32)
		if cap(fnodes) < len(nodes) {
			fnodes = make([][]float32, 0, len(nodes))
		} else {
			fnodes = fnodes[:0]
		}
		fidx := fidxSlicePool.Get().([]int)
		if cap(fidx) < len(nodes) {
			fidx = make([]int, 0, len(nodes))
		} else {
			fidx = fidx[:0]
		}
		for i, n := range nodes {
			if n != nil && n.Vec != nil {
				fnodes = append(fnodes, n.Vec)
				fidx = append(fidx, i)
			}
		}
		if len(fnodes) >= dotCgoBatchMin {
			tmp := ftmpSlicePool.Get().([]float32)
			if cap(tmp) < len(fnodes) {
				tmp = make([]float32, len(fnodes))
			} else {
				tmp = tmp[:len(fnodes)]
			}
			if dotProductFloatBatchSIMD(vec, fnodes, tmp) {
				for i, idx := range fidx {
					distances[idx] = 1 - tmp[i]
				}
				for i, n := range nodes {
					if n == nil || n.Vec != nil {
						continue
					}
					distances[i] = h.distanceToNode(vec, qvec, n)
				}
				fnodeSlicePool.Put(fnodes[:0])
				fidxSlicePool.Put(fidx[:0])
				ftmpSlicePool.Put(tmp[:0])
				return
			}
			ftmpSlicePool.Put(tmp[:0])
		}
		fnodeSlicePool.Put(fnodes[:0])
		fidxSlicePool.Put(fidx[:0])
	}

	parallelism := h.config.SearchParallelism
	if parallelism <= 0 {
		parallelism = runtime.GOMAXPROCS(0)
		if parallelism < 1 {
			parallelism = 1
		}
	}
	// OPTIMIZATION: Avoid goroutine overhead for small batches.
	minParallelSize := 32
	if parallelism*4 > minParallelSize {
		minParallelSize = parallelism * 4
	}
	if parallelism <= 1 || len(nodes) < minParallelSize {
		for i, n := range nodes {
			distances[i] = h.distanceToNode(vec, qvec, n)
		}
		return
	}

	chunk := (len(nodes) + parallelism - 1) / parallelism
	var wg sync.WaitGroup
	for start := 0; start < len(nodes); start += chunk {
		end := start + chunk
		if end > len(nodes) {
			end = len(nodes)
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				distances[i] = h.distanceToNode(vec, qvec, nodes[i])
			}
		}(start, end)
	}
	wg.Wait()
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
