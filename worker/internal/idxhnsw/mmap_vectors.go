//go:build !windows

package idxhnsw

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

const (
	defaultMmapInitialSize = 64 << 20 // 64MB
)

type vectorMmap struct {
	mu   sync.Mutex
	file *os.File
	data []byte
	size int64
	used int64
}

func newVectorMmap(path string, initialSize int64) (*vectorMmap, error) {
	if initialSize <= 0 {
		initialSize = defaultMmapInitialSize
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0640)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(initialSize); err != nil {
		_ = f.Close()
		return nil, err
	}
	data, err := syscall.Mmap(int(f.Fd()), 0, int(initialSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return &vectorMmap{
		file: f,
		data: data,
		size: initialSize,
	}, nil
}

func (m *vectorMmap) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data != nil {
		_ = syscall.Munmap(m.data)
		m.data = nil
	}
	if m.file != nil {
		err := m.file.Close()
		m.file = nil
		return err
	}
	return nil
}

func (m *vectorMmap) ensureCapacity(extra int64) error {
	needed := m.used + extra
	if needed <= m.size {
		return nil
	}
	newSize := m.size * 2
	if newSize < needed {
		newSize = needed
	}
	if err := syscall.Munmap(m.data); err != nil {
		return err
	}
	if err := m.file.Truncate(newSize); err != nil {
		return err
	}
	data, err := syscall.Mmap(int(m.file.Fd()), 0, int(newSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return err
	}
	m.data = data
	m.size = newSize
	return nil
}

func (m *vectorMmap) allocFloat32(n int) ([]float32, error) {
	if n <= 0 {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bytes := int64(n) * 4
	if err := m.ensureCapacity(bytes); err != nil {
		return nil, err
	}
	offset := m.used
	m.used += bytes
	buf := m.data[offset : offset+bytes]
	ptr := unsafe.Pointer(&buf[0])
	return unsafe.Slice((*float32)(ptr), n), nil
}

func (h *HNSW) enableMmapVectors(path string, initialSize int64) error {
	if h.mmapStore != nil {
		return nil
	}
	store, err := newVectorMmap(path, initialSize)
	if err != nil {
		return err
	}
	for _, node := range h.nodes {
		if node == nil || node.Vec == nil {
			continue
		}
		dst, err := store.allocFloat32(len(node.Vec))
		if err != nil {
			_ = store.Close()
			return err
		}
		copy(dst, node.Vec)
		node.Vec = dst
	}
	h.mmapStore = store
	return nil
}

func (h *HNSW) EnableMmapVectors(path string, initialSize int64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if path == "" {
		return fmt.Errorf("mmap path is empty")
	}
	return h.enableMmapVectors(path, initialSize)
}

func (h *HNSW) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mmapStore != nil {
		_ = h.mmapStore.Close()
		h.mmapStore = nil
	}
}
