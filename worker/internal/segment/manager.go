package segment

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
)

type ShardIndexManager struct {
	shardID    uint64
	db         *pebble.DB
	config     Config
	manifest   *ShardManifest
	mutable    *MutableSegment
	immutables *ImmutableSegmentStore
	searcher   *Searcher
	sealer     *Sealer
	mu         sync.RWMutex
	wg         sync.WaitGroup
	stopCh     chan struct{}
	tombstones map[string]int64
}

func NewShardIndexManager(shardID uint64, db *pebble.DB, opts Config) (*ShardIndexManager, error) {
	shardPath := filepath.Join(opts.ShardPath, fmt.Sprintf("shard-%d", shardID))
	if err := os.MkdirAll(shardPath, 0755); err != nil {
		return nil, err
	}

	opts.ShardPath = opts.ShardPath

	mgr := &ShardIndexManager{
		shardID:    shardID,
		db:         db,
		config:     opts,
		stopCh:     make(chan struct{}),
		tombstones: make(map[string]int64),
	}

	manifestStore := NewManifestStore(db, shardID)
	if manifest, err := manifestStore.Load(); err == nil && manifest != nil {
		mgr.manifest = manifest
	} else {
		mgr.manifest = mgr.createDefaultManifest()
	}

	if mgr.manifest.MutableSegmentID == "" {
		mutable, err := NewMutableSegment(shardID, opts)
		if err != nil {
			return nil, err
		}
		mgr.mutable = mutable
		mgr.manifest.MutableSegmentID = mutable.ID()
		mgr.manifest.UpdatedAt = time.Now()
		if err := manifestStore.Save(mgr.manifest); err != nil {
			return nil, err
		}
	} else {
		mutable, err := LoadMutableSegment(shardID, mgr.manifest.MutableSegmentID, opts, manifestStore)
		if err != nil {
			return nil, err
		}
		mgr.mutable = mutable
	}

	mgr.immutables = NewImmutableSegmentStore(opts)

	for _, segID := range mgr.manifest.ImmutableSegments {
		seg, err := LoadImmutableSegment(shardID, segID, opts, false)
		if err != nil {
			log.Printf("Failed to load immutable segment %s: %v", segID, err)
			continue
		}
		mgr.immutables.Add(seg)
	}

	mgr.searcher = NewSearcher(mgr.mutable, mgr.immutables)
	mgr.sealer = NewSealer(db, shardID, opts)

	mgr.wg.Add(1)
	go mgr.compactionLoop()

	return mgr, nil
}

func (m *ShardIndexManager) createDefaultManifest() *ShardManifest {
	return &ShardManifest{
		ShardID:           m.shardID,
		Version:           1,
		ImmutableSegments: []SegmentID{},
		TombstoneEpoch:    0,
		CompactionMeta:    &CompactionMeta{},
		UpdatedAt:         time.Now(),
	}
}

func (m *ShardIndexManager) Add(id string, vec []float32) error {
	if m.mutable == nil {
		return fmt.Errorf("no mutable segment available")
	}

	if err := m.mutable.Add(id, vec); err != nil {
		return err
	}
	if err := m.db.Set(SegmentDocKey(m.shardID, m.mutable.ID(), id), encodeSegmentVector(vec), nil); err != nil {
		return err
	}

	if m.mutable.ShouldSeal() {
		m.triggerSeal()
	}

	return nil
}

func (m *ShardIndexManager) Delete(id string) error {
	if m.mutable != nil {
		if err := m.mutable.Delete(id); err != nil {
			return err
		}
		_ = m.db.Delete(SegmentDocKey(m.shardID, m.mutable.ID(), id), nil)
	}

	m.mu.Lock()
	m.tombstones[id] = time.Now().UnixNano()
	m.mu.Unlock()

	m.manifest.TombstoneEpoch = time.Now().UnixNano()
	epoch := m.manifest.TombstoneEpoch
	_ = m.sealer.WriteTombstone(id, epoch)

	return nil
}

func (m *ShardIndexManager) Search(query []float32, k int, opts ...SearchOptions) ([]string, []float32, error) {
	m.mu.RLock()
	currentTombstones := make(map[string]int64, len(m.tombstones))
	for k, v := range m.tombstones {
		currentTombstones[k] = v
	}
	m.mu.RUnlock()

	searchOpts := DefaultSearchOptions()
	if len(opts) > 0 {
		searchOpts = opts[0]
	}

	searchOpts.FilterTombstones = func(id string) bool {
		ts, ok := currentTombstones[id]
		if !ok {
			return false
		}
		return ts > m.manifest.TombstoneEpoch
	}

	return m.searcher.Search(query, k, searchOpts)
}

func (m *ShardIndexManager) triggerSeal() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mutable == nil || !m.mutable.ShouldSeal() {
		return
	}

	currentMutable := m.mutable
	mutableID := currentMutable.ID()

	if err := m.sealer.Seal(currentMutable, m.manifest); err != nil {
		log.Printf("Failed to seal segment: %v", err)
		return
	}

	seg, err := LoadImmutableSegment(m.shardID, mutableID, m.config, false)
	if err != nil {
		log.Printf("Failed to load sealed segment: %v", err)
	} else {
		m.immutables.Add(seg)
	}

	newMutable, err := NewMutableSegment(m.shardID, m.config)
	if err != nil {
		log.Printf("Failed to create new mutable segment: %v", err)
		return
	}

	m.mutable = newMutable
	m.manifest.MutableSegmentID = newMutable.ID()
	m.manifest.UpdatedAt = time.Now()

	manifestStore := NewManifestStore(m.db, m.shardID)
	if err := manifestStore.Save(m.manifest); err != nil {
		log.Printf("Failed to save manifest: %v", err)
	}
}

func (m *ShardIndexManager) compactionLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.runCompaction()
		}
	}
}

func (m *ShardIndexManager) runCompaction() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.sealer.RunCompaction(m.manifest); err != nil {
		log.Printf("Compaction failed: %v", err)
		return
	}

	manifestStore := NewManifestStore(m.db, m.shardID)
	if err := manifestStore.Save(m.manifest); err != nil {
		log.Printf("Failed to save manifest after compaction: %v", err)
	}
}

func (m *ShardIndexManager) Close() error {
	close(m.stopCh)
	m.wg.Wait()

	if m.mutable != nil {
		m.mutable.Close()
	}
	if m.immutables != nil {
		m.immutables.Close()
	}
	if m.sealer != nil {
		m.sealer.Stop()
	}

	return nil
}

func (m *ShardIndexManager) GetStats() IndexStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := IndexStats{
		ShardID: m.shardID,
	}

	if m.mutable != nil {
		stats.MutableCount = m.mutable.Size()
	}

	stats.ImmutableCount = 0
	for _, seg := range m.immutables.List() {
		stats.ImmutableCount += seg.Size()
	}

	stats.TotalSegments = int64(len(m.immutables.List())) + 1
	stats.TombstoneCount = int64(len(m.tombstones))

	return stats
}

type IndexStats struct {
	ShardID        uint64
	MutableCount   int64
	ImmutableCount int64
	TotalSegments  int64
	TombstoneCount int64
}
