//go:build amd64 && cgo && avx512

package idxhnsw

/*
#cgo CFLAGS: -O3 -mavx2
#include <immintrin.h>

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
*/
import "C"

import "golang.org/x/sys/cpu"

func dotProductInt8SIMD(a, b []int8) int32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	if cpu.X86.HasAVX2 {
		return int32(C.dotProductInt8AVX2((*C.schar)(&a[0]), (*C.schar)(&b[0]), C.int(len(a))))
	}
	return dotProductInt8(a, b)
}
