// This file implements the core database lifecycle and administrative functions
// for the PebbleDB storage engine. It handles initialization, closing, and the
// persistence of the HNSW index, including a write-ahead log (WAL) for durability.

package storage

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
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

	// Apply custom PebbleDB options if provided.
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

	var err error
	r.db, err = pebble.Open(path, dbOpts)
	if err != nil {
		return fmt.Errorf("failed to open pebble db at %s: %w", path, err)
	}

	r.writeOpts = pebble.Sync // Use synchronous writes for durability.
	r.stop = make(chan struct{})

	// Load the HNSW index from the database or create a new one.
	if err := r.loadHNSW(opts); err != nil {
		return fmt.Errorf("failed to load HNSW index: %w", err)
	}

	// If the HNSW WAL is enabled, start the background persistence loop.
	if opts.HNSWConfig.WALEnabled {
		r.wg.Add(1)
		go r.persistenceLoop(5 * time.Minute) // Periodically save the index.
	}

	return nil
}

// Close gracefully closes the PebbleDB instance, ensuring all data is flushed.
// It also triggers a final save of the HNSW index.
func (r *PebbleDB) Close() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	// Stop the background persistence loop if it's running.
	if r.opts.HNSWConfig.WALEnabled {
		close(r.stop)
		r.wg.Wait()
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
		M:              opts.HNSWConfig.M,
		EfConstruction: opts.HNSWConfig.EfConstruction,
		EfSearch:       opts.HNSWConfig.EfSearch,
		Distance:       opts.HNSWConfig.DistanceMetric,
	})

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
			M:              opts.HNSWConfig.M,
			EfConstruction: opts.HNSWConfig.EfConstruction,
			EfSearch:       opts.HNSWConfig.EfSearch,
			Distance:       opts.HNSWConfig.DistanceMetric,
		})
		return nil
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

	return batch.Commit(r.writeOpts)
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
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.saveHNSW(); err != nil {
				log.Printf("Error during periodic HNSW save: %v", err)
			}
		case <-r.stop:
			log.Println("Stopping HNSW persistence loop.")
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

// Backup is not yet implemented.
func (r *PebbleDB) Backup(path string) error {
	return errors.New("backup not implemented")
}

// Restore is not yet implemented.
func (r *PebbleDB) Restore(backupPath string) error {
	return errors.New("restore not implemented")
}
