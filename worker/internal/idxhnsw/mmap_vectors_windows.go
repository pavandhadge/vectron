//go:build windows

package idxhnsw

import "fmt"

type vectorMmap struct{}

func newVectorMmap(path string, initialSize int64) (*vectorMmap, error) {
	return nil, fmt.Errorf("mmap vectors not supported on windows")
}

func (m *vectorMmap) Close() error {
	return nil
}

func (m *vectorMmap) allocFloat32(n int) ([]float32, error) {
	return nil, fmt.Errorf("mmap vectors not supported on windows")
}

func (h *HNSW) EnableMmapVectors(path string, initialSize int64) error {
	return fmt.Errorf("mmap vectors not supported on windows")
}

func (h *HNSW) Close() {}
