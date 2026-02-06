//go:build amd64 && cgo

package idxhnsw

/*
#cgo CFLAGS: -O3 -mavx2
#include <immintrin.h>

static void quantizeAVX2(const float* in, signed char* out, int n) {
    const __m256 one = _mm256_set1_ps(1.0f);
    const __m256 negOne = _mm256_set1_ps(-1.0f);
    const __m256 scale = _mm256_set1_ps(127.0f);
    int i = 0;
    for (; i + 7 < n; i += 8) {
        __m256 v = _mm256_loadu_ps(in + i);
        v = _mm256_max_ps(v, negOne);
        v = _mm256_min_ps(v, one);
        v = _mm256_mul_ps(v, scale);

        __m256i vi = _mm256_cvtps_epi32(v); // round to nearest
        __m128i lo = _mm256_castsi256_si128(vi);
        __m128i hi = _mm256_extracti128_si256(vi, 1);
        __m128i packed16 = _mm_packs_epi32(lo, hi);
        __m128i packed8 = _mm_packs_epi16(packed16, packed16);
        _mm_storel_epi64((__m128i*)(out + i), packed8);
    }
    for (; i < n; i++) {
        float v = in[i];
        if (v > 1.0f) v = 1.0f;
        else if (v < -1.0f) v = -1.0f;
        int q = (int)(v * 127.0f + (v >= 0 ? 0.5f : -0.5f));
        if (q > 127) q = 127;
        if (q < -127) q = -127;
        out[i] = (signed char)q;
    }
}
*/
import "C"

import "golang.org/x/sys/cpu"

func quantizeVectorSIMD(dst []int8, vec []float32) bool {
	if len(vec) == 0 || len(dst) < len(vec) {
		return false
	}
	if !cpu.X86.HasAVX2 {
		return false
	}
	C.quantizeAVX2((*C.float)(&vec[0]), (*C.schar)(&dst[0]), C.int(len(vec)))
	return true
}
