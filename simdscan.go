package simdscan

import (
	"math/bits"
	"simd"

	"github.com/dnonakolesax/simdscan/internal/maskbits"
)

const BlockSize = 64

// Go 1.27 supports portable SIMD vectors up to 512 bits wide.
const maxVectorBytes = 512 / 8

const (
	vectorsPerBatch     = 16
	vectorsPerTreeGroup = 4
	treeGroupCount      = 4
	vectorsPerTree      = vectorsPerTreeGroup * treeGroupCount
)

type Mask uint64

type Set struct {
	// unexported
}

// MatchByteBlock returns a mask containing one set bit for each occurrence of
// c in src. Bit i corresponds to src[i]. It panics if len(src) exceeds
// BlockSize.
func MatchByteBlock(src []byte, c byte) Mask {
	return matchByteBlock(src, simd.BroadcastUint8s(c))
}

func matchByteBlock(src []byte, target simd.Uint8s) Mask {
	if len(src) > BlockSize {
		panic("simdscan: MatchByteBlock source exceeds BlockSize")
	}

	var vector simd.Uint8s
	width := vector.Len()
	var words [maxVectorBytes / 8]uint64
	var result Mask

	for offset := 0; offset < len(src); offset += width {
		remaining := len(src) - offset
		if remaining >= width {
			vector = simd.LoadUint8s(src[offset : offset+width])
		} else {
			vector, _ = simd.LoadUint8sPart(src[offset:])
		}
		result |= mask8sToMask(vector.Equal(target), &words, width/8) << offset
	}

	return result & maskForLength(len(src))
}

func mask8sToMask(mask simd.Mask8s, words *[maxVectorBytes / 8]uint64, wordCount int) Mask {
	return Mask(maskbits.FromMask8s(mask, words[:wordCount]))
}

func maskForLength(length int) Mask {
	if length == BlockSize {
		return ^Mask(0)
	}
	return (Mask(1) << length) - 1
}

// single byte
// IndexByte returns the index of the first occurrence of c in src,
// or -1 if c is not present.
func IndexByte(src []byte, c byte) int {
	var vector simd.Uint8s
	width := vector.Len()
	if len(src) < width {
		return indexByteScalar(src, c)
	}

	target := simd.BroadcastUint8s(c)
	var matches [maxVectorBytes / 8]uint64

	// Keep the common early-match case cheap before switching to tree scans.
	mask := simd.LoadUint8s(src[:width]).Equal(target)
	if match := firstMaskIndex(mask, &matches, width/8); match >= 0 {
		return match
	}

	i := width
	treeBytes := vectorsPerTree * width
	for ; i+treeBytes <= len(src); i += treeBytes {
		tree := src[i : i+treeBytes]
		groupBytes := vectorsPerTreeGroup * width

		m01 := simd.LoadUint8s(tree[:width]).Equal(target).Or(
			simd.LoadUint8s(tree[width : 2*width]).Equal(target),
		)
		m23 := simd.LoadUint8s(tree[2*width : 3*width]).Equal(target).Or(
			simd.LoadUint8s(tree[3*width : 4*width]).Equal(target),
		)
		m45 := simd.LoadUint8s(tree[4*width : 5*width]).Equal(target).Or(
			simd.LoadUint8s(tree[5*width : 6*width]).Equal(target),
		)
		m67 := simd.LoadUint8s(tree[6*width : 7*width]).Equal(target).Or(
			simd.LoadUint8s(tree[7*width : 8*width]).Equal(target),
		)
		m89 := simd.LoadUint8s(tree[8*width : 9*width]).Equal(target).Or(
			simd.LoadUint8s(tree[9*width : 10*width]).Equal(target),
		)
		m1011 := simd.LoadUint8s(tree[10*width : 11*width]).Equal(target).Or(
			simd.LoadUint8s(tree[11*width : 12*width]).Equal(target),
		)
		m1213 := simd.LoadUint8s(tree[12*width : 13*width]).Equal(target).Or(
			simd.LoadUint8s(tree[13*width : 14*width]).Equal(target),
		)
		m1415 := simd.LoadUint8s(tree[14*width : 15*width]).Equal(target).Or(
			simd.LoadUint8s(tree[15*width : 16*width]).Equal(target),
		)
		group0 := m01.Or(m23)
		group1 := m45.Or(m67)
		group2 := m89.Or(m1011)
		group3 := m1213.Or(m1415)
		combined := group0.Or(group1).Or(group2.Or(group3))
		if firstMaskIndex(combined, &matches, width/8) < 0 {
			continue
		}

		group := 0
		switch {
		case firstMaskIndex(group0, &matches, width/8) >= 0:
		case firstMaskIndex(group1, &matches, width/8) >= 0:
			group = 1
		case firstMaskIndex(group2, &matches, width/8) >= 0:
			group = 2
		case firstMaskIndex(group3, &matches, width/8) >= 0:
			group = 3
		default:
			panic("simdscan: positive SIMD tree has no positive group")
		}

		// Recheck only the first positive group's vectors in source order.
		groupStart := group * groupBytes
		for j := 0; j < vectorsPerTreeGroup; j++ {
			start := groupStart + j*width
			mask := simd.LoadUint8s(tree[start : start+width]).Equal(target)
			if match := firstMaskIndex(mask, &matches, width/8); match >= 0 {
				return i + start + match
			}
		}
	}

	for ; i+width <= len(src); i += width {
		mask := simd.LoadUint8s(src[i : i+width]).Equal(target)
		if match := firstMaskIndex(mask, &matches, width/8); match >= 0 {
			return i + match
		}
	}

	if tail := indexByteScalar(src[i:], c); tail >= 0 {
		return i + tail
	}
	return -1
}

func firstMaskIndex(mask simd.Mask8s, matches *[maxVectorBytes / 8]uint64, words int) int {
	mask.ToInt8s().ToBits().ReshapeToUint64s().Store(matches[:words])
	for i, word := range matches[:words] {
		if word != 0 {
			return 8*i + bits.TrailingZeros64(word)/8
		}
	}
	return -1
}

func indexByteScalar(src []byte, c byte) int {
	for i, b := range src {
		if b == c {
			return i
		}
	}
	return -1
}
