package simdscan

import (
	"math/bits"
	"simd"
	"slices"
)

// RawMasks contains the raw byte classifications for one JSON block. Bit i
// corresponds to source byte i. Structural includes {}[],: regardless of
// whether those bytes occur inside a string.
type RawMasks struct {
	Structural Mask // {}[],:
	Quotes     Mask // "
	Slashes    Mask // \
}

// JSONRawBlockMasks returns the raw structural, quote, and backslash masks for
// src without applying JSON string state. It panics if len(src) exceeds
// BlockSize.
func JSONRawBlockMasks(src []byte) RawMasks {
	return rawJSONBlockMasks(src, makeJSONSIMDTargets())
}

// JSONState carries JSON string state between consecutive blocks.
type JSONState struct {
	InString bool

	// Escaped reports that the next byte is escaped by an odd run of trailing
	// backslashes in the preceding block.
	Escaped bool
}

// JSONBlockMasks returns a mask for the JSON structural bytes {}[],: that
// occur outside strings and the state to pass to the next consecutive block.
// It panics if len(src) exceeds BlockSize.
func JSONBlockMasks(src []byte, state JSONState) (structural Mask, next JSONState) {
	return jsonBlockMasks(src, state, makeJSONSIMDTargets())
}

// appendJSONStructuralMasks appends one outside-string structural mask for
// each logical block in src. It is the whole-buffer JSON kernel: complete
// 256-byte groups remain in one function so SIMD classification masks and the
// scalar quote state can stay live together.
func appendJSONStructuralMasks(dst []Mask, src []byte, state JSONState) ([]Mask, JSONState) {
	blockCount := (len(src) + BlockSize - 1) / BlockSize
	start := len(dst)
	dst = slices.Grow(dst, blockCount)
	dst = dst[:start+blockCount]
	out := dst[start:]

	targets := makeJSONSIMDTargets()
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64

	offset := 0
	output := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		var structural0, structural1, structural2, structural3 Mask
		var quotes0, quotes1, quotes2, quotes3 Mask
		var slashes0, slashes1, slashes2, slashes3 Mask

		// Classify four logical blocks before doing scalar quote-state work.
		// Keeping these loops here deliberately avoids four RawMasks returns and
		// four calls to the single-block scanner on every 256 input bytes.
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

		// Correct escaped quotes in source order because a trailing slash run
		// carries into the following block. Slash-free blocks stay on the cheap
		// path and consume an incoming carry at byte zero.
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

		// Propagate the four string-boundary states. Structural and quote masks
		// are disjoint, so delimiter quote bits need not be cleared from inside.
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

		out[output+0] = structural0 &^ inside0
		out[output+1] = structural1 &^ inside1
		out[output+2] = structural2 &^ inside2
		out[output+3] = structural3 &^ inside3
		output += 4
		state.InString = inString3 != (prefix3&(Mask(1)<<63) != 0)
		state.Escaped = escapedCarry
	}

	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		out[output], state = jsonBlockMasks(src[offset:end], state, targets)
		output++
	}
	return dst, state
}

// AppendJSONStructuralIndexes appends the indexes of JSON structural bytes
// outside strings to dst and returns the resulting slice and trailing string
// state. Appended indexes are in ascending order. It panics if src is too
// large for its indexes to be represented as uint32 values.
func AppendJSONStructuralIndexes(dst []uint32, src []byte, state JSONState) ([]uint32, JSONState) {
	if uint64(len(src)) > uint64(1)<<32 {
		panic("simdscan: AppendJSONStructuralIndexes source exceeds uint32 index range")
	}

	targets := makeJSONSIMDTargets()
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64

	offset := 0
	for ; offset+4*BlockSize <= len(src); offset += 4 * BlockSize {
		var structural0, structural1, structural2, structural3 Mask
		var quotes0, quotes1, quotes2, quotes3 Mask
		var slashes0, slashes1, slashes2, slashes3 Mask

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

		mask0 := structural0 &^ inside0
		for mask0 != 0 {
			bit := bits.TrailingZeros64(uint64(mask0))
			dst = append(dst, uint32(offset+bit))
			mask0 &= mask0 - 1
		}
		mask1 := structural1 &^ inside1
		for mask1 != 0 {
			bit := bits.TrailingZeros64(uint64(mask1))
			dst = append(dst, uint32(offset+BlockSize+bit))
			mask1 &= mask1 - 1
		}
		mask2 := structural2 &^ inside2
		for mask2 != 0 {
			bit := bits.TrailingZeros64(uint64(mask2))
			dst = append(dst, uint32(offset+2*BlockSize+bit))
			mask2 &= mask2 - 1
		}
		mask3 := structural3 &^ inside3
		for mask3 != 0 {
			bit := bits.TrailingZeros64(uint64(mask3))
			dst = append(dst, uint32(offset+3*BlockSize+bit))
			mask3 &= mask3 - 1
		}

		state.InString = inString3 != (prefix3&(Mask(1)<<63) != 0)
		state.Escaped = escapedCarry
	}

	for ; offset < len(src); offset += BlockSize {
		end := min(offset+BlockSize, len(src))
		mask, next := jsonBlockMasks(src[offset:end], state, targets)
		for mask != 0 {
			bit := bits.TrailingZeros64(uint64(mask))
			dst = append(dst, uint32(offset+bit))
			mask &= mask - 1
		}
		state = next
	}
	return dst, state
}

type jsonSIMDTargets struct {
	// '[' and '{' differ only by ASCII bit 0x20, as do ']' and '}'. Folding
	// that bit lets one comparison recognize each pair.
	structuralFold simd.Uint8s
	openFolded     simd.Uint8s // '[' or '{' after folding => '{'
	closeFolded    simd.Uint8s // ']' or '}' after folding => '}'
	comma          simd.Uint8s
	colon          simd.Uint8s
	quote          simd.Uint8s
	slash          simd.Uint8s
}

func makeJSONSIMDTargets() jsonSIMDTargets {
	return jsonSIMDTargets{
		structuralFold: simd.BroadcastUint8s(0x20),
		openFolded:     simd.BroadcastUint8s('{'),
		closeFolded:    simd.BroadcastUint8s('}'),
		comma:          simd.BroadcastUint8s(','),
		colon:          simd.BroadcastUint8s(':'),
		quote:          simd.BroadcastUint8s('"'),
		slash:          simd.BroadcastUint8s('\\'),
	}
}

func jsonBlockMasks(src []byte, state JSONState, targets jsonSIMDTargets) (Mask, JSONState) {
	raw := rawJSONBlockMasks(src, targets)

	// Common fast path: no string boundary and no escape byte in the block.
	// String state cannot change. A carry escape from the preceding block is
	// consumed by byte zero when the block is non-empty, but it only affects a
	// quote; there are no quotes here.
	if raw.Quotes == 0 && raw.Slashes == 0 {
		next := state
		if len(src) != 0 {
			next.Escaped = false
		}
		if state.InString {
			return 0, next
		}
		return raw.Structural, next
	}

	insideString, next := jsonStringMask(raw, len(src), state)
	return raw.Structural &^ insideString, next
}

func rawJSONBlockMasks(src []byte, targets jsonSIMDTargets) RawMasks {
	if len(src) > BlockSize {
		panic("simdscan: JSON block source exceeds BlockSize")
	}
	return rawJSONBlockMasksDynamic(src, targets)
}

// rawJSONBlockMasksDynamic is the production scanner and the scanner-dispatch
// benchmark baseline. Its source-level mask materializer contains a type
// switch, which Go 1.27 can fold while specializing portable SIMD widths.
func rawJSONBlockMasksDynamic(src []byte, targets jsonSIMDTargets) RawMasks {
	var vector simd.Uint8s
	width := vector.Len()
	wordCount := width / 8
	var words [maxVectorBytes / 8]uint64
	var result RawMasks

	// Keep partial loads out of the common vector loop. For 64-byte blocks on
	// current Go 1.27 SIMD widths this loop handles the complete hot path.
	offset := 0
	for ; offset+width <= len(src); offset += width {
		vector = simd.LoadUint8s(src[offset : offset+width])
		plane0, plane1 := jsonVectorPlanes(vector, targets)

		// These three classes are mutually exclusive, so two bit planes retain
		// all information while saving one mask materialization per vector.
		bits0 := mask8sToMask(plane0, &words, wordCount)
		bits1 := mask8sToMask(plane1, &words, wordCount)
		appendJSONPlaneBits(&result, bits0, bits1, offset)
	}

	if offset < len(src) {
		vector, _ = simd.LoadUint8sPart(src[offset:])
		plane0, plane1 := jsonVectorPlanes(vector, targets)
		bits0 := mask8sToMask(plane0, &words, wordCount)
		bits1 := mask8sToMask(plane1, &words, wordCount)
		appendJSONPlaneBits(&result, bits0, bits1, offset)
	}
	return trimRawJSONMasks(result, len(src))
}

func jsonVectorPlanes(vector simd.Uint8s, targets jsonSIMDTargets) (simd.Mask8s, simd.Mask8s) {
	folded := vector.Or(targets.structuralFold)
	structural := folded.Equal(targets.openFolded).
		Or(folded.Equal(targets.closeFolded)).
		Or(vector.Equal(targets.comma)).
		Or(vector.Equal(targets.colon))
	quotes := vector.Equal(targets.quote)
	slashes := vector.Equal(targets.slash)
	return structural.Or(slashes), quotes.Or(slashes)
}

func appendJSONPlaneBits(result *RawMasks, plane0, plane1 Mask, offset int) {
	result.Slashes |= (plane0 & plane1) << offset
	result.Structural |= (plane0 &^ plane1) << offset
	result.Quotes |= (plane1 &^ plane0) << offset
}

func trimRawJSONMasks(result RawMasks, length int) RawMasks {
	// Keep the public logical block contract independent from partial-load
	// padding semantics.
	valid := maskForLength(length)
	result.Structural &= valid
	result.Quotes &= valid
	result.Slashes &= valid
	return result
}

func jsonStringMask(raw RawMasks, length int, state JSONState) (Mask, JSONState) {
	quotes, nextEscaped := unescapedJSONQuotes(raw, length, state.Escaped)

	// No delimiter quote means the entire block remains in the incoming string
	// state. Avoid prefix propagation entirely on this common case.
	if quotes == 0 {
		inside := Mask(0)
		if state.InString {
			inside = maskForLength(length)
		}
		return inside, JSONState{InString: state.InString, Escaped: nextEscaped}
	}

	prefix := prefixXOR(quotes)
	inside := prefix
	if state.InString {
		inside = ^inside
	}
	inside &= maskForLength(length)

	// Delimiter quotes are boundaries, not bytes inside the string. Escaped
	// quotes were removed from quotes and remain in inside.
	inside &^= quotes

	// prefix bit 63 is the parity of every quote bit in the block, so it also
	// gives the outgoing string state without a separate POPCNT.
	nextInString := state.InString
	if prefix&(Mask(1)<<63) != 0 {
		nextInString = !nextInString
	}

	return inside, JSONState{InString: nextInString, Escaped: nextEscaped}
}

func unescapedJSONQuotes(raw RawMasks, length int, carry bool) (Mask, bool) {
	if length == 0 {
		return 0, carry
	}

	// Most JSON blocks contain no backslash at all. In that case the expensive
	// run-parity calculation is unnecessary. An incoming carry can only escape
	// byte zero and is consumed by this non-empty block.
	if raw.Slashes == 0 {
		quotes := raw.Quotes
		if carry {
			quotes &^= 1
		}
		return quotes, false
	}

	escaped, nextCarry := escapedJSONBytes(raw.Slashes, carry)
	if length < BlockSize {
		// The first padding bit is a zero and therefore acts as the byte after
		// the block. If it is escaped, the real block ended in an odd slash run.
		nextCarry = escaped&(Mask(1)<<length) != 0
	}

	return raw.Quotes &^ escaped, nextCarry
}

// escapedJSONBytes returns the bytes immediately following odd-length runs of
// backslashes. carry reports that byte zero follows an odd run from the
// preceding block. nextCarry reports the same condition after bit 63.
//
// The operation count is fixed: additions propagate through every slash run
// in parallel, while the alternating masks select only odd-length run ends.
func escapedJSONBytes(backslashes Mask, carry bool) (escaped Mask, nextCarry bool) {
	const evenBits = Mask(0x5555555555555555)

	starts := backslashes &^ (backslashes << 1)
	evenStarts := starts & evenBits
	oddStarts := starts &^ evenBits

	if carry {
		if backslashes&1 == 0 {
			escaped |= 1
		} else {
			// The first run started in the preceding block. An odd incoming run
			// reverses the parity class of its apparent start at bit zero.
			evenStarts &^= 1
			oddStarts |= 1
		}
	}

	evenCarries, _ := bits.Add64(uint64(backslashes), uint64(evenStarts), 0)
	oddCarries, oddOverflow := bits.Add64(uint64(backslashes), uint64(oddStarts), 0)
	evenCarryEnds := Mask(evenCarries) &^ backslashes
	oddCarryEnds := Mask(oddCarries) &^ backslashes
	escaped |= (evenCarryEnds &^ evenBits) | (oddCarryEnds & evenBits)
	return escaped, oddOverflow != 0
}

func jsonStringMaskWithoutEscapes(raw RawMasks, length int, state JSONState) (Mask, JSONState) {
	quotes := raw.Quotes & maskForLength(length)
	if quotes == 0 {
		inside := Mask(0)
		if state.InString {
			inside = maskForLength(length)
		}
		state.Escaped = false
		return inside, state
	}

	prefix := prefixXOR(quotes)
	inside := prefix
	if state.InString {
		inside = ^inside
	}
	inside &= maskForLength(length)
	inside &^= quotes

	if prefix&(Mask(1)<<63) != 0 {
		state.InString = !state.InString
	}
	state.Escaped = false
	return inside, state
}

func prefixXOR(mask Mask) Mask {
	mask ^= mask << 1
	mask ^= mask << 2
	mask ^= mask << 4
	mask ^= mask << 8
	mask ^= mask << 16
	mask ^= mask << 32
	return mask
}

func prefixXOR4(a, b, c, d Mask) (Mask, Mask, Mask, Mask) {
	a ^= a << 1
	b ^= b << 1
	c ^= c << 1
	d ^= d << 1
	a ^= a << 2
	b ^= b << 2
	c ^= c << 2
	d ^= d << 2
	a ^= a << 4
	b ^= b << 4
	c ^= c << 4
	d ^= d << 4
	a ^= a << 8
	b ^= b << 8
	c ^= c << 8
	d ^= d << 8
	a ^= a << 16
	b ^= b << 16
	c ^= c << 16
	d ^= d << 16
	a ^= a << 32
	b ^= b << 32
	c ^= c << 32
	d ^= d << 32
	return a, b, c, d
}
