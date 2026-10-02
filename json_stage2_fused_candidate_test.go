package simdscan

import (
	"encoding/json"
	"math/bits"
	"simd"
	"testing"

	"github.com/dnonakolesax/simdscan/internal/stage2bench"
	"github.com/mailru/easyjson"
)

var (
	benchmarkFusedDirectRecord stage2bench.LargeStrings
	benchmarkFusedDirectError  error
)

// parseGeneratedStage2FusedDirect is an intentionally monolithic generated
// decoder experiment for LargeStrings. The four structural masks produced by
// each 256-byte classification batch are consumed while they are still local;
// the decoder does not materialize a []Mask or []uint32 tape.
//
// Like the other generated stage 2 prototypes, this decoder accepts the compact
// flat-object encoding produced by the benchmark fixture. It is not a generic
// JSON decoder.
func parseGeneratedStage2FusedDirect(src []byte, dst *stage2bench.LargeStrings) error {
	*dst = stage2bench.LargeStrings{}

	targets := makeJSONSIMDTargets()
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64

	var jsonState JSONState
	parserState := uint8(0) // 0: object open, 1: colon, 2: comma/object close.
	fieldStart := 0
	colon := 0

	offset := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		var structural0, structural1, structural2, structural3 Mask
		var quotes0, quotes1, quotes2, quotes3 Mask
		var slashes0, slashes1, slashes2, slashes3 Mask

		// This is the production 4x classifier kept inside the generated
		// decoder so its scalar outputs can flow directly into stage 2.
		for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
			start := offset + vectorOffset
			vector = simd.LoadUint8s(src[start : start+width])
			plane0, plane1 := jsonVectorPlanes(vector, targets)
			bits0 := mask8sToMask(plane0, &words, wordCount)
			bits1 := mask8sToMask(plane1, &words, wordCount)
			slashes0 |= (bits0 & bits1) << vectorOffset
			structural0 |= (bits0 &^ bits1) << vectorOffset
			quotes0 |= (bits1 &^ bits0) << vectorOffset
		}
		for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
			start := offset + BlockSize + vectorOffset
			vector = simd.LoadUint8s(src[start : start+width])
			plane0, plane1 := jsonVectorPlanes(vector, targets)
			bits0 := mask8sToMask(plane0, &words, wordCount)
			bits1 := mask8sToMask(plane1, &words, wordCount)
			slashes1 |= (bits0 & bits1) << vectorOffset
			structural1 |= (bits0 &^ bits1) << vectorOffset
			quotes1 |= (bits1 &^ bits0) << vectorOffset
		}
		for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
			start := offset + 2*BlockSize + vectorOffset
			vector = simd.LoadUint8s(src[start : start+width])
			plane0, plane1 := jsonVectorPlanes(vector, targets)
			bits0 := mask8sToMask(plane0, &words, wordCount)
			bits1 := mask8sToMask(plane1, &words, wordCount)
			slashes2 |= (bits0 & bits1) << vectorOffset
			structural2 |= (bits0 &^ bits1) << vectorOffset
			quotes2 |= (bits1 &^ bits0) << vectorOffset
		}
		for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
			start := offset + 3*BlockSize + vectorOffset
			vector = simd.LoadUint8s(src[start : start+width])
			plane0, plane1 := jsonVectorPlanes(vector, targets)
			bits0 := mask8sToMask(plane0, &words, wordCount)
			bits1 := mask8sToMask(plane1, &words, wordCount)
			slashes3 |= (bits0 & bits1) << vectorOffset
			structural3 |= (bits0 &^ bits1) << vectorOffset
			quotes3 |= (bits1 &^ bits0) << vectorOffset
		}

		escapedCarry := jsonState.Escaped
		if slashes0 == 0 {
			if escapedCarry {
				quotes0 &^= 1
			}
			escapedCarry = false
		} else {
			escaped, next := escapedJSONBytes(slashes0, escapedCarry)
			quotes0 &^= escaped
			escapedCarry = next
		}
		if slashes1 == 0 {
			if escapedCarry {
				quotes1 &^= 1
			}
			escapedCarry = false
		} else {
			escaped, next := escapedJSONBytes(slashes1, escapedCarry)
			quotes1 &^= escaped
			escapedCarry = next
		}
		if slashes2 == 0 {
			if escapedCarry {
				quotes2 &^= 1
			}
			escapedCarry = false
		} else {
			escaped, next := escapedJSONBytes(slashes2, escapedCarry)
			quotes2 &^= escaped
			escapedCarry = next
		}
		if slashes3 == 0 {
			if escapedCarry {
				quotes3 &^= 1
			}
			escapedCarry = false
		} else {
			escaped, next := escapedJSONBytes(slashes3, escapedCarry)
			quotes3 &^= escaped
			escapedCarry = next
		}

		prefix0, prefix1, prefix2, prefix3 := prefixXOR4(quotes0, quotes1, quotes2, quotes3)

		inside0 := prefix0
		if jsonState.InString {
			inside0 = ^inside0
		}
		inString1 := jsonState.InString != (prefix0&(Mask(1)<<63) != 0)
		inside1 := prefix1
		if inString1 {
			inside1 = ^inside1
		}
		inString2 := inString1 != (prefix1&(Mask(1)<<63) != 0)
		inside2 := prefix2
		if inString2 {
			inside2 = ^inside2
		}
		inString3 := inString2 != (prefix2&(Mask(1)<<63) != 0)
		inside3 := prefix3
		if inString3 {
			inside3 = ^inside3
		}

		// Walk the four local masks through one generated state-machine body.
		// The switch only selects the next local mask four times per batch; it
		// avoids both a callback and four copies of the parser's cold checks.
		mask := structural0 &^ inside0
		maskBase := offset
		nextMask := uint8(1)
		for {
			for mask != 0 {
				position := maskBase + bits.TrailingZeros64(uint64(mask))
				mask &= mask - 1
				switch parserState {
				case 0:
					if src[position] != '{' {
						return errGeneratedStage2Syntax
					}
					fieldStart = position + 1
					parserState = 1
				case 1:
					if src[position] != ':' {
						return errGeneratedStage2Syntax
					}
					colon = position
					parserState = 2
				case 2:
					end := src[position]
					if end != ',' && end != '}' {
						return errGeneratedStage2Syntax
					}
					if err := decodeGeneratedStage2Field(dst, src[fieldStart:colon], src[colon+1:position]); err != nil {
						return err
					}
					if end == '}' {
						return nil
					}
					fieldStart = position + 1
					parserState = 1
				}
			}

			switch nextMask {
			case 1:
				mask = structural1 &^ inside1
				maskBase = offset + BlockSize
				nextMask = 2
			case 2:
				mask = structural2 &^ inside2
				maskBase = offset + 2*BlockSize
				nextMask = 3
			case 3:
				mask = structural3 &^ inside3
				maskBase = offset + 3*BlockSize
				nextMask = 4
			default:
				goto masksConsumed
			}
		}

	masksConsumed:

		jsonState.InString = inString3 != (prefix3&(Mask(1)<<63) != 0)
		jsonState.Escaped = escapedCarry
	}

	// Match the production 4x kernel's tail path. No tape is materialized: each
	// tail mask is consumed before the next block is classified.
	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		mask, next := jsonBlockMasks(src[offset:end], jsonState, targets)
		jsonState = next
		for mask != 0 {
			position := offset + bits.TrailingZeros64(uint64(mask))
			mask &= mask - 1
			switch parserState {
			case 0:
				if src[position] != '{' {
					return errGeneratedStage2Syntax
				}
				fieldStart = position + 1
				parserState = 1
			case 1:
				if src[position] != ':' {
					return errGeneratedStage2Syntax
				}
				colon = position
				parserState = 2
			case 2:
				valueEnd := src[position]
				if valueEnd != ',' && valueEnd != '}' {
					return errGeneratedStage2Syntax
				}
				if err := decodeGeneratedStage2Field(dst, src[fieldStart:colon], src[colon+1:position]); err != nil {
					return err
				}
				if valueEnd == '}' {
					return nil
				}
				fieldStart = position + 1
				parserState = 1
			}
		}
	}

	return errGeneratedStage2Syntax
}

func TestJSONStage2FusedDirect(t *testing.T) {
	want, src := generatedLargeStringsFixture(t)
	var got stage2bench.LargeStrings
	if err := parseGeneratedStage2FusedDirect(src, &got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("decoded %+v, want %+v", got, want)
	}

	if err := parseGeneratedStage2FusedDirect(src[:len(src)-1], &got); err == nil {
		t.Fatal("truncated input unexpectedly decoded successfully")
	}
}

func BenchmarkJSONStage2Fusion(b *testing.B) {
	want, src := generatedLargeStringsFixture(b)
	variants := []struct {
		name  string
		parse func([]byte, *stage2bench.LargeStrings) error
	}{
		{name: "fused_masks", parse: parseGeneratedStage2FusedMasks},
		{name: "fused_indexes", parse: parseGeneratedStage2FusedIndexes},
		{name: "fused_direct", parse: parseGeneratedStage2FusedDirect},
	}
	for _, variant := range variants {
		var got stage2bench.LargeStrings
		if err := variant.parse(src, &got); err != nil {
			b.Fatalf("%s: %v", variant.name, err)
		}
		if got != want {
			b.Fatalf("%s decoded %+v, want %+v", variant.name, got, want)
		}
	}

	blockCount := (len(src) + BlockSize - 1) / BlockSize
	masks, _ := appendJSONStructuralMasks(make([]Mask, 0, blockCount), src, JSONState{})
	indexCount := 0
	for _, mask := range masks {
		indexCount += bits.OnesCount64(uint64(mask))
	}

	setup := func(b *testing.B) {
		b.Helper()
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
	}
	report := func(b *testing.B) {
		b.Helper()
		b.ReportMetric(float64(blockCount), "blocks/op")
		b.ReportMetric(float64(indexCount), "structurals/op")
	}

	b.Run("materialized_masks", func(b *testing.B) {
		dst := make([]Mask, 0, blockCount)
		setup(b)
		for b.Loop() {
			dst, benchmarkJSONState = appendJSONStructuralMasks(dst[:0], src, JSONState{})
			benchmarkStage2Error = parseGeneratedStage2Masks(src, dst, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("materialized_indexes", func(b *testing.B) {
		dst := make([]uint32, 0, indexCount)
		setup(b)
		for b.Loop() {
			dst, benchmarkJSONState = AppendJSONStructuralIndexes(dst[:0], src, JSONState{})
			benchmarkStage2Error = parseGeneratedStage2Indexes(src, dst, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("fused_masks", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkFusedDirectError = parseGeneratedStage2FusedMasks(src, &benchmarkFusedDirectRecord)
		}
		report(b)
	})

	b.Run("fused_indexes", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkFusedDirectError = parseGeneratedStage2FusedIndexes(src, &benchmarkFusedDirectRecord)
		}
		report(b)
	})

	b.Run("fused_direct", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkFusedDirectError = parseGeneratedStage2FusedDirect(src, &benchmarkFusedDirectRecord)
		}
		report(b)
	})

	b.Run("easyjson", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkFusedDirectRecord = stage2bench.LargeStrings{}
			benchmarkFusedDirectError = easyjson.Unmarshal(src, &benchmarkFusedDirectRecord)
		}
		report(b)
	})

	b.Run("encoding_json", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkFusedDirectRecord = stage2bench.LargeStrings{}
			benchmarkFusedDirectError = json.Unmarshal(src, &benchmarkFusedDirectRecord)
		}
		report(b)
	})
}
