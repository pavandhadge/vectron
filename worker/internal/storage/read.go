package storage

import (
	"errors"

	"github.com/cockroachdb/pebble"
)

// Get retrieves the value for a given key.
func (r *PebbleDB) Get(key []byte) ([]byte, error) {
	if r.db == nil {
		return nil, errors.New("db not initialized")
	}
	value, closer, err := r.db.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil // Not found
		}
		return nil, err
	}
	if closer != nil {
		defer closer.Close()
	}
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
	if err == nil {
		if closer != nil {
			closer.Close()
		}
		return true, nil
	}
	if errors.Is(err, pebble.ErrNotFound) {
		return false, nil
	}
	return false, err
}

// NewIterator creates a new iterator over a given prefix.
func (r *PebbleDB) NewIterator(prefix []byte) (Iterator, error) {
	if r.db == nil {
		return &pebbleIterator{iter: nil}, nil
	}

	var iterOpts pebble.IterOptions
	if len(prefix) > 0 {
		iterOpts.LowerBound = prefix

		// Calculate the upper bound for the prefix.
		// This creates a key that is just past the prefix, ensuring we only iterate within it.
		// E.g., for prefix "foo", upper bound would be "fop" if "foo" is the last byte.
		// More robustly: find the first byte from the right that is not 0xFF.
		// Increment that byte and use the prefix up to that point as the UpperBound.
		// If all bytes are 0xFF, then there is no upper bound.
		limit := make([]byte, len(prefix))
		copy(limit, prefix)
		i := len(limit) - 1
		for i >= 0 && limit[i] == 0xFF {
			i--
		}
		if i >= 0 {
			limit[i]++
			iterOpts.UpperBound = limit[:i+1]
		}
	}

	iter, err := r.db.NewIter(&iterOpts)
	if err != nil {
		return nil, err
	}
	// Even with LowerBound set, calling SeekGE ensures the iterator is positioned correctly.
	iter.SeekGE(prefix)

	return &pebbleIterator{iter: iter}, nil
}

// Scan retrieves a limited number of key-value pairs for a given prefix.
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
		key := iter.Key()
		value := iter.Value()

		results = append(results, KeyValuePair{Key: key, Value: value})
	}
	return results, iter.Error()
}

// Iterate iterates over key-value pairs for a given prefix and calls the provided function.
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

// GetVector retrieves a vector and its metadata by ID.
func (r *PebbleDB) GetVector(id string) ([]float32, []byte, error) {
	if r.db == nil {
		return nil, nil, errors.New("db not initialized")
	}
	val, err := r.Get([]byte(id))
	if err != nil || val == nil {
		return nil, nil, err
	}
	return decodeVectorWithMeta(val)
}

// Size returns the size of the database on disk.
func (r *PebbleDB) Size() (int64, error) {
	return 0, errors.New("size calculation not implemented")
}

// EntryCount returns the number of entries for a given prefix.
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

// decodeVectorWithMeta is a placeholder for vector and metadata deserialization.
func decodeVectorWithMeta(data []byte) ([]float32, []byte, error) {
	// Simplified (not production ready)
	return nil, data, nil // Stub; properly deserialize
}

// pebbleIterator implements the Iterator interface for PebbleDB.
type pebbleIterator struct {
	iter *pebble.Iterator
}

func (pi *pebbleIterator) Valid() bool {
	if pi.iter == nil {
		return false
	}
	return pi.iter.Valid()
}

func (pi *pebbleIterator) Seek(key []byte) {
	if pi.iter == nil {
		return
	}
	pi.iter.SeekGE(key)
}

func (pi *pebbleIterator) Next() bool {
	if pi.iter == nil {
		return false
	}
	return pi.iter.Next()
}

func (pi *pebbleIterator) Key() []byte {
	if pi.iter == nil {
		return nil
	}
	// Clone to ensure the slice is safe after Next/Close
	key := pi.iter.Key()
	keyCopy := make([]byte, len(key))
	copy(keyCopy, key)
	return keyCopy
}

func (pi *pebbleIterator) Value() []byte {
	if pi.iter == nil {
		return nil
	}
	// Clone to ensure the slice is safe after Next/Close
	value := pi.iter.Value()
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	return valueCopy
}

func (pi *pebbleIterator) Close() error {
	if pi.iter == nil {
		return nil
	}
	return pi.iter.Close()
}

func (pi *pebbleIterator) Error() error {
	if pi.iter == nil {
		return nil
	}
	return pi.iter.Error()
}
