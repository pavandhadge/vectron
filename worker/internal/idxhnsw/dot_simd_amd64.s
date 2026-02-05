//go:build amd64

#include "textflag.h"

// func dotProductAVX2(a, b *float32, n int) float32
TEXT ·dotProductAVX2(SB), NOSPLIT, $0-32
	MOVQ a+0(FP), SI
	MOVQ b+8(FP), DI
	MOVQ n+16(FP), CX

	// if n < 8, fall back to scalar loop
	CMPQ CX, $8
	JL scalar

	// ymm0 = 0
	VXORPS Y0, Y0, Y0

vector_loop:
	CMPQ CX, $8
	JL vector_done
	VMOVUPS (SI), Y1
	VMOVUPS (DI), Y2
	VMULPS Y2, Y1, Y1
	VADDPS Y1, Y0, Y0
	ADDQ $32, SI
	ADDQ $32, DI
	SUBQ $8, CX
	JMP vector_loop

vector_done:
	// horizontal sum ymm0 into xmm0
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
