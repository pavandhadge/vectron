// This file implements the core database lifecycle and administrative functions
// for the PebbleDB storage engine. It handles initialization, closing, and the
// persistence of the HNSW index, including a write-ahead log (WAL) for durability.

package storage

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

// Constants for internal keys used in PebbleDB.
const (
	hnswIndexKey          = "_hnsw_index"    // Key for the serialized HNSW index.
	hnswIndexTimestampKey = "_hnsw_index_ts" // Key for the timestamp of the last HNSW save.
	hnswWALPrefix         = "_hnsw_wal_"     // Prefix for HNSW write-ahead log entries.
)

// Init initializes the PebbleDB instance and the HNSW index.
// It configures the database, opens it, and loads or creates the HNSW index.
func (r *PebbleDB) Init(path string, opts *Options) error {
	dbOpts := &pebble.Options{}
	r.opts = opts

	applyPebbleTuning(dbOpts, opts)

	var err error
	r.path = path
	r.db, err = pebble.Open(path, dbOpts)
	if err != nil {
		return fmt.Errorf("failed to open pebble db at %s: %w", path, err)
	}

	// Use async writes for high throughput with periodic background sync.
	// This provides 5-10x better write performance while still ensuring durability
	// through periodic flushing and the WAL.
	r.writeOpts = pebble.NoSync
	r.stop = make(chan struct{})

	// Start background sync loop to periodically flush data to disk.
	// This balances durability with performance.
	r.wg.Add(1)
	go r.backgroundSyncLoop(100 * time.Millisecond)

	// Load the HNSW index from the database or create a new one.
	if err := r.loadHNSW(opts); err != nil {
		return fmt.Errorf("failed to load HNSW index: %w", err)
	}

	// If the HNSW WAL is enabled, start the background persistence loop.
	if opts.HNSWConfig.WALEnabled {
		baseInterval := opts.HNSWConfig.SnapshotInterval
		if baseInterval <= 0 {
			baseInterval = 5 * time.Minute
		}
		maxInterval := opts.HNSWConfig.SnapshotMaxInterval
		if maxInterval <= 0 {
			maxInterval = 30 * time.Minute
		}
		writeThreshold := opts.HNSWConfig.SnapshotWriteThreshold
		if writeThreshold == 0 {
			writeThreshold = 10000
		}
		r.hnswSnapshotBaseInterval = baseInterval
		r.hnswSnapshotMaxInterval = maxInterval
		r.hnswSnapshotWriteThreshold = writeThreshold

		r.wg.Add(1)
		go r.persistenceLoop(baseInterval) // Periodically save the index.
	}

	if opts.HNSWConfig.MaintenanceEnabled {
		interval := opts.HNSWConfig.MaintenanceInterval
		if interval <= 0 {
			interval = 30 * time.Minute
		}
		r.wg.Add(1)
		go r.maintenanceLoop(interval)
	}

	return nil
}

// Close gracefully closes the PebbleDB instance, ensuring all data is flushed.
// It also triggers a final save of the HNSW index.
func (r *PebbleDB) Close() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	// Signal all background loops to stop.
	close(r.stop)
	// Wait for all background goroutines to finish.
	r.wg.Wait()

	// Perform a final flush to ensure all data is written to disk.
	if err := r.Flush(); err != nil {
		log.Printf("Warning: failed to flush data on close: %v", err)
	}

	// Perform a final save of the HNSW index.
	if err := r.saveHNSW(); err != nil {
		log.Printf("Warning: failed to save HNSW index on close: %v", err)
	}

	return r.db.Close()
}

// loadHNSW loads the HNSW index from a snapshot in the database and replays the WAL.
func (r *PebbleDB) loadHNSW(opts *Options) error {
	r.hnsw = idxhnsw.NewHNSW(r, opts.HNSWConfig.Dim, idxhnsw.HNSWConfig{
		M:                 opts.HNSWConfig.M,
		EfConstruction:    opts.HNSWConfig.EfConstruction,
		EfSearch:          opts.HNSWConfig.EfSearch,
		Distance:          opts.HNSWConfig.DistanceMetric,
		PersistNodes:      opts.HNSWConfig.PersistNodes,
		EnableNorms:       opts.HNSWConfig.EnableNorms,
		NormalizeVectors:  opts.HNSWConfig.NormalizeVectors,
		QuantizeVectors:   opts.HNSWConfig.QuantizeVectors,
		SearchParallelism: opts.HNSWConfig.SearchParallelism,
	})

	if opts.HNSWConfig.HotIndexEnabled {
		hotStore := idxhnsw.NewMemStore()
		r.hnswHot = idxhnsw.NewHNSW(hotStore, opts.HNSWConfig.Dim, idxhnsw.HNSWConfig{
			M:                 opts.HNSWConfig.M,
			EfConstruction:    opts.HNSWConfig.EfConstruction,
			EfSearch:          opts.HNSWConfig.EfSearch,
			Distance:          opts.HNSWConfig.DistanceMetric,
			PersistNodes:      false,
			EnableNorms:       opts.HNSWConfig.EnableNorms,
			NormalizeVectors:  opts.HNSWConfig.NormalizeVectors,
			QuantizeVectors:   opts.HNSWConfig.QuantizeVectors,
			SearchParallelism: opts.HNSWConfig.SearchParallelism,
			HotIndex:          true,
		})
		r.hotSet = make(map[string]struct{})
	}

	data, closer, err := r.db.Get([]byte(hnswIndexKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			log.Println("No existing HNSW index found, creating a new one.")
			return nil // No index exists, so nothing to load.
		}
		return err
	}
	defer closer.Close()

	if err := r.hnsw.Load(bytes.NewReader(data)); err != nil {
		log.Printf("Failed to load HNSW index from snapshot, creating a new one: %v", err)
		// Reset to a new index if loading fails.
		r.hnsw = idxhnsw.NewHNSW(r, opts.HNSWConfig.Dim, idxhnsw.HNSWConfig{
			M:                 opts.HNSWConfig.M,
			EfConstruction:    opts.HNSWConfig.EfConstruction,
			EfSearch:          opts.HNSWConfig.EfSearch,
			Distance:          opts.HNSWConfig.DistanceMetric,
			PersistNodes:      opts.HNSWConfig.PersistNodes,
			EnableNorms:       opts.HNSWConfig.EnableNorms,
			NormalizeVectors:  opts.HNSWConfig.NormalizeVectors,
			QuantizeVectors:   opts.HNSWConfig.QuantizeVectors,
			SearchParallelism: opts.HNSWConfig.SearchParallelism,
		})
		return nil
	}

	r.hnswSnapshotLoaded = true
	if tsData, closer, err := r.db.Get([]byte(hnswIndexTimestampKey)); err == nil {
		if ts, err := strconv.ParseInt(string(tsData), 10, 64); err == nil {
			r.hnswSnapshotMu.Lock()
			r.hnswLastSnapshot = time.Unix(0, ts)
			r.hnswSnapshotMu.Unlock()
		}
		closer.Close()
	}

	// If WAL is enabled, replay any operations that occurred after the last snapshot.
	if opts.HNSWConfig.WALEnabled {
		return r.replayWAL()
	}

	return nil
}

// saveHNSW serializes the current HNSW index and writes it to the database.
// It also cleans up old WAL entries that are now covered by the new snapshot.
func (r *PebbleDB) saveHNSW() error {
	var buf bytes.Buffer
	if err := r.hnsw.Save(&buf); err != nil {
		return fmt.Errorf("failed to serialize HNSW index: %w", err)
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	ts := time.Now().UnixNano()
	tsStr := strconv.FormatInt(ts, 10)

	// Write the new index snapshot and its timestamp.
	if err := batch.Set([]byte(hnswIndexKey), buf.Bytes(), r.writeOpts); err != nil {
		return err
	}
	if err := batch.Set([]byte(hnswIndexTimestampKey), []byte(tsStr), r.writeOpts); err != nil {
		return err
	}

	// Delete all WAL entries created before this snapshot.
	iter := r.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(hnswWALPrefix),
		UpperBound: []byte(fmt.Sprintf("%s%s", hnswWALPrefix, tsStr)),
	})
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := batch.Delete(iter.Key(), r.writeOpts); err != nil {
			// Log error but continue trying to clean up.
			log.Printf("Error deleting old WAL entry %s: %v", iter.Key(), err)
		}
	}
	if err := iter.Error(); err != nil {
		return err
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		return err
	}

	r.markHNSWSnapshotSaved(ts)
	return nil
}

func (r *PebbleDB) markHNSWSnapshotSaved(ts int64) {
	atomic.StoreUint64(&r.hnswWriteCount, 0)
	r.hnswSnapshotMu.Lock()
	if ts > 0 {
		r.hnswLastSnapshot = time.Unix(0, ts)
	} else {
		r.hnswLastSnapshot = time.Now()
	}
	r.hnswSnapshotMu.Unlock()
	r.hnswSnapshotLoaded = true
}

func (r *PebbleDB) recordHNSWWrite(count uint64) {
	if count == 0 {
		return
	}
	if r.opts != nil && r.opts.HNSWConfig.WALEnabled {
		atomic.AddUint64(&r.hnswWriteCount, count)
	}
}

func (r *PebbleDB) maintenanceLoop(interval time.Duration) {
	defer r.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.maybeRebuildHNSW()
		case <-r.stop:
			log.Println("Stopping HNSW maintenance loop.")
			return
		}
	}
}

func (r *PebbleDB) maybeRebuildHNSW() {
	if r.hnsw == nil {
		return
	}
	stats := r.hnsw.Stats()
	if stats.TotalNodes == 0 {
		return
	}
	minDeleted := r.opts.HNSWConfig.RebuildMinDeleted
	if minDeleted <= 0 {
		minDeleted = 5000
	}
	threshold := r.opts.HNSWConfig.RebuildDeletedRatio
	if threshold <= 0 {
		threshold = 0.30
	}
	if stats.DeletedNodes < minDeleted {
		return
	}
	deletedRatio := float64(stats.DeletedNodes) / float64(stats.TotalNodes)
	if deletedRatio < threshold {
		return
	}

	r.hnswRebuildMu.Lock()
	if r.hnswRebuildInProgress {
		r.hnswRebuildMu.Unlock()
		return
	}
	r.hnswRebuildInProgress = true
	r.hnswRebuildMu.Unlock()

	go func() {
		defer func() {
			r.hnswRebuildMu.Lock()
			r.hnswRebuildInProgress = false
			r.hnswRebuildMu.Unlock()
		}()

		log.Printf("Starting HNSW rebuild: total=%d deleted=%d ratio=%.2f", stats.TotalNodes, stats.DeletedNodes, deletedRatio)
		if err := r.hnsw.Rebuild(); err != nil {
			log.Printf("HNSW rebuild failed: %v", err)
			return
		}
		if err := r.saveHNSW(); err != nil {
			log.Printf("HNSW rebuild snapshot failed: %v", err)
			return
		}
		log.Printf("HNSW rebuild completed")
	}()
}

// replayWAL replays write-ahead log entries that occurred after the last HNSW snapshot.
func (r *PebbleDB) replayWAL() error {
	tsData, closer, err := r.db.Get([]byte(hnswIndexTimestampKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil // No timestamp, so no WAL to replay.
		}
		return err
	}
	defer closer.Close()

	ts, err := strconv.ParseInt(string(tsData), 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse WAL timestamp: %w", err)
	}

	// Iterate over WAL entries created after the last snapshot timestamp.
	iter := r.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(fmt.Sprintf("%s%d", hnswWALPrefix, ts)),
	})
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		key := string(iter.Key())
		parts := strings.Split(strings.TrimPrefix(key, hnswWALPrefix), "_")
		if len(parts) < 2 {
			continue
		}

		id := parts[1]
		if strings.HasSuffix(key, "_delete") {
			if err := r.hnsw.Delete(id); err != nil {
				log.Printf("Failed to replay delete from WAL for ID %s: %v", id, err)
			}
		} else {
			vector, _, err := decodeVectorWithMeta(iter.Value())
			if err != nil {
				log.Printf("Failed to decode vector from WAL for ID %s: %v", id, err)
				continue
			}
			if err := r.hnsw.Add(id, vector); err != nil {
				log.Printf("Failed to replay add from WAL for ID %s: %v", id, err)
			}
		}
	}
	return iter.Error()
}

// persistenceLoop is a background goroutine that periodically saves the HNSW index.
func (r *PebbleDB) persistenceLoop(interval time.Duration) {
	defer r.wg.Done()
	baseInterval := interval
	if baseInterval <= 0 {
		baseInterval = 5 * time.Minute
	}
	maxInterval := r.hnswSnapshotMaxInterval
	if maxInterval <= 0 {
		maxInterval = 30 * time.Minute
	}
	writeThreshold := r.hnswSnapshotWriteThreshold
	if writeThreshold == 0 {
		writeThreshold = 10000
	}
	currentInterval := baseInterval

	timer := time.NewTimer(currentInterval)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			writes := atomic.LoadUint64(&r.hnswWriteCount)
			r.hnswSnapshotMu.Lock()
			lastSnapshot := r.hnswLastSnapshot
			r.hnswSnapshotMu.Unlock()

			timeSinceSnapshot := time.Duration(0)
			if !lastSnapshot.IsZero() {
				timeSinceSnapshot = time.Since(lastSnapshot)
			}

			shouldSave := false
			if writes == 0 {
				shouldSave = false
			} else if timeSinceSnapshot >= maxInterval {
				shouldSave = true
			} else if writes >= writeThreshold {
				shouldSave = false
			} else {
				shouldSave = true
			}

			if shouldSave {
				if err := r.saveHNSW(); err != nil {
					log.Printf("Error during periodic HNSW save: %v", err)
				}
				currentInterval = baseInterval
			} else {
				if currentInterval < maxInterval {
					currentInterval *= 2
					if currentInterval > maxInterval {
						currentInterval = maxInterval
					}
				}
			}

			timer.Reset(currentInterval)
		case <-r.stop:
			log.Println("Stopping HNSW persistence loop.")
			return
		}
	}
}

// backgroundSyncLoop periodically flushes data to disk when using async writes.
// This ensures durability without the latency penalty of synchronous writes.
func (r *PebbleDB) backgroundSyncLoop(interval time.Duration) {
	defer r.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.Flush(); err != nil {
				log.Printf("Error during background sync: %v", err)
			}
		case <-r.stop:
			log.Println("Stopping background sync loop.")
			return
		}
	}
}

// Status returns detailed metrics and status information from the PebbleDB instance.
func (r *PebbleDB) Status() (string, error) {
	if r.db == nil {
		return "", errors.New("db not initialized")
	}
	return r.db.Metrics().String(), nil
}

// Compact triggers a manual compaction of the entire database.
func (r *PebbleDB) Compact() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Compact(nil, nil)
}

// Flush forces all in-memory data to be written to disk.
func (r *PebbleDB) Flush() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Flush()
}

// Backup creates a checkpoint of the database at the specified path.
func (r *PebbleDB) Backup(path string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Checkpoint(path)
}

// Restore closes the current database, restores from a backup, and reopens.
func (r *PebbleDB) Restore(backupPath string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	if r.path == "" {
		return errors.New("db path not set")
	}

	// Close the current database
	if err := r.Close(); err != nil {
		return fmt.Errorf("failed to close db: %w", err)
	}

	// Remove the current database directory
	if err := os.RemoveAll(r.path); err != nil {
		return fmt.Errorf("failed to remove current db: %w", err)
	}

	// Copy the backup to the current database location
	if err := copyDir(backupPath, r.path); err != nil {
		return fmt.Errorf("failed to restore backup: %w", err)
	}

	// Reopen the database
	dbOpts := &pebble.Options{}
	applyPebbleTuning(dbOpts, r.opts)

	var err error
	r.db, err = pebble.Open(r.path, dbOpts)
	if err != nil {
		return fmt.Errorf("failed to reopen db: %w", err)
	}

	r.writeOpts = pebble.Sync
	r.stop = make(chan struct{})

	// Reload the HNSW index
	if err := r.loadHNSW(r.opts); err != nil {
		return fmt.Errorf("failed to load HNSW index: %w", err)
	}

	// Restart persistence loop if WAL is enabled
	if r.opts != nil && r.opts.HNSWConfig.WALEnabled {
		r.wg.Add(1)
		go r.persistenceLoop(5 * time.Minute)
	}

	return nil
}

func applyPebbleTuning(dbOpts *pebble.Options, opts *Options) {
	if opts != nil {
		if opts.MaxOpenFiles > 0 {
			dbOpts.MaxOpenFiles = opts.MaxOpenFiles
		}
		if opts.WriteBufferSize > 0 {
			dbOpts.MemTableSize = opts.WriteBufferSize
		}
		if opts.CacheSize > 0 {
			dbOpts.Cache = pebble.NewCache(opts.CacheSize)
		}
	}

	// Performance-oriented defaults (override via env or Options).
	if dbOpts.MemTableSize == 0 {
		dbOpts.MemTableSize = envInt("PEBBLE_MEMTABLE_MB", 64) * 1024 * 1024
	}
	if dbOpts.MemTableStopWritesThreshold == 0 {
		dbOpts.MemTableStopWritesThreshold = envInt("PEBBLE_MEMTABLE_STOP", 4)
	}
	if dbOpts.L0CompactionThreshold == 0 {
		dbOpts.L0CompactionThreshold = envInt("PEBBLE_L0_COMPACT", 4)
	}
	if dbOpts.L0StopWritesThreshold == 0 {
		dbOpts.L0StopWritesThreshold = envInt("PEBBLE_L0_STOP", 12)
	}
	if dbOpts.MaxConcurrentCompactions == 0 {
		compactions := runtime.NumCPU() / 2
		if compactions < 2 {
			compactions = 2
		}
		if compactions > 4 {
			compactions = 4
		}
		dbOpts.MaxConcurrentCompactions = compactions
	}
	if dbOpts.Cache == nil {
		cacheMB := envInt("PEBBLE_CACHE_MB", 256)
		if cacheMB > 0 {
			dbOpts.Cache = pebble.NewCache(int64(cacheMB) * 1024 * 1024)
		}
	}
}

func envInt(key string, def int) int {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return def
	}
	parsed, err := strconv.Atoi(val)
	if err != nil || parsed <= 0 {
		return def
	}
	return parsed
}

// copyDir recursively copies a directory from src to dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
