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
	"math"
	"strconv"
	"sync/atomic"
	"time"
)

// Put inserts or updates a single key-value pair in the database.
func (r *PebbleDB) Put(key []byte, value []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	if err := r.db.Set(key, value, r.writeOpts); err != nil {
		return err
	}
	r.markDirty()
	return nil
}

// Delete removes a single key from the database.
func (r *PebbleDB) Delete(key []byte) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	if err := r.db.Delete(key, r.writeOpts); err != nil {
		return err
	}
	r.markDirty()
	return nil
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
	if err := batch.Commit(r.writeOpts); err != nil {
		return err
	}
	r.markDirty()
	return nil
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
	if r.opts != nil && r.opts.HNSWConfig.Dim > 0 && len(vector) != r.opts.HNSWConfig.Dim {
		return fmt.Errorf("vector dimension %d does not match index dimension %d", len(vector), r.opts.HNSWConfig.Dim)
	}

	asyncIndex := r.opts != nil && r.opts.HNSWConfig.AsyncIndexingEnabled && r.indexerCh != nil && !r.ingestMode

	// 2. If HNSW add is successful, commit the vector and WAL entry to PebbleDB.
	val, err := encodeVectorWithMeta(vector, metadata, r.shouldCompressVectors())
	if err != nil {
		return fmt.Errorf("failed to encode vector with meta: %w", err)
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	// Add the vector data to the batch.
	if err := batch.Set(vectorKey(id), val, nil); err != nil {
		return fmt.Errorf("failed to set vector in batch: %w", err)
	}

	// If the WAL is enabled, also add a WAL entry to the batch.
	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, val, nil); err != nil {
			return fmt.Errorf("failed to set WAL in batch: %w", err)
		}
	}
	if r.ingestMode {
		if err := r.markIngestDirtyBatch(batch); err != nil {
			return err
		}
	}

	// Commit the batch atomically.
	if err := batch.Commit(r.writeOpts); err != nil {
		return fmt.Errorf("failed to commit batch for StoreVector: %w", err)
	}
	r.markDirty()

	if r.ingestMode {
		return nil
	}

	if asyncIndex {
		atomic.AddUint64(&r.indexPending, 1)
		select {
		case r.indexerCh <- indexOp{opType: indexOpAdd, id: id, vector: vector}:
		default:
			atomic.AddUint64(&r.indexPending, ^uint64(0))
			// Queue is full; fall back to synchronous index update.
			if err := r.hnsw.Add(id, vector); err != nil {
				return fmt.Errorf("failed to add vector to HNSW index: %w", err)
			}
			r.hotAdd(id, vector)
		}
	} else {
		// Synchronous index update.
		if err := r.hnsw.Add(id, vector); err != nil {
			return fmt.Errorf("failed to add vector to HNSW index: %w", err)
		}
		r.hotAdd(id, vector)
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
	if !r.ingestMode && r.shouldBulkLoad(len(vectors)) {
		return r.bulkLoadVectors(vectors)
	}

	added := make([]string, 0, len(vectors))
	batch := r.db.NewBatch()
	defer batch.Close()
	asyncIndex := r.opts != nil && r.opts.HNSWConfig.AsyncIndexingEnabled && r.indexerCh != nil && !r.ingestMode
	walBatchEnabled := r.opts != nil && r.opts.HNSWConfig.WALEnabled && r.opts.HNSWConfig.WALBatchEnabled && len(vectors) > 1
	var walBatchIDs []string
	var walBatchVals [][]byte
	if walBatchEnabled {
		walBatchIDs = make([]string, 0, len(vectors))
		walBatchVals = make([][]byte, 0, len(vectors))
	}

	baseTS := time.Now().UnixNano()
	for i, v := range vectors {
		if v.ID == "" || len(v.Vector) == 0 {
			return errors.New("vector id or data missing")
		}
		if r.opts != nil && r.opts.HNSWConfig.Dim > 0 && len(v.Vector) != r.opts.HNSWConfig.Dim {
			return fmt.Errorf("vector dimension %d does not match index dimension %d", len(v.Vector), r.opts.HNSWConfig.Dim)
		}

		added = append(added, v.ID)

		val, err := encodeVectorWithMeta(v.Vector, v.Metadata, r.shouldCompressVectors())
		if err != nil {
			return fmt.Errorf("failed to encode vector with meta: %w", err)
		}

		if err := batch.Set(vectorKey(v.ID), val, nil); err != nil {
			return fmt.Errorf("failed to set vector in batch: %w", err)
		}

		if r.opts.HNSWConfig.WALEnabled && !walBatchEnabled {
			walKey := []byte(fmt.Sprintf("%s%d_%s", hnswWALPrefix, baseTS+int64(i), v.ID))
			if err := batch.Set(walKey, val, nil); err != nil {
				return fmt.Errorf("failed to set WAL in batch: %w", err)
			}
		}
		if walBatchEnabled {
			walBatchIDs = append(walBatchIDs, v.ID)
			walBatchVals = append(walBatchVals, val)
		}
	}
	if walBatchEnabled {
		walBatchBuf := encodeWALBatchEncoded(walBatchIDs, walBatchVals)
		if walBatchBuf == nil {
			return fmt.Errorf("failed to encode WAL batch")
		}
		walKey := []byte(fmt.Sprintf("%s%s%d", hnswWALPrefix, "batch_", baseTS))
		if err := batch.Set(walKey, walBatchBuf, nil); err != nil {
			return fmt.Errorf("failed to set WAL batch: %w", err)
		}
	}

	if r.ingestMode {
		if err := r.markIngestDirtyBatch(batch); err != nil {
			return err
		}
	}
	if err := batch.Commit(r.writeOpts); err != nil {
		return fmt.Errorf("failed to commit batch for StoreVectorBatch: %w", err)
	}
	r.markDirty()

	if r.ingestMode {
		return nil
	}

	if asyncIndex {
		for _, v := range vectors {
			atomic.AddUint64(&r.indexPending, 1)
			select {
			case r.indexerCh <- indexOp{opType: indexOpAdd, id: v.ID, vector: v.Vector}:
			default:
				atomic.AddUint64(&r.indexPending, ^uint64(0))
				if err := r.hnsw.Add(v.ID, v.Vector); err != nil {
					return fmt.Errorf("failed to add vector to HNSW index: %w", err)
				}
				r.hotAdd(v.ID, v.Vector)
			}
		}
	} else {
		for _, v := range vectors {
			if err := r.hnsw.Add(v.ID, v.Vector); err != nil {
				return fmt.Errorf("failed to add vector to HNSW index: %w", err)
			}
			r.hotAdd(v.ID, v.Vector)
		}
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
	if !r.ingestMode {
		if err := r.hnsw.Delete(id); err != nil {
			return fmt.Errorf("failed to delete vector from HNSW index: %w", err)
		}
		r.hotDelete(id)
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

	if r.ingestMode {
		if err := r.markIngestDirtyBatch(batch); err != nil {
			return err
		}
	}
	if err := batch.Commit(r.writeOpts); err != nil {
		return fmt.Errorf("failed to commit batch for DeleteVector: %w", err)
	}
	r.markDirty()

	if r.ingestMode {
		return nil
	}
	r.recordHNSWWrite(1)
	return nil
}

func (r *PebbleDB) hotAdd(id string, vector []float32) {
	if r.hnswHot == nil {
		return
	}
	r.hotMu.Lock()
	defer r.hotMu.Unlock()

	if _, exists := r.hotSet[id]; exists {
		return
	}

	if err := r.hnswHot.Add(id, vector); err != nil {
		return
	}
	r.hotSet[id] = struct{}{}
	r.hotQueue = append(r.hotQueue, id)

	maxSize := r.opts.HNSWConfig.HotIndexMaxSize
	if maxSize <= 0 {
		maxSize = 100000
	}
	for len(r.hotQueue) > maxSize {
		oldest := r.hotQueue[0]
		r.hotQueue = r.hotQueue[1:]
		delete(r.hotSet, oldest)
		_ = r.hnswHot.Delete(oldest)
	}
}

func (r *PebbleDB) hotDelete(id string) {
	if r.hnswHot == nil {
		return
	}
	r.hotMu.Lock()
	defer r.hotMu.Unlock()
	if _, exists := r.hotSet[id]; !exists {
		return
	}
	delete(r.hotSet, id)
	_ = r.hnswHot.Delete(id)
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

		if err := r.hnsw.Add(v.ID, v.Vector); err != nil {
			for _, id := range added {
				_ = r.hnsw.Delete(id)
			}
			return fmt.Errorf("failed to add vector to HNSW index: %w", err)
		}
		added = append(added, v.ID)

		val, err := encodeVectorWithMeta(v.Vector, v.Metadata, r.shouldCompressVectors())
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
	r.markDirty()

	r.markHNSWSnapshotSaved(ts)
	return nil
}

const vectorCompressionFlag = uint32(1 << 31)

func (r *PebbleDB) shouldCompressVectors() bool {
	if r.opts == nil {
		return false
	}
	cfg := r.opts.HNSWConfig
	if !cfg.VectorCompressionEnabled {
		return false
	}
	if cfg.DistanceMetric != "cosine" || !cfg.NormalizeVectors {
		return false
	}
	return true
}

func encodeVectorWithMeta(vector []float32, metadata []byte, compress bool) ([]byte, error) {
	vecLen := len(vector)
	if vecLen < 0 {
		return nil, fmt.Errorf("invalid vector length")
	}
	if !compress {
		totalLen := 4 + vecLen*4 + len(metadata)
		buf := make([]byte, totalLen)
		binary.LittleEndian.PutUint32(buf[:4], uint32(vecLen))
		offset := 4
		for _, v := range vector {
			binary.LittleEndian.PutUint32(buf[offset:offset+4], math.Float32bits(v))
			offset += 4
		}
		if len(metadata) > 0 {
			copy(buf[offset:], metadata)
		}
		return buf, nil
	}

	if vecLen > int(^vectorCompressionFlag) {
		return nil, fmt.Errorf("vector length too large")
	}

	scale := float32(0)
	for _, v := range vector {
		abs := float32(math.Abs(float64(v)))
		if abs > scale {
			scale = abs
		}
	}
	if scale == 0 {
		scale = 1
	}
	invScale := 127.0 / float64(scale)

	totalLen := 4 + 4 + vecLen + len(metadata)
	buf := make([]byte, totalLen)
	binary.LittleEndian.PutUint32(buf[:4], uint32(vecLen)|vectorCompressionFlag)
	binary.LittleEndian.PutUint32(buf[4:8], math.Float32bits(scale))
	for i, v := range vector {
		q := int(math.Round(float64(v) * invScale))
		if q > 127 {
			q = 127
		} else if q < -127 {
			q = -127
		}
		buf[8+i] = byte(int8(q))
	}
	if len(metadata) > 0 {
		copy(buf[8+vecLen:], metadata)
	}
	return buf, nil
}

func encodeWALBatchEncoded(ids []string, values [][]byte) []byte {
	if len(ids) == 0 || len(ids) != len(values) {
		return nil
	}
	total := 4
	for i, id := range ids {
		total += 4 + len(id) + 4 + len(values[i])
	}
	buf := make([]byte, 0, total)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(len(ids)))
	buf = append(buf, tmp...)
	for i, id := range ids {
		binary.LittleEndian.PutUint32(tmp, uint32(len(id)))
		buf = append(buf, tmp...)
		buf = append(buf, id...)
		val := values[i]
		binary.LittleEndian.PutUint32(tmp, uint32(len(val)))
		buf = append(buf, tmp...)
		buf = append(buf, val...)
	}
	return buf
}
