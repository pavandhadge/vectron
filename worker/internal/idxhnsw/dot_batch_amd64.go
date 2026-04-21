//go:build amd64 && !cgo

package idxhnsw

func dotProductInt8BatchSIMD(q []int8, vecs [][]int8, out []int32) bool {
	return false
}

func dotProductFloatBatchSIMD(q []float32, vecs [][]float32, out []float32) bool {
	return false
}