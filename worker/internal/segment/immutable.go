package segment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

type ImmutableSegment struct {
	meta    SegmentMeta
	hnsw    *idxhnsw.HNSW
	hnswHot *idxhnsw.HNSW
	mu      sync.RWMutex
	config  Config
	mmap    bool
}

func LoadImmutableSegment(shardID uint64, id SegmentID, cfg Config, mmap bool) (*ImmutableSegment, error) {
	shardPath := filepath.Join(cfg.ShardPath, fmt.Sprintf("shard-%d", shardID))
	segPath := filepath.Join(shardPath, "segments", string(id))

	if _, err := os.Stat(segPath); err != nil {
		return nil, err
	}

	hnswCfg := idxhnsw.HNSWConfig{
		M:                 cfg.HNSWConfig.M,
		EfConstruction:    cfg.HNSWConfig.EfConstruction,
		EfSearch:          cfg.HNSWConfig.EfSearch,
		Distance:          cfg.HNSWConfig.Distance,
		NormalizeVectors:  cfg.HNSWConfig.NormalizeVectors,
		QuantizeVectors:   cfg.HNSWConfig.QuantizeVectors,
		SearchParallelism: cfg.HNSWConfig.SearchParallelism,
		SkipPersistNode:   true,
	}

	store := newMmapStore(segPath)
	hnsw := idxhnsw.NewHNSW(store, cfg.Dimension, hnswCfg)

	indexPath := filepath.Join(segPath, "index.hnsw")
	if data, err := os.ReadFile(indexPath); err == nil {
		if err := hnsw.Load(bytes.NewReader(data)); err != nil {
			return nil, fmt.Errorf("failed to load HNSW index: %w", err)
		}
	}

	meta := SegmentMeta{ID: id, State: SegmentStateImmutable, FilePath: segPath}
	if data, err := os.ReadFile(filepath.Join(segPath, "meta.json")); err == nil {
		_ = json.Unmarshal(data, &meta)
	}

	return &ImmutableSegment{
		meta:   meta,
		hnsw:   hnsw,
		config: cfg,
		mmap:   mmap,
	}, nil
}

func (s *ImmutableSegment) Search(vec []float32, k, ef int) ([]string, []float32) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.hnswHot != nil {
		hotIDs, hotScores := s.hnswHot.SearchWithEf(vec, k, ef)
		coldIDs, coldScores := s.hnsw.SearchWithEf(vec, k, ef/2)
		return mergeSearchResults(hotIDs, hotScores, coldIDs, coldScores, k)
	}

	return s.hnsw.SearchWithEf(vec, k, ef)
}

func (s *ImmutableSegment) Size() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta.VectorCount
}

func (s *ImmutableSegment) Meta() SegmentMeta {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta
}

func (s *ImmutableSegment) HNSW() *idxhnsw.HNSW {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hnsw
}

func (s *ImmutableSegment) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.hnsw != nil {
		s.hnsw.Close()
	}
	if s.hnswHot != nil {
		s.hnswHot.Close()
	}
	return nil
}

type mmapStore struct {
	path string
	mu   sync.RWMutex
}

func newMmapStore(path string) *mmapStore {
	return &mmapStore{path: path}
}

func (s *mmapStore) Put(key, value []byte) error {
	return nil
}

func (s *mmapStore) Get(key []byte) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *mmapStore) Delete(key []byte) error {
	return nil
}

func (s *mmapStore) Close() error {
	return nil
}

var _ idxhnsw.NodeStore = (*mmapStore)(nil)

type ImmutableSegmentStore struct {
	segments map[SegmentID]*ImmutableSegment
	mu       sync.RWMutex
	config   Config
}

func NewImmutableSegmentStore(cfg Config) *ImmutableSegmentStore {
	return &ImmutableSegmentStore{
		segments: make(map[SegmentID]*ImmutableSegment),
		config:   cfg,
	}
}

func (s *ImmutableSegmentStore) Add(seg *ImmutableSegment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.segments[seg.meta.ID] = seg
}

func (s *ImmutableSegmentStore) Get(id SegmentID) (*ImmutableSegment, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seg, ok := s.segments[id]
	return seg, ok
}

func (s *ImmutableSegmentStore) Remove(id SegmentID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if seg, ok := s.segments[id]; ok {
		_ = seg.Close()
		delete(s.segments, id)
	}
}

func (s *ImmutableSegmentStore) List() []*ImmutableSegment {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*ImmutableSegment, 0, len(s.segments))
	for _, seg := range s.segments {
		result = append(result, seg)
	}
	return result
}

func (s *ImmutableSegmentStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, seg := range s.segments {
		_ = seg.Close()
	}
	s.segments = make(map[SegmentID]*ImmutableSegment)
}
