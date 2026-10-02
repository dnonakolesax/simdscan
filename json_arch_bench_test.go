//go:build amd64

package simdscan

import (
	"bytes"
	"simd"
	"simd/archsimd"
	"testing"
)

// BenchmarkMask8sToMaskProduction isolates the production mask-materialization
// backend. On native amd64 SIMD it uses archsimd ToBits; in emulation it uses
// the portable fallback.
func BenchmarkMask8sToMaskProduction(b *testing.B) {
	var vector simd.Uint8s
	width := vector.Len()
	src := bytes.Repeat([]byte{'a'}, width)
	loaded := simd.LoadUint8s(src)
	var words [maxVectorBytes / 8]uint64
	wordCount := width / 8

	for _, test := range []struct {
		name   string
		target byte
	}{
		{name: "none", target: 'z'},
		{name: "dense", target: 'a'},
	} {
		mask := loaded.Equal(simd.BroadcastUint8s(test.target))
		b.Run(test.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				benchmarkMaskConversion = mask8sToMask(mask, &words, wordCount)
			}
			b.ReportMetric(float64(width*8), "vector-bits")
		})
	}
}

// BenchmarkMask8sToMaskFixedWidth measures the fixed-width primitive with the
// width selection outside the timed loop.
func BenchmarkMask8sToMaskFixedWidth(b *testing.B) {
	if simd.Emulated() {
		b.Skip("archsimd ToBits is unavailable under portable SIMD emulation")
	}
	var vector simd.Uint8s
	width := vector.Len()
	src := bytes.Repeat([]byte{'a'}, width)
	mask := simd.LoadUint8s(src).Equal(simd.BroadcastUint8s('a'))

	b.ReportAllocs()
	switch width {
	case 16:
		for b.Loop() {
			benchmarkMaskConversion = Mask(mask.ToArch().(archsimd.Mask8x16).ToBits())
		}
	case 32:
		for b.Loop() {
			benchmarkMaskConversion = Mask(mask.ToArch().(archsimd.Mask8x32).ToBits())
		}
	case 64:
		for b.Loop() {
			benchmarkMaskConversion = Mask(mask.ToArch().(archsimd.Mask8x64).ToBits())
		}
	default:
		b.Fatalf("unsupported native SIMD width %d", width)
	}
	b.ReportMetric(float64(width*8), "vector-bits")
}

// BenchmarkJSONMaskMaterialization compares both portable baselines, the
// production helper type switch, and the scanner-level dispatch candidate.
func BenchmarkJSONMaskMaterialization(b *testing.B) {
	totalSize := 0
	sources := make([][]byte, 0, len(jsonBenchmarkFiles))
	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(b, file.path, file.compressed)
		sources = append(sources, src)
		totalSize += len(src)
	}

	src := make([]byte, 0, totalSize)
	for _, data := range sources {
		src = append(src, data...)
	}

	implementations := []struct {
		name string
		fn   func([]byte) RawMasks
	}{
		{name: "portable_3_masks", fn: scanJSONMasks3Portable},
		{name: "portable_2_planes", fn: scanJSONMasks2Portable},
		{name: "helper_type_switch_2_planes", fn: scanJSONMasksDynamic},
		{name: "scanner_dispatch_2_planes", fn: scanJSONMasksScannerDispatch},
	}
	for _, implementation := range implementations {
		b.Run(implementation.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONMasks = implementation.fn(src)
			}
		})
	}
}

func scanJSONMasksDynamic(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasksDynamic(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func scanJSONMasksScannerDispatch(src []byte) RawMasks {
	if simd.Emulated() {
		return scanJSONMasksDynamic(src)
	}

	var vector simd.Uint8s
	switch vector.Len() {
	case 16:
		return scanJSONMasks128(src)
	case 32:
		return scanJSONMasks256(src)
	case 64:
		return scanJSONMasks512(src)
	default:
		panic("simdscan: unsupported native SIMD width")
	}
}

func scanJSONMasks128(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasks128(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func scanJSONMasks256(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasks256(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func scanJSONMasks512(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasks512(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func rawJSONBlockMasks128(src []byte, targets jsonSIMDTargets) RawMasks {
	const width = 16
	var result RawMasks
	offset := 0
	for ; offset+width <= len(src); offset += width {
		vector := simd.LoadUint8s(src[offset : offset+width])
		appendJSONVector128(&result, vector, targets, offset)
	}
	if offset < len(src) {
		vector, _ := simd.LoadUint8sPart(src[offset:])
		appendJSONVector128(&result, vector, targets, offset)
	}
	return trimRawJSONMasks(result, len(src))
}

func rawJSONBlockMasks256(src []byte, targets jsonSIMDTargets) RawMasks {
	const width = 32
	var result RawMasks
	offset := 0
	for ; offset+width <= len(src); offset += width {
		vector := simd.LoadUint8s(src[offset : offset+width])
		appendJSONVector256(&result, vector, targets, offset)
	}
	if offset < len(src) {
		vector, _ := simd.LoadUint8sPart(src[offset:])
		appendJSONVector256(&result, vector, targets, offset)
	}
	return trimRawJSONMasks(result, len(src))
}

func rawJSONBlockMasks512(src []byte, targets jsonSIMDTargets) RawMasks {
	const width = 64
	var result RawMasks
	offset := 0
	for ; offset+width <= len(src); offset += width {
		vector := simd.LoadUint8s(src[offset : offset+width])
		appendJSONVector512(&result, vector, targets, offset)
	}
	if offset < len(src) {
		vector, _ := simd.LoadUint8sPart(src[offset:])
		appendJSONVector512(&result, vector, targets, offset)
	}
	return trimRawJSONMasks(result, len(src))
}

func appendJSONVector128(result *RawMasks, vector simd.Uint8s, targets jsonSIMDTargets, offset int) {
	plane0, plane1 := jsonVectorPlanes(vector, targets)
	appendJSONPlaneBits(
		result,
		Mask(plane0.ToArch().(archsimd.Mask8x16).ToBits()),
		Mask(plane1.ToArch().(archsimd.Mask8x16).ToBits()),
		offset,
	)
}

func appendJSONVector256(result *RawMasks, vector simd.Uint8s, targets jsonSIMDTargets, offset int) {
	plane0, plane1 := jsonVectorPlanes(vector, targets)
	appendJSONPlaneBits(
		result,
		Mask(plane0.ToArch().(archsimd.Mask8x32).ToBits()),
		Mask(plane1.ToArch().(archsimd.Mask8x32).ToBits()),
		offset,
	)
}

func appendJSONVector512(result *RawMasks, vector simd.Uint8s, targets jsonSIMDTargets, offset int) {
	plane0, plane1 := jsonVectorPlanes(vector, targets)
	appendJSONPlaneBits(
		result,
		Mask(plane0.ToArch().(archsimd.Mask8x64).ToBits()),
		Mask(plane1.ToArch().(archsimd.Mask8x64).ToBits()),
		offset,
	)
}

func TestProductionMaskMaterializationMatchesPortable(t *testing.T) {
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64

	for value := 0; value < 256; value++ {
		src := make([]byte, width)
		for i := range src {
			src[i] = byte(i*37 + value)
		}
		loaded := simd.LoadUint8s(src)
		for target := 0; target < 256; target++ {
			mask := loaded.Equal(simd.BroadcastUint8s(byte(target)))
			want := mask8sToMaskPortableBenchmark(mask, &words, wordCount)
			if got := mask8sToMask(mask, &words, wordCount); got != want {
				t.Fatalf("value %d, target %d: got %#x, want %#x", value, target, got, want)
			}
		}
	}
}

func TestJSONScannerDispatchMatchesDynamic(t *testing.T) {
	if simd.Emulated() {
		t.Skip("archsimd ToBits is unavailable under portable SIMD emulation")
	}

	var vector simd.Uint8s
	var scanBlock func([]byte, jsonSIMDTargets) RawMasks
	switch vector.Len() {
	case 16:
		scanBlock = rawJSONBlockMasks128
	case 32:
		scanBlock = rawJSONBlockMasks256
	case 64:
		scanBlock = rawJSONBlockMasks512
	default:
		t.Fatalf("unsupported native SIMD width %d", vector.Len())
	}

	targets := makeJSONSIMDTargets()
	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(t, file.path, file.compressed)
		for offset := 0; offset < len(src); offset += BlockSize {
			end := min(offset+BlockSize, len(src))
			block := src[offset:end]
			want := rawJSONBlockMasksDynamic(block, targets)
			if got := scanBlock(block, targets); got != want {
				t.Fatalf("%s block %d: got %+v, want %+v", file.name, offset, got, want)
			}
		}
	}
}
