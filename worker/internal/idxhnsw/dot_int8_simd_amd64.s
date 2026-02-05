//go:build amd64

#include "textflag.h"

// func dotProductInt8AVX2(a, b *int8, n int) int32
TEXT ·dotProductInt8AVX2(SB), NOSPLIT, $0-32
	MOVQ a+0(FP), SI
	MOVQ b+8(FP), DI
	MOVQ n+16(FP), CX

	// Vector accumulator in Y0 (int32 lanes)
	VXORPS Y0, Y0, Y0
	VXORPS X0, X0, X0

	// Process 16 bytes per iteration (n is already aligned)
	CMPQ CX, $16
	JL done

loop:
	VMOVDQU (SI), X1
	VMOVDQU (DI), X2
	VPMOVSXBW X1, Y1
	VPMOVSXBW X2, Y2
	VPMADDWD Y2, Y1, Y3
	VPADDD Y3, Y0, Y0
	ADDQ $16, SI
	ADDQ $16, DI
	SUBQ $16, CX
	JGT loop

done:
	// Horizontal sum Y0 into X0
	VEXTRACTF128 $0, Y0, X0
	VEXTRACTF128 $1, Y0, X1
	PADDD X1, X0
	PHADDD X0, X0
	PHADDD X0, X0
	MOVD X0, AX
	MOVL AX, ret+24(FP)
	VZEROUPPER
	RET
