// pools.go - Memory pools for reducing GC pressure
//
// This file provides sync.Pool instances for frequently allocated objects
// in the worker's hot paths. Using pools reduces garbage collection pressure
// and improves throughput under high load.

package internal

import (
	"sync"

	"github.com/pavandhadge/vectron/shared/proto/worker"
)

// Pool for search response objects
var searchResponsePool = sync.Pool{
	New: func() interface{} {
		return &worker.SearchResponse{
			Ids:    make([]string, 0, 10),
			Scores: make([]float32, 0, 10),
		}
	},
}

// Pool for string slices (used in search results)
var stringSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]string, 0, 64)
		return &s
	},
}

// Pool for float32 slices (used in scores)
var float32SlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]float32, 0, 64)
		return &s
	},
}

// Pool for byte slices (used in metadata)
var byteSlicePool = sync.Pool{
	New: func() interface{} {
		s := make([]byte, 0, 256)
		return &s
	},
}

// GetSearchResponse gets a SearchResponse from the pool
func GetSearchResponse() *worker.SearchResponse {
	resp := searchResponsePool.Get().(*worker.SearchResponse)
	resp.Ids = resp.Ids[:0]
	resp.Scores = resp.Scores[:0]
	return resp
}

// PutSearchResponse returns a SearchResponse to the pool
func PutSearchResponse(resp *worker.SearchResponse) {
	if resp == nil {
		return
	}
	// Clear but keep capacity
	resp.Ids = resp.Ids[:0]
	resp.Scores = resp.Scores[:0]
	searchResponsePool.Put(resp)
}

// GetStringSlice gets a string slice from the pool
func GetStringSlice(n int) *[]string {
	s := stringSlicePool.Get().(*[]string)
	if cap(*s) < n {
		*s = make([]string, 0, n)
	}
	*s = (*s)[:0]
	return s
}

// PutStringSlice returns a string slice to the pool
func PutStringSlice(s *[]string) {
	if s == nil {
		return
	}
	*s = (*s)[:0]
	stringSlicePool.Put(s)
}

// GetFloat32Slice gets a float32 slice from the pool
func GetFloat32Slice(n int) *[]float32 {
	s := float32SlicePool.Get().(*[]float32)
	if cap(*s) < n {
		*s = make([]float32, 0, n)
	}
	*s = (*s)[:0]
	return s
}

// PutFloat32Slice returns a float32 slice to the pool
func PutFloat32Slice(s *[]float32) {
	if s == nil {
		return
	}
	*s = (*s)[:0]
	float32SlicePool.Put(s)
}

// GetByteSlice gets a byte slice from the pool
func GetByteSlice(n int) *[]byte {
	s := byteSlicePool.Get().(*[]byte)
	if cap(*s) < n {
		*s = make([]byte, 0, n)
	}
	*s = (*s)[:0]
	return s
}

// PutByteSlice returns a byte slice to the pool
func PutByteSlice(s *[]byte) {
	if s == nil {
		return
	}
	*s = (*s)[:0]
	byteSlicePool.Put(s)
}
