//go:build amd64 && cgo

package idxhnsw

/*
#cgo CFLAGS: -O3 -mavx2
#include <immintrin.h>

static float dotProductAVX2(const float* a, const float* b, int n) {
    __m256 sum = _mm256_setzero_ps();
    int i = 0;
    for (; i + 7 < n; i += 8) {
        __m256 va = _mm256_loadu_ps(a + i);
        __m256 vb = _mm256_loadu_ps(b + i);
        sum = _mm256_add_ps(sum, _mm256_mul_ps(va, vb));
    }
    float tmp[8];
    _mm256_storeu_ps(tmp, sum);
    float acc = tmp[0] + tmp[1] + tmp[2] + tmp[3] + tmp[4] + tmp[5] + tmp[6] + tmp[7];
    for (; i < n; i++) {
        acc += a[i] * b[i];
    }
    return acc;
}

static void dotProductBatchAVX2(const float* q, const float** vecs, float* out, int count, int n) {
    for (int i = 0; i < count; i++) {
        out[i] = dotProductAVX2(q, vecs[i], n);
    }
}

*/
import "C"

import (
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/cpu"
)

var floatPtrPool = sync.Pool{
	New: func() interface{} {
		return make([]*C.float, 0, 256)
	},
}

func dotProductFloatBatchSIMD(q []float32, vecs [][]float32, out []float32) bool {
	if !dotCgoBatchEnabled {
		return false
	}
	if len(q) == 0 || len(vecs) == 0 {
		return false
	}
	if len(vecs) != len(out) {
		return false
	}
	if dotCgoMinDim > 0 && len(q) < dotCgoMinDim {
		return false
	}
	if len(vecs) < dotCgoBatchMin {
		return false
	}
	if !cpu.X86.HasAVX2 {
		return false
	}

	ptrs := floatPtrPool.Get().([]*C.float)
	if cap(ptrs) < len(vecs) {
		ptrs = make([]*C.float, len(vecs))
	} else {
		ptrs = ptrs[:len(vecs)]
	}
	for i := range vecs {
		if len(vecs[i]) != len(q) || len(vecs[i]) == 0 {
			floatPtrPool.Put(ptrs[:0])
			return false
		}
		ptrs[i] = (*C.float)(unsafe.Pointer(&vecs[i][0]))
	}
	C.dotProductBatchAVX2(
		(*C.float)(unsafe.Pointer(&q[0])),
		(**C.float)(unsafe.Pointer(&ptrs[0])),
		(*C.float)(unsafe.Pointer(&out[0])),
		C.int(len(vecs)),
		C.int(len(q)),
	)
	floatPtrPool.Put(ptrs[:0])
	runtime.KeepAlive(q)
	runtime.KeepAlive(vecs)
	return true
}
