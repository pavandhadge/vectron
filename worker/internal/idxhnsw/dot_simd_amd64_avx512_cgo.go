//go:build amd64 && cgo && avx512

package idxhnsw

/*
#cgo CFLAGS: -O3 -mavx512f -mavx2
#include <immintrin.h>

static float dotProductAVX512(const float* a, const float* b, int n) {
    __m512 sum = _mm512_setzero_ps();
    int i = 0;
    for (; i + 15 < n; i += 16) {
        __m512 va = _mm512_loadu_ps(a + i);
        __m512 vb = _mm512_loadu_ps(b + i);
        sum = _mm512_add_ps(sum, _mm512_mul_ps(va, vb));
    }
    alignas(64) float tmp[16];
    _mm512_storeu_ps(tmp, sum);
    float acc = 0.0f;
    for (int j = 0; j < 16; j++) {
        acc += tmp[j];
    }
    for (; i < n; i++) {
        acc += a[i] * b[i];
    }
    return acc;
}

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
*/
import "C"

import "golang.org/x/sys/cpu"

// dotProductSIMD dispatches to AVX-512 (cgo) when available, else AVX2 or scalar.
func dotProductSIMD(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	if cpu.X86.HasAVX512F {
		return float32(C.dotProductAVX512((*C.float)(&a[0]), (*C.float)(&b[0]), C.int(len(a))))
	}
	if cpu.X86.HasAVX2 {
		return float32(C.dotProductAVX2((*C.float)(&a[0]), (*C.float)(&b[0]), C.int(len(a))))
	}
	return dotProduct(a, b)
}
