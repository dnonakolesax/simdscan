package simdscan

import (
	"math/bits"
	"simd"
)

// IndexAny4 returns the index of the first occurrence in src of a, b, c, or d,
// or -1 if none of those bytes is present.
func IndexAny4(src []byte, a, b, c, d byte) int {
	var vector simd.Uint8s
	width := vector.Len()
	if len(src) < width {
		return indexAny4Scalar(src, a, b, c, d)
	}

	firstBlockEnd := min(BlockSize, len(src))
	if mask := MatchAny4Block(src[:firstBlockEnd], a, b, c, d); mask != 0 {
		return bits.TrailingZeros64(uint64(mask))
	}
	if firstBlockEnd == len(src) {
		return -1
	}

	ta := simd.BroadcastUint8s(a)
	tb := simd.BroadcastUint8s(b)
	tc := simd.BroadcastUint8s(c)
	td := simd.BroadcastUint8s(d)
	var matches [maxVectorBytes / 8]uint64

	i := firstBlockEnd
	batchBytes := vectorsPerBatch * width
	for ; i+batchBytes <= len(src); i += batchBytes {
		batch := src[i : i+batchBytes]
		m01 := matchAny4(simd.LoadUint8s(batch[:width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[width:2*width]), ta, tb, tc, td),
		)
		m23 := matchAny4(simd.LoadUint8s(batch[2*width:3*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[3*width:4*width]), ta, tb, tc, td),
		)
		m45 := matchAny4(simd.LoadUint8s(batch[4*width:5*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[5*width:6*width]), ta, tb, tc, td),
		)
		m67 := matchAny4(simd.LoadUint8s(batch[6*width:7*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[7*width:8*width]), ta, tb, tc, td),
		)
		m89 := matchAny4(simd.LoadUint8s(batch[8*width:9*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[9*width:10*width]), ta, tb, tc, td),
		)
		m1011 := matchAny4(simd.LoadUint8s(batch[10*width:11*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[11*width:12*width]), ta, tb, tc, td),
		)
		m1213 := matchAny4(simd.LoadUint8s(batch[12*width:13*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[13*width:14*width]), ta, tb, tc, td),
		)
		m1415 := matchAny4(simd.LoadUint8s(batch[14*width:15*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(batch[15*width:16*width]), ta, tb, tc, td),
		)
		m07 := m01.Or(m23).Or(m45.Or(m67))
		m815 := m89.Or(m1011).Or(m1213.Or(m1415))
		if firstMaskIndex(m07.Or(m815), &matches, width/8) < 0 {
			continue
		}

		for j := 0; j < vectorsPerBatch; j++ {
			start := j * width
			mask := matchAny4(simd.LoadUint8s(batch[start:start+width]), ta, tb, tc, td)
			if match := firstMaskIndex(mask, &matches, width/8); match >= 0 {
				return i + start + match
			}
		}
	}

	for ; i+width <= len(src); i += width {
		mask := matchAny4(simd.LoadUint8s(src[i:i+width]), ta, tb, tc, td)
		if match := firstMaskIndex(mask, &matches, width/8); match >= 0 {
			return i + match
		}
	}

	if tail := indexAny4Scalar(src[i:], a, b, c, d); tail >= 0 {
		return i + tail
	}
	return -1
}

// MatchAny4Block returns a mask containing one set bit for each byte in src
// equal to a, b, c, or d. Bit i corresponds to src[i]. It panics if len(src)
// exceeds BlockSize.
func MatchAny4Block(src []byte, a, b, c, d byte) Mask {
	return matchAny4Block(
		src,
		simd.BroadcastUint8s(a),
		simd.BroadcastUint8s(b),
		simd.BroadcastUint8s(c),
		simd.BroadcastUint8s(d),
	)
}

// AppendAny4Indexes appends the indexes of all bytes in src equal to a, b, c,
// or d to dst and returns the resulting slice. Appended indexes are in
// ascending order. It panics if src is too large for its indexes to be
// represented as uint32 values.
func AppendAny4Indexes(dst []uint32, src []byte, a, b, c, d byte) []uint32 {
	if uint64(len(src)) > uint64(1)<<32 {
		panic("simdscan: AppendAny4Indexes source exceeds uint32 index range")
	}

	var vector simd.Uint8s
	width := vector.Len()

	// For very small inputs SIMD setup + mask materialization is not worth it.
	if len(src) < width {
		for i, value := range src {
			if value == a || value == b || value == c || value == d {
				dst = append(dst, uint32(i))
			}
		}
		return dst
	}

	ta := simd.BroadcastUint8s(a)
	tb := simd.BroadcastUint8s(b)
	tc := simd.BroadcastUint8s(c)
	td := simd.BroadcastUint8s(d)

	var words [maxVectorBytes / 8]uint64
	wordCount := width / 8

	i := 0
	treeBytes := vectorsPerTree * width
	groupBytes := vectorsPerTreeGroup * width

	// Fast path for large match-free regions.
	//
	// Sixteen vectors are reduced to four groups of four. An empty aggregate
	// skips the whole tree; otherwise only positive groups and their four
	// vectors are materialized.
	for ; i+treeBytes <= len(src); i += treeBytes {
		tree := src[i : i+treeBytes]

		m01 := matchAny4(simd.LoadUint8s(tree[:width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[width:2*width]), ta, tb, tc, td),
		)
		m23 := matchAny4(simd.LoadUint8s(tree[2*width:3*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[3*width:4*width]), ta, tb, tc, td),
		)
		m45 := matchAny4(simd.LoadUint8s(tree[4*width:5*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[5*width:6*width]), ta, tb, tc, td),
		)
		m67 := matchAny4(simd.LoadUint8s(tree[6*width:7*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[7*width:8*width]), ta, tb, tc, td),
		)
		m89 := matchAny4(simd.LoadUint8s(tree[8*width:9*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[9*width:10*width]), ta, tb, tc, td),
		)
		m1011 := matchAny4(simd.LoadUint8s(tree[10*width:11*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[11*width:12*width]), ta, tb, tc, td),
		)
		m1213 := matchAny4(simd.LoadUint8s(tree[12*width:13*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[13*width:14*width]), ta, tb, tc, td),
		)
		m1415 := matchAny4(simd.LoadUint8s(tree[14*width:15*width]), ta, tb, tc, td).Or(
			matchAny4(simd.LoadUint8s(tree[15*width:16*width]), ta, tb, tc, td),
		)
		group0 := m01.Or(m23)
		group1 := m45.Or(m67)
		group2 := m89.Or(m1011)
		group3 := m1213.Or(m1415)
		combined := group0.Or(group1).Or(group2.Or(group3))
		if firstMaskIndex(combined, &words, wordCount) < 0 {
			continue
		}

		var positiveGroups uint8
		if firstMaskIndex(group0, &words, wordCount) >= 0 {
			positiveGroups |= 1 << 0
		}
		if firstMaskIndex(group1, &words, wordCount) >= 0 {
			positiveGroups |= 1 << 1
		}
		if firstMaskIndex(group2, &words, wordCount) >= 0 {
			positiveGroups |= 1 << 2
		}
		if firstMaskIndex(group3, &words, wordCount) >= 0 {
			positiveGroups |= 1 << 3
		}
		for positiveGroups != 0 {
			group := bits.TrailingZeros8(positiveGroups)
			groupStart := group * groupBytes
			for vectorIndex := 0; vectorIndex < vectorsPerTreeGroup; vectorIndex++ {
				start := groupStart + vectorIndex*width
				mask := matchAny4(
					simd.LoadUint8s(tree[start:start+width]),
					ta, tb, tc, td,
				)
				compact := mask8sToMask(mask, &words, wordCount)
				for compact != 0 {
					bit := bits.TrailingZeros64(uint64(compact))
					dst = append(dst, uint32(i+start+bit))
					compact &= compact - 1
				}
			}
			positiveGroups &= positiveGroups - 1
		}
	}

	// Remaining complete native vectors.
	for ; i+width <= len(src); i += width {
		mask := matchAny4(
			simd.LoadUint8s(src[i:i+width]),
			ta, tb, tc, td,
		)

		compact := mask8sToMask(mask, &words, wordCount)

		for compact != 0 {
			bit := bits.TrailingZeros64(uint64(compact))
			dst = append(dst, uint32(i+bit))
			compact &= compact - 1
		}
	}

	// Short tail.
	for ; i < len(src); i++ {
		value := src[i]
		if value == a || value == b || value == c || value == d {
			dst = append(dst, uint32(i))
		}
	}

	return dst
}

func matchAny4Block(src []byte, a, b, c, d simd.Uint8s) Mask {
	if len(src) > BlockSize {
		panic("simdscan: MatchAny4Block source exceeds BlockSize")
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
		mask := matchAny4(vector, a, b, c, d)
		result |= mask8sToMask(mask, &words, width/8) << offset
	}

	return result & maskForLength(len(src))
}

func matchAny4(vector, a, b, c, d simd.Uint8s) simd.Mask8s {
	return vector.Equal(a).Or(vector.Equal(b)).Or(vector.Equal(c).Or(vector.Equal(d)))
}

func indexAny4Scalar(src []byte, a, b, c, d byte) int {
	for i, value := range src {
		if value == a || value == b || value == c || value == d {
			return i
		}
	}
	return -1
}
