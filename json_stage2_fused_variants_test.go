package simdscan

import (
	"math/bits"
	"simd"
	"strings"
	"testing"

	"github.com/dnonakolesax/simdscan/internal/stage2bench"
	"github.com/mailru/easyjson"
)

// classifyGeneratedStage2JSON4 is the benchmark-only function-boundary
// counterpart to the production whole-buffer kernel. It classifies exactly
// four complete logical blocks and returns their outside-string structural
// masks without materializing a tape.
func classifyGeneratedStage2JSON4(
	src []byte,
	state JSONState,
	targets jsonSIMDTargets,
) (Mask, Mask, Mask, Mask, JSONState) {
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64

	var structural0, structural1, structural2, structural3 Mask
	var quotes0, quotes1, quotes2, quotes3 Mask
	var slashes0, slashes1, slashes2, slashes3 Mask

	for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
		vector = simd.LoadUint8s(src[vectorOffset : vectorOffset+width])
		plane0, plane1 := jsonVectorPlanes(vector, targets)
		bits0 := mask8sToMask(plane0, &words, wordCount)
		bits1 := mask8sToMask(plane1, &words, wordCount)
		slashes0 |= (bits0 & bits1) << vectorOffset
		structural0 |= (bits0 &^ bits1) << vectorOffset
		quotes0 |= (bits1 &^ bits0) << vectorOffset
	}
	for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
		start := BlockSize + vectorOffset
		vector = simd.LoadUint8s(src[start : start+width])
		plane0, plane1 := jsonVectorPlanes(vector, targets)
		bits0 := mask8sToMask(plane0, &words, wordCount)
		bits1 := mask8sToMask(plane1, &words, wordCount)
		slashes1 |= (bits0 & bits1) << vectorOffset
		structural1 |= (bits0 &^ bits1) << vectorOffset
		quotes1 |= (bits1 &^ bits0) << vectorOffset
	}
	for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
		start := 2*BlockSize + vectorOffset
		vector = simd.LoadUint8s(src[start : start+width])
		plane0, plane1 := jsonVectorPlanes(vector, targets)
		bits0 := mask8sToMask(plane0, &words, wordCount)
		bits1 := mask8sToMask(plane1, &words, wordCount)
		slashes2 |= (bits0 & bits1) << vectorOffset
		structural2 |= (bits0 &^ bits1) << vectorOffset
		quotes2 |= (bits1 &^ bits0) << vectorOffset
	}
	for vectorOffset := 0; vectorOffset < BlockSize; vectorOffset += width {
		start := 3*BlockSize + vectorOffset
		vector = simd.LoadUint8s(src[start : start+width])
		plane0, plane1 := jsonVectorPlanes(vector, targets)
		bits0 := mask8sToMask(plane0, &words, wordCount)
		bits1 := mask8sToMask(plane1, &words, wordCount)
		slashes3 |= (bits0 & bits1) << vectorOffset
		structural3 |= (bits0 &^ bits1) << vectorOffset
		quotes3 |= (bits1 &^ bits0) << vectorOffset
	}

	escapedCarry := state.Escaped
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
	if state.InString {
		inside0 = ^inside0
	}
	inString1 := state.InString != (prefix0&(Mask(1)<<63) != 0)
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

	return structural0 &^ inside0,
		structural1 &^ inside1,
		structural2 &^ inside2,
		structural3 &^ inside3,
		JSONState{
			InString: inString3 != (prefix3&(Mask(1)<<63) != 0),
			Escaped:  escapedCarry,
		}
}

// parseGeneratedStage2FusedMasks keeps each four-mask batch in scalar locals.
// Stage 2 pulls structural positions from those locals through a small cursor;
// no []Mask tape or per-token callback is involved.
func parseGeneratedStage2FusedMasks(src []byte, dst *stage2bench.LargeStrings) error {
	*dst = stage2bench.LargeStrings{}

	targets := makeJSONSIMDTargets()
	var jsonState JSONState
	parserState := uint8(0) // 0: object open, 1: colon, 2: comma/object close.
	fieldStart := 0
	colon := 0

	offset := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		mask0, mask1, mask2, mask3, next := classifyGeneratedStage2JSON4(
			src[offset:offset+4*BlockSize], jsonState, targets,
		)
		jsonState = next

		mask := mask0
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
				mask = mask1
				maskBase = offset + BlockSize
				nextMask = 2
			case 2:
				mask = mask2
				maskBase = offset + 2*BlockSize
				nextMask = 3
			case 3:
				mask = mask3
				maskBase = offset + 3*BlockSize
				nextMask = 4
			default:
				goto masksConsumed
			}
		}

	masksConsumed:
	}

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

// parseGeneratedStage2FusedIndexes extracts positions from each local mask and
// immediately pushes them through the generated parser state machine. It does
// not materialize the positions as a []uint32 tape.
func parseGeneratedStage2FusedIndexes(src []byte, dst *stage2bench.LargeStrings) error {
	*dst = stage2bench.LargeStrings{}

	targets := makeJSONSIMDTargets()
	var jsonState JSONState
	parserState := uint8(0) // 0: object open, 1: colon, 2: comma/object close.
	fieldStart := 0
	colon := 0

	offset := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		mask0, mask1, mask2, mask3, next := classifyGeneratedStage2JSON4(
			src[offset:offset+4*BlockSize], jsonState, targets,
		)
		jsonState = next

		for mask0 != 0 {
			position := offset + bits.TrailingZeros64(uint64(mask0))
			mask0 &= mask0 - 1
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
		for mask1 != 0 {
			position := offset + BlockSize + bits.TrailingZeros64(uint64(mask1))
			mask1 &= mask1 - 1
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
		for mask2 != 0 {
			position := offset + 2*BlockSize + bits.TrailingZeros64(uint64(mask2))
			mask2 &= mask2 - 1
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
		for mask3 != 0 {
			position := offset + 3*BlockSize + bits.TrailingZeros64(uint64(mask3))
			mask3 &= mask3 - 1
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
	}

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

func TestJSONStage2FusedVariants(t *testing.T) {
	variants := []struct {
		name  string
		parse func([]byte, *stage2bench.LargeStrings) error
	}{
		{name: "masks", parse: parseGeneratedStage2FusedMasks},
		{name: "indexes", parse: parseGeneratedStage2FusedIndexes},
	}

	want, src := generatedLargeStringsFixture(t)
	for _, variant := range variants {
		t.Run(variant.name+"/fixture", func(t *testing.T) {
			var got stage2bench.LargeStrings
			if err := variant.parse(src, &got); err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("decoded %+v, want %+v", got, want)
			}
			if err := variant.parse(src[:len(src)-1], &got); err == nil {
				t.Fatal("truncated input unexpectedly decoded successfully")
			}
		})
	}

	// Varying the first string length moves later escaped quotes and slash runs
	// across logical-block and 4x-batch boundaries.
	for padding := 0; padding <= 260; padding++ {
		want := stage2bench.LargeStrings{
			ID:     -123456789,
			Name:   strings.Repeat("x", padding),
			Active: true,
			Score:  1.25,
			Count:  987654321,
			Note:   "escaped quote \" and slash \\ and tail",
		}
		src, err := easyjson.Marshal(&want)
		if err != nil {
			t.Fatal(err)
		}
		for _, variant := range variants {
			var got stage2bench.LargeStrings
			if err := variant.parse(src, &got); err != nil {
				t.Fatalf("%s padding %d: %v", variant.name, padding, err)
			}
			if got != want {
				t.Fatalf("%s padding %d decoded %+v, want %+v", variant.name, padding, got, want)
			}
		}
	}
}

func TestClassifyGeneratedStage2JSON4(t *testing.T) {
	src := make([]byte, 4*BlockSize)
	for index := range src {
		src[index] = 'x'
	}
	pattern := []byte(`{\"\\,:}`)
	for _, index := range []int{0, 5, 62, 63, 64, 65, 126, 127, 128, 191, 192, 254, 255} {
		src[index] = pattern[index%len(pattern)]
	}

	states := []JSONState{
		{},
		{InString: true},
		{Escaped: true},
		{InString: true, Escaped: true},
	}
	for _, initial := range states {
		want, wantState := appendJSONStructuralMasks(nil, src, initial)
		mask0, mask1, mask2, mask3, gotState := classifyGeneratedStage2JSON4(
			src, initial, makeJSONSIMDTargets(),
		)
		got := [4]Mask{mask0, mask1, mask2, mask3}
		wantMasks := [4]Mask{want[0], want[1], want[2], want[3]}
		if got != wantMasks || gotState != wantState {
			t.Fatalf("initial %+v: masks=%#x, state=%+v; want masks=%#x, state=%+v",
				initial, got, gotState, want, wantState)
		}
	}
}
