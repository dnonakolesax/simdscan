package maskbits

import "simd"

func portable(mask simd.Mask8s, words []uint64) uint64 {
	mask.ToInt8s().ToBits().ReshapeToUint64s().Store(words)

	const (
		byteLowBits   = uint64(0x0101010101010101)
		compressBytes = uint64(0x0102040810204080)
	)
	var result uint64
	for i, word := range words {
		bits := ((word & byteLowBits) * compressBytes) >> 56
		result |= bits << (8 * i)
	}
	return result
}
