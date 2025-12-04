package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Put inserts or updates a key-value pair.
func (r *PebbleDB) Put(key []byte, value []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Set(key, value, r.writeOpts)
}

// Delete removes a key from the database.
func (r *PebbleDB) Delete(key []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Delete(key, r.writeOpts)
}

// BatchWrite performs a series of puts and deletes in a single atomic operation.
func (r *PebbleDB) BatchWrite(ops BatchOperations) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	batch := r.db.NewBatch()
	defer batch.Close()

	for k, v := range ops.Puts {
		batch.Set([]byte(k), v, nil)
	}
	for _, k := range ops.Deletes {
		batch.Delete([]byte(k), nil)
	}
	return batch.Commit(r.writeOpts)
}

// BatchPut performs a series of puts in a single atomic operation.
func (r *PebbleDB) BatchPut(puts map[string][]byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	batch := r.db.NewBatch()
	defer batch.Close()

	for k, v := range puts {
		batch.Set([]byte(k), v, nil)
	}
	return batch.Commit(r.writeOpts)
}

// BatchDelete performs a series of deletes in a single atomic operation.
func (r *PebbleDB) BatchDelete(keys []string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	batch := r.db.NewBatch()
	defer batch.Close()

	for _, k := range keys {
		batch.Delete([]byte(k), nil)
	}
	return batch.Commit(r.writeOpts)
}

// StoreVector stores a vector and its metadata.

func (r *PebbleDB) StoreVector(id string, vector []float32, metadata []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	// 1. Add to HNSW index first
	if err := r.hnsw.Add(id, vector); err != nil {
		return fmt.Errorf("failed to add vector to HNSW index: %w", err)
	}

	// 2. If HNSW update is successful, commit to PebbleDB
	val, err := encodeVectorWithMeta(vector, metadata)
	if err != nil {
		// Rollback HNSW add
		if rollbackErr := r.hnsw.Delete(id); rollbackErr != nil {
			return fmt.Errorf("failed to encode vector with meta, and also failed to rollback HNSW add: %w", rollbackErr)
		}
		return fmt.Errorf("failed to encode vector with meta: %w", err)
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	if err := batch.Set([]byte(id), val, nil); err != nil {
		// Rollback HNSW add
		if rollbackErr := r.hnsw.Delete(id); rollbackErr != nil {
			return fmt.Errorf("failed to set vector in batch, and also failed to rollback HNSW add: %w", rollbackErr)
		}
		return fmt.Errorf("failed to set vector in batch: %w", err)
	}

	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, val, nil); err != nil {
			// Rollback HNSW add
			if rollbackErr := r.hnsw.Delete(id); rollbackErr != nil {
				return fmt.Errorf("failed to set WAL in batch, and also failed to rollback HNSW add: %w", rollbackErr)
			}
			return fmt.Errorf("failed to set WAL in batch: %w", err)
		}
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		// Rollback HNSW add
		if rollbackErr := r.hnsw.Delete(id); rollbackErr != nil {
			return fmt.Errorf("failed to commit batch, and also failed to rollback HNSW add: %w", rollbackErr)
		}
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

// DeleteVector deletes a vector by its ID.
func (r *PebbleDB) DeleteVector(id string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	// 1. Fetch the vector from PebbleDB to get the vector data for rollback.
	vec, _, err := r.GetVector(id)
	if err != nil {
		return fmt.Errorf("failed to get vector for rollback: %w", err)
	}
	if vec == nil {
		// Vector doesn't exist, so nothing to delete.
		return nil
	}

	// 2. Delete from HNSW index first
	if err := r.hnsw.Delete(id); err != nil {
		return fmt.Errorf("failed to delete vector from HNSW index: %w", err)
	}

	// 3. If HNSW update is successful, commit to PebbleDB
	batch := r.db.NewBatch()
	defer batch.Close()

	if err := batch.Delete([]byte(id), nil); err != nil {
		// Rollback HNSW delete
		if rollbackErr := r.hnsw.Add(id, vec); rollbackErr != nil {
			return fmt.Errorf("failed to delete vector from batch, and also failed to rollback HNSW delete: %w", rollbackErr)
		}
		return fmt.Errorf("failed to delete vector from batch: %w", err)
	}

	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s_delete", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, nil, nil); err != nil {
			// Rollback HNSW delete
			if rollbackErr := r.hnsw.Add(id, vec); rollbackErr != nil {
				return fmt.Errorf("failed to set WAL in batch, and also failed to rollback HNSW delete: %w", rollbackErr)
			}
			return fmt.Errorf("failed to set WAL in batch: %w", err)
		}
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		// Rollback HNSW delete
		if rollbackErr := r.hnsw.Add(id, vec); rollbackErr != nil {
			return fmt.Errorf("failed to commit batch, and also failed to rollback HNSW delete: %w", rollbackErr)
		}
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

// encodeVectorWithMeta serializes a vector and metadata into a byte slice.
func encodeVectorWithMeta(vector []float32, metadata []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	// Write the length of the vector
	if err := binary.Write(buf, binary.LittleEndian, int32(len(vector))); err != nil {
		return nil, err
	}
	// Write the vector itself
	if err := binary.Write(buf, binary.LittleEndian, vector); err != nil {
		return nil, err
	}
	// Append the metadata
	if _, err := buf.Write(metadata); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
