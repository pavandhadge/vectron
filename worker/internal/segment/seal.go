package segment

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

type Sealer struct {
	db      *pebble.DB
	shardID uint64
	config  Config
	mu      sync.Mutex
	wg      sync.WaitGroup
	stopCh  chan struct{}
}

func NewSealer(db *pebble.DB, shardID uint64, cfg Config) *Sealer {
	return &Sealer{
		db:      db,
		shardID: shardID,
		config:  cfg,
		stopCh:  make(chan struct{}),
	}
}

func (s *Sealer) Seal(mutable *MutableSegment, manifest *ShardManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := mutable.Seal(); err != nil {
		return err
	}

	segID := mutable.ID()
	segPath := mutable.Meta().FilePath

	if err := os.MkdirAll(segPath, 0755); err != nil {
		return fmt.Errorf("failed to create segment directory: %w", err)
	}

	hnsw := mutable.HNSW()
	var buf bytes.Buffer
	if err := hnsw.Save(&buf); err != nil {
		return fmt.Errorf("failed to serialize HNSW: %w", err)
	}
	indexPath := filepath.Join(segPath, "index.hnsw")
	if err := os.WriteFile(indexPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}

	vectorsPath := filepath.Join(segPath, "vectors.bin")
	offsetsPath := filepath.Join(segPath, "offsets.bin")
	idsPath := filepath.Join(segPath, "ids.json")
	if err := s.persistVectors(mutable.ID(), vectorsPath, offsetsPath, idsPath); err != nil {
		return fmt.Errorf("failed to persist vectors: %w", err)
	}

	metaPath := filepath.Join(segPath, "meta.json")
	meta := mutable.Meta()
	meta.State = SegmentStateImmutable
	metaData, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal meta: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return fmt.Errorf("failed to write meta file: %w", err)
	}

	mutable.SetImmutable()

	s.updateManifestOnSeal(manifest, segID)

	return nil
}

func (s *Sealer) persistVectors(segID SegmentID, vectorsPath, offsetsPath, idsPath string) error {
	vectorsFile, err := os.Create(vectorsPath)
	if err != nil {
		return err
	}
	defer vectorsFile.Close()

	offsetsFile, err := os.Create(offsetsPath)
	if err != nil {
		return err
	}
	defer offsetsFile.Close()
	prefix := SegmentDocPrefix(s.config.Namespace, s.shardID, segID)
	iter := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixPrefixSuccessor(prefix),
	})
	defer iter.Close()
	var (
		offsets []uint64
		ids     []string
		offset  uint64
	)
	for iter.First(); iter.Valid(); iter.Next() {
		_, docID, ok := ParseSegmentDocKey(s.config.Namespace, iter.Key())
		if !ok {
			continue
		}
		val, closer, err := s.db.Get(primaryVectorKey(s.config.Namespace, docID))
		if err != nil {
			continue
		}
		vec, err := decodePrimaryVector(val)
		closer.Close()
		if err != nil {
			continue
		}
		offsets = append(offsets, offset)
		ids = append(ids, docID)
		n, err := writeVectorPayload(vectorsFile, vec)
		if err != nil {
			return err
		}
		offset += uint64(n)
	}
	if err := iter.Error(); err != nil {
		return err
	}
	buf := make([]byte, 8)
	for _, off := range offsets {
		binary.LittleEndian.PutUint64(buf, off)
		if _, err := offsetsFile.Write(buf); err != nil {
			return err
		}
	}
	idData, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return os.WriteFile(idsPath, idData, 0644)
}

func (s *Sealer) updateManifestOnSeal(manifest *ShardManifest, sealedID SegmentID) {
	manifest.ImmutableSegments = append(manifest.ImmutableSegments, sealedID)
	manifest.Version++
	manifest.UpdatedAt = time.Now()
}

func (s *Sealer) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *Sealer) RunCompaction(manifest *ShardManifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(manifest.ImmutableSegments) < 2 {
		return nil
	}

	threshold := s.config.Thresholds.CompactionFanIn
	if len(manifest.ImmutableSegments) < threshold {
		return nil
	}

	segmentsToCompact := manifest.ImmutableSegments[:threshold]
	newSegID := GenerateSegmentID()

	if err := s.compactSegments(segmentsToCompact, newSegID, manifest); err != nil {
		return err
	}

	for _, id := range segmentsToCompact {
		manifest.ImmutableSegments = removeSegment(manifest.ImmutableSegments, id)
	}
	manifest.ImmutableSegments = append(manifest.ImmutableSegments, newSegID)

	manifest.Version++
	manifest.UpdatedAt = time.Now()

	return nil
}

func (s *Sealer) compactSegments(segIDs []SegmentID, newSegID SegmentID, manifest *ShardManifest) error {
	tombstones, err := s.GetTombstones(0)
	if err != nil {
		return err
	}
	shardPath := filepath.Join(s.config.ShardPath, fmt.Sprintf("shard-%d", s.shardID))
	segPath := filepath.Join(shardPath, "segments", string(newSegID))
	if err := os.MkdirAll(segPath, 0755); err != nil {
		return err
	}
	hnswCfg := idxhnsw.HNSWConfig{
		M:                 s.config.HNSWConfig.M,
		EfConstruction:    s.config.HNSWConfig.EfConstruction,
		EfSearch:          s.config.HNSWConfig.EfSearch,
		Distance:          s.config.HNSWConfig.Distance,
		NormalizeVectors:  s.config.HNSWConfig.NormalizeVectors,
		QuantizeVectors:   s.config.HNSWConfig.QuantizeVectors,
		SearchParallelism: s.config.HNSWConfig.SearchParallelism,
		SkipPersistNode:   true,
	}
	hnsw := idxhnsw.NewHNSW(newMemStore(), s.config.Dimension, hnswCfg)
	entries := make(map[string][]float32)
	for i := len(segIDs) - 1; i >= 0; i-- {
		seg, err := LoadImmutableSegment(s.shardID, segIDs[i], s.config, false)
		if err != nil {
			return err
		}
		defer seg.Close()
		for _, id := range seg.AllIDs() {
			if _, dead := tombstones[id]; dead {
				continue
			}
			if _, seen := entries[id]; seen {
				continue
			}
			vec, ok := seg.VectorByID(id)
			if !ok {
				continue
			}
			entries[id] = vec
		}
	}
	ids := make([]string, 0, len(entries))
	offsets := make([]uint64, 0, len(entries))
	vectorsPath := filepath.Join(segPath, segmentVectorsFile)
	offsetsPath := filepath.Join(segPath, segmentOffsetsFile)
	idsPath := filepath.Join(segPath, segmentIDsFile)
	vf, err := os.Create(vectorsPath)
	if err != nil {
		return err
	}
	defer vf.Close()
	of, err := os.Create(offsetsPath)
	if err != nil {
		return err
	}
	defer of.Close()
	var totalBytes int64
	var off uint64
	for id, vec := range entries {
		if err := hnsw.Add(id, vec); err != nil {
			return err
		}
		ids = append(ids, id)
		offsets = append(offsets, off)
		n, err := writeVectorPayload(vf, vec)
		if err != nil {
			return err
		}
		off += uint64(n)
		totalBytes += estimateVectorBytes(vec)
	}
	buf := make([]byte, 8)
	for _, v := range offsets {
		binary.LittleEndian.PutUint64(buf, v)
		if _, err := of.Write(buf); err != nil {
			return err
		}
	}
	idData, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	if err := os.WriteFile(idsPath, idData, 0644); err != nil {
		return err
	}
	var indexBuf bytes.Buffer
	if err := hnsw.Save(&indexBuf); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(segPath, "index.hnsw"), indexBuf.Bytes(), 0644); err != nil {
		return err
	}
	meta := SegmentMeta{
		ID:            newSegID,
		State:         SegmentStateImmutable,
		CreatedAt:     time.Now(),
		SealedAt:      time.Now(),
		VectorCount:   int64(len(ids)),
		BytesEstimate: totalBytes,
		FilePath:      segPath,
	}
	metaData, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(segPath, "meta.json"), metaData, 0644); err != nil {
		return err
	}
	for _, oldID := range segIDs {
		_ = os.RemoveAll(filepath.Join(shardPath, "segments", string(oldID)))
	}
	return nil
}

func removeSegment(slice []SegmentID, elem SegmentID) []SegmentID {
	for i, s := range slice {
		if s == elem {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func (s *Sealer) DeleteObsoleteSegments(manifest *ShardManifest) error {
	return nil
}

func (s *Sealer) WriteTombstone(docID string, epoch int64) error {
	key := SegmentTombstoneKey(s.config.Namespace, s.shardID, docID, epoch)
	return s.db.Set(key, []byte("1"), nil)
}

func (s *Sealer) GetTombstones(sinceEpoch int64) (map[string]int64, error) {
	prefix := SegmentTombstonePrefix(s.config.Namespace, s.shardID)
	iter := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixPrefixSuccessor(prefix),
	})
	defer iter.Close()

	tombstones := make(map[string]int64)
	for iter.First(); iter.Valid(); iter.Next() {
		_, docID, epoch, ok := ParseSegmentTombstoneKey(s.config.Namespace, iter.Key())
		if !ok {
			continue
		}
		if epoch > sinceEpoch {
			if epoch > tombstones[docID] {
				tombstones[docID] = epoch
			}
		}
	}
	return tombstones, iter.Error()
}

func writeVectorPayload(w io.Writer, vec []float32) (int, error) {
	buf := make([]byte, 4+len(vec)*4)
	binary.LittleEndian.PutUint32(buf[:4], uint32(len(vec)))
	off := 4
	for _, v := range vec {
		binary.LittleEndian.PutUint32(buf[off:off+4], math.Float32bits(v))
		off += 4
	}
	n, err := w.Write(buf)
	return n, err
}
