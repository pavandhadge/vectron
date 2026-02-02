// This file implements the read operations for the PebbleDB storage engine.
// It provides methods for getting single keys, checking for existence, and iterating
// over key-value pairs. It also includes vector-specific read operations.

package storage

import (
	"encoding/binary"
	"errors"
	"math"

	"github.com/cockroachdb/pebble"
)

// Get retrieves the value for a given key from the database.
// It returns nil if the key is not found.
func (r *PebbleDB) Get(key []byte) ([]byte, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}
	value, closer, err := r.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil // Treat not found as nil, not an error.
		}
		return nil, err
	}
	defer closer.Close()
	// Return a copy of the value to avoid issues with the underlying buffer being reused.
	data := make([]byte, len(value))
	copy(data, value)
	return data, nil
}

// Exists checks if a key exists in the database.
func (r *PebbleDB) Exists(key []byte) (bool, error) {
	if r.db == nil {
		return false, errors.New("db not initialized")
	}
	_, closer, err := r.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return false, nil // Not found is not an error here.
		}
		return false, err
	}
	if closer != nil {
		closer.Close()
	}
	return true, nil
}

// ExistsBatch checks if multiple keys exist in the database in a single snapshot.
// Returns a map of key string to existence boolean.
func (r *PebbleDB) ExistsBatch(keys [][]byte) (map[string]bool, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}

	results := make(map[string]bool, len(keys))

	// Use a snapshot for consistent reads
	snap := r.db.NewSnapshot()
	defer snap.Close()

	for _, key := range keys {
		_, closer, err := snap.Get(key)
		if err != nil {
			if errors.Is(err, pebble.ErrNotFound) {
				results[string(key)] = false
				continue
			}
			return nil, err
		}
		if closer != nil {
			closer.Close()
		}
		results[string(key)] = true
	}

	return results, nil
}

// NewIterator creates a new iterator over a given key prefix.
func (r *PebbleDB) NewIterator(prefix []byte) (Iterator, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}

	iterOpts := &pebble.IterOptions{}
	if len(prefix) > 0 {
		iterOpts.LowerBound = prefix
		iterOpts.UpperBound = pebble.DefaultComparer.Successor(nil, prefix)
	}

	iter := r.db.NewIter(iterOpts)
	return &pebbleIterator{iter: iter}, nil
}

// Scan retrieves up to `limit` key-value pairs for a given prefix.
func (r *PebbleDB) Scan(prefix []byte, limit int) ([]KeyValuePair, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}
	iter, err := r.NewIterator(prefix)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	results := make([]KeyValuePair, 0, limit)
	for iter.Next() {
		if limit > 0 && len(results) >= limit {
			break
		}
		// The iterator implementation returns copies, so they are safe to use.
		results = append(results, KeyValuePair{Key: iter.Key(), Value: iter.Value()})
	}
	return results, iter.Error()
}

// Iterate calls the provided function for each key-value pair in the given prefix range.
// Iteration stops if the function returns false.
func (r *PebbleDB) Iterate(prefix []byte, fn func(key, value []byte) bool) error {
	if r.db == nil {
		return errors.New("db not initialized")
	}
	iter, err := r.NewIterator(prefix)
	if err != nil {
		return err
	}
	defer iter.Close()

	for iter.Next() {
		if !fn(iter.Key(), iter.Value()) {
			break
		}
	}
	return iter.Error()
}

// GetVector retrieves a vector and its metadata by its string ID.
// It correctly handles soft-deletions, returning nil if the vector is marked as deleted.
func (r *PebbleDB) GetVector(id string) ([]float32, []byte, error) {
	val, err := r.Get(vectorKey(id))
	if err != nil {
		return nil, nil, err
	}
	if val == nil {
		return nil, nil, nil // Not found.
	}

	return decodeVectorWithMeta(val)
}

// IsDeleted checks if a vector is considered deleted (either soft or hard deleted).
func (r *PebbleDB) IsDeleted(id string) (bool, error) {
	val, err := r.Get(vectorKey(id))
	if err != nil {
		return false, err
	}
	if val == nil {
		return true, nil // Hard deleted (key does not exist).
	}

	vec, _, _ := decodeVectorWithMeta(val)
	return vec == nil, nil // Soft deleted if vector part is nil.
}

// Size is not yet implemented for PebbleDB.
func (r *PebbleDB) Size() (int64, error) {
	// A proper implementation would need to iterate over disk usage stats.
	return 0, errors.New("size calculation not implemented")
}

// EntryCount counts the number of entries under a given prefix.
func (r *PebbleDB) EntryCount(prefix []byte) (int64, error) {
	if r.db == nil {
		return 0, errors.New("db not initialized")
	}
	var count int64
	err := r.Iterate(prefix, func(key, value []byte) bool {
		count++
		return true
	})
	return count, err
}

// decodeVectorWithMeta deserializes a byte slice into a vector and its metadata.
func decodeVectorWithMeta(data []byte) ([]float32, []byte, error) {
	if len(data) == 0 {
		// This indicates a soft-deleted vector.
		return nil, nil, nil
	}
	if len(data) < 4 {
		return nil, nil, errors.New("data too short to contain vector length")
	}

	vecLen32 := binary.LittleEndian.Uint32(data[:4])
	vecLen := int(vecLen32)
	expectedLen := 4 + vecLen*4

	if len(data) < expectedLen {
		return nil, nil, errors.New("data too short for declared vector length")
	}

	vector := make([]float32, vecLen)
	for i := 0; i < vecLen; i++ {
		start := 4 + i*4
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[start : start+4]))
	}

	var metadata []byte
	if len(data) > expectedLen {
		metadata = data[expectedLen:]
	}

	return vector, metadata, nil
}

// pebbleIterator implements the Iterator interface for PebbleDB.
type pebbleIterator struct {
	iter *pebble.Iterator
}

// Valid returns true if the iterator is positioned at a valid key-value pair.
func (pi *pebbleIterator) Valid() bool {
	if pi.iter == nil {
		return false
	}
	return pi.iter.Valid()
}

// Seek positions the iterator at the first key greater than or equal to the given key.
func (pi *pebbleIterator) Seek(key []byte) {
	if pi.iter != nil {
		pi.iter.SeekGE(key)
	}
}

// Next moves the iterator to the next key-value pair.
func (pi *pebbleIterator) Next() bool {
	if pi.iter == nil {
		return false
	}
	return pi.iter.Next()
}

// Key returns a copy of the current key.
func (pi *pebbleIterator) Key() []byte {
	if pi.iter == nil {
		return nil
	}
	// A copy is returned to ensure the slice is safe after the iterator is advanced.
	return append([]byte(nil), pi.iter.Key()...)
}

// Value returns a copy of the current value.
func (pi *pebbleIterator) Value() []byte {
	if pi.iter == nil {
		return nil
	}
	// A copy is returned to ensure the slice is safe after the iterator is advanced.
	return append([]byte(nil), pi.iter.Value()...)
}

// Close closes the iterator and releases its resources.
func (pi *pebbleIterator) Close() error {
	if pi.iter == nil {
		return nil
	}
	return pi.iter.Close()
}

// Error returns any accumulated error.
func (pi *pebbleIterator) Error() error {
	if pi.iter == nil {
		return nil
	}
	return pi.iter.Error()
}
