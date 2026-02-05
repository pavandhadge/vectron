//go:build amd64

package idxhnsw

import "golang.org/x/sys/cpu"

func dotProductInt8SIMD(a, b []int8) int32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	if cpu.X86.HasAVX2 {
		n := len(a)
		nVec := n &^ 15
		var sum int32
		if nVec > 0 {
			sum = dotProductInt8AVX2(&a[0], &b[0], nVec)
		}
		if nVec < n {
			sum += dotProductInt8(a[nVec:], b[nVec:])
		}
		return sum
	}
	return dotProductInt8(a, b)
}

// dotProductInt8AVX2 computes dot product of int8 vectors using AVX2.
func dotProductInt8AVX2(a, b *int8, n int) int32
