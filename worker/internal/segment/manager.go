package segment

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
)

type segmentIndexOpType int

const (
	segmentIndexOpAdd segmentIndexOpType = iota
	segmentIndexOpDelete
	segmentIndexOpBarrier
)

type segmentIndexOp struct {
	opType  segmentIndexOpType
	id      string
	vector  []float32
	ids     []string
	vectors [][]float32
	segID   SegmentID
	done    chan struct{}
}

type ShardIndexManager struct {
	shardID      uint64
	db           *pebble.DB
	config       Config
	manifest     *ShardManifest
	mutable      *MutableSegment
	immutables   *ImmutableSegmentStore
	searcher     *Searcher
	sealer       *Sealer
	mu           sync.RWMutex
	wg           sync.WaitGroup
	stopCh       chan struct{}
	tombstones   map[string]int64
	indexerCh    chan segmentIndexOp
	indexerStop  chan struct{}
	indexerWg    sync.WaitGroup
	indexPending uint64
	asyncIndex   bool
}

func NewShardIndexManager(shardID uint64, db *pebble.DB, opts Config) (*ShardIndexManager, error) {
	shardPath := filepath.Join(opts.ShardPath, fmt.Sprintf("shard-%d", shardID))
	if err := os.MkdirAll(shardPath, 0755); err != nil {
		return nil, err
	}

	mgr := &ShardIndexManager{
		shardID:    shardID,
		db:         db,
		config:     opts,
		stopCh:     make(chan struct{}),
		tombstones: make(map[string]int64),
	}
	mgr.initIndexer()

	manifestStore := NewManifestStore(db, shardID, opts.Namespace)
	if manifest, err := manifestStore.Load(); err == nil && manifest != nil {
		mgr.manifest = manifest
	} else {
		mgr.manifest = mgr.createDefaultManifest()
	}

	mgr.sealer = NewSealer(db, shardID, opts)
	if tombstones, err := mgr.sealTombstones(); err == nil {
		mgr.tombstones = tombstones
	}
	mgr.immutables = NewImmutableSegmentStore(opts)
	immutableIDs := make(map[string]struct{})
	for _, segID := range mgr.manifest.ImmutableSegments {
		seg, err := LoadImmutableSegment(shardID, segID, opts, false)
		if err != nil {
			log.Printf("Failed to load immutable segment %s: %v", segID, err)
			continue
		}
		mgr.immutables.Add(seg)
		for _, id := range seg.AllIDs() {
			immutableIDs[id] = struct{}{}
		}
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
		mutable, err := LoadMutableSegment(shardID, mgr.manifest.MutableSegmentID, opts, db, immutableIDs, mgr.tombstones)
		if err != nil {
			return nil, err
		}
		mgr.mutable = mutable
	}

	mgr.searcher = NewSearcher(mgr.mutable, mgr.immutables)

	mgr.wg.Add(1)
	go mgr.compactionLoop()

	return mgr, nil
}

func (m *ShardIndexManager) initIndexer() {
	m.asyncIndex = envBoolSegment("VECTRON_SEGMENT_ASYNC_INDEX", true)
	if !m.asyncIndex {
		return
	}
	queueSize := envIntSegment("VECTRON_SEGMENT_INDEX_QUEUE_SIZE", 20000)
	if queueSize <= 0 {
		queueSize = 20000
	}
	m.indexerCh = make(chan segmentIndexOp, queueSize)
	m.indexerStop = make(chan struct{})
	m.indexerWg.Add(1)
	go m.indexerLoop()
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
	m.mu.RLock()
	mutable := m.mutable
	m.mu.RUnlock()
	if mutable == nil {
		return fmt.Errorf("no mutable segment available")
	}
	segID := mutable.ID()
	if m.asyncIndex {
		if err := mutable.ReserveAdd(vec); err != nil {
			return err
		}
		m.enqueueIndexOp(segmentIndexOp{opType: segmentIndexOpAdd, id: id, vector: vec, segID: segID})
	} else {
		if err := mutable.Add(id, vec); err != nil {
			return err
		}
	}
	if mutable.ShouldSeal() {
		m.triggerSeal()
	}

	return nil
}

func (m *ShardIndexManager) AddBatch(ids []string, vectors [][]float32) error {
	if len(ids) != len(vectors) {
		return fmt.Errorf("ids/vectors length mismatch")
	}
	m.mu.RLock()
	mutable := m.mutable
	m.mu.RUnlock()
	if mutable == nil {
		return fmt.Errorf("no mutable segment available")
	}
	segID := mutable.ID()
	if m.asyncIndex {
		if err := mutable.ReserveAddBatch(vectors); err != nil {
			return err
		}
		m.enqueueIndexBatch(segID, ids, vectors)
	} else {
		if err := mutable.ApplyAddBatch(ids, vectors); err != nil {
			return err
		}
	}
	if mutable.ShouldSeal() {
		m.triggerSeal()
	}
	return nil
}

func (m *ShardIndexManager) enqueueIndexBatch(segID SegmentID, ids []string, vectors [][]float32) {
	if m.indexerCh == nil {
		return
	}
	op := segmentIndexOp{
		opType:  segmentIndexOpAdd,
		segID:   segID,
		ids:     ids,
		vectors: vectors,
	}
	atomic.AddUint64(&m.indexPending, uint64(len(ids)))
	select {
	case m.indexerCh <- op:
	default:
		atomic.AddUint64(&m.indexPending, ^uint64(len(ids)-1))
		m.applyIndexOp(op)
	}
}

func (m *ShardIndexManager) Delete(id string) error {
	ts := time.Now().UnixNano()
	m.mu.RLock()
	mutable := m.mutable
	m.mu.RUnlock()
	if mutable != nil {
		if !m.asyncIndex {
			if err := mutable.Delete(id); err != nil {
				return err
			}
		} else {
			m.enqueueIndexOp(segmentIndexOp{opType: segmentIndexOpDelete, id: id, segID: mutable.ID()})
		}
	}

	m.mu.Lock()
	m.tombstones[id] = ts
	m.mu.Unlock()

	m.manifest.TombstoneEpoch = ts
	_ = m.sealer.WriteTombstone(id, ts)

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
		_, ok := currentTombstones[id]
		return ok
	}

	ids, scores, err := m.searcher.Search(query, k, searchOpts)
	if err == nil && len(ids) == 0 && atomic.LoadUint64(&m.indexPending) > 0 {
		waitMs := envIntSegment("VECTRON_SEGMENT_SEARCH_WAIT_MS", 50)
		if waitMs > 0 {
			deadline := time.Now().Add(time.Duration(waitMs) * time.Millisecond)
			for time.Now().Before(deadline) {
				if atomic.LoadUint64(&m.indexPending) == 0 {
					break
				}
				time.Sleep(time.Millisecond)
			}
			return m.searcher.Search(query, k, searchOpts)
		}
	}
	return ids, scores, err
}

func (m *ShardIndexManager) triggerSeal() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.mutable == nil || !m.mutable.ShouldSeal() {
		return
	}

	currentMutable := m.mutable
	mutableID := currentMutable.ID()
	if err := m.flushIndexer(); err != nil {
		log.Printf("Failed to flush segment indexer before seal: %v", err)
		return
	}

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
	m.searcher.SetMutable(newMutable)
	m.manifest.MutableSegmentID = newMutable.ID()
	m.manifest.UpdatedAt = time.Now()

	manifestStore := NewManifestStore(m.db, m.shardID, m.config.Namespace)
	if err := manifestStore.Save(m.manifest); err != nil {
		log.Printf("Failed to save manifest: %v", err)
	}
}

func (m *ShardIndexManager) sealTombstones() (map[string]int64, error) {
	if m.sealer == nil {
		return map[string]int64{}, nil
	}
	return m.sealer.GetTombstones(0)
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
	before := append([]SegmentID(nil), m.manifest.ImmutableSegments...)

	if err := m.sealer.RunCompaction(m.manifest); err != nil {
		log.Printf("Compaction failed: %v", err)
		return
	}

	manifestStore := NewManifestStore(m.db, m.shardID, m.config.Namespace)
	if err := manifestStore.Save(m.manifest); err != nil {
		log.Printf("Failed to save manifest after compaction: %v", err)
		return
	}
	afterSet := make(map[SegmentID]struct{}, len(m.manifest.ImmutableSegments))
	for _, id := range m.manifest.ImmutableSegments {
		afterSet[id] = struct{}{}
		if _, ok := m.immutables.Get(id); !ok {
			if seg, err := LoadImmutableSegment(m.shardID, id, m.config, false); err == nil {
				m.immutables.Add(seg)
			}
		}
	}
	for _, id := range before {
		if _, ok := afterSet[id]; !ok {
			m.immutables.Remove(id)
		}
	}
}

func (m *ShardIndexManager) Close() error {
	close(m.stopCh)
	m.wg.Wait()
	if m.indexerStop != nil {
		close(m.indexerStop)
		m.indexerWg.Wait()
	}

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

func (m *ShardIndexManager) enqueueIndexOp(op segmentIndexOp) {
	if m.indexerCh == nil {
		return
	}
	atomic.AddUint64(&m.indexPending, 1)
	select {
	case m.indexerCh <- op:
	default:
		atomic.AddUint64(&m.indexPending, ^uint64(0))
		m.applyIndexOp(op)
	}
}

func (m *ShardIndexManager) applyIndexOp(op segmentIndexOp) {
	m.mu.RLock()
	mutable := m.mutable
	m.mu.RUnlock()
	if mutable == nil || mutable.ID() != op.segID {
		return
	}
	switch op.opType {
	case segmentIndexOpAdd:
		if len(op.ids) > 0 {
			_ = mutable.ApplyAddBatch(op.ids, op.vectors)
			return
		}
		_ = mutable.ApplyAdd(op.id, op.vector)
	case segmentIndexOpDelete:
		_ = mutable.ApplyDelete(op.id)
	}
}

func (m *ShardIndexManager) indexerLoop() {
	defer m.indexerWg.Done()
	batchSize := envIntSegment("VECTRON_SEGMENT_INDEX_BATCH_SIZE", 2048)
	if batchSize <= 0 {
		batchSize = 2048
	}
	flushInterval := time.Duration(envIntSegment("VECTRON_SEGMENT_INDEX_FLUSH_MS", 200)) * time.Millisecond
	if flushInterval <= 0 {
		flushInterval = 200 * time.Millisecond
	}
	timer := time.NewTimer(flushInterval)
	defer timer.Stop()
	ops := make([]segmentIndexOp, 0, batchSize)
	flush := func() {
		if len(ops) == 0 {
			return
		}
		var applied uint64
		for _, op := range ops {
			m.applyIndexOp(op)
			if len(op.ids) > 0 {
				applied += uint64(len(op.ids))
			} else {
				applied++
			}
		}
		if applied > 0 {
			atomic.AddUint64(&m.indexPending, ^uint64(applied-1))
		}
		ops = ops[:0]
	}
	for {
		select {
		case <-m.indexerStop:
			flush()
			return
		case op := <-m.indexerCh:
			if op.opType == segmentIndexOpBarrier {
				flush()
				close(op.done)
				continue
			}
			ops = append(ops, op)
			if len(ops) >= batchSize {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(flushInterval)
			}
		case <-timer.C:
			flush()
			timer.Reset(flushInterval)
		}
	}
}

func (m *ShardIndexManager) flushIndexer() error {
	if m.indexerCh == nil {
		return nil
	}
	done := make(chan struct{})
	m.indexerCh <- segmentIndexOp{opType: segmentIndexOpBarrier, done: done}
	<-done
	return nil
}

func envIntSegment(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return fallback
	}
	return n
}

func envBoolSegment(key string, fallback bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		return fallback
	}
	return b
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
	stats.PendingIndexOps = int64(atomic.LoadUint64(&m.indexPending))
	if m.mutable != nil {
		stats.MutableBytesEstimate = m.mutable.Meta().BytesEstimate
	}
	for _, seg := range m.immutables.List() {
		stats.ImmutableBytesEstimate += seg.BytesEstimate()
		stats.ImmutablePayloadBytes += seg.PayloadBytes()
	}

	return stats
}

type IndexStats struct {
	ShardID                uint64
	MutableCount           int64
	ImmutableCount         int64
	TotalSegments          int64
	TombstoneCount         int64
	PendingIndexOps        int64
	MutableBytesEstimate   int64
	ImmutableBytesEstimate int64
	ImmutablePayloadBytes  int64
}
