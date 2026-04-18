package segment

import (
	"sort"
	"sync"
)

type Searcher struct {
	mutable    *MutableSegment
	immutables *ImmutableSegmentStore
	mu         sync.RWMutex
}

func NewSearcher(mutable *MutableSegment, immutables *ImmutableSegmentStore) *Searcher {
	return &Searcher{
		mutable:    mutable,
		immutables: immutables,
	}
}

func (s *Searcher) Search(query []float32, k int, opts SearchOptions) ([]string, []float32, error) {
	s.mu.RLock()
	mutable := s.mutable
	immutables := s.immutables
	s.mu.RUnlock()

	ef := opts.EfSearch
	if ef <= 0 {
		ef = 100
	}
	if ef < k {
		ef = k
	}
	perSegK := k
	if opts.CandidatesPerSeg > 0 && (perSegK <= 0 || opts.CandidatesPerSeg < perSegK) {
		perSegK = opts.CandidatesPerSeg
	}
	if perSegK <= 0 {
		perSegK = k
	}

	dedup := make(map[string]float32, k*4+16)
	if mutable != nil {
		mergeCandidates(dedup, s.searchMutable(mutable, query, perSegK, ef))
	}

	segs := immutables.List()
	sort.Slice(segs, func(i, j int) bool {
		im := segs[i].Meta()
		jm := segs[j].Meta()
		if !im.SealedAt.Equal(jm.SealedAt) {
			return im.SealedAt.After(jm.SealedAt)
		}
		return im.CreatedAt.After(jm.CreatedAt)
	})
	maxFanout := opts.MaxFanout
	if maxFanout <= 0 {
		maxFanout = envIntSegment("VECTRON_SEGMENT_SEARCH_MAX_FANOUT", 8)
	}
	minFanout := opts.MinFanout
	if minFanout <= 0 {
		minFanout = envIntSegment("VECTRON_SEGMENT_SEARCH_MIN_FANOUT", 2)
	}
	if minFanout > len(segs) {
		minFanout = len(segs)
	}
	earlyStopFactor := opts.EarlyStopFactor
	if earlyStopFactor <= 0 {
		earlyStopFactor = 2
	}
	for i, seg := range segs {
		mergeCandidates(dedup, s.searchImmutable(seg, query, perSegK, ef))
		if maxFanout > 0 && i+1 >= maxFanout && i+1 >= minFanout && len(dedup) >= k*earlyStopFactor {
			break
		}
	}

	if len(dedup) == 0 {
		return nil, nil, nil
	}
	if opts.FilterTombstones != nil {
		for id := range dedup {
			if opts.FilterTombstones(id) {
				delete(dedup, id)
			}
		}
	}
	if len(dedup) == 0 {
		return nil, nil, nil
	}

	allCandidates := make([]candidate, 0, len(dedup))
	for id, score := range dedup {
		allCandidates = append(allCandidates, candidate{id: id, score: score})
	}
	sort.Slice(allCandidates, func(i, j int) bool {
		return allCandidates[i].score < allCandidates[j].score
	})

	resultK := k
	if resultK > len(allCandidates) {
		resultK = len(allCandidates)
	}
	resultIDs := make([]string, resultK)
	resultScores := make([]float32, resultK)
	for i := 0; i < resultK; i++ {
		resultIDs[i] = allCandidates[i].id
		resultScores[i] = allCandidates[i].score
	}
	return resultIDs, resultScores, nil
}

func mergeCandidates(dst map[string]float32, cands []candidate) {
	for _, cand := range cands {
		if prev, ok := dst[cand.id]; !ok || cand.score < prev {
			dst[cand.id] = cand.score
		}
	}
}

func (s *Searcher) SetMutable(mutable *MutableSegment) {
	s.mu.Lock()
	s.mutable = mutable
	s.mu.Unlock()
}

func (s *Searcher) searchMutable(mutable *MutableSegment, query []float32, k, ef int) []candidate {
	ids, scores := mutable.Search(query, k, ef)
	cands := make([]candidate, len(ids))
	for i, id := range ids {
		cands[i] = candidate{id: id, score: scores[i], segmentType: "mutable"}
	}
	return cands
}

func (s *Searcher) searchImmutable(seg *ImmutableSegment, query []float32, k, ef int) []candidate {
	ids, scores := seg.Search(query, k, ef)
	cands := make([]candidate, len(ids))
	for i, id := range ids {
		cands[i] = candidate{id: id, score: scores[i], segmentType: "immutable"}
	}
	return cands
}

type candidate struct {
	id          string
	score       float32
	segmentType string
}

type SearchOptions struct {
	EfSearch         int
	CandidatesPerSeg int
	FilterTombstones func(string) bool
	Rerank           bool
	MaxFanout        int
	MinFanout        int
	EarlyStopFactor  int
}

func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		EfSearch:         100,
		CandidatesPerSeg: 100,
		Rerank:           false,
	}
}
