//go:build !cgo || !amd64

package idxhnsw

// quantizeVectorSIMD returns false when SIMD is unavailable.
func quantizeVectorSIMD(_ []int8, _ []float32) bool {
	return false
}
