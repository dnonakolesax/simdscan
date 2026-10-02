package simdscan

import (
	"math/bits"
	"simd"
	"slices"
	"testing"
)

// AppendAny4IndexesDirect is a benchmark-only implementation that
// materializes every native vector mask without a tree-level precheck.
func AppendAny4IndexesDirect(dst []uint32, src []byte, a, b, c, d byte) []uint32 {
	if uint64(len(src)) > uint64(1)<<32 {
		panic("simdscan: AppendAny4IndexesDirect source exceeds uint32 index range")
	}

	var vector simd.Uint8s
	width := vector.Len()
	if len(src) < width {
		return appendAny4IndexesScalar(dst, src, a, b, c, d)
	}

	ta := simd.BroadcastUint8s(a)
	tb := simd.BroadcastUint8s(b)
	tc := simd.BroadcastUint8s(c)
	td := simd.BroadcastUint8s(d)
	var words [maxVectorBytes / 8]uint64
	wordCount := width / 8

	i := 0
	for ; i+width <= len(src); i += width {
		mask := matchAny4(simd.LoadUint8s(src[i:i+width]), ta, tb, tc, td)
		compact := mask8sToMask(mask, &words, wordCount)
		for compact != 0 {
			bit := bits.TrailingZeros64(uint64(compact))
			dst = append(dst, uint32(i+bit))
			compact &= compact - 1
		}
	}

	for ; i < len(src); i++ {
		value := src[i]
		if value == a || value == b || value == c || value == d {
			dst = append(dst, uint32(i))
		}
	}
	return dst
}

// AppendAny4IndexesRetainedGroup4 is a benchmark-only implementation that
// retains four individual vector masks, checks their combined mask, and
// compacts the retained masks without repeating their SIMD comparisons.
func AppendAny4IndexesRetainedGroup4(dst []uint32, src []byte, a, b, c, d byte) []uint32 {
	if uint64(len(src)) > uint64(1)<<32 {
		panic("simdscan: AppendAny4IndexesRetainedGroup4 source exceeds uint32 index range")
	}

	var vector simd.Uint8s
	width := vector.Len()
	if len(src) < width {
		return appendAny4IndexesScalar(dst, src, a, b, c, d)
	}

	ta := simd.BroadcastUint8s(a)
	tb := simd.BroadcastUint8s(b)
	tc := simd.BroadcastUint8s(c)
	td := simd.BroadcastUint8s(d)
	var words [maxVectorBytes / 8]uint64
	wordCount := width / 8

	i := 0
	groupBytes := 4 * width
	for ; i+groupBytes <= len(src); i += groupBytes {
		group := src[i : i+groupBytes]
		m0 := matchAny4(simd.LoadUint8s(group[:width]), ta, tb, tc, td)
		m1 := matchAny4(simd.LoadUint8s(group[width:2*width]), ta, tb, tc, td)
		m2 := matchAny4(simd.LoadUint8s(group[2*width:3*width]), ta, tb, tc, td)
		m3 := matchAny4(simd.LoadUint8s(group[3*width:4*width]), ta, tb, tc, td)
		combined := m0.Or(m1).Or(m2.Or(m3))
		if firstMaskIndex(combined, &words, wordCount) < 0 {
			continue
		}

		dst = appendAny4RetainedMask(dst, m0, i, &words, wordCount)
		dst = appendAny4RetainedMask(dst, m1, i+width, &words, wordCount)
		dst = appendAny4RetainedMask(dst, m2, i+2*width, &words, wordCount)
		dst = appendAny4RetainedMask(dst, m3, i+3*width, &words, wordCount)
	}

	for ; i+width <= len(src); i += width {
		mask := matchAny4(simd.LoadUint8s(src[i:i+width]), ta, tb, tc, td)
		dst = appendAny4RetainedMask(dst, mask, i, &words, wordCount)
	}
	for ; i < len(src); i++ {
		value := src[i]
		if value == a || value == b || value == c || value == d {
			dst = append(dst, uint32(i))
		}
	}
	return dst
}

func appendAny4RetainedMask(
	dst []uint32,
	mask simd.Mask8s,
	base int,
	words *[maxVectorBytes / 8]uint64,
	wordCount int,
) []uint32 {
	compact := mask8sToMask(mask, words, wordCount)
	for compact != 0 {
		bit := bits.TrailingZeros64(uint64(compact))
		dst = append(dst, uint32(base+bit))
		compact &= compact - 1
	}
	return dst
}

// AppendAny4IndexesTree4x2x2 is a benchmark-only implementation that reduces
// 16 vectors into four groups, each group into two pairs, and then rechecks
// only the vectors belonging to positive pairs.
func AppendAny4IndexesTree4x2x2(dst []uint32, src []byte, a, b, c, d byte) []uint32 {
	if uint64(len(src)) > uint64(1)<<32 {
		panic("simdscan: AppendAny4IndexesTree4x2x2 source exceeds uint32 index range")
	}

	var vector simd.Uint8s
	width := vector.Len()
	if len(src) < width {
		return appendAny4IndexesScalar(dst, src, a, b, c, d)
	}

	ta := simd.BroadcastUint8s(a)
	tb := simd.BroadcastUint8s(b)
	tc := simd.BroadcastUint8s(c)
	td := simd.BroadcastUint8s(d)
	var words [maxVectorBytes / 8]uint64
	wordCount := width / 8

	i := 0
	treeBytes := vectorsPerTree * width
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

		var positivePairs uint8
		if positiveGroups&(1<<0) != 0 {
			if firstMaskIndex(m01, &words, wordCount) >= 0 {
				positivePairs |= 1 << 0
			}
			if firstMaskIndex(m23, &words, wordCount) >= 0 {
				positivePairs |= 1 << 1
			}
		}
		if positiveGroups&(1<<1) != 0 {
			if firstMaskIndex(m45, &words, wordCount) >= 0 {
				positivePairs |= 1 << 2
			}
			if firstMaskIndex(m67, &words, wordCount) >= 0 {
				positivePairs |= 1 << 3
			}
		}
		if positiveGroups&(1<<2) != 0 {
			if firstMaskIndex(m89, &words, wordCount) >= 0 {
				positivePairs |= 1 << 4
			}
			if firstMaskIndex(m1011, &words, wordCount) >= 0 {
				positivePairs |= 1 << 5
			}
		}
		if positiveGroups&(1<<3) != 0 {
			if firstMaskIndex(m1213, &words, wordCount) >= 0 {
				positivePairs |= 1 << 6
			}
			if firstMaskIndex(m1415, &words, wordCount) >= 0 {
				positivePairs |= 1 << 7
			}
		}

		for positivePairs != 0 {
			pair := bits.TrailingZeros8(positivePairs)
			pairStart := pair * 2 * width
			for vectorIndex := 0; vectorIndex < 2; vectorIndex++ {
				start := pairStart + vectorIndex*width
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
			positivePairs &= positivePairs - 1
		}
	}

	for ; i+width <= len(src); i += width {
		mask := matchAny4(simd.LoadUint8s(src[i:i+width]), ta, tb, tc, td)
		compact := mask8sToMask(mask, &words, wordCount)
		for compact != 0 {
			bit := bits.TrailingZeros64(uint64(compact))
			dst = append(dst, uint32(i+bit))
			compact &= compact - 1
		}
	}

	for ; i < len(src); i++ {
		value := src[i]
		if value == a || value == b || value == c || value == d {
			dst = append(dst, uint32(i))
		}
	}
	return dst
}

func TestAppendAny4IndexesBenchmarkVariants(t *testing.T) {
	variants := []struct {
		name string
		fn   func([]uint32, []byte, byte, byte, byte, byte) []uint32
	}{
		{name: "direct", fn: AppendAny4IndexesDirect},
		{name: "retained-group4", fn: AppendAny4IndexesRetainedGroup4},
		{name: "tree4x2x2", fn: AppendAny4IndexesTree4x2x2},
	}
	targets := [4]byte{'{', '}', '"', '\\'}
	size := 2*vectorsPerTree*maxVectorBytes + 1

	for _, variant := range variants {
		t.Run(variant.name, func(t *testing.T) {
			src := make([]byte, size)
			for i := range src {
				src[i] = byte(i*37 + size)
			}
			prefix := []uint32{900, 901}
			want := appendAny4IndexesScalar(slices.Clone(prefix), src, targets[0], targets[1], targets[2], targets[3])
			got := variant.fn(slices.Clone(prefix), src, targets[0], targets[1], targets[2], targets[3])
			if !slices.Equal(got, want) {
				t.Fatalf("got %v, want %v", got, want)
			}

			for pos := range src {
				clear(src)
				src[pos] = targets[pos%len(targets)]
				got := variant.fn(got[:0], src, targets[0], targets[1], targets[2], targets[3])
				want := []uint32{uint32(pos)}
				if !slices.Equal(got, want) {
					t.Fatalf("position %d: got %v, want %v", pos, got, want)
				}
			}
		})
	}
}
