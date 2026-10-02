package simdscan

import (
	"bytes"
	"compress/gzip"
	"io"
	"math/bits"
	"os"
	"simd"
	"testing"
)

var benchmarkJSONMasks RawMasks
var benchmarkJSONStructural Mask
var benchmarkJSONStructuralMasks []Mask
var benchmarkJSONStructuralIndexes []uint32
var benchmarkJSONState JSONState
var benchmarkEscapedMask Mask
var benchmarkEscapedCarry bool
var benchmarkMaskConversion Mask
var benchmarkPrefixMasks [8]Mask
var benchmarkPrefixInputs = [8]Mask{
	0x0123456789abcdef,
	0xfedcba9876543210,
	0x1111222233334444,
	0xaaaacccc5555ffff,
	0x8000000000000001,
	0x0101010101010101,
	0x7fffffffffffffff,
	0x13579bdf2468ace0,
}

var jsonBenchmarkFiles = []struct {
	name       string
	path       string
	compressed bool
}{
	{name: "canada_geometry", path: "testdata/canada_geometry.json.gz", compressed: true},
	{name: "citm_catalog", path: "testdata/citm_catalog.json"},
	{name: "golang_source", path: "testdata/golang_source.json.gz", compressed: true},
	{name: "string_unicode", path: "testdata/string_unicode.json.gz", compressed: true},
	{name: "synthea_fhir", path: "testdata/synthea_fhir.json.gz", compressed: true},
	{name: "twitter_status", path: "testdata/twitter_status.json.gz", compressed: true},
}

func BenchmarkJSONMasks(b *testing.B) {
	implementations := []struct {
		name string
		fn   func([]byte) RawMasks
	}{
		{name: "simd", fn: scanJSONMasksSIMD},
		{name: "bytes", fn: scanJSONMasksBytes},
		{name: "scalar", fn: scanJSONMasksScalar},
	}

	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(b, file.path, file.compressed)
		counts := countJSONMasks(src)
		for _, implementation := range implementations {
			b.Run(file.name+"/"+implementation.name, func(b *testing.B) {
				b.SetBytes(int64(len(src)))
				b.ReportAllocs()
				for b.Loop() {
					benchmarkJSONMasks = implementation.fn(src)
				}
				b.ReportMetric(float64(counts.Structural), "structural/op")
				b.ReportMetric(float64(counts.Quotes), "quotes/op")
				b.ReportMetric(float64(counts.Slashes), "slashes/op")
			})
		}
	}
}

func BenchmarkJSONStages(b *testing.B) {
	stages := []struct {
		name string
		fn   func([]byte) (Mask, JSONState)
	}{
		{name: "raw_classification", fn: scanJSONRawStage},
		{name: "string_state", fn: scanJSONStringStateStage},
		{name: "escaped_quotes", fn: scanJSONEscapedQuotesStage},
	}

	type dataset struct {
		name string
		src  []byte
	}
	datasets := make([]dataset, 0, len(jsonBenchmarkFiles)+1)
	totalSize := 0
	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(b, file.path, file.compressed)
		datasets = append(datasets, dataset{name: file.name, src: src})
		totalSize += len(src)
	}

	all := make([]byte, 0, totalSize)
	for _, data := range datasets {
		all = append(all, data.src...)
	}
	datasets = append(datasets, dataset{name: "all_fixtures", src: all})

	for _, data := range datasets {
		blockStats := countJSONBlockStats(data.src)
		for _, stage := range stages {
			b.Run(data.name+"/"+stage.name, func(b *testing.B) {
				b.SetBytes(int64(len(data.src)))
				b.ReportAllocs()
				for b.Loop() {
					benchmarkJSONStructural, benchmarkJSONState = stage.fn(data.src)
				}
				b.ReportMetric(float64(blockStats.Blocks), "blocks/op")
				b.ReportMetric(float64(blockStats.Plain), "plain-blocks/op")
				b.ReportMetric(float64(blockStats.WithQuotes), "quote-blocks/op")
				b.ReportMetric(float64(blockStats.WithSlashes), "slash-blocks/op")
			})
		}

		blockCount := (len(data.src) + BlockSize - 1) / BlockSize
		masks, _ := appendJSONStructuralMasks(make([]Mask, 0, blockCount), data.src, JSONState{})
		indexCount := 0
		for _, mask := range masks {
			indexCount += bits.OnesCount64(uint64(mask))
		}

		b.Run(data.name+"/stage1_masks", func(b *testing.B) {
			dst := make([]Mask, 0, blockCount)
			b.SetBytes(int64(len(data.src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructuralMasks, benchmarkJSONState =
					appendJSONStructuralMasks(dst[:0], data.src, JSONState{})
			}
			b.ReportMetric(float64(blockStats.Blocks), "blocks/op")
			b.ReportMetric(float64(blockStats.Plain), "plain-blocks/op")
			b.ReportMetric(float64(blockStats.WithQuotes), "quote-blocks/op")
			b.ReportMetric(float64(blockStats.WithSlashes), "slash-blocks/op")
		})

		b.Run(data.name+"/stage1_indexes", func(b *testing.B) {
			dst := make([]uint32, 0, indexCount)
			b.SetBytes(int64(len(data.src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructuralIndexes, benchmarkJSONState =
					AppendJSONStructuralIndexes(dst[:0], data.src, JSONState{})
			}
			b.ReportMetric(float64(blockStats.Blocks), "blocks/op")
			b.ReportMetric(float64(indexCount), "indexes/op")
			b.ReportMetric(float64(blockStats.Plain), "plain-blocks/op")
			b.ReportMetric(float64(blockStats.WithQuotes), "quote-blocks/op")
			b.ReportMetric(float64(blockStats.WithSlashes), "slash-blocks/op")
		})
	}
}

func BenchmarkJSONStringStateBatching(b *testing.B) {
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
		fn   func([]byte) (Mask, JSONState)
	}{
		{name: "prefixXOR1", fn: scanJSONStringStateStage},
		{name: "prefixXOR2", fn: scanJSONStringStateStage2},
		{name: "prefixXOR4", fn: scanJSONStringStateStage4},
		{name: "prefixXOR8", fn: scanJSONStringStateStage8},
	}
	for _, implementation := range implementations {
		b.Run(implementation.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructural, benchmarkJSONState = implementation.fn(src)
			}
		})
	}
}

func BenchmarkJSONEscapedQuotesBatching(b *testing.B) {
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
		fn   func([]byte) (Mask, JSONState)
	}{
		{name: "prefixXOR1", fn: scanJSONEscapedQuotesStage},
		{name: "prefixXOR4", fn: scanJSONEscapedQuotesStage4},
	}
	for _, implementation := range implementations {
		b.Run(implementation.name, func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructural, benchmarkJSONState = implementation.fn(src)
			}
		})
	}
}

func BenchmarkJSONWholeBufferKernel(b *testing.B) {
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
	blockCount := (len(src) + BlockSize - 1) / BlockSize

	implementations := []struct {
		name string
		fn   func([]Mask, []byte, JSONState) ([]Mask, JSONState)
	}{
		{name: "blockwise", fn: appendJSONStructuralMasksBlockwise},
		{name: "whole_buffer_4x", fn: appendJSONStructuralMasks},
	}
	for _, implementation := range implementations {
		b.Run(implementation.name, func(b *testing.B) {
			dst := make([]Mask, 0, blockCount)
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructuralMasks, benchmarkJSONState = implementation.fn(dst[:0], src, JSONState{})
			}
		})
	}
}

func BenchmarkJSONBlockMasksPatterns(b *testing.B) {
	cases := []struct {
		name  string
		src   []byte
		state JSONState
	}{
		{name: "plain_outside", src: bytes.Repeat([]byte{'a'}, BlockSize)},
		{name: "plain_inside", src: bytes.Repeat([]byte{'a'}, BlockSize), state: JSONState{InString: true}},
		{name: "structural_no_strings", src: repeatToBlock([]byte(`{},[],:0123456789`))},
		{name: "quotes_no_slashes", src: repeatToBlock([]byte(`"abcdefghij","klmnop"`))},
		{name: "isolated_escapes", src: repeatToBlock([]byte(`"a\"b\nc\td"`))},
		{name: "slash_runs", src: repeatToBlock([]byte(`"a\\\\b\\\"c"`))},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			b.SetBytes(int64(len(test.src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructural, benchmarkJSONState = JSONBlockMasks(test.src, test.state)
			}
		})
	}
}

func BenchmarkEscapedJSONBytes(b *testing.B) {
	patterns := []struct {
		name string
		mask Mask
	}{
		{name: "none", mask: 0},
		{name: "one", mask: 1 << 17},
		{name: "isolated", mask: 0x1111111111111111},
		{name: "short_runs", mask: 0x000f00e001c00038},
		{name: "alternating", mask: 0x5555555555555555},
		{name: "dense", mask: ^Mask(0)},
	}

	for _, pattern := range patterns {
		for _, carry := range []bool{false, true} {
			name := pattern.name + "/carry=" + map[bool]string{false: "0", true: "1"}[carry]
			b.Run(name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					benchmarkEscapedMask, benchmarkEscapedCarry = escapedJSONBytes(pattern.mask, carry)
				}
			})
		}
	}
}

func BenchmarkPrefixXOR(b *testing.B) {
	inputs := benchmarkPrefixInputs

	report := func(b *testing.B, masks int) {
		b.ReportMetric(float64(masks), "masks/op")
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*masks), "ns/mask")
	}

	b.Run("prefixXOR1", func(b *testing.B) {
		b.SetBytes(8)
		b.ReportAllocs()
		for b.Loop() {
			benchmarkPrefixMasks[0] = prefixXOR(inputs[0])
		}
		report(b, 1)
	})
	b.Run("prefixXOR2", func(b *testing.B) {
		b.SetBytes(16)
		b.ReportAllocs()
		for b.Loop() {
			benchmarkPrefixMasks[0], benchmarkPrefixMasks[1] = prefixXOR2(inputs[0], inputs[1])
		}
		report(b, 2)
	})
	b.Run("prefixXOR4", func(b *testing.B) {
		b.SetBytes(32)
		b.ReportAllocs()
		for b.Loop() {
			benchmarkPrefixMasks[0], benchmarkPrefixMasks[1], benchmarkPrefixMasks[2], benchmarkPrefixMasks[3] =
				prefixXOR4(inputs[0], inputs[1], inputs[2], inputs[3])
		}
		report(b, 4)
	})
	b.Run("prefixXOR8", func(b *testing.B) {
		b.SetBytes(64)
		b.ReportAllocs()
		for b.Loop() {
			benchmarkPrefixMasks[0], benchmarkPrefixMasks[1], benchmarkPrefixMasks[2], benchmarkPrefixMasks[3],
				benchmarkPrefixMasks[4], benchmarkPrefixMasks[5], benchmarkPrefixMasks[6], benchmarkPrefixMasks[7] =
				prefixXOR8(inputs[0], inputs[1], inputs[2], inputs[3], inputs[4], inputs[5], inputs[6], inputs[7])
		}
		report(b, 8)
	})
}

func prefixXOR2(a, b Mask) (Mask, Mask) {
	a ^= a << 1
	b ^= b << 1
	a ^= a << 2
	b ^= b << 2
	a ^= a << 4
	b ^= b << 4
	a ^= a << 8
	b ^= b << 8
	a ^= a << 16
	b ^= b << 16
	a ^= a << 32
	b ^= b << 32
	return a, b
}

func prefixXOR8(a, b, c, d, e, f, g, h Mask) (Mask, Mask, Mask, Mask, Mask, Mask, Mask, Mask) {
	a ^= a << 1
	b ^= b << 1
	c ^= c << 1
	d ^= d << 1
	e ^= e << 1
	f ^= f << 1
	g ^= g << 1
	h ^= h << 1
	a ^= a << 2
	b ^= b << 2
	c ^= c << 2
	d ^= d << 2
	e ^= e << 2
	f ^= f << 2
	g ^= g << 2
	h ^= h << 2
	a ^= a << 4
	b ^= b << 4
	c ^= c << 4
	d ^= d << 4
	e ^= e << 4
	f ^= f << 4
	g ^= g << 4
	h ^= h << 4
	a ^= a << 8
	b ^= b << 8
	c ^= c << 8
	d ^= d << 8
	e ^= e << 8
	f ^= f << 8
	g ^= g << 8
	h ^= h << 8
	a ^= a << 16
	b ^= b << 16
	c ^= c << 16
	d ^= d << 16
	e ^= e << 16
	f ^= f << 16
	g ^= g << 16
	h ^= h << 16
	a ^= a << 32
	b ^= b << 32
	c ^= c << 32
	d ^= d << 32
	e ^= e << 32
	f ^= f << 32
	g ^= g << 32
	h ^= h << 32
	return a, b, c, d, e, f, g, h
}

// BenchmarkMask8sToMask isolates the portable mask-materialization baseline.
// BenchmarkMask8sToMaskProduction measures the production backend.
func BenchmarkMask8sToMask(b *testing.B) {
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
				benchmarkMaskConversion = mask8sToMaskPortableBenchmark(mask, &words, wordCount)
			}
			b.ReportMetric(float64(width*8), "vector-bits")
		})
	}
}

func mask8sToMaskPortableBenchmark(
	mask simd.Mask8s,
	words *[maxVectorBytes / 8]uint64,
	wordCount int,
) Mask {
	mask.ToInt8s().ToBits().ReshapeToUint64s().Store(words[:wordCount])

	const (
		byteLowBits   = uint64(0x0101010101010101)
		compressBytes = uint64(0x0102040810204080)
	)
	var result Mask
	for i, word := range words[:wordCount] {
		bits := ((word & byteLowBits) * compressBytes) >> 56
		result |= Mask(bits) << (8 * i)
	}
	return result
}

func scanJSONMasks3Portable(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasks3Portable(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func rawJSONBlockMasks3Portable(src []byte, targets jsonSIMDTargets) RawMasks {
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64
	var result RawMasks
	offset := 0
	for ; offset+width <= len(src); offset += width {
		vector = simd.LoadUint8s(src[offset : offset+width])
		appendJSON3PortableMasks(&result, vector, targets, &words, wordCount, offset)
	}
	if offset < len(src) {
		vector, _ = simd.LoadUint8sPart(src[offset:])
		appendJSON3PortableMasks(&result, vector, targets, &words, wordCount, offset)
	}

	valid := maskForLength(len(src))
	result.Structural &= valid
	result.Quotes &= valid
	result.Slashes &= valid
	return result
}

func appendJSON3PortableMasks(
	result *RawMasks,
	vector simd.Uint8s,
	targets jsonSIMDTargets,
	words *[maxVectorBytes / 8]uint64,
	wordCount int,
	offset int,
) {
	folded := vector.Or(targets.structuralFold)
	structural := folded.Equal(targets.openFolded).
		Or(folded.Equal(targets.closeFolded)).
		Or(vector.Equal(targets.comma)).
		Or(vector.Equal(targets.colon))
	result.Structural |= mask8sToMaskPortableBenchmark(structural, words, wordCount) << offset
	result.Quotes |= mask8sToMaskPortableBenchmark(vector.Equal(targets.quote), words, wordCount) << offset
	result.Slashes |= mask8sToMaskPortableBenchmark(vector.Equal(targets.slash), words, wordCount) << offset
}

func scanJSONMasks2Portable(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasks2Portable(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func rawJSONBlockMasks2Portable(src []byte, targets jsonSIMDTargets) RawMasks {
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64
	var result RawMasks
	offset := 0
	for ; offset+width <= len(src); offset += width {
		vector = simd.LoadUint8s(src[offset : offset+width])
		appendJSON2PortablePlanes(&result, vector, targets, &words, wordCount, offset)
	}
	if offset < len(src) {
		vector, _ = simd.LoadUint8sPart(src[offset:])
		appendJSON2PortablePlanes(&result, vector, targets, &words, wordCount, offset)
	}

	valid := maskForLength(len(src))
	result.Structural &= valid
	result.Quotes &= valid
	result.Slashes &= valid
	return result
}

func appendJSON2PortablePlanes(
	result *RawMasks,
	vector simd.Uint8s,
	targets jsonSIMDTargets,
	words *[maxVectorBytes / 8]uint64,
	wordCount int,
	offset int,
) {
	folded := vector.Or(targets.structuralFold)
	structural := folded.Equal(targets.openFolded).
		Or(folded.Equal(targets.closeFolded)).
		Or(vector.Equal(targets.comma)).
		Or(vector.Equal(targets.colon))
	quotes := vector.Equal(targets.quote)
	slashes := vector.Equal(targets.slash)

	plane0 := mask8sToMaskPortableBenchmark(structural.Or(slashes), words, wordCount)
	plane1 := mask8sToMaskPortableBenchmark(quotes.Or(slashes), words, wordCount)
	result.Slashes |= (plane0 & plane1) << offset
	result.Structural |= (plane0 &^ plane1) << offset
	result.Quotes |= (plane1 &^ plane0) << offset
}

func scanJSONMasksSIMD(src []byte) RawMasks {
	targets := makeJSONSIMDTargets()

	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := rawJSONBlockMasks(src[offset:end], targets)
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func scanJSONRawStage(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		raw := rawJSONBlockMasks(src[offset:end], targets)
		result ^= raw.Structural ^ raw.Quotes ^ raw.Slashes
	}
	return result, JSONState{}
}

func scanJSONStringStateStage(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	var state JSONState
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		raw := rawJSONBlockMasks(src[offset:end], targets)
		insideString, next := jsonStringMaskWithoutEscapes(raw, end-offset, state)
		result ^= raw.Structural &^ insideString
		state = next
	}
	return result, state
}

func scanJSONStringStateStage2(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	var state JSONState
	offset := 0
	for ; offset+2*BlockSize <= len(src); offset += 2 * BlockSize {
		raw0 := rawJSONBlockMasks(src[offset:offset+BlockSize], targets)
		raw1 := rawJSONBlockMasks(src[offset+BlockSize:offset+2*BlockSize], targets)
		prefix0, prefix1 := prefixXOR2(raw0.Quotes, raw1.Quotes)
		result ^= applyJSONStringPrefix(raw0, BlockSize, prefix0, &state)
		result ^= applyJSONStringPrefix(raw1, BlockSize, prefix1, &state)
	}
	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		raw := rawJSONBlockMasks(src[offset:end], targets)
		insideString, next := jsonStringMaskWithoutEscapes(raw, end-offset, state)
		result ^= raw.Structural &^ insideString
		state = next
	}
	return result, state
}

func scanJSONStringStateStage4(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	var state JSONState
	offset := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		raw0 := rawJSONBlockMasks(src[offset:offset+BlockSize], targets)
		raw1 := rawJSONBlockMasks(src[offset+BlockSize:offset+2*BlockSize], targets)
		raw2 := rawJSONBlockMasks(src[offset+2*BlockSize:offset+3*BlockSize], targets)
		raw3 := rawJSONBlockMasks(src[offset+3*BlockSize:offset+4*BlockSize], targets)
		prefix0, prefix1, prefix2, prefix3 := prefixXOR4(raw0.Quotes, raw1.Quotes, raw2.Quotes, raw3.Quotes)
		result ^= applyJSONStringPrefix(raw0, BlockSize, prefix0, &state)
		result ^= applyJSONStringPrefix(raw1, BlockSize, prefix1, &state)
		result ^= applyJSONStringPrefix(raw2, BlockSize, prefix2, &state)
		result ^= applyJSONStringPrefix(raw3, BlockSize, prefix3, &state)
	}
	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		raw := rawJSONBlockMasks(src[offset:end], targets)
		insideString, next := jsonStringMaskWithoutEscapes(raw, end-offset, state)
		result ^= raw.Structural &^ insideString
		state = next
	}
	return result, state
}

func scanJSONStringStateStage8(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	var state JSONState
	offset := 0
	for ; offset+8*BlockSize <= len(src); offset += 8 * BlockSize {
		raw0 := rawJSONBlockMasks(src[offset:offset+BlockSize], targets)
		raw1 := rawJSONBlockMasks(src[offset+BlockSize:offset+2*BlockSize], targets)
		raw2 := rawJSONBlockMasks(src[offset+2*BlockSize:offset+3*BlockSize], targets)
		raw3 := rawJSONBlockMasks(src[offset+3*BlockSize:offset+4*BlockSize], targets)
		raw4 := rawJSONBlockMasks(src[offset+4*BlockSize:offset+5*BlockSize], targets)
		raw5 := rawJSONBlockMasks(src[offset+5*BlockSize:offset+6*BlockSize], targets)
		raw6 := rawJSONBlockMasks(src[offset+6*BlockSize:offset+7*BlockSize], targets)
		raw7 := rawJSONBlockMasks(src[offset+7*BlockSize:offset+8*BlockSize], targets)
		prefix0, prefix1, prefix2, prefix3, prefix4, prefix5, prefix6, prefix7 := prefixXOR8(
			raw0.Quotes,
			raw1.Quotes,
			raw2.Quotes,
			raw3.Quotes,
			raw4.Quotes,
			raw5.Quotes,
			raw6.Quotes,
			raw7.Quotes,
		)
		result ^= applyJSONStringPrefix(raw0, BlockSize, prefix0, &state)
		result ^= applyJSONStringPrefix(raw1, BlockSize, prefix1, &state)
		result ^= applyJSONStringPrefix(raw2, BlockSize, prefix2, &state)
		result ^= applyJSONStringPrefix(raw3, BlockSize, prefix3, &state)
		result ^= applyJSONStringPrefix(raw4, BlockSize, prefix4, &state)
		result ^= applyJSONStringPrefix(raw5, BlockSize, prefix5, &state)
		result ^= applyJSONStringPrefix(raw6, BlockSize, prefix6, &state)
		result ^= applyJSONStringPrefix(raw7, BlockSize, prefix7, &state)
	}
	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		raw := rawJSONBlockMasks(src[offset:end], targets)
		insideString, next := jsonStringMaskWithoutEscapes(raw, end-offset, state)
		result ^= raw.Structural &^ insideString
		state = next
	}
	return result, state
}

func applyJSONStringPrefix(raw RawMasks, length int, prefix Mask, state *JSONState) Mask {
	structural := applyJSONPrefix(raw, length, raw.Quotes, prefix, &state.InString)
	state.Escaped = false
	return structural
}

func applyJSONPrefix(raw RawMasks, length int, quotes, prefix Mask, inString *bool) Mask {
	insideString := prefix
	if *inString {
		insideString = ^insideString
	}
	insideString &= maskForLength(length)
	insideString &^= quotes

	if prefix&(Mask(1)<<63) != 0 {
		*inString = !*inString
	}
	return raw.Structural &^ insideString
}

func scanJSONEscapedQuotesStage(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	var state JSONState
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		structural, next := jsonBlockMasks(src[offset:end], state, targets)
		result ^= structural
		state = next
	}
	return result, state
}

func scanJSONEscapedQuotesStage4(src []byte) (Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	var result Mask
	var state JSONState
	offset := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		raw0 := rawJSONBlockMasks(src[offset:offset+BlockSize], targets)
		raw1 := rawJSONBlockMasks(src[offset+BlockSize:offset+2*BlockSize], targets)
		raw2 := rawJSONBlockMasks(src[offset+2*BlockSize:offset+3*BlockSize], targets)
		raw3 := rawJSONBlockMasks(src[offset+3*BlockSize:offset+4*BlockSize], targets)

		quotes0, escaped0 := unescapedJSONQuotes(raw0, BlockSize, state.Escaped)
		quotes1, escaped1 := unescapedJSONQuotes(raw1, BlockSize, escaped0)
		quotes2, escaped2 := unescapedJSONQuotes(raw2, BlockSize, escaped1)
		quotes3, escaped3 := unescapedJSONQuotes(raw3, BlockSize, escaped2)
		prefix0, prefix1, prefix2, prefix3 := prefixXOR4(quotes0, quotes1, quotes2, quotes3)

		result ^= applyJSONPrefix(raw0, BlockSize, quotes0, prefix0, &state.InString)
		result ^= applyJSONPrefix(raw1, BlockSize, quotes1, prefix1, &state.InString)
		result ^= applyJSONPrefix(raw2, BlockSize, quotes2, prefix2, &state.InString)
		result ^= applyJSONPrefix(raw3, BlockSize, quotes3, prefix3, &state.InString)
		state.Escaped = escaped3
	}
	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		structural, next := jsonBlockMasks(src[offset:end], state, targets)
		result ^= structural
		state = next
	}
	return result, state
}

func appendJSONStructuralMasksBlockwise(dst []Mask, src []byte, state JSONState) ([]Mask, JSONState) {
	targets := makeJSONSIMDTargets()
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		var structural Mask
		structural, state = jsonBlockMasks(src[offset:end], state, targets)
		dst = append(dst, structural)
	}
	return dst, state
}

func scanJSONMasksBytes(src []byte) RawMasks {
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := matchJSONBlockBytes(src[offset:end])
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func matchJSONBlockBytes(src []byte) RawMasks {
	var result RawMasks
	for base := 0; base < len(src); {
		index := bytes.IndexAny(src[base:], "{}[],:")
		if index < 0 {
			break
		}
		index += base
		result.Structural |= Mask(1) << index
		base = index + 1
	}
	for base := 0; base < len(src); {
		index := bytes.IndexByte(src[base:], '"')
		if index < 0 {
			break
		}
		index += base
		result.Quotes |= Mask(1) << index
		base = index + 1
	}
	for base := 0; base < len(src); {
		index := bytes.IndexByte(src[base:], '\\')
		if index < 0 {
			break
		}
		index += base
		result.Slashes |= Mask(1) << index
		base = index + 1
	}
	return result
}

func scanJSONMasksScalar(src []byte) RawMasks {
	var result RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := matchJSONBlockScalar(src[offset:end])
		result.Structural ^= masks.Structural
		result.Quotes ^= masks.Quotes
		result.Slashes ^= masks.Slashes
	}
	return result
}

func matchJSONBlockScalar(src []byte) RawMasks {
	var result RawMasks
	for i, value := range src {
		bit := Mask(1) << i
		switch value {
		case '{', '}', '[', ']', ',', ':':
			result.Structural |= bit
		case '"':
			result.Quotes |= bit
		case '\\':
			result.Slashes |= bit
		}
	}
	return result
}

func countJSONMasks(src []byte) RawMasks {
	var counts RawMasks
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		masks := matchJSONBlockScalar(src[offset:end])
		counts.Structural += Mask(bits.OnesCount64(uint64(masks.Structural)))
		counts.Quotes += Mask(bits.OnesCount64(uint64(masks.Quotes)))
		counts.Slashes += Mask(bits.OnesCount64(uint64(masks.Slashes)))
	}
	return counts
}

type jsonBlockStats struct {
	Blocks      int
	Plain       int
	WithQuotes  int
	WithSlashes int
}

func countJSONBlockStats(src []byte) jsonBlockStats {
	var stats jsonBlockStats
	for offset := 0; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		raw := matchJSONBlockScalar(src[offset:end])
		stats.Blocks++
		if raw.Quotes == 0 && raw.Slashes == 0 {
			stats.Plain++
		}
		if raw.Quotes != 0 {
			stats.WithQuotes++
		}
		if raw.Slashes != 0 {
			stats.WithSlashes++
		}
	}
	return stats
}

func repeatToBlock(pattern []byte) []byte {
	dst := make([]byte, BlockSize)
	for i := range dst {
		dst[i] = pattern[i%len(pattern)]
	}
	return dst
}

func readJSONBenchmarkFile(tb testing.TB, path string, compressed bool) []byte {
	tb.Helper()
	file, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	defer file.Close()

	var reader io.Reader = file
	if compressed {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			tb.Fatal(err)
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	src, err := io.ReadAll(reader)
	if err != nil {
		tb.Fatal(err)
	}
	return src
}

func TestJSONMaskImplementations(t *testing.T) {
	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(t, file.path, file.compressed)
		targets := makeJSONSIMDTargets()
		var state JSONState
		var wantState JSONState
		for offset := 0; offset < len(src); offset += BlockSize {
			end := min(offset+BlockSize, len(src))
			block := src[offset:end]
			want := matchJSONBlockScalar(block)
			if got := matchJSONBlockBytes(block); got != want {
				t.Fatalf("%s block %d bytes: got %+v, want %+v", file.name, offset, got, want)
			}

			if got := rawJSONBlockMasks(block, targets); got != want {
				t.Fatalf("%s block %d SIMD: got %+v, want %+v", file.name, offset, got, want)
			}

			wantStructural, nextWantState := jsonBlockMasksScalarReference(block, wantState)
			gotStructural, nextState := jsonBlockMasks(block, state, targets)
			if gotStructural != wantStructural || nextState != nextWantState {
				t.Fatalf(
					"%s block %d JSON state: got (%#x, %+v), want (%#x, %+v)",
					file.name, offset, gotStructural, nextState, wantStructural, nextWantState,
				)
			}
			state = nextState
			wantState = nextWantState
		}
	}
}

func TestPrefixXORBatches(t *testing.T) {
	inputs := [8]Mask{
		0,
		1,
		Mask(1) << 63,
		^Mask(0),
		0xaaaaaaaaaaaaaaaa,
		0x5555555555555555,
		0x0123456789abcdef,
		0xfedcba9876543210,
	}
	want := inputs
	for i := range want {
		want[i] = prefixXOR(want[i])
	}

	got2a, got2b := prefixXOR2(inputs[0], inputs[1])
	if got2a != want[0] || got2b != want[1] {
		t.Fatalf("prefixXOR2 = (%#x, %#x), want (%#x, %#x)", got2a, got2b, want[0], want[1])
	}

	got4a, got4b, got4c, got4d := prefixXOR4(inputs[0], inputs[1], inputs[2], inputs[3])
	if got4a != want[0] || got4b != want[1] || got4c != want[2] || got4d != want[3] {
		t.Fatalf("prefixXOR4 mismatch: got (%#x, %#x, %#x, %#x)", got4a, got4b, got4c, got4d)
	}

	got8a, got8b, got8c, got8d, got8e, got8f, got8g, got8h := prefixXOR8(
		inputs[0], inputs[1], inputs[2], inputs[3], inputs[4], inputs[5], inputs[6], inputs[7],
	)
	got8 := [...]Mask{got8a, got8b, got8c, got8d, got8e, got8f, got8g, got8h}
	if got8 != want {
		t.Fatalf("prefixXOR8 = %#x, want %#x", got8, want)
	}
}

func TestJSONStringStateBatching(t *testing.T) {
	implementations := []struct {
		name string
		fn   func([]byte) (Mask, JSONState)
	}{
		{name: "prefixXOR2", fn: scanJSONStringStateStage2},
		{name: "prefixXOR4", fn: scanJSONStringStateStage4},
		{name: "prefixXOR8", fn: scanJSONStringStateStage8},
	}

	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(t, file.path, file.compressed)
		wantMask, wantState := scanJSONStringStateStage(src)
		for _, implementation := range implementations {
			gotMask, gotState := implementation.fn(src)
			if gotMask != wantMask || gotState != wantState {
				t.Fatalf(
					"%s/%s: got (%#x, %+v), want (%#x, %+v)",
					file.name,
					implementation.name,
					gotMask,
					gotState,
					wantMask,
					wantState,
				)
			}
		}
	}
}

func TestJSONEscapedQuotesBatching(t *testing.T) {
	for _, file := range jsonBenchmarkFiles {
		src := readJSONBenchmarkFile(t, file.path, file.compressed)
		wantMask, wantState := scanJSONEscapedQuotesStage(src)
		gotMask, gotState := scanJSONEscapedQuotesStage4(src)
		if gotMask != wantMask || gotState != wantState {
			t.Fatalf(
				"%s: got (%#x, %+v), want (%#x, %+v)",
				file.name,
				gotMask,
				gotState,
				wantMask,
				wantState,
			)
		}
	}
}
