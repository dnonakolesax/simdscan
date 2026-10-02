package simdscan

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"
)

func TestEscapedJSONBytes(t *testing.T) {
	check := func(backslashes Mask, carry bool) {
		t.Helper()
		want, wantCarry := escapedJSONBytesScalarReference(backslashes, carry)
		got, gotCarry := escapedJSONBytes(backslashes, carry)
		if got != want || gotCarry != wantCarry {
			t.Fatalf(
				"backslashes %#016x, carry %t: got (%#016x, %t), want (%#016x, %t)",
				backslashes, carry, got, gotCarry, want, wantCarry,
			)
		}
	}

	for backslashes := Mask(0); backslashes < 1<<16; backslashes++ {
		check(backslashes, false)
		check(backslashes, true)
	}

	patterns := []Mask{
		^Mask(0),
		1 << 63,
		3 << 62,
		7 << 61,
		15 << 60,
		0xaaaaaaaaaaaaaaaa,
		0x5555555555555555,
	}
	for _, backslashes := range patterns {
		check(backslashes, false)
		check(backslashes, true)
	}

	rng := rand.New(rand.NewPCG(3, 4))
	for range 10_000 {
		backslashes := Mask(rng.Uint64())
		check(backslashes, false)
		check(backslashes, true)
	}
}

func TestJSONRawBlockMasksAllByteValues(t *testing.T) {
	for value := 0; value <= 0xff; value++ {
		src := bytes.Repeat([]byte{byte(value)}, BlockSize)
		want := matchJSONBlockScalar(src)
		got := JSONRawBlockMasks(src)
		if got != want {
			t.Fatalf("value %#02x: got %+v, want %+v", value, got, want)
		}
	}
}

func TestJSONBlockMasks(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		state JSONState
	}{
		{name: "empty"},
		{name: "raw structural", src: `{ "a": [1, 2] }`},
		{name: "structural in string", src: `{"{}[],:":"[,]"}`},
		{name: "escaped quote", src: `{"value":"before\"{after"}`},
		{name: "even backslashes", src: `"two\\\\"{}`},
		{name: "starts in string", src: `inside}:still"{}`, state: JSONState{InString: true}},
		{
			name:  "escaped first byte",
			src:   `"still inside}:"{}`,
			state: JSONState{InString: true, Escaped: true},
		},
		{
			name:  "slash free carry consumed",
			src:   `plain}:bytes`,
			state: JSONState{InString: true, Escaped: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantRaw := matchJSONBlockScalar([]byte(test.src))
			if gotRaw := JSONRawBlockMasks([]byte(test.src)); gotRaw != wantRaw {
				t.Fatalf("raw masks: got %+v, want %+v", gotRaw, wantRaw)
			}

			wantMask, wantState := jsonBlockMasksScalarReference([]byte(test.src), test.state)
			gotMask, gotState := JSONBlockMasks([]byte(test.src), test.state)
			if gotMask != wantMask || gotState != wantState {
				t.Fatalf("got (%#x, %+v), want (%#x, %+v)", gotMask, gotState, wantMask, wantState)
			}
		})
	}
}

func TestJSONBlockMasksFastPaths(t *testing.T) {
	tests := []struct {
		name  string
		src   []byte
		state JSONState
	}{
		{name: "plain outside", src: bytes.Repeat([]byte{'a'}, BlockSize)},
		{name: "plain inside", src: bytes.Repeat([]byte{'a'}, BlockSize), state: JSONState{InString: true}},
		{name: "structural outside no strings", src: []byte(`{}[],: 1234567890`)},
		{name: "structural inside no quotes", src: []byte(`{}[],: 1234567890`), state: JSONState{InString: true}},
		{name: "incoming escape ordinary byte", src: []byte(`abc`), state: JSONState{InString: true, Escaped: true}},
		{name: "incoming escape quote", src: []byte(`"abc"`), state: JSONState{InString: true, Escaped: true}},
		{name: "empty carries state", src: nil, state: JSONState{InString: true, Escaped: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			wantMask, wantState := jsonBlockMasksScalarReference(test.src, test.state)
			gotMask, gotState := JSONBlockMasks(test.src, test.state)
			if gotMask != wantMask || gotState != wantState {
				t.Fatalf("got (%#x, %+v), want (%#x, %+v)", gotMask, gotState, wantMask, wantState)
			}
		})
	}
}

func TestJSONBlockMasksAllByteValues(t *testing.T) {
	states := []JSONState{
		{},
		{InString: true},
		{InString: true, Escaped: true},
		{Escaped: true},
	}

	rng := rand.New(rand.NewPCG(1, 2))
	for size := 0; size <= BlockSize; size++ {
		for iteration := 0; iteration < 32; iteration++ {
			src := make([]byte, size)
			for i := range src {
				src[i] = byte(rng.Uint32())
			}
			for _, state := range states {
				wantMask, wantState := jsonBlockMasksScalarReference(src, state)
				gotMask, gotState := JSONBlockMasks(src, state)
				if gotMask != wantMask || gotState != wantState {
					t.Fatalf(
						"size %d, iteration %d, state %+v: got (%#x, %+v), want (%#x, %+v); src=%q",
						size, iteration, state, gotMask, gotState, wantMask, wantState, src,
					)
				}
			}
		}
	}
}

func TestJSONBlockMasksCarriesTrailingBackslashes(t *testing.T) {
	prefix := `{"value":"`
	suffix := `"{}]", "next": [1]}`

	for slashCount := 1; slashCount <= 8; slashCount++ {
		whole := []byte(prefix)
		for range slashCount {
			whole = append(whole, '\\')
		}
		whole = append(whole, suffix...)

		wantMask, wantState := jsonBlockMasksScalarReference(whole, JSONState{})
		for split := len(prefix); split <= len(prefix)+slashCount; split++ {
			firstMask, state := JSONBlockMasks(whole[:split], JSONState{})
			secondMask, gotState := JSONBlockMasks(whole[split:], state)
			gotMask := firstMask | secondMask<<split
			if gotMask != wantMask || gotState != wantState {
				t.Fatalf(
					"slashes %d, split %d: got (%#x, %+v), want (%#x, %+v)",
					slashCount, split, gotMask, gotState, wantMask, wantState,
				)
			}
		}
	}
}

func TestJSONBlockMasksPanicsAboveBlockSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("JSONBlockMasks did not panic for oversized input")
		}
	}()
	JSONBlockMasks(make([]byte, BlockSize+1), JSONState{})
}

func TestJSONBlockMasksDoesNotAllocate(t *testing.T) {
	src := []byte(`{"key":"value with \"escaped\" {[]}: inside", "n":1}`)
	if len(src) > BlockSize {
		t.Fatalf("test source has length %d, want at most %d", len(src), BlockSize)
	}
	if allocs := testing.AllocsPerRun(100, func() {
		JSONBlockMasks(src, JSONState{})
	}); allocs != 0 {
		t.Fatalf("JSONBlockMasks allocated %v times, want 0", allocs)
	}
}

func TestAppendJSONStructuralMasks(t *testing.T) {
	states := []JSONState{
		{},
		{InString: true},
		{Escaped: true},
		{InString: true, Escaped: true},
	}
	sizes := []int{
		0, 1, 15, 16, 31, 32, 63, 64, 65,
		127, 128, 191, 192, 255, 256, 257, 511, 512, 513, 1024,
	}
	rng := rand.New(rand.NewPCG(11, 12))

	for _, size := range sizes {
		for iteration := 0; iteration < 16; iteration++ {
			src := make([]byte, size)
			for i := range src {
				src[i] = byte(rng.Uint32())
			}
			// Force quote/slash and structural boundary cases into otherwise
			// arbitrary data, including carries between 64-byte blocks.
			for boundary := BlockSize; boundary < len(src); boundary += BlockSize {
				if boundary >= 2 {
					src[boundary-2] = '\\'
					src[boundary-1] = '\\'
				}
				if boundary < len(src) {
					src[boundary] = '"'
				}
				if boundary+1 < len(src) {
					src[boundary+1] = '{'
				}
			}

			for _, initial := range states {
				want := []Mask{0xfeed}
				wantState := initial
				for offset := 0; offset < len(src); offset += BlockSize {
					end := min(offset+BlockSize, len(src))
					var mask Mask
					mask, wantState = jsonBlockMasksScalarReference(src[offset:end], wantState)
					want = append(want, mask)
				}

				got, gotState := appendJSONStructuralMasks([]Mask{0xfeed}, src, initial)
				if !slices.Equal(got, want) || gotState != wantState {
					t.Fatalf(
						"size %d, iteration %d, state %+v: got (%#x, %+v), want (%#x, %+v)",
						size, iteration, initial, got, gotState, want, wantState,
					)
				}
			}
		}
	}
}

func TestAppendJSONStructuralMasksDoesNotAllocateWithCapacity(t *testing.T) {
	src := bytes.Repeat([]byte(`{"key":"value with \\"escapes\\"", "items":[1,2,3]}`), 32)
	blockCount := (len(src) + BlockSize - 1) / BlockSize
	dst := make([]Mask, 0, blockCount)
	if allocs := testing.AllocsPerRun(100, func() {
		dst, _ = appendJSONStructuralMasks(dst[:0], src, JSONState{})
	}); allocs != 0 {
		t.Fatalf("appendJSONStructuralMasks allocated %v times, want 0", allocs)
	}
}

func TestAppendJSONStructuralMasksCarriesSlashRunsAcrossBatches(t *testing.T) {
	for _, boundary := range []int{BlockSize, 2 * BlockSize, 3 * BlockSize, 4 * BlockSize} {
		for slashCount := 1; slashCount <= 8; slashCount++ {
			src := bytes.Repeat([]byte{'a'}, 5*BlockSize+1)
			for i := boundary - slashCount; i < boundary; i++ {
				src[i] = '\\'
			}
			src[boundary] = '"'
			src[boundary+1] = '{'

			want := make([]Mask, 0, 6)
			var wantState JSONState
			for offset := 0; offset < len(src); offset += BlockSize {
				end := min(offset+BlockSize, len(src))
				var mask Mask
				mask, wantState = jsonBlockMasksScalarReference(src[offset:end], wantState)
				want = append(want, mask)
			}

			got, gotState := appendJSONStructuralMasks(nil, src, JSONState{})
			if !slices.Equal(got, want) || gotState != wantState {
				t.Fatalf(
					"boundary %d, slash count %d: got (%#x, %+v), want (%#x, %+v)",
					boundary, slashCount, got, gotState, want, wantState,
				)
			}
		}
	}
}

func TestAppendJSONStructuralIndexes(t *testing.T) {
	states := []JSONState{
		{},
		{InString: true},
		{Escaped: true},
		{InString: true, Escaped: true},
	}
	sizes := []int{
		0, 1, 15, 16, 31, 32, 63, 64, 65,
		127, 128, 191, 192, 255, 256, 257, 511, 512, 513, 1024,
	}
	rng := rand.New(rand.NewPCG(21, 22))

	for _, size := range sizes {
		for iteration := 0; iteration < 16; iteration++ {
			src := make([]byte, size)
			for i := range src {
				src[i] = byte(rng.Uint32())
			}
			for boundary := BlockSize; boundary < len(src); boundary += BlockSize {
				if boundary >= 3 {
					src[boundary-3] = '\\'
					src[boundary-2] = '\\'
					src[boundary-1] = '\\'
				}
				src[boundary] = '"'
				if boundary+1 < len(src) {
					src[boundary+1] = '{'
				}
			}

			for _, initial := range states {
				want, wantState := appendJSONStructuralIndexesScalarReference(
					[]uint32{0xfeed}, src, initial,
				)
				got, gotState := AppendJSONStructuralIndexes([]uint32{0xfeed}, src, initial)
				if !slices.Equal(got, want) || gotState != wantState {
					t.Fatalf(
						"size %d, iteration %d, state %+v: got (%v, %+v), want (%v, %+v)",
						size, iteration, initial, got, gotState, want, wantState,
					)
				}
			}
		}
	}
}

func TestAppendJSONStructuralIndexesDoesNotAllocateWithCapacity(t *testing.T) {
	src := bytes.Repeat([]byte(`{"key":"value with \\"escapes\\"", "items":[1,2,3]}`), 32)
	want, _ := appendJSONStructuralIndexesScalarReference(nil, src, JSONState{})
	dst := make([]uint32, 0, len(want))
	if allocs := testing.AllocsPerRun(100, func() {
		dst, _ = AppendJSONStructuralIndexes(dst[:0], src, JSONState{})
	}); allocs != 0 {
		t.Fatalf("AppendJSONStructuralIndexes allocated %v times, want 0", allocs)
	}
}

func appendJSONStructuralIndexesScalarReference(
	dst []uint32,
	src []byte,
	state JSONState,
) ([]uint32, JSONState) {
	inString := state.InString
	escaped := state.Escaped
	for i, value := range src {
		if value == '\\' {
			escaped = !escaped
			continue
		}

		isEscaped := escaped
		escaped = false
		if value == '"' {
			if !isEscaped {
				inString = !inString
			}
			continue
		}
		if inString {
			continue
		}
		switch value {
		case '{', '}', '[', ']', ',', ':':
			dst = append(dst, uint32(i))
		}
	}
	return dst, JSONState{InString: inString, Escaped: escaped}
}

func jsonBlockMasksScalarReference(src []byte, state JSONState) (Mask, JSONState) {
	inString := state.InString
	escaped := state.Escaped
	var structural Mask

	for i, value := range src {
		bit := Mask(1) << i
		if value == '\\' {
			escaped = !escaped
			continue
		}

		isEscaped := escaped
		escaped = false
		if value == '"' {
			if !isEscaped {
				inString = !inString
			}
			continue
		}
		if !inString {
			switch value {
			case '{', '}', '[', ']', ',', ':':
				structural |= bit
			}
		}
	}

	return structural, JSONState{InString: inString, Escaped: escaped}
}

func escapedJSONBytesScalarReference(backslashes Mask, carry bool) (Mask, bool) {
	var escaped Mask
	for i := 0; i < BlockSize; i++ {
		bit := Mask(1) << i
		if backslashes&bit != 0 {
			carry = !carry
			continue
		}
		if carry {
			escaped |= bit
		}
		carry = false
	}
	return escaped, carry
}
