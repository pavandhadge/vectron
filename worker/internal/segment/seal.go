package segment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
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
	if err := s.persistVectors(mutable, vectorsPath, offsetsPath); err != nil {
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

func (s *Sealer) persistVectors(segment *MutableSegment, vectorsPath, offsetsPath string) error {
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

	return nil
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
	key := SegmentTombstoneKey(s.shardID, docID, epoch)
	return s.db.Set(key, []byte("1"), nil)
}

func (s *Sealer) GetTombstones(sinceEpoch int64) (map[string]int64, error) {
	prefix := SegmentTombstonePrefix(s.shardID)
	iter := s.db.NewIter(&pebble.IterOptions{
		LowerBound: prefix,
		UpperBound: prefixPrefixSuccessor(prefix),
	})
	defer iter.Close()

	tombstones := make(map[string]int64)
	for iter.First(); iter.Valid(); iter.Next() {
		_, docID, epoch, ok := ParseSegmentTombstoneKey(iter.Key())
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
