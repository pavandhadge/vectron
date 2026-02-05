//go:build !amd64

package idxhnsw

// dotProductSIMD dispatches to a SIMD implementation when available.
func dotProductSIMD(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	return dotProduct(a, b)
}
