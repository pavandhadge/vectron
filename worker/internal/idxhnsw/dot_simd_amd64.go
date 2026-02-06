//go:build amd64 && !avx512

package idxhnsw

import "golang.org/x/sys/cpu"

// dotProductSIMD dispatches to AVX2 when available, else falls back.
func dotProductSIMD(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	if cpu.X86.HasAVX2 {
		return dotProductAVX2(&a[0], &b[0], len(a))
	}
	return dotProduct(a, b)
}

// dotProductAVX2 computes dot product using AVX2.
func dotProductAVX2(a, b *float32, n int) float32
