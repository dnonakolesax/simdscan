//go:build !amd64

package maskbits

import "simd"

// FromMask8s converts a portable byte mask to its scalar bitmap.
func FromMask8s(mask simd.Mask8s, words []uint64) uint64 {
	return portable(mask, words)
}
