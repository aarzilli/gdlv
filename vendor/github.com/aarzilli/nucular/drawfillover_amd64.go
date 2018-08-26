package nucular

import "image"

func drawFillOver_SIMD_internal(base *uint8, i0, i1 int, stride, n int, adivm, sr, sg, sb, sa uint32)

func drawFillOver(dst *image.RGBA, r image.Rectangle, sr, sg, sb, sa uint32) {
	const m = 1<<16 - 1
	a := (m - sa) * 0x101
	adivm := a / m
	i0 := dst.PixOffset(r.Min.X, r.Min.Y)
	i1 := i0 + r.Dx()*4
	drawFillOver_SIMD_internal(&dst.Pix[0], i0, i1, dst.Stride, r.Max.Y-r.Min.Y, adivm, sr, sg, sb, sa)
}
