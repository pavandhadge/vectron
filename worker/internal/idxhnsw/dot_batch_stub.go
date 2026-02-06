//go:build !cgo || !amd64

package idxhnsw

func dotProductInt8BatchSIMD(_ []int8, _ [][]int8, _ []int32) bool {
	return false
}
