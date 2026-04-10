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
	ef := opts.EfSearch
	if ef <= 0 {
		ef = 100
	}
	if ef < k {
		ef = k
	}

	var allCandidates []candidate

	if s.mutable != nil {
		cands := s.searchMutable(query, k, ef)
		allCandidates = append(allCandidates, cands...)
	}

	for _, seg := range s.immutables.List() {
		cands := s.searchImmutable(seg, query, k, ef)
		allCandidates = append(allCandidates, cands...)
	}

	if len(allCandidates) == 0 {
		return nil, nil, nil
	}

	if opts.FilterTombstones != nil {
		filtered := allCandidates[:0]
		for _, cand := range allCandidates {
			if !opts.FilterTombstones(cand.id) {
				filtered = append(filtered, cand)
			}
		}
		allCandidates = filtered
	}

	if len(allCandidates) == 0 {
		return nil, nil, nil
	}

	dedup := make(map[string]float32, len(allCandidates))
	for _, cand := range allCandidates {
		if prev, ok := dedup[cand.id]; !ok || cand.score < prev {
			dedup[cand.id] = cand.score
		}
	}
	allCandidates = allCandidates[:0]
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

func (s *Searcher) searchMutable(query []float32, k, ef int) []candidate {
	ids, scores := s.mutable.Search(query, k, ef)
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
}

func DefaultSearchOptions() SearchOptions {
	return SearchOptions{
		EfSearch:         100,
		CandidatesPerSeg: 100,
		Rerank:           false,
	}
}
