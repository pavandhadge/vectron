package idxhnsw

import (
	"errors"
	"sync"
)

// MemStore is an in-memory NodeStore for hot indexes.
type MemStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func NewMemStore() *MemStore {
	return &MemStore{
		data: make(map[string][]byte),
	}
}

func (m *MemStore) Put(key, value []byte) error {
	if m == nil {
		return errors.New("memstore is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := make([]byte, len(value))
	copy(buf, value)
	m.data[string(key)] = buf
	return nil
}

func (m *MemStore) Get(key []byte) ([]byte, error) {
	if m == nil {
		return nil, errors.New("memstore is nil")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	val, ok := m.data[string(key)]
	if !ok {
		return nil, errors.New("key not found")
	}
	out := make([]byte, len(val))
	copy(out, val)
	return out, nil
}

func (m *MemStore) Delete(key []byte) error {
	if m == nil {
		return errors.New("memstore is nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, string(key))
	return nil
}
