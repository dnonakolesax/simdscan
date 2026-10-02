package simdscan

import (
	"bytes"
	"testing"
)

func TestMatchByteBlock(t *testing.T) {
	for size := 0; size <= BlockSize; size++ {
		for value := 0; value <= 255; value++ {
			target := byte(value)
			src := bytes.Repeat([]byte{target + 1}, size)
			if got := MatchByteBlock(src, target); got != 0 {
				t.Fatalf("size %d, value %d, no match: got %#x, want 0", size, value, got)
			}

			for pos := range src {
				src[pos] = target
				want := Mask(1) << pos
				if got := MatchByteBlock(src, target); got != want {
					t.Fatalf("size %d, value %d, position %d: got %#x, want %#x", size, value, pos, got, want)
				}
				src[pos] = target + 1
			}

			for i := range src {
				src[i] = target
			}
			want := ^Mask(0)
			if size < BlockSize {
				want = (Mask(1) << size) - 1
			}
			if got := MatchByteBlock(src, target); got != want {
				t.Fatalf("size %d, value %d, dense matches: got %#x, want %#x", size, value, got, want)
			}
		}
	}
}

func TestMatchByteBlockPanicsAboveBlockSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MatchByteBlock did not panic for oversized input")
		}
	}()
	MatchByteBlock(make([]byte, BlockSize+1), 0)
}

func TestMatchByteBlockDoesNotAllocate(t *testing.T) {
	src := bytes.Repeat([]byte{'a'}, BlockSize)
	if allocs := testing.AllocsPerRun(100, func() {
		MatchByteBlock(src, 'a')
	}); allocs != 0 {
		t.Fatalf("MatchByteBlock allocated %v times, want 0", allocs)
	}
}

func TestIndexByte(t *testing.T) {
	tests := []struct {
		name string
		src  []byte
		c    byte
		want int
	}{
		{name: "nil", c: 'x', want: -1},
		{name: "empty", src: []byte{}, c: 'x', want: -1},
		{name: "one match", src: []byte{'x'}, c: 'x', want: 0},
		{name: "one miss", src: []byte{'y'}, c: 'x', want: -1},
		{name: "first", src: []byte("xyzzy"), c: 'x', want: 0},
		{name: "middle", src: []byte("xyzzy"), c: 'z', want: 2},
		{name: "last", src: []byte("xzzzy"), c: 'y', want: 4},
		{name: "zero", src: []byte{1, 2, 0, 3}, c: 0, want: 2},
		{name: "miss", src: []byte("xyzzy"), c: 'q', want: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IndexByte(test.src, test.c); got != test.want {
				t.Fatalf("IndexByte(%v, %d) = %d, want %d", test.src, test.c, got, test.want)
			}
		})
	}
}

func TestIndexByteAllPositionsAndValues(t *testing.T) {
	for size := 0; size <= 3*BlockSize+1; size++ {
		for value := 0; value <= 255; value++ {
			src := bytes.Repeat([]byte{byte(value + 1)}, size)
			if got := IndexByte(src, byte(value)); got != -1 {
				t.Fatalf("size %d, value %d, no match: got %d, want -1", size, value, got)
			}

			for pos := range src {
				src[pos] = byte(value)
				if got := IndexByte(src, byte(value)); got != pos {
					t.Fatalf("size %d, value %d, position %d: got %d", size, value, pos, got)
				}
				src[pos] = byte(value + 1)
			}
		}
	}
}

func TestIndexByteTreePositions(t *testing.T) {
	for _, size := range []int{255, 256, 257, 511, 512, 513, 1024, 1025, 2048, 2049} {
		src := bytes.Repeat([]byte{'a'}, size)
		for pos := range src {
			src[pos] = 'z'
			if got := IndexByte(src, 'z'); got != pos {
				t.Fatalf("size %d, position %d: got %d", size, pos, got)
			}
			src[pos] = 'a'
		}
	}
}

func TestIndexByteDoesNotAllocate(t *testing.T) {
	src := bytes.Repeat([]byte{'a'}, 1024)
	if allocs := testing.AllocsPerRun(100, func() {
		IndexByte(src, 'z')
	}); allocs != 0 {
		t.Fatalf("IndexByte allocated %v times, want 0", allocs)
	}
}
