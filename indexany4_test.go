package simdscan

import (
	"bytes"
	"slices"
	"testing"
)

func TestMatchAny4Block(t *testing.T) {
	targetSets := [][4]byte{
		{'{', '}', '"', '\\'},
		{0, 1, 2, 3},
		{0xfc, 0xfd, 0xfe, 0xff},
		{'x', 'x', 'x', 'x'},
	}

	for size := 0; size <= BlockSize; size++ {
		src := make([]byte, size)
		for i := range src {
			src[i] = byte(i*37 + size)
		}
		for _, targets := range targetSets {
			want := matchAny4BlockScalar(src, targets[0], targets[1], targets[2], targets[3])
			got := MatchAny4Block(src, targets[0], targets[1], targets[2], targets[3])
			if got != want {
				t.Fatalf("size %d, targets %v: got %#x, want %#x", size, targets, got, want)
			}
		}
	}
}

func TestMatchAny4BlockAllPositions(t *testing.T) {
	targets := [...]byte{'{', '}', '"', '\\'}
	for size := 1; size <= BlockSize; size++ {
		for _, target := range targets {
			src := bytes.Repeat([]byte{'a'}, size)
			for pos := range src {
				src[pos] = target
				want := Mask(1) << pos
				got := MatchAny4Block(src, targets[0], targets[1], targets[2], targets[3])
				if got != want {
					t.Fatalf("size %d, target %d, position %d: got %#x, want %#x", size, target, pos, got, want)
				}
				src[pos] = 'a'
			}
		}
	}
}

func TestMatchAny4BlockPanicsAboveBlockSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MatchAny4Block did not panic for oversized input")
		}
	}()
	MatchAny4Block(make([]byte, BlockSize+1), 0, 1, 2, 3)
}

func TestMatchAny4BlockDoesNotAllocate(t *testing.T) {
	src := bytes.Repeat([]byte{'a'}, BlockSize)
	if allocs := testing.AllocsPerRun(100, func() {
		MatchAny4Block(src, '{', '}', '"', '\\')
	}); allocs != 0 {
		t.Fatalf("MatchAny4Block allocated %v times, want 0", allocs)
	}
}

func matchAny4BlockScalar(src []byte, a, b, c, d byte) Mask {
	var result Mask
	for i, value := range src {
		if value == a || value == b || value == c || value == d {
			result |= Mask(1) << i
		}
	}
	return result
}

func TestAppendAny4Indexes(t *testing.T) {
	tests := []struct {
		name       string
		dst        []uint32
		src        []byte
		a, b, c, d byte
		want       []uint32
	}{
		{name: "nil", a: '{', b: '}', c: '"', d: '\\'},
		{name: "empty", dst: []uint32{100}, src: []byte{}, a: '{', b: '}', c: '"', d: '\\', want: []uint32{100}},
		{name: "preserves prefix", dst: []uint32{100}, src: []byte("a{b}c\"d\\"), a: '{', b: '}', c: '"', d: '\\', want: []uint32{100, 1, 3, 5, 7}},
		{name: "duplicates", src: []byte("xaxbxcx"), a: 'x', b: 'x', c: 'x', d: 'x', want: []uint32{0, 2, 4, 6}},
		{name: "zero and high", src: []byte{0, 1, 0xff, 2, 0xfe, 0}, a: 0, b: 0xff, c: 0xfe, d: 3, want: []uint32{0, 2, 4, 5}},
		{name: "no matches", src: []byte("abcdef"), a: '{', b: '}', c: '"', d: '\\'},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := AppendAny4Indexes(test.dst, test.src, test.a, test.b, test.c, test.d)
			if !slices.Equal(got, test.want) {
				t.Fatalf("AppendAny4Indexes(%v, %v, %d, %d, %d, %d) = %v, want %v",
					test.dst, test.src, test.a, test.b, test.c, test.d, got, test.want)
			}
		})
	}
}

func TestAppendAny4IndexesMatchesScalar(t *testing.T) {
	targetSets := [][4]byte{
		{'{', '}', '"', '\\'},
		{0, 1, 2, 3},
		{0xfc, 0xfd, 0xfe, 0xff},
		{'x', 'x', 'x', 'x'},
	}
	for size := 0; size <= 2*vectorsPerTree*maxVectorBytes+1; size++ {
		src := make([]byte, size)
		for i := range src {
			src[i] = byte(i*37 + size)
		}
		for _, targets := range targetSets {
			prefix := []uint32{900, 901}
			want := appendAny4IndexesScalar(slices.Clone(prefix), src, targets[0], targets[1], targets[2], targets[3])
			got := AppendAny4Indexes(slices.Clone(prefix), src, targets[0], targets[1], targets[2], targets[3])
			if !slices.Equal(got, want) {
				t.Fatalf("size %d, targets %v: got %v, want %v", size, targets, got, want)
			}
		}
	}
}

func TestAppendAny4IndexesAllPositions(t *testing.T) {
	targets := [...]byte{'{', '}', '"', '\\'}
	for _, size := range []int{1, 31, 32, 33, 63, 64, 65, 127, 128, 129, 255, 256, 257} {
		for _, target := range targets {
			src := bytes.Repeat([]byte{'a'}, size)
			for pos := range src {
				src[pos] = target
				got := AppendAny4Indexes(nil, src, targets[0], targets[1], targets[2], targets[3])
				want := []uint32{uint32(pos)}
				if !slices.Equal(got, want) {
					t.Fatalf("size %d, target %d, position %d: got %v, want %v", size, target, pos, got, want)
				}
				src[pos] = 'a'
			}
		}
	}
}

func TestAppendAny4IndexesDoesNotAllocateWithCapacity(t *testing.T) {
	src := bytes.Repeat([]byte("a{"), 512)
	dst := make([]uint32, 1, 1+len(src)/2)
	dst[0] = 100
	if allocs := testing.AllocsPerRun(100, func() {
		got := AppendAny4Indexes(dst[:1], src, '{', '}', '"', '\\')
		if len(got) != 1+len(src)/2 {
			panic("unexpected result length")
		}
	}); allocs != 0 {
		t.Fatalf("AppendAny4Indexes allocated %v times, want 0", allocs)
	}
}

func appendAny4IndexesScalar(dst []uint32, src []byte, a, b, c, d byte) []uint32 {
	for i, value := range src {
		if value == a || value == b || value == c || value == d {
			dst = append(dst, uint32(i))
		}
	}
	return dst
}

func TestIndexAny4(t *testing.T) {
	tests := []struct {
		name       string
		src        []byte
		a, b, c, d byte
		want       int
	}{
		{name: "nil", a: '{', b: '}', c: '"', d: '\\', want: -1},
		{name: "empty", src: []byte{}, a: '{', b: '}', c: '"', d: '\\', want: -1},
		{name: "first target", src: []byte("a{bc"), a: '{', b: '}', c: '"', d: '\\', want: 1},
		{name: "second target", src: []byte("ab}c"), a: '{', b: '}', c: '"', d: '\\', want: 2},
		{name: "third target", src: []byte("abc\""), a: '{', b: '}', c: '"', d: '\\', want: 3},
		{name: "fourth target", src: []byte("abc\\"), a: '{', b: '}', c: '"', d: '\\', want: 3},
		{name: "earliest target", src: []byte("a}b{c"), a: '{', b: '}', c: '"', d: '\\', want: 1},
		{name: "duplicates", src: []byte("abc"), a: 'x', b: 'x', c: 'b', d: 'x', want: 1},
		{name: "zero and high", src: []byte{1, 0xff, 0, 2}, a: 0, b: 0xff, c: 3, d: 4, want: 1},
		{name: "miss", src: []byte("abc"), a: '{', b: '}', c: '"', d: '\\', want: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IndexAny4(test.src, test.a, test.b, test.c, test.d); got != test.want {
				t.Fatalf("IndexAny4(%v, %d, %d, %d, %d) = %d, want %d",
					test.src, test.a, test.b, test.c, test.d, got, test.want)
			}
		})
	}
}

func TestIndexAny4AllBatchedPositions(t *testing.T) {
	targets := [...]byte{'{', '}', '"', '\\'}
	for _, size := range []int{0, 1, 31, 32, 33, 63, 64, 65, 255, 256, 257, 511, 512, 513, 1024, 1025, 2048, 2049} {
		for _, target := range targets {
			src := bytes.Repeat([]byte{'a'}, size)
			for pos := range src {
				src[pos] = target
				got := IndexAny4(src, targets[0], targets[1], targets[2], targets[3])
				if got != pos {
					t.Fatalf("size %d, target %d, position %d: got %d", size, target, pos, got)
				}
				src[pos] = 'a'
			}
		}
	}
}

func TestIndexAny4MatchesScalar(t *testing.T) {
	targetSets := [][4]byte{
		{'{', '}', '"', '\\'},
		{0, 1, 2, 3},
		{0xfc, 0xfd, 0xfe, 0xff},
		{'x', 'x', 'x', 'x'},
	}
	for size := 0; size <= 4096; size += 7 {
		src := make([]byte, size)
		for i := range src {
			src[i] = byte(i*37 + size)
		}
		for _, targets := range targetSets {
			want := indexAny4Scalar(src, targets[0], targets[1], targets[2], targets[3])
			got := IndexAny4(src, targets[0], targets[1], targets[2], targets[3])
			if got != want {
				t.Fatalf("size %d, targets %v: got %d, want %d", size, targets, got, want)
			}
		}
	}
}

func TestIndexAny4DoesNotAllocate(t *testing.T) {
	src := bytes.Repeat([]byte{'a'}, 1024)
	if allocs := testing.AllocsPerRun(100, func() {
		IndexAny4(src, '{', '}', '"', '\\')
	}); allocs != 0 {
		t.Fatalf("IndexAny4 allocated %v times, want 0", allocs)
	}
}
