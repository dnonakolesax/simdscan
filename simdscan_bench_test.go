package simdscan

import (
	"bytes"
	"fmt"
	"testing"
)

var benchmarkIndex int
var benchmarkMask Mask
var benchmarkIndexes []uint32

var benchmarkSizes = []int{0, 8, 16, 32, 64, 128, 256, 1024, 4096, 8 << 10, 16 << 10, 32 << 10, 64 << 10, 1 << 20}

var benchmarkMatches = []string{"first", "middle", "end", "none"}

func BenchmarkMatchByteBlock(b *testing.B) {
	for _, size := range []int{0, 8, 16, 32, 63, 64} {
		for _, match := range []string{"none", "dense"} {
			src := bytes.Repeat([]byte{'a'}, size)
			if match == "dense" {
				for i := range src {
					src[i] = 'z'
				}
			}

			b.Run(fmt.Sprintf("%d/%s", size, match), func(b *testing.B) {
				b.SetBytes(int64(size))
				b.ReportAllocs()
				for b.Loop() {
					benchmarkMask = MatchByteBlock(src, 'z')
				}
			})
		}
	}
}

func BenchmarkMatchAny4Block(b *testing.B) {
	for _, size := range []int{0, 8, 16, 32, 63, 64} {
		for _, match := range []string{"none", "dense"} {
			src := bytes.Repeat([]byte{'a'}, size)
			if match == "dense" {
				for i := range src {
					src[i] = "{}\"\\"[i%4]
				}
			}

			b.Run(fmt.Sprintf("%d/%s", size, match), func(b *testing.B) {
				b.SetBytes(int64(size))
				b.ReportAllocs()
				for b.Loop() {
					benchmarkMask = MatchAny4Block(src, '{', '}', '"', '\\')
				}
			})
		}
	}
}

func BenchmarkAppendAny4IndexesSynthetic(b *testing.B) {
	const chars = "{}\"\\"

	implementations := []struct {
		name string
		fn   func([]uint32, []byte) []uint32
	}{
		{
			name: "Tree4x4",
			fn: func(dst []uint32, src []byte) []uint32 {
				return AppendAny4Indexes(
					dst,
					src,
					'{', '}', '"', '\\',
				)
			},
		},
		{
			name: "Tree4x2x2",
			fn: func(dst []uint32, src []byte) []uint32 {
				return AppendAny4IndexesTree4x2x2(
					dst,
					src,
					'{', '}', '"', '\\',
				)
			},
		},
		{
			name: "Direct",
			fn: func(dst []uint32, src []byte) []uint32 {
				return AppendAny4IndexesDirect(
					dst,
					src,
					'{', '}', '"', '\\',
				)
			},
		},
		{
			name: "RetainedGroup4",
			fn: func(dst []uint32, src []byte) []uint32 {
				return AppendAny4IndexesRetainedGroup4(
					dst,
					src,
					'{', '}', '"', '\\',
				)
			},
		},
		{
			name: "scalar",
			fn: func(dst []uint32, src []byte) []uint32 {
				return appendAny4IndexesScalar(
					dst,
					src,
					'{', '}', '"', '\\',
				)
			},
		},
		{
			name: "bytes",
			fn: func(dst []uint32, src []byte) []uint32 {
				return appendAny4IndexesBytes(dst, src, chars)
			},
		},
	}

	densities := []struct {
		name   string
		stride int
	}{
		{name: "none", stride: 0},

		// 0.39%
		{name: "1-in-256", stride: 256},

		// 1.56%
		{name: "1-in-64", stride: 64},

		// 3.125%
		{name: "1-in-32", stride: 32},

		// 6.25%
		{name: "1-in-16", stride: 16},

		// 8.3%
		{name: "1-in-12", stride: 12},

		// 10%
		{name: "1-in-10", stride: 10},

		// 12.5%
		{name: "1-in-8", stride: 8},

		// 16.7%
		{name: "1-in-6", stride: 6},

		// 20%
		{name: "1-in-5", stride: 5},

		// 25%
		{name: "1-in-4", stride: 4},

		// 100%
		{name: "dense", stride: 1},
	}

	sizes := []int{
		64,
		1024,
		64 << 10,
		1 << 20,
	}

	for _, size := range sizes {
		for _, density := range densities {
			src := bytes.Repeat([]byte{'a'}, size)

			matches := 0

			if density.stride != 0 {
				for i := 0; i < size; i += density.stride {
					src[i] = chars[matches%len(chars)]
					matches++
				}
			}

			// Capacity is prepared outside the timed loop, so all implementations
			// can be compared without measuring output-slice allocation.
			dst := make([]uint32, 0, matches)

			for _, implementation := range implementations {
				name := fmt.Sprintf(
					"%d/%s/%s",
					size,
					density.name,
					implementation.name,
				)

				b.Run(name, func(b *testing.B) {
					b.SetBytes(int64(size))
					b.ReportAllocs()
					b.ReportMetric(float64(matches), "matches/op")

					for b.Loop() {
						benchmarkIndexes = implementation.fn(dst[:0], src)
					}
				})
			}
		}
	}
}
func appendAny4IndexesBytes(dst []uint32, src []byte, chars string) []uint32 {
	base := 0
	for len(src) > 0 {
		index := bytes.IndexAny(src, chars)
		if index < 0 {
			break
		}
		dst = append(dst, uint32(base+index))
		advance := index + 1
		base += advance
		src = src[advance:]
	}
	return dst
}

func BenchmarkIndexByte(b *testing.B) {
	implementations := []struct {
		name string
		fn   func([]byte, byte) int
	}{
		{name: "simd", fn: IndexByte},
		{name: "scalar", fn: indexByteScalar},
		{name: "bytes", fn: bytes.IndexByte},
	}

	for _, implementation := range implementations {
		for _, size := range benchmarkSizes {
			for _, match := range benchmarkMatches {
				src := bytes.Repeat([]byte{'a'}, size)
				scanned := size
				if size > 0 {
					switch match {
					case "first":
						src[0] = 'z'
						scanned = 1
					case "middle":
						src[size/2] = 'z'
						scanned = size/2 + 1
					case "end":
						src[size-1] = 'z'
					}
				}

				name := fmt.Sprintf("%s/%d/%s", implementation.name, size, match)
				b.Run(name, func(b *testing.B) {
					b.SetBytes(int64(scanned))
					b.ReportAllocs()
					for b.Loop() {
						benchmarkIndex = implementation.fn(src, 'z')
					}
				})
			}
		}
	}
}

func BenchmarkIndexAny4(b *testing.B) {
	const chars = "{}\"\\"
	implementations := []struct {
		name string
		fn   func([]byte) int
	}{
		{name: "simd", fn: func(src []byte) int { return IndexAny4(src, '{', '}', '"', '\\') }},
		{name: "scalar", fn: func(src []byte) int { return indexAny4Scalar(src, '{', '}', '"', '\\') }},
		{name: "bytes", fn: func(src []byte) int { return bytes.IndexAny(src, chars) }},
	}

	for _, implementation := range implementations {
		for _, size := range benchmarkSizes {
			for _, match := range benchmarkMatches {
				src := bytes.Repeat([]byte{'a'}, size)
				scanned := size
				if size > 0 {
					switch match {
					case "first":
						src[0] = '{'
						scanned = 1
					case "middle":
						src[size/2] = '{'
						scanned = size/2 + 1
					case "end":
						src[size-1] = '{'
					}
				}

				name := fmt.Sprintf("%s/%d/%s", implementation.name, size, match)
				b.Run(name, func(b *testing.B) {
					b.SetBytes(int64(scanned))
					b.ReportAllocs()
					for b.Loop() {
						benchmarkIndex = implementation.fn(src)
					}
				})
			}
		}
	}
}
