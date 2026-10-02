package simdscan

import (
	"bytes"
	"encoding/json"
	"errors"
	"math/bits"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/dnonakolesax/simdscan/internal/stage2bench"
	"github.com/mailru/easyjson"
)

var (
	errGeneratedStage2Syntax = errors.New("simdscan: invalid generated stage 2 input")
	benchmarkStage2Record    stage2bench.LargeStrings
	benchmarkStage2Error     error
)

type jsonStructuralMaskCursor struct {
	masks []Mask
	mask  Mask
	block int
}

func (cursor *jsonStructuralMaskCursor) next() (int, bool) {
	for cursor.mask == 0 {
		if cursor.block == len(cursor.masks) {
			return 0, false
		}
		cursor.mask = cursor.masks[cursor.block]
		cursor.block++
	}
	bit := bits.TrailingZeros64(uint64(cursor.mask))
	cursor.mask &= cursor.mask - 1
	return (cursor.block-1)*BlockSize + bit, true
}

// parseGeneratedStage2Masks is the prototype generated parser for the mask
// representation. The schema-specific field decoder is shared with the index
// variant; only structural-token traversal differs.
func parseGeneratedStage2Masks(src []byte, masks []Mask, dst *stage2bench.LargeStrings) error {
	*dst = stage2bench.LargeStrings{}
	cursor := jsonStructuralMaskCursor{masks: masks}
	open, ok := cursor.next()
	if !ok || open >= len(src) || src[open] != '{' {
		return errGeneratedStage2Syntax
	}

	fieldStart := open + 1
	for {
		colon, ok := cursor.next()
		if !ok || colon >= len(src) || src[colon] != ':' {
			return errGeneratedStage2Syntax
		}
		end, ok := cursor.next()
		if !ok || end >= len(src) || (src[end] != ',' && src[end] != '}') {
			return errGeneratedStage2Syntax
		}
		if err := decodeGeneratedStage2Field(dst, src[fieldStart:colon], src[colon+1:end]); err != nil {
			return err
		}
		if src[end] == '}' {
			return nil
		}
		fieldStart = end + 1
	}
}

// parseGeneratedStage2Indexes is the prototype generated parser for the
// uint32 structural-index representation.
func parseGeneratedStage2Indexes(src []byte, indexes []uint32, dst *stage2bench.LargeStrings) error {
	*dst = stage2bench.LargeStrings{}
	if len(indexes) == 0 || int(indexes[0]) >= len(src) || src[indexes[0]] != '{' {
		return errGeneratedStage2Syntax
	}

	fieldStart := int(indexes[0]) + 1
	position := 1
	for {
		if position+1 >= len(indexes) {
			return errGeneratedStage2Syntax
		}
		colon := int(indexes[position])
		end := int(indexes[position+1])
		position += 2
		if colon >= len(src) || src[colon] != ':' || end >= len(src) || (src[end] != ',' && src[end] != '}') {
			return errGeneratedStage2Syntax
		}
		if err := decodeGeneratedStage2Field(dst, src[fieldStart:colon], src[colon+1:end]); err != nil {
			return err
		}
		if src[end] == '}' {
			return nil
		}
		fieldStart = end + 1
	}
}

func decodeGeneratedStage2Field(dst *stage2bench.LargeStrings, rawKey, rawValue []byte) error {
	// The stage 1 parser state guarantees that a compact generated member name
	// is a quoted string immediately before the colon. Dispatch on the unquoted
	// bytes instead of running a generic sequence of full key comparisons.
	if len(rawKey) < 2 || rawKey[0] != '"' || rawKey[len(rawKey)-1] != '"' {
		return errGeneratedStage2Syntax
	}
	key := rawKey[1 : len(rawKey)-1]

	switch len(key) {
	case 2: // id
		if key[0] == 'i' && key[1] == 'd' {
			value, ok := parseGeneratedStage2Int64(rawValue)
			if !ok {
				return errGeneratedStage2Syntax
			}
			dst.ID = value
		}
	case 4: // name, note
		switch key[1] {
		case 'a':
			if key[0] == 'n' && key[2] == 'm' && key[3] == 'e' {
				value, err := decodeGeneratedStage2String(rawValue)
				if err != nil {
					return err
				}
				dst.Name = value
			}
		case 'o':
			if key[0] == 'n' && key[2] == 't' && key[3] == 'e' {
				value, err := decodeGeneratedStage2String(rawValue)
				if err != nil {
					return err
				}
				dst.Note = value
			}
		}
	case 5: // score, count
		switch key[0] {
		case 's':
			if key[1] == 'c' && key[2] == 'o' && key[3] == 'r' && key[4] == 'e' {
				value, err := strconv.ParseFloat(string(rawValue), 64)
				if err != nil {
					return err
				}
				dst.Score = value
			}
		case 'c':
			if key[1] == 'o' && key[2] == 'u' && key[3] == 'n' && key[4] == 't' {
				value, ok := parseGeneratedStage2Uint64(rawValue)
				if !ok {
					return errGeneratedStage2Syntax
				}
				dst.Count = value
			}
		}
	case 6: // active
		if key[0] == 'a' && key[1] == 'c' && key[2] == 't' &&
			key[3] == 'i' && key[4] == 'v' && key[5] == 'e' {
			switch {
			case len(rawValue) == 4 && rawValue[0] == 't' && rawValue[1] == 'r' &&
				rawValue[2] == 'u' && rawValue[3] == 'e':
				dst.Active = true
			case len(rawValue) == 5 && rawValue[0] == 'f' && rawValue[1] == 'a' &&
				rawValue[2] == 'l' && rawValue[3] == 's' && rawValue[4] == 'e':
				dst.Active = false
			default:
				return errGeneratedStage2Syntax
			}
		}
	}
	return nil
}

func parseGeneratedStage2Uint64(src []byte) (uint64, bool) {
	if len(src) == 0 {
		return 0, false
	}

	const maxUint64 = ^uint64(0)
	var value uint64
	for _, char := range src {
		if char < '0' || char > '9' {
			return 0, false
		}
		digit := uint64(char - '0')
		if value > (maxUint64-digit)/10 {
			return 0, false
		}
		value = value*10 + digit
	}
	return value, true
}

func parseGeneratedStage2Int64(src []byte) (int64, bool) {
	if len(src) == 0 {
		return 0, false
	}
	negative := src[0] == '-'
	if negative {
		src = src[1:]
		if len(src) == 0 {
			return 0, false
		}
	}

	magnitude, ok := parseGeneratedStage2Uint64(src)
	if !ok {
		return 0, false
	}
	const maxInt64 = uint64(1<<63 - 1)
	if negative {
		if magnitude > maxInt64+1 {
			return 0, false
		}
		if magnitude == maxInt64+1 {
			return -1 << 63, true
		}
		return -int64(magnitude), true
	}
	if magnitude > maxInt64 {
		return 0, false
	}
	return int64(magnitude), true
}

func decodeGeneratedStage2String(raw []byte) (string, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return "", errGeneratedStage2Syntax
	}
	value := raw[1 : len(raw)-1]
	if bytes.IndexByte(value, '\\') < 0 {
		return string(value), nil
	}

	var decoded strings.Builder
	decoded.Grow(len(value))
	for offset := 0; offset < len(value); {
		relativeSlash := bytes.IndexByte(value[offset:], '\\')
		if relativeSlash < 0 {
			decoded.Write(value[offset:])
			break
		}
		slash := offset + relativeSlash
		decoded.Write(value[offset:slash])
		if slash+1 >= len(value) {
			return "", errGeneratedStage2Syntax
		}

		switch value[slash+1] {
		case '"', '\\', '/':
			decoded.WriteByte(value[slash+1])
			offset = slash + 2
		case 'b':
			decoded.WriteByte('\b')
			offset = slash + 2
		case 'f':
			decoded.WriteByte('\f')
			offset = slash + 2
		case 'n':
			decoded.WriteByte('\n')
			offset = slash + 2
		case 'r':
			decoded.WriteByte('\r')
			offset = slash + 2
		case 't':
			decoded.WriteByte('\t')
			offset = slash + 2
		case 'u':
			first, ok := decodeGeneratedStage2Hex4(value[slash+2:])
			if !ok {
				return "", errGeneratedStage2Syntax
			}
			offset = slash + 6
			r := rune(first)
			if 0xd800 <= first && first <= 0xdbff {
				if offset+6 > len(value) || value[offset] != '\\' || value[offset+1] != 'u' {
					return "", errGeneratedStage2Syntax
				}
				second, ok := decodeGeneratedStage2Hex4(value[offset+2:])
				if !ok || second < 0xdc00 || second > 0xdfff {
					return "", errGeneratedStage2Syntax
				}
				r = utf16.DecodeRune(rune(first), rune(second))
				offset += 6
			} else if 0xdc00 <= first && first <= 0xdfff {
				return "", errGeneratedStage2Syntax
			}
			decoded.WriteRune(r)
		default:
			return "", errGeneratedStage2Syntax
		}
	}
	return decoded.String(), nil
}

func decodeGeneratedStage2Hex4(src []byte) (uint16, bool) {
	if len(src) < 4 {
		return 0, false
	}
	var value uint16
	for _, digit := range src[:4] {
		value <<= 4
		switch {
		case '0' <= digit && digit <= '9':
			value |= uint16(digit - '0')
		case 'a' <= digit && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case 'A' <= digit && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

func TestGeneratedStage2IntegerParsers(t *testing.T) {
	uintTests := []struct {
		src  string
		want uint64
		ok   bool
	}{
		{src: "0", ok: true},
		{src: "42", want: 42, ok: true},
		{src: "18446744073709551615", want: ^uint64(0), ok: true},
		{src: ""},
		{src: "-1"},
		{src: "12x"},
		{src: "18446744073709551616"},
	}
	for _, test := range uintTests {
		got, ok := parseGeneratedStage2Uint64([]byte(test.src))
		if got != test.want || ok != test.ok {
			t.Errorf("parseGeneratedStage2Uint64(%q) = (%d, %t), want (%d, %t)", test.src, got, ok, test.want, test.ok)
		}
	}

	intTests := []struct {
		src  string
		want int64
		ok   bool
	}{
		{src: "0", ok: true},
		{src: "-0", ok: true},
		{src: "42", want: 42, ok: true},
		{src: "-42", want: -42, ok: true},
		{src: "9223372036854775807", want: 1<<63 - 1, ok: true},
		{src: "-9223372036854775808", want: -1 << 63, ok: true},
		{src: ""},
		{src: "-"},
		{src: "+1"},
		{src: "9223372036854775808"},
		{src: "-9223372036854775809"},
	}
	for _, test := range intTests {
		got, ok := parseGeneratedStage2Int64([]byte(test.src))
		if got != test.want || ok != test.ok {
			t.Errorf("parseGeneratedStage2Int64(%q) = (%d, %t), want (%d, %t)", test.src, got, ok, test.want, test.ok)
		}
	}
}

func generatedLargeStringsFixture(tb testing.TB) (stage2bench.LargeStrings, []byte) {
	tb.Helper()
	want := stage2bench.LargeStrings{
		ID:     -9223372036854770000,
		Name:   "simdscan stage 2 prototype",
		Active: true,
		Score:  12345.6789,
		Count:  18446744073709551000,
		Note: strings.Repeat(
			"generated payload with an escaped quote \" and backslash \\ and unicode λ; ",
			8,
		),
	}
	src, err := easyjson.Marshal(&want)
	if err != nil {
		tb.Fatal(err)
	}
	if len(src) < 4*BlockSize {
		tb.Fatalf("stage 2 fixture is only %d bytes; want at least one 4x batch", len(src))
	}
	return want, src
}

func TestJSONStage2Prototype(t *testing.T) {
	want, src := generatedLargeStringsFixture(t)
	blockCount := (len(src) + BlockSize - 1) / BlockSize
	masks, maskState := appendJSONStructuralMasks(make([]Mask, 0, blockCount), src, JSONState{})
	indexes, indexState := AppendJSONStructuralIndexes(nil, src, JSONState{})
	if maskState != (JSONState{}) || indexState != (JSONState{}) {
		t.Fatalf("unexpected stage 1 states: masks=%+v indexes=%+v", maskState, indexState)
	}

	var fromMasks, fromIndexes stage2bench.LargeStrings
	if err := parseGeneratedStage2Masks(src, masks, &fromMasks); err != nil {
		t.Fatal(err)
	}
	if err := parseGeneratedStage2Indexes(src, indexes, &fromIndexes); err != nil {
		t.Fatal(err)
	}
	if fromMasks != want || fromIndexes != want {
		t.Fatalf("decoded masks=%+v indexes=%+v, want %+v", fromMasks, fromIndexes, want)
	}
}

func BenchmarkJSONStage2Breakdown(b *testing.B) {
	_, src := generatedLargeStringsFixture(b)
	blockCount := (len(src) + BlockSize - 1) / BlockSize
	masks, maskState := appendJSONStructuralMasks(make([]Mask, 0, blockCount), src, JSONState{})
	indexCount := 0
	for _, mask := range masks {
		indexCount += bits.OnesCount64(uint64(mask))
	}
	indexes, indexState := AppendJSONStructuralIndexes(make([]uint32, 0, indexCount), src, JSONState{})
	if maskState != (JSONState{}) || indexState != (JSONState{}) {
		b.Fatalf("unexpected stage 1 states: masks=%+v indexes=%+v", maskState, indexState)
	}

	setup := func(b *testing.B) {
		b.Helper()
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
	}
	report := func(b *testing.B) {
		b.Helper()
		b.ReportMetric(float64(len(masks)), "blocks/op")
		b.ReportMetric(float64(len(indexes)), "structurals/op")
	}

	b.Run("stage1_masks_only", func(b *testing.B) {
		dst := make([]Mask, 0, blockCount)
		setup(b)
		for b.Loop() {
			benchmarkJSONStructuralMasks, benchmarkJSONState =
				appendJSONStructuralMasks(dst[:0], src, JSONState{})
		}
		report(b)
	})

	b.Run("stage2_masks_only", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkStage2Error = parseGeneratedStage2Masks(src, masks, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("stage1_indexes_only", func(b *testing.B) {
		dst := make([]uint32, 0, indexCount)
		setup(b)
		for b.Loop() {
			benchmarkJSONStructuralIndexes, benchmarkJSONState =
				AppendJSONStructuralIndexes(dst[:0], src, JSONState{})
		}
		report(b)
	})

	b.Run("stage2_indexes_only", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkStage2Error = parseGeneratedStage2Indexes(src, indexes, &benchmarkStage2Record)
		}
		report(b)
	})
}

func BenchmarkJSONStage2EndToEnd(b *testing.B) {
	want, src := generatedLargeStringsFixture(b)

	blockCount := (len(src) + BlockSize - 1) / BlockSize
	masks, maskState := appendJSONStructuralMasks(make([]Mask, 0, blockCount), src, JSONState{})
	indexCount := 0
	for _, mask := range masks {
		indexCount += bits.OnesCount64(uint64(mask))
	}
	indexes, indexState := AppendJSONStructuralIndexes(make([]uint32, 0, indexCount), src, JSONState{})
	if maskState != (JSONState{}) || indexState != (JSONState{}) {
		b.Fatalf("unexpected stage 1 states: masks=%+v indexes=%+v", maskState, indexState)
	}

	assertResult := func(name string, got stage2bench.LargeStrings, err error) {
		b.Helper()
		if err != nil {
			b.Fatalf("%s: %v", name, err)
		}
		if got != want {
			b.Fatalf("%s decoded %+v, want %+v", name, got, want)
		}
	}
	var got stage2bench.LargeStrings
	var err error
	err = parseGeneratedStage2Masks(src, masks, &got)
	assertResult("SIMD masks", got, err)
	err = parseGeneratedStage2Indexes(src, indexes, &got)
	assertResult("SIMD indexes", got, err)
	got = stage2bench.LargeStrings{}
	err = easyjson.Unmarshal(src, &got)
	assertResult("easyjson", got, err)
	got = stage2bench.LargeStrings{}
	err = json.Unmarshal(src, &got)
	assertResult("encoding/json", got, err)

	setup := func(b *testing.B) {
		b.Helper()
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
	}
	report := func(b *testing.B) {
		b.Helper()
		b.ReportMetric(float64(len(masks)), "blocks/op")
		b.ReportMetric(float64(len(indexes)), "structurals/op")
	}

	b.Run("simd_masks_generated_stage2", func(b *testing.B) {
		dst := make([]Mask, 0, blockCount)
		setup(b)
		for b.Loop() {
			var state JSONState
			dst, state = appendJSONStructuralMasks(dst[:0], src, JSONState{})
			benchmarkJSONState = state
			benchmarkStage2Error = parseGeneratedStage2Masks(src, dst, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("simd_indexes_generated_stage2", func(b *testing.B) {
		dst := make([]uint32, 0, indexCount)
		setup(b)
		for b.Loop() {
			var state JSONState
			dst, state = AppendJSONStructuralIndexes(dst[:0], src, JSONState{})
			benchmarkJSONState = state
			benchmarkStage2Error = parseGeneratedStage2Indexes(src, dst, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("easyjson", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkStage2Record = stage2bench.LargeStrings{}
			benchmarkStage2Error = easyjson.Unmarshal(src, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("encoding_json", func(b *testing.B) {
		setup(b)
		for b.Loop() {
			benchmarkStage2Record = stage2bench.LargeStrings{}
			benchmarkStage2Error = json.Unmarshal(src, &benchmarkStage2Record)
		}
		report(b)
	})
}
