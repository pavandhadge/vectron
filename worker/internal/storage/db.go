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

const (
	hnswIndexKey          = "_hnsw_index"
	hnswIndexTimestampKey = "_hnsw_index_ts"
	hnswWALPrefix         = "_hnsw_wal_"
)

// Init initializes and opens a PebbleDB instance at the given path.
func (r *PebbleDB) Init(path string, opts *Options) error {
	dbOpts := &pebble.Options{}
	r.opts = opts

	if opts != nil {
		if opts.MaxOpenFiles > 0 {
			dbOpts.MaxOpenFiles = opts.MaxOpenFiles
		}
		if opts.WriteBufferSize > 0 {
			dbOpts.MemTableSize = uint64(opts.WriteBufferSize)
		}
		if opts.CacheSize > 0 {
			cache := pebble.NewCache(opts.CacheSize)
			dbOpts.Cache = cache
		}
	}

	var err error
	r.db, err = pebble.Open(path, dbOpts)
	if err != nil {
		return err
	}

	r.iterOpts = &pebble.IterOptions{}
	r.writeOpts = &pebble.WriteOptions{}
	r.stop = make(chan struct{})

	if err := r.loadHNSW(opts); err != nil {
		return err
	}

	if opts.HNSWConfig.WALEnabled {
		r.wg.Add(1)
		go r.persistenceLoop(5 * time.Minute)
	}

	return nil
}

// Close closes the PebbleDB instance.
func (r *PebbleDB) Close() error {
	if r.db != nil {
		if r.opts.HNSWConfig.WALEnabled {
			close(r.stop)
			r.wg.Wait()
		}

		if err := r.saveHNSW(); err != nil {
			return err
		}
		r.db.Close()
		return nil
	}
	return errors.New("db not initialized")
}

// loadHNSW loads the HNSW index from the database.
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
			return nil
		}
		return err
	}

	defer closer.Close()

	if err := r.hnsw.Load(bytes.NewReader(data)); err != nil {
		log.Printf("failed to load hnsw index, creating a new one: %v", err)
		r.hnsw = idxhnsw.NewHNSW(r, opts.HNSWConfig.Dim, idxhnsw.HNSWConfig{
			M:              opts.HNSWConfig.M,
			EfConstruction: opts.HNSWConfig.EfConstruction,
			EfSearch:       opts.HNSWConfig.EfSearch,
			Distance:       opts.HNSWConfig.DistanceMetric,
		})
		return nil
	}

	if opts.HNSWConfig.WALEnabled {
		return r.replayWAL()
	}

	return nil
}

// saveHNSW saves the HNSW index to the database.
func (r *PebbleDB) saveHNSW() error {
	var buf bytes.Buffer
	if err := r.hnsw.Save(&buf); err != nil {
		return err
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	ts := time.Now().UnixNano()
	tsStr := strconv.FormatInt(ts, 10)

	if err := batch.Set([]byte(hnswIndexKey), buf.Bytes(), nil); err != nil {
		return err
	}
	if err := batch.Set([]byte(hnswIndexTimestampKey), []byte(tsStr), nil); err != nil {
		return err
	}

	// Clean up old WAL entries
	iter, err := r.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(hnswWALPrefix),
		UpperBound: []byte(fmt.Sprintf("%s%d", hnswWALPrefix, ts)),
	})
	if err != nil {
		return err
	}
	defer iter.Close()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := batch.Delete(iter.Key(), nil); err != nil {
			return err
		}
	}

	return batch.Commit(r.writeOpts)
}

func (r *PebbleDB) replayWAL() error {
	tsData, closer, err := r.db.Get([]byte(hnswIndexTimestampKey))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			// No timestamp found, so nothing to replay
			return nil
		}
		return err
	}
	defer closer.Close()

	ts, err := strconv.ParseInt(string(tsData), 10, 64)
	if err != nil {
		return err
	}

	iter, err := r.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(fmt.Sprintf("%s%d", hnswWALPrefix, ts)),
	})
	if err != nil {
		return err
	}
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
				log.Printf("failed to replay delete from WAL: %v", err)
			}
		} else {
			vector, _, err := decodeVectorWithMeta(iter.Value())
			if err != nil {
				log.Printf("failed to decode vector from WAL: %v", err)
				continue
			}
			if err := r.hnsw.Add(id, vector); err != nil {
				log.Printf("failed to replay add from WAL: %v", err)
			}
		}
	}
	return nil
}

func (r *PebbleDB) persistenceLoop(interval time.Duration) {
	defer r.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := r.saveHNSW(); err != nil {
				log.Printf("failed to save hnsw index: %v", err)
			}
		case <-r.stop:
			return
		}
	}
}

// Status returns the status of the database.
func (r *PebbleDB) Status() (string, error) {
	if r.db == nil {
		return "", errors.New("db not initialized")
	}
	// PebbleDB.stats is not a valid property in Pebble.
	// We can use r.db.Metrics().String() for detailed status information.
	return r.db.Metrics().String(), nil
}

// Compact compacts the entire database.
func (r *PebbleDB) Compact() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Compact(nil, nil, false)
}

// Flush flushes the database writes to disk.
func (r *PebbleDB) Flush() error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Flush()
}

// Backup creates a backup of the database.
func (r *PebbleDB) Backup(path string) error {
	return errors.New("backup not implemented")
}

// Restore restores a backup of the database.
func (r *PebbleDB) Restore(backupPath string) error {
	return errors.New("restore not implemented")
}
