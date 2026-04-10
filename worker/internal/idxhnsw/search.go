// This file contains the search logic for the HNSW index.
// It implements the greedy search algorithm that traverses the graph from the
// entry point at the top layer down to the bottom layer to find the nearest neighbors.

package idxhnsw

import (
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

type visitTracker struct {
	marks []uint32
	epoch uint32
}

var visitedPool = sync.Pool{
	New: func() interface{} {
		return &visitTracker{marks: make([]uint32, 0)}
	},
}

// Max visit tracker marks size to prevent unbounded memory growth.
// If a tracker's marks array exceeds this, it won't be returned to the pool.
var maxVisitTrackerMarksSize = 1 << 20 // 1M entries = 4MB per tracker

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
const maxPoolSliceCap = 4096 // Cap pool slices to prevent unbounded memory growth

// putCandidateSlice returns a candidate slice to the pool, nil-ing node pointers
// to avoid preventing GC of deleted nodes. Oversized slices are discarded.
func putCandidateSlice(pool *sync.Pool, s []candidate) {
	// Nil out node pointers so GC can collect nodes
	for i := range s {
		s[i].node = nil
	}
	// Also nil out entries beyond len but within cap (from previous uses)
	extra := cap(s) - len(s)
	if extra > 0 {
		full := s[:cap(s)]
		for i := len(s); i < len(s)+extra && i < len(s)+64; i++ {
			full[i].node = nil
		}
	}
	if cap(s) > maxPoolSliceCap {
		return // Discard oversized slices
	}
	pool.Put(s[:0])
}

// putVisitTracker returns a visit tracker to the pool, discarding oversized ones.
func putVisitTracker(t *visitTracker) {
	if cap(t.marks) > maxVisitTrackerMarksSize {
		return // Discard oversized tracker, let GC collect it
	}
	visitedPool.Put(t)
}
func putUint32Slice(pool *sync.Pool, s []uint32) {
	if cap(s) > maxPoolSliceCap {
		return
	}
	pool.Put(s[:0])
}

// putNodeSlice returns a node slice to the pool, nil-ing pointers and capping.
func putNodeSlice(pool *sync.Pool, s []*Node) {
	for i := range s {
		s[i] = nil
	}
	if cap(s) > maxPoolSliceCap {
		return
	}
	pool.Put(s[:0])
}

// Manual heap operations for min-heap (candidate queue).
// These avoid interface{} boxing from container/heap, giving ~2-3x speedup
// on Push/Pop in the tight inner loop of searchLayer.
func candHeapPush(h *[]candidate, c candidate) {
	*h = append(*h, c)
	candHeapUp(*h, len(*h)-1)
}

func candHeapPop(h *[]candidate) candidate {
	old := *h
	n := len(old)
	old[0], old[n-1] = old[n-1], old[0]
	candHeapDown(*h, 0, n-1)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func candHeapUp(h []candidate, j int) {
	for {
		i := (j - 1) / 2
		if i == j || !(h[j].dist < h[i].dist) {
			break
		}
		h[i], h[j] = h[j], h[i]
		j = i
	}
}

func candHeapDown(h []candidate, i, n int) {
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && h[j2].dist < h[j1].dist {
			j = j2
		}
		if !(h[j].dist < h[i].dist) {
			break
		}
		h[i], h[j] = h[j], h[i]
		i = j
	}
}

// Manual heap operations for max-heap (results queue).
func resHeapPush(h *[]candidate, c candidate) {
	*h = append(*h, c)
	resHeapUp(*h, len(*h)-1)
}

func resHeapPop(h *[]candidate) candidate {
	old := *h
	n := len(old)
	old[0], old[n-1] = old[n-1], old[0]
	resHeapDown(*h, 0, n-1)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func resHeapUp(h []candidate, j int) {
	for {
		i := (j - 1) / 2
		if i == j || !(h[j].dist > h[i].dist) {
			break
		}
		h[i], h[j] = h[j], h[i]
		j = i
	}
}

func resHeapDown(h []candidate, i, n int) {
	for {
		j1 := 2*i + 1
		if j1 >= n || j1 < 0 {
			break
		}
		j := j1
		if j2 := j1 + 1; j2 < n && h[j2].dist > h[j1].dist {
			j = j2
		}
		if !(h[j].dist > h[i].dist) {
			break
		}
		h[i], h[j] = h[j], h[i]
		i = j
	}
}

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
		// Median-of-three pivot selection to avoid O(n^2) on sorted/reverse-sorted data.
		mid := (lo + hi) / 2
		if cands[lo].dist > cands[mid].dist {
			cands[lo], cands[mid] = cands[mid], cands[lo]
		}
		if cands[lo].dist > cands[hi].dist {
			cands[lo], cands[hi] = cands[hi], cands[lo]
		}
		if cands[mid].dist > cands[hi].dist {
			cands[mid], cands[hi] = cands[hi], cands[mid]
		}
		pivot := cands[mid].dist
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
	results, release := h.searchLayer(vec, qvec, curr, h.config.EfSearch, 0)
	defer release()

	// 3. Sort the final results by distance and take the top K.
	if len(results) > k {
		results = selectTopKByDist(results, k)
	}
	sort.Slice(results[:min(len(results), k)], func(i, j int) bool {
		return results[i].dist < results[j].dist
	})
	if len(results) > k {
		results = results[:k]
	}

	// 4. Convert internal IDs back to external string IDs and collect scores.
	ids := make([]string, len(results))
	scores := make([]float32, len(results))
	for i, c := range results {
		if int(c.id) < len(h.extIDCache) {
			ids[i] = h.extIDCache[c.id]
		} else {
			ids[i] = h.uint32ToID[c.id]
		}
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
	results, release := h.searchLayer(vec, qvec, curr, ef, 0)
	defer release()

	// 3. Sort the final results by distance and take the top K.
	if len(results) > k {
		results = selectTopKByDist(results, k)
	}
	sort.Slice(results[:min(len(results), k)], func(i, j int) bool {
		return results[i].dist < results[j].dist
	})
	if len(results) > k {
		results = results[:k]
	}

	// 4. Convert internal IDs back to external string IDs and collect scores.
	ids := make([]string, len(results))
	scores := make([]float32, len(results))
	for i, c := range results {
		if int(c.id) < len(h.extIDCache) {
			ids[i] = h.extIDCache[c.id]
		} else {
			ids[i] = h.uint32ToID[c.id]
		}
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

	candidates, release := h.searchLayer(vec, qvec, curr, stage1Ef, 0)
	defer release()
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
		if int(c.id) < len(h.extIDCache) {
			ids[i] = h.extIDCache[c.id]
		} else {
			ids[i] = h.uint32ToID[c.id]
		}
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
			n := h.getNodeFast(nid)
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
func (h *HNSW) searchLayer(vec []float32, qvec []int8, start *Node, ef, layer int) ([]candidate, func()) {
	if ef <= smallEfThreshold {
		return h.searchLayerSmall(vec, qvec, start, ef, layer)
	}
	tracker := visitedPool.Get().(*visitTracker)
	maxID := int(h.nextID)
	if maxID < 0 {
		maxID = 0
	}
	if len(tracker.marks) <= maxID {
		// Grow in larger chunks to avoid frequent reallocations as the index grows.
		newSize := maxID + 1
		if newSize < 4096 {
			newSize = 4096
		}
		// Round up to next power of 2 for efficient memory alignment.
		newSize--
		newSize |= newSize >> 1
		newSize |= newSize >> 2
		newSize |= newSize >> 4
		newSize |= newSize >> 8
		newSize |= newSize >> 16
		newSize++
		tracker.marks = make([]uint32, newSize)
	}
	tracker.epoch++
	if tracker.epoch == 0 {
		for i := range tracker.marks {
			tracker.marks[i] = 0
		}
		tracker.epoch = 1
	}
	defer putVisitTracker(tracker)

	// Candidate queue (min-heap) to explore promising nodes.
	candidateSlice := candidateSlicePool.Get().([]candidate)
	candidates := candidateSlice[:0]
	startDist := h.distanceToNode(vec, qvec, start)
	candHeapPush(&candidates, candidate{id: start.ID, dist: startDist, node: start})

	// Results queue (max-heap) to keep track of the best `ef` candidates found so far.
	resultSlice := resultSlicePool.Get().([]candidate)
	results := resultSlice[:0]
	resHeapPush(&results, candidate{id: start.ID, dist: startDist, node: start})

	if int(start.ID) < len(tracker.marks) {
		tracker.marks[start.ID] = tracker.epoch
	}

	for len(candidates) > 0 {
		// Get the closest candidate from the min-heap to explore next.
		c := candHeapPop(&candidates)

		// If this candidate is further than the furthest in our results, we can stop exploring this path.
		if len(results) >= ef && c.dist > results[0].dist {
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
			n := h.getNodeFast(nid)
			if n == nil {
				continue
			}
			nodes = append(nodes, n)
			ids = append(ids, nid)
		}
		if len(nodes) == 0 {
			putNodeSlice(&neighborNodeSlicePool, nodes)
			putUint32Slice(&neighborIDSlicePool, ids)
			continue
		}

		distances := getDistanceSlice(len(nodes))
		h.computeDistancesInto(vec, qvec, nodes, distances)
		if len(results) >= ef && len(nodes) >= 256 {
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
					if len(local) > 0 && len(results) > 0 {
						local = selectTopKByDist(local, len(results))
					}
					for _, cand := range local {
						if len(results) < ef || cand.dist < results[0].dist {
							candHeapPush(&candidates, cand)
							resHeapPush(&results, cand)
							if len(results) > ef {
								resHeapPop(&results)
							}
						}
					}
					putTempCandidateSlice(local)
				}
				putNodeSlice(&neighborNodeSlicePool, nodes)
				putUint32Slice(&neighborIDSlicePool, ids)
				putDistanceSlice(distances)
				continue
			}
		}
		if len(nodes) > ef*2 {
			tmp := getTempCandidateSlice(len(nodes))
			for i, n := range nodes {
				d := distances[i]
				if len(results) < ef || d < results[0].dist {
					tmp = append(tmp, candidate{id: ids[i], dist: d, node: n})
				}
			}
			if len(tmp) > 0 {
				tmp = selectTopKByDist(tmp, ef)
				for _, cand := range tmp {
					if len(results) < ef || cand.dist < results[0].dist {
						candHeapPush(&candidates, cand)
						resHeapPush(&results, cand)
						if len(results) > ef {
							resHeapPop(&results)
						}
					}
				}
			}
			putTempCandidateSlice(tmp)
		} else {
			for i, n := range nodes {
				d := distances[i]
				if len(results) < ef || d < results[0].dist {
					candHeapPush(&candidates, candidate{id: ids[i], dist: d, node: n})
					resHeapPush(&results, candidate{id: ids[i], dist: d, node: n})
					if len(results) > ef {
						resHeapPop(&results)
					}
				}
			}
		}
		putDistanceSlice(distances)
		putNodeSlice(&neighborNodeSlicePool, nodes)
		putUint32Slice(&neighborIDSlicePool, ids)
	}

	// The result is a max-heap, so we need to convert it to a simple slice.
	putCandidateSlice(&candidateSlicePool, candidates)
	finalResults := results
	release := func() {
		putCandidateSlice(&resultSlicePool, finalResults)
	}
	return finalResults, release
}

func (h *HNSW) searchLayerSmall(vec []float32, qvec []int8, start *Node, ef, layer int) ([]candidate, func()) {
	tracker := visitedPool.Get().(*visitTracker)
	maxID := int(h.nextID)
	if maxID < 0 {
		maxID = 0
	}
	if len(tracker.marks) <= maxID {
		// Grow in larger chunks to avoid frequent reallocations as the index grows.
		newSize := maxID + 1
		if newSize < 4096 {
			newSize = 4096
		}
		// Round up to next power of 2 for efficient memory alignment.
		newSize--
		newSize |= newSize >> 1
		newSize |= newSize >> 2
		newSize |= newSize >> 4
		newSize |= newSize >> 8
		newSize |= newSize >> 16
		newSize++
		tracker.marks = make([]uint32, newSize)
	}
	tracker.epoch++
	if tracker.epoch == 0 {
		for i := range tracker.marks {
			tracker.marks[i] = 0
		}
		tracker.epoch = 1
	}
	defer putVisitTracker(tracker)

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
			n := h.getNodeFast(nid)
			if n == nil {
				continue
			}
			nodes = append(nodes, n)
			ids = append(ids, nid)
		}
		if len(nodes) == 0 {
			putNodeSlice(&neighborNodeSlicePool, nodes)
			putUint32Slice(&neighborIDSlicePool, ids)
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
		putNodeSlice(&neighborNodeSlicePool, nodes)
		putUint32Slice(&neighborIDSlicePool, ids)
	}

	putCandidateSlice(&candidateSlicePool, candidateSlice)
	finalResults := results
	release := func() {
		putCandidateSlice(&resultSlicePool, finalResults)
	}
	return finalResults, release
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
