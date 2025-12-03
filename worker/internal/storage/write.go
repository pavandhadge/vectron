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
	val, err := encodeVectorWithMeta(vector, metadata)
	if err != nil {
		return err
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	if err := batch.Set([]byte(id), val, nil); err != nil {
		return err
	}

	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, val, nil); err != nil {
			return err
		}
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		return err
	}
	return r.hnsw.Add(id, vector)
}

// DeleteVector deletes a vector by its ID.

func (r *PebbleDB) DeleteVector(id string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}

	batch := r.db.NewBatch()
	defer batch.Close()

	if err := batch.Delete([]byte(id), nil); err != nil {
		return err
	}

	if r.opts.HNSWConfig.WALEnabled {
		walKey := []byte(fmt.Sprintf("%s%d_%s_delete", hnswWALPrefix, time.Now().UnixNano(), id))
		if err := batch.Set(walKey, nil, nil); err != nil {
			return err
		}
	}

	if err := batch.Commit(r.writeOpts); err != nil {
		return err
	}
	return r.hnsw.Delete(id)

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
