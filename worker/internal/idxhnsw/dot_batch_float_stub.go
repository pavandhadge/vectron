//go:build !cgo || !amd64

package idxhnsw

func dotProductFloatBatchSIMD(_ []float32, _ [][]float32, _ []float32) bool {
	return false
}
