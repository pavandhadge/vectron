//go:build !amd64

package idxhnsw

func dotProductInt8SIMD(a, b []int8) int32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	return dotProductInt8(a, b)
}
