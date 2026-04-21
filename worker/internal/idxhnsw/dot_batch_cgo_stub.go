//go:build amd64 && cgo

package idxhnsw

func dotProductInt8BatchSIMD(q []int8, vecs [][]int8, out []int32) bool {
	if !dotCgoBatchEnabled {
		return false
	}
	if len(q) == 0 || len(vecs) == 0 || len(out) < len(vecs) {
		return false
	}
	if dotCgoMinDim > 0 && len(q) < dotCgoMinDim {
		return false
	}
	if len(vecs) < dotCgoBatchMin {
		return false
	}
	// Default fallback - use slow scalar path
	for i, v := range vecs {
		var sum int32
		for j := range v {
			sum += int32(q[j]) * int32(v[j])
		}
		out[i] = sum
	}
	return true
}

func dotProductFloatBatchSIMD(q []float32, vecs [][]float32, out []float32) bool {
	if !dotCgoBatchEnabled {
		return false
	}
	if len(q) == 0 || len(vecs) == 0 || len(out) < len(vecs) {
		return false
	}
	if dotCgoMinDim > 0 && len(q) < dotCgoMinDim {
		return false
	}
	if len(vecs) < dotCgoBatchMin {
		return false
	}
	// Default fallback - use slow scalar path
	for i, v := range vecs {
		var sum float32
		for j := range v {
			sum += q[j] * v[j]
		}
		out[i] = sum
	}
	return true
}