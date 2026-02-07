//go:build amd64 && cgo

package idxhnsw

/*
#cgo CFLAGS: -O3 -mavx2
#include <immintrin.h>
#include <stdlib.h>

static int dotProductInt8AVX2(const signed char* a, const signed char* b, int n) {
    __m256i sum = _mm256_setzero_si256();
    int i = 0;
    for (; i + 31 < n; i += 32) {
        __m256i va = _mm256_loadu_si256((const __m256i*)(a + i));
        __m256i vb = _mm256_loadu_si256((const __m256i*)(b + i));
        __m256i va16 = _mm256_cvtepi8_epi16(_mm256_extracti128_si256(va, 0));
        __m256i vb16 = _mm256_cvtepi8_epi16(_mm256_extracti128_si256(vb, 0));
        __m256i prod0 = _mm256_madd_epi16(va16, vb16);
        __m256i va16b = _mm256_cvtepi8_epi16(_mm256_extracti128_si256(va, 1));
        __m256i vb16b = _mm256_cvtepi8_epi16(_mm256_extracti128_si256(vb, 1));
        __m256i prod1 = _mm256_madd_epi16(va16b, vb16b);
        sum = _mm256_add_epi32(sum, prod0);
        sum = _mm256_add_epi32(sum, prod1);
    }
    int tmp[8];
    _mm256_storeu_si256((__m256i*)tmp, sum);
    int acc = tmp[0] + tmp[1] + tmp[2] + tmp[3] + tmp[4] + tmp[5] + tmp[6] + tmp[7];
    for (; i < n; i++) {
        acc += (int)a[i] * (int)b[i];
    }
    return acc;
}

static void dotProductInt8BatchAVX2(const signed char* q, const signed char** vecs, int* out, int count, int n) {
    for (int i = 0; i < count; i++) {
        out[i] = dotProductInt8AVX2(q, vecs[i], n);
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

func dotProductInt8BatchSIMD(q []int8, vecs [][]int8, out []int32) bool {
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

	buf := getCPtrBuf(len(vecs))
	if buf.ptr == nil {
		putCPtrBuf(buf)
		return false
	}
	ptrs := unsafe.Slice((**C.schar)(buf.ptr), len(vecs))
	for i := range vecs {
		if len(vecs[i]) != len(q) || len(vecs[i]) == 0 {
			putCPtrBuf(buf)
			return false
		}
		ptrs[i] = (*C.schar)(unsafe.Pointer(&vecs[i][0]))
	}
	C.dotProductInt8BatchAVX2(
		(*C.schar)(unsafe.Pointer(&q[0])),
		(**C.schar)(buf.ptr),
		(*C.int)(unsafe.Pointer(&out[0])),
		C.int(len(vecs)),
		C.int(len(q)),
	)
	putCPtrBuf(buf)
	runtime.KeepAlive(q)
	runtime.KeepAlive(vecs)
	return true
}

type cPtrBuf struct {
	ptr unsafe.Pointer
	cap int
}

var cPtrBufPool = sync.Pool{
	New: func() interface{} {
		return &cPtrBuf{}
	},
}

func getCPtrBuf(n int) *cPtrBuf {
	buf := cPtrBufPool.Get().(*cPtrBuf)
	if buf.cap < n {
		if buf.ptr != nil {
			C.free(buf.ptr)
		}
		size := C.size_t(n) * C.size_t(unsafe.Sizeof(uintptr(0)))
		buf.ptr = C.malloc(size)
		buf.cap = n
	}
	return buf
}

func putCPtrBuf(buf *cPtrBuf) {
	cPtrBufPool.Put(buf)
}
