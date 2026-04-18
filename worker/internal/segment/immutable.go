package segment

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"

	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

const (
	segmentVectorsFile = "vectors.bin"
	segmentOffsetsFile = "offsets.bin"
	segmentIDsFile     = "ids.json"
)

type ImmutableSegment struct {
	meta      SegmentMeta
	hnsw      *idxhnsw.HNSW
	hnswHot   *idxhnsw.HNSW
	mu        sync.RWMutex
	config    Config
	mmap      bool
	vectorsFD *os.File
	vectorsMM []byte
	offsets   []uint64
	ids       []string
	idIndex   map[string]int
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

	offsets, ids, vectorsFD, vectorsMM, err := loadImmutablePayload(segPath)
	if err != nil {
		return nil, err
	}
	idIndex := make(map[string]int, len(ids))
	for i, id := range ids {
		idIndex[id] = i
	}

	return &ImmutableSegment{
		meta:      meta,
		hnsw:      hnsw,
		config:    cfg,
		mmap:      mmap,
		vectorsFD: vectorsFD,
		vectorsMM: vectorsMM,
		offsets:   offsets,
		ids:       ids,
		idIndex:   idIndex,
	}, nil
}

func (s *ImmutableSegment) Search(vec []float32, k, ef int) ([]string, []float32) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	candidateK := k
	if candidateK < ef {
		candidateK = ef
	}
	if candidateK < k*4 {
		candidateK = k * 4
	}
	if candidateK <= 0 {
		candidateK = k
	}

	var ids []string
	var scores []float32
	if s.hnswHot != nil {
		hotIDs, hotScores := s.hnswHot.SearchWithEf(vec, candidateK, ef)
		coldIDs, coldScores := s.hnsw.SearchWithEf(vec, candidateK, ef/2)
		ids, scores = mergeSearchResults(hotIDs, hotScores, coldIDs, coldScores, candidateK)
	} else {
		ids, scores = s.hnsw.SearchWithEf(vec, candidateK, ef)
	}
	return s.rerankLocked(vec, ids, scores, k)
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
	if len(s.vectorsMM) > 0 {
		_ = syscall.Munmap(s.vectorsMM)
		s.vectorsMM = nil
	}
	if s.vectorsFD != nil {
		_ = s.vectorsFD.Close()
		s.vectorsFD = nil
	}
	return nil
}

func (s *ImmutableSegment) BytesEstimate() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.meta.BytesEstimate
}

func (s *ImmutableSegment) PayloadBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return int64(len(s.vectorsMM)) + int64(len(s.offsets))*8
}

func (s *ImmutableSegment) AllIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, len(s.ids))
	copy(out, s.ids)
	return out
}

func (s *ImmutableSegment) VectorByID(id string) ([]float32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	idx, ok := s.idIndex[id]
	if !ok {
		return nil, false
	}
	vec, err := s.vectorAtLocked(idx)
	if err != nil {
		return nil, false
	}
	return vec, true
}

func (s *ImmutableSegment) vectorAtLocked(idx int) ([]float32, error) {
	if idx < 0 || idx >= len(s.offsets) || len(s.vectorsMM) == 0 {
		return nil, fmt.Errorf("vector index out of range")
	}
	off := s.offsets[idx]
	if off > uint64(len(s.vectorsMM)) || off+4 > uint64(len(s.vectorsMM)) {
		return nil, fmt.Errorf("invalid vector offset")
	}
	buf := s.vectorsMM[off:]
	n := int(binary.LittleEndian.Uint32(buf[:4]))
	if 4+n*4 > len(buf) {
		return nil, fmt.Errorf("invalid vector payload size")
	}
	vec := make([]float32, n)
	pos := 4
	for i := 0; i < n; i++ {
		vec[i] = mathFloat32frombits(binary.LittleEndian.Uint32(buf[pos : pos+4]))
		pos += 4
	}
	return vec, nil
}

func (s *ImmutableSegment) rerankLocked(query []float32, ids []string, scores []float32, k int) ([]string, []float32) {
	if len(ids) == 0 || len(s.offsets) == 0 {
		if k > 0 && len(ids) > k {
			return ids[:k], scores[:k]
		}
		return ids, scores
	}
	type cand struct {
		id    string
		score float32
	}
	ranked := make([]cand, 0, len(ids))
	for _, id := range ids {
		idx, ok := s.idIndex[id]
		if !ok {
			continue
		}
		vec, err := s.vectorAtLocked(idx)
		if err != nil {
			continue
		}
		score := idxhnsw.EuclideanDistance(query, vec)
		if s.config.HNSWConfig.Distance == "cosine" {
			score = idxhnsw.CosineDistance(query, vec)
		}
		ranked = append(ranked, cand{id: id, score: score})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score < ranked[j].score })
	if k > 0 && len(ranked) > k {
		ranked = ranked[:k]
	}
	outIDs := make([]string, len(ranked))
	outScores := make([]float32, len(ranked))
	for i := range ranked {
		outIDs[i] = ranked[i].id
		outScores[i] = ranked[i].score
	}
	return outIDs, outScores
}

func loadImmutablePayload(segPath string) ([]uint64, []string, *os.File, []byte, error) {
	offsetBytes, err := os.ReadFile(filepath.Join(segPath, segmentOffsetsFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, err
	}
	var offsets []uint64
	if len(offsetBytes) > 0 {
		offsets = make([]uint64, len(offsetBytes)/8)
		for i := range offsets {
			offsets[i] = binary.LittleEndian.Uint64(offsetBytes[i*8 : i*8+8])
		}
	}
	idBytes, err := os.ReadFile(filepath.Join(segPath, segmentIDsFile))
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, nil, nil, err
	}
	var ids []string
	if len(idBytes) > 0 {
		if err := json.Unmarshal(idBytes, &ids); err != nil {
			return nil, nil, nil, nil, err
		}
	}
	f, err := os.Open(filepath.Join(segPath, segmentVectorsFile))
	if err != nil {
		if os.IsNotExist(err) {
			return offsets, ids, nil, nil, nil
		}
		return nil, nil, nil, nil, err
	}
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, nil, nil, err
	}
	if stat.Size() == 0 {
		return offsets, ids, f, nil, nil
	}
	mm, err := syscall.Mmap(int(f.Fd()), 0, int(stat.Size()), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, nil, nil, nil, err
	}
	return offsets, ids, f, mm, nil
}

func mathFloat32frombits(v uint32) float32 { return math.Float32frombits(v) }

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
