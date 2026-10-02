//go:build amd64

package maskbits

import (
	"simd"
	"simd/archsimd"
)

// FromMask8s converts a portable byte mask to its scalar bitmap. It uses the
// native amd64 mask materialization when available and falls back to the
// portable implementation when SIMD is emulated.
func FromMask8s(mask simd.Mask8s, words []uint64) uint64 {
	switch arch := mask.ToArch().(type) {
	case archsimd.Mask8x16:
		return uint64(arch.ToBits())
	case archsimd.Mask8x32:
		return uint64(arch.ToBits())
	case archsimd.Mask8x64:
		return arch.ToBits()
	default:
		return portable(mask, words)
	}
}
