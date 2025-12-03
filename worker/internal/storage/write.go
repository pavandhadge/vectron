package storage

import (
	"errors"
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
	val := encodeVectorWithMeta(vector, metadata)
	return r.Put([]byte(id), val)
}

// DeleteVector deletes a vector by its ID.
func (r *PebbleDB) DeleteVector(id string) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	return r.Delete([]byte(id))
}

// encodeVectorWithMeta is a placeholder for vector and metadata serialization.
func encodeVectorWithMeta(vector []float32, metadata []byte) []byte {
	// Simplified (not production ready)
	return metadata // Stub; properly serialize vector with meta
}
