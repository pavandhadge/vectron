//go:build amd64 && avx512asm

#include "textflag.h"

// func dotProductAVX512(a, b *float32, n int) float32
TEXT ·dotProductAVX512(SB), NOSPLIT, $0-32
	MOVQ a+0(FP), SI
	MOVQ b+8(FP), DI
	MOVQ n+16(FP), CX

	// if n < 16, fall back to scalar loop
	CMPQ CX, $16
	JL scalar

	// zmm0 = 0
	VXORPS Z0, Z0, Z0

vector_loop:
	CMPQ CX, $16
	JL vector_done
	VMOVUPS (SI), Z1
	VMOVUPS (DI), Z2
	VMULPS Z2, Z1, Z1
	VADDPS Z1, Z0, Z0
	ADDQ $64, SI
	ADDQ $64, DI
	SUBQ $16, CX
	JMP vector_loop

vector_done:
	// Reduce zmm0 -> ymm0
	VEXTRACTF32x8 $1, Z0, Y1
	VEXTRACTF32x8 $0, Z0, Y0
	VADDPS Y1, Y0, Y0

	// horizontal sum ymm0 into xmm0
	VEXTRACTF128 $0, Y0, X0
	VEXTRACTF128 $1, Y0, X1
	VADDPS X1, X0, X0
	VHADDPS X0, X0, X0
	VHADDPS X0, X0, X0

	// scalar accumulator in X0
	JMP scalar_tail

scalar:
	VXORPS X0, X0, X0

scalar_tail:
	// process remaining elements in CX
	TESTQ CX, CX
	JLE done
scalar_loop:
	MOVSS (SI), X2
	MOVSS (DI), X3
	MULSS X3, X2
	ADDSS X2, X0
	ADDQ $4, SI
	ADDQ $4, DI
	DECQ CX
	JNZ scalar_loop

done:
	VZEROUPPER
	// return value in X0
	RET
