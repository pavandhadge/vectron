package segment

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/cockroachdb/pebble"
)

type Compactor struct {
	config Config
	db     *pebble.DB
	mu     sync.Mutex
	wg     sync.WaitGroup
	stopCh chan struct{}
}

func NewCompactor(cfg Config, db *pebble.DB) *Compactor {
	return &Compactor{
		config: cfg,
		db:     db,
		stopCh: make(chan struct{}),
	}
}

func (c *Compactor) Compact(manifest *ShardManifest) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(manifest.ImmutableSegments) < 2 {
		return nil
	}

	segmentsBySize := make(map[SegmentID]int64)
	for _, segID := range manifest.ImmutableSegments {
		segmentsBySize[segID] = 0
	}

	if len(segmentsBySize) < 2 {
		return nil
	}

	var sortedIDs []SegmentID
	for id := range segmentsBySize {
		sortedIDs = append(sortedIDs, id)
	}

	for i := 0; i < len(sortedIDs)-1; i++ {
		for j := i + 1; j < len(sortedIDs); j++ {
			if segmentsBySize[sortedIDs[j]] < segmentsBySize[sortedIDs[i]] {
				sortedIDs[i], sortedIDs[j] = sortedIDs[j], sortedIDs[i]
			}
		}
	}

	fanIn := c.config.Thresholds.CompactionFanIn
	if fanIn <= 0 {
		fanIn = 4
	}
	if len(sortedIDs) > fanIn {
		sortedIDs = sortedIDs[:fanIn]
	}

	log.Printf("Compacting %d segments: %v", len(sortedIDs), sortedIDs)

	newSegID := GenerateSegmentID()
	if err := c.mergeSegments(sortedIDs, newSegID); err != nil {
		return fmt.Errorf("failed to merge segments: %w", err)
	}

	for _, oldID := range sortedIDs {
		manifest.ImmutableSegments = removeSegment(manifest.ImmutableSegments, oldID)
		c.markObsolete(oldID)
	}

	manifest.ImmutableSegments = append(manifest.ImmutableSegments, newSegID)
	manifest.Version++
	manifest.UpdatedAt = time.Now()

	if manifest.CompactionMeta == nil {
		manifest.CompactionMeta = &CompactionMeta{}
	}
	manifest.CompactionMeta.CompactionCount++
	manifest.CompactionMeta.LastCompactionAt = time.Now()

	return nil
}

func (c *Compactor) mergeSegments(segIDs []SegmentID, newSegID SegmentID) error {
	return nil
}

func (c *Compactor) markObsolete(segID SegmentID) error {
	return nil
}

func (c *Compactor) Stop() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Compactor) ShouldCompact(manifest *ShardManifest) bool {
	minSegments := 3
	if c.config.Thresholds.CompactionFanIn > 0 {
		minSegments = c.config.Thresholds.CompactionFanIn
	}

	if len(manifest.ImmutableSegments) >= minSegments {
		return true
	}

	if manifest.CompactionMeta == nil {
		return false
	}

	compactionInterval := 10 * time.Minute
	if time.Since(manifest.CompactionMeta.LastCompactionAt) > compactionInterval && len(manifest.ImmutableSegments) >= 2 {
		return true
	}

	return false
}

type CompactionStats struct {
	SegmentsCompacted int
	NewSegmentID      SegmentID
	Duration          time.Duration
}

func (c *Compactor) RunManualCompaction(manifest *ShardManifest) (*CompactionStats, error) {
	start := time.Now()

	if err := c.Compact(manifest); err != nil {
		return nil, err
	}

	stats := &CompactionStats{
		SegmentsCompacted: len(manifest.ImmutableSegments),
		Duration:          time.Since(start),
	}

	return stats, nil
}
