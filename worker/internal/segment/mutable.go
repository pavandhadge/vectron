package segment

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

type MutableSegment struct {
	meta    SegmentMeta
	hnsw    *idxhnsw.HNSW
	hnswHot *idxhnsw.HNSW
	mu      sync.RWMutex
	config  Config
}

func NewMutableSegment(shardID uint64, cfg Config) (*MutableSegment, error) {
	return newMutableSegmentWithID(shardID, GenerateSegmentID(), cfg)
}

func LoadMutableSegment(shardID uint64, segmentID SegmentID, cfg Config, db *ManifestStore) (*MutableSegment, error) {
	ms, err := newMutableSegmentWithID(shardID, segmentID, cfg)
	if err != nil {
		return nil, err
	}
	iter := db.db.NewIter(&pebble.IterOptions{
		LowerBound: SegmentDocPrefix(shardID, segmentID),
		UpperBound: prefixPrefixSuccessor(SegmentDocPrefix(shardID, segmentID)),
	})
	defer iter.Close()
	for iter.First(); iter.Valid(); iter.Next() {
		_, docID, ok := ParseSegmentDocKey(iter.Key())
		if !ok {
			continue
		}
		vec, err := decodeSegmentVector(iter.Value())
		if err != nil {
			continue
		}
		if err := ms.Add(docID, vec); err != nil {
			return nil, err
		}
	}
	return ms, iter.Error()
}

func newMutableSegmentWithID(shardID uint64, segmentID SegmentID, cfg Config) (*MutableSegment, error) {
	shardPath := filepath.Join(cfg.ShardPath, fmt.Sprintf("shard-%d", shardID))
	segPath := filepath.Join(shardPath, "segments", string(segmentID))
	if err := os.MkdirAll(segPath, 0755); err != nil {
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

	hnswStore := newMemStore()
	hnsw := idxhnsw.NewHNSW(hnswStore, cfg.Dimension, hnswCfg)

	ms := &MutableSegment{
		meta: SegmentMeta{
			ID:        segmentID,
			State:     SegmentStateMutable,
			CreatedAt: time.Now(),
			FilePath:  segPath,
		},
		hnsw:   hnsw,
		config: cfg,
	}

	return ms, nil
}

func (m *MutableSegment) ID() SegmentID {
	return m.meta.ID
}

func (m *MutableSegment) Add(id string, vec []float32) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.meta.State != SegmentStateMutable {
		return fmt.Errorf("segment %s is not mutable", m.meta.ID)
	}

	if err := m.hnsw.Add(id, vec); err != nil {
		return err
	}

	if m.hnswHot != nil {
		_ = m.hnswHot.Add(id, vec)
	}

	m.meta.VectorCount++
	m.meta.BytesEstimate += estimateVectorBytes(vec)

	m.checkSealThreshold()

	return nil
}

func (m *MutableSegment) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.meta.State != SegmentStateMutable {
		return fmt.Errorf("segment %s is not mutable", m.meta.ID)
	}

	_ = m.hnsw.Delete(id)
	if m.hnswHot != nil {
		_ = m.hnswHot.Delete(id)
	}

	return nil
}

func (m *MutableSegment) Search(vec []float32, k, ef int) ([]string, []float32) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.hnswHot != nil {
		hotIDs, hotScores := m.hnswHot.SearchWithEf(vec, k, ef)
		coldIDs, coldScores := m.hnsw.SearchWithEf(vec, k, ef/2)
		return mergeSearchResults(hotIDs, hotScores, coldIDs, coldScores, k)
	}

	return m.hnsw.SearchWithEf(vec, k, ef)
}

func (m *MutableSegment) Size() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.meta.VectorCount
}

func (m *MutableSegment) ShouldSeal() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	thresholds := m.config.Thresholds

	if m.meta.VectorCount >= int64(thresholds.MaxVectors) {
		return true
	}
	if m.meta.BytesEstimate >= thresholds.MaxBytes {
		return true
	}
	if time.Since(m.meta.CreatedAt) >= thresholds.MaxAge {
		return true
	}
	return false
}

func (m *MutableSegment) Seal() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.meta.State != SegmentStateMutable {
		return fmt.Errorf("segment %s is not mutable", m.meta.ID)
	}

	m.meta.State = SegmentStateSealing
	m.meta.SealedAt = time.Now()

	return nil
}

func (m *MutableSegment) SetImmutable() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.meta.State = SegmentStateImmutable
}

func (m *MutableSegment) Meta() SegmentMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.meta
}

func (m *MutableSegment) HNSW() *idxhnsw.HNSW {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hnsw
}

func (m *MutableSegment) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.hnsw != nil {
		m.hnsw.Close()
	}
	if m.hnswHot != nil {
		m.hnswHot.Close()
	}
	return nil
}

func (m *MutableSegment) checkSealThreshold() {
	if !m.ShouldSeal() {
		return
	}
}

func estimateVectorBytes(vec []float32) int64 {
	return int64(4 * len(vec))
}

func encodeSegmentVector(vec []float32) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 4+len(vec)*4))
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(vec)))
	for _, v := range vec {
		_ = binary.Write(buf, binary.LittleEndian, v)
	}
	return buf.Bytes()
}

func decodeSegmentVector(data []byte) ([]float32, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("invalid segment vector payload")
	}
	n := int(binary.LittleEndian.Uint32(data[:4]))
	if len(data) < 4+n*4 {
		return nil, fmt.Errorf("short segment vector payload")
	}
	out := make([]float32, n)
	off := 4
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[off : off+4]))
		off += 4
	}
	return out, nil
}

func mergeSearchResults(idsA []string, scoresA []float32, idsB []string, scoresB []float32, k int) ([]string, []float32) {
	type pair struct {
		id    string
		score float32
	}
	merged := make(map[string]float32)

	for i, id := range idsA {
		if i >= len(scoresA) {
			break
		}
		merged[id] = scoresA[i]
	}
	for i, id := range idsB {
		if i >= len(scoresB) {
			break
		}
		if prev, ok := merged[id]; !ok || scoresB[i] < prev {
			merged[id] = scoresB[i]
		}
	}

	out := make([]pair, 0, len(merged))
	for id, score := range merged {
		out = append(out, pair{id: id, score: score})
	}

	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].score < out[i].score {
				out[i], out[j] = out[j], out[i]
			}
		}
	}

	if k > 0 && len(out) > k {
		out = out[:k]
	}

	resultIDs := make([]string, len(out))
	resultScores := make([]float32, len(out))
	for i, p := range out {
		resultIDs[i] = p.id
		resultScores[i] = p.score
	}

	return resultIDs, resultScores
}

func GenerateSegmentID() SegmentID {
	return SegmentID(fmt.Sprintf("seg-%d-%d", time.Now().UnixNano(), time.Now().UnixNano()))
}

type memStore struct {
	data map[string][]byte
	mu   sync.RWMutex
}

func newMemStore() *memStore {
	return &memStore{
		data: make(map[string][]byte),
	}
}

func (s *memStore) Put(key, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[string(key)] = value
	return nil
}

func (s *memStore) Get(key []byte) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[string(key)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return v, nil
}

func (s *memStore) Delete(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, string(key))
	return nil
}

func (s *memStore) Close() error {
	return nil
}

var _ idxhnsw.NodeStore = (*memStore)(nil)
