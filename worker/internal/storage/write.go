// This file implements the write operations for the PebbleDB storage engine.
// It provides methods for single and batch writes, as well as the logic for
// storing and deleting vectors, which involves coordinating writes between
// the HNSW index and the underlying PebbleDB store.

package storage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/pavandhadge/vectron/worker/internal/idxhnsw"
)

// Put inserts or updates a single key-value pair in the database.
func (r *PebbleDB) Put(key []byte, value []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Set(key, value, r.writeOpts)
}

// Delete removes a single key from the database.
func (r *PebbleDB) Delete(key []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.db.Delete(key, r.writeOpts)
}

// BatchWrite performs a series of puts and deletes in a single atomic batch.
func (r *PebbleDB) BatchWrite(ops BatchOperations) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	batch := r.db.NewBatch()
	defer batch.Close()

	for k, v := range ops.Puts {
		if err := batch.Set([]byte(k), v, nil); err != nil {
			return err
		}
	}
	for _, k := range ops.Deletes {
		if err := batch.Delete([]byte(k), nil); err != nil {
			return err
		}
	}
	return batch.Commit(r.writeOpts)
}

// BatchPut performs a series of puts in a single atomic batch.
func (r *PebbleDB) BatchPut(puts map[string][]byte) error {
	return r.BatchWrite(BatchOperations{Puts: puts})
}

// BatchDelete performs a series of deletes in a single atomic batch.
func (r *PebbleDB) BatchDelete(keys []string) error {
	return r.BatchWrite(BatchOperations{Deletes: keys})
}

// vectorKey creates the database key for a vector from its string ID.
func vectorKey(id string) []byte {
	return []byte("v_" + id)
}

// StoreVector stores a vector and its metadata in both the HNSW index and PebbleDB.
// It also writes to a write-ahead log (WAL) if enabled.
func (r *PebbleDB) StoreVector(id string, vector []float32, metadata []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	// 1. Add to HNSW index first. This is an in-memory operation.
	indexVector := vector
	if r.opts != nil && r.opts.HNSWConfig.DistanceMetric == "cosine" && r.opts.HNSWConfig.NormalizeVectors {
		indexVector = idxhnsw.NormalizeVector(vector)
	}
	if err := r.hnsw.Add(id, indexVector); err != nil {
		return fmt.Errorf("failed to add vector to HNSW index: %w", err)
	}

	// 2. If HNSW add is successful, commit the vector and WAL entry to PebbleDB.
	val, err := encodeVectorWithMeta(vector, metadata)
	if err != nil {
		// Attempt to rollback the HNSW add.
		_ = r.hnsw.Delete(id)
		return fmt.Errorf("failed to encode vector with meta: %w", err)
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	// Add the vector data to the batch.
	if err := batch.Set(vectorKey(id), val, nil); err != nil {
		_ = r.hnsw.Delete(id)
		return fmt.Errorf("failed to set vector in batch: %w", err)
	}

	// If the WAL is enabled, also add a WAL entry to the batch.
	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, val, nil); err != nil {
			_ = r.hnsw.Delete(id)
			return fmt.Errorf("failed to set WAL in batch: %w", err)
		}
	}

	// Commit the batch atomically.
	if err := batch.Commit(r.writeOpts); err != nil {
		_ = r.hnsw.Delete(id)
		return fmt.Errorf("failed to commit batch for StoreVector: %w", err)
	}

	r.recordHNSWWrite(1)
	return nil
}

// StoreVectorBatch stores multiple vectors in a single atomic batch.
// It updates the HNSW index for each vector and writes all records in one commit.
func (r *PebbleDB) StoreVectorBatch(vectors []VectorEntry) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	if len(vectors) == 0 {
		return nil
	}
	if r.shouldBulkLoad(len(vectors)) {
		return r.bulkLoadVectors(vectors)
	}

	added := make([]string, 0, len(vectors))
	batch := r.db.NewBatch()
	defer batch.Close()

	for _, v := range vectors {
		if v.ID == "" || len(v.Vector) == 0 {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return errors.New("vector id or data missing")
		}

		indexVector := v.Vector
		if r.opts != nil && r.opts.HNSWConfig.DistanceMetric == "cosine" && r.opts.HNSWConfig.NormalizeVectors {
			indexVector = idxhnsw.NormalizeVector(v.Vector)
		}
		if err := r.hnsw.Add(v.ID, indexVector); err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to add vector to HNSW index: %w", err)
		}
		added = append(added, v.ID)

		val, err := encodeVectorWithMeta(v.Vector, v.Metadata)
		if err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to encode vector with meta: %w", err)
		}

		if err := batch.Set(vectorKey(v.ID), val, nil); err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to set vector in batch: %w", err)
		}

		if r.opts.HNSWConfig.WALEnabled {
			walKey := []byte(fmt.Sprintf("%s%d_%s", hnswWALPrefix, time.Now().UnixNano(), v.ID))
			if err := batch.Set(walKey, val, nil); err != nil {
				for _, id := range added {
					_ = r.hnsw.Delete(id)
				}
				return fmt.Errorf("failed to set WAL in batch: %w", err)
			}
		}
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		for _, id := range added {
			_ = r.hnsw.Delete(id)
		}
		return fmt.Errorf("failed to commit batch for StoreVectorBatch: %w", err)
	}

	r.recordHNSWWrite(uint64(len(added)))
	return nil
}

// DeleteVector deletes a vector by its ID from both the HNSW index and PebbleDB.
// It performs a soft delete in the HNSW index and a hard delete in PebbleDB.
func (r *PebbleDB) DeleteVector(id string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	// 1. Soft-delete from the HNSW index first.
	if err := r.hnsw.Delete(id); err != nil {
		return fmt.Errorf("failed to delete vector from HNSW index: %w", err)
	}

	// 2. If HNSW delete is successful, hard-delete from PebbleDB and write to WAL.
	batch := r.db.NewBatch()
	defer batch.Close()

	// We use a tombstone (empty value) to mark deletion in the WAL.
	if err := batch.Delete(vectorKey(id), nil); err != nil {
		// Attempt to rollback HNSW delete. This might not be perfect,
		// as the original vector data is not available here.
		// A more robust implementation would fetch the vector before deleting.
		return fmt.Errorf("failed to delete vector from batch: %w", err)
	}

	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s_delete", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, nil, nil); err != nil {
			return fmt.Errorf("failed to set WAL delete marker in batch: %w", err)
		}
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		return fmt.Errorf("failed to commit batch for DeleteVector: %w", err)
	}

	r.recordHNSWWrite(1)
	return nil
}

func (r *PebbleDB) shouldBulkLoad(batchSize int) bool {
	if r.opts == nil || !r.opts.HNSWConfig.BulkLoadEnabled {
		return false
	}
	threshold := r.opts.HNSWConfig.BulkLoadThreshold
	if threshold <= 0 {
		threshold = 1000
	}
	if batchSize < threshold {
		return false
	}
	if r.hnswSnapshotLoaded {
		return false
	}
	if r.hnsw != nil && r.hnsw.Size() > 0 {
		return false
	}
	return true
}

func (r *PebbleDB) bulkLoadVectors(vectors []VectorEntry) error {
	added := make([]string, 0, len(vectors))
	batch := r.db.NewBatch()
	defer batch.Close()

	for _, v := range vectors {
		if v.ID == "" || len(v.Vector) == 0 {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return errors.New("vector id or data missing")
		}

		indexVector := v.Vector
		if r.opts != nil && r.opts.HNSWConfig.DistanceMetric == "cosine" && r.opts.HNSWConfig.NormalizeVectors {
			indexVector = idxhnsw.NormalizeVector(v.Vector)
		}
		if err := r.hnsw.Add(v.ID, indexVector); err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to add vector to HNSW index: %w", err)
		}
		added = append(added, v.ID)

		val, err := encodeVectorWithMeta(v.Vector, v.Metadata)
		if err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to encode vector with meta: %w", err)
		}

		if err := batch.Set(vectorKey(v.ID), val, nil); err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to set vector in batch: %w", err)
		}
	}

	var buf bytes.Buffer
	if err := r.hnsw.Save(&buf); err != nil {
		for _, id := range added {
			_ = r.hnsw.Delete(id)
		}
		return fmt.Errorf("failed to serialize HNSW index: %w", err)
	}

	ts := time.Now().UnixNano()
	tsStr := strconv.FormatInt(ts, 10)
	if err := batch.Set([]byte(hnswIndexKey), buf.Bytes(), r.writeOpts); err != nil {
		for _, id := range added {
			_ = r.hnsw.Delete(id)
		}
		return err
	}
	if err := batch.Set([]byte(hnswIndexTimestampKey), []byte(tsStr), r.writeOpts); err != nil {
		for _, id := range added {
			_ = r.hnsw.Delete(id)
		}
		return err
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		for _, id := range added {
			_ = r.hnsw.Delete(id)
		}
		return fmt.Errorf("failed to commit batch for bulk load: %w", err)
	}

	r.markHNSWSnapshotSaved(ts)
	return nil
}

// encodeVectorWithMeta serializes a vector and its metadata into a single byte slice.
// The format is: [vector_length (4 bytes)] [vector_data] [metadata_data].
func encodeVectorWithMeta(vector []float32, metadata []byte) ([]byte, error) {
	buf := new(bytes.Buffer)
	vecLen := int32(len(vector))

	// Write the length of the vector.
	if err := binary.Write(buf, binary.LittleEndian, vecLen); err != nil {
		return nil, err
	}
	// Write the vector itself.
	if err := binary.Write(buf, binary.LittleEndian, vector); err != nil {
		return nil, err
	}
	// Append the metadata.
	if len(metadata) > 0 {
		if _, err := buf.Write(metadata); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}
