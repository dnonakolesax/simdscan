package simdscan

import (
	"encoding/json"
	"math/bits"
	"reflect"
	"strings"
	"testing"

	"github.com/dnonakolesax/simdscan/internal/stage2bench"
	"github.com/mailru/easyjson"
)

type generatedJSONFixture struct {
	name       string
	src        []byte
	want       easyjson.Marshaler
	easyTarget easyjson.Unmarshaler
	stdTarget  any
}

func generatedJSONFixtures(tb testing.TB) []generatedJSONFixture {
	tb.Helper()
	smallFlat := &stage2bench.SmallFlat{
		ID:     -123456,
		Count:  987654,
		Active: true,
		Score:  12.5,
		Level:  7,
		Ready:  false,
		Ratio:  0.75,
		Code:   42,
		Size:   4096,
		Valid:  true,
	}
	largeStrings, largeStringsSrc := generatedLargeStringsFixture(tb)
	numeric := &stage2bench.Numeric{
		Signed0:   -1,
		Signed1:   -9223372036854770000,
		Signed2:   42,
		Signed3:   7654321,
		Unsigned0: 0,
		Unsigned1: 18446744073709551000,
		Unsigned2: 99,
		Unsigned3: 123456789,
		Float0:    0.125,
		Float1:    -12345.6789,
		Float2:    1.7976931348623157e+308,
		Float3:    2.2250738585072014e-308,
		Bool0:     true,
		Bool1:     false,
		Bool2:     true,
		Bool3:     false,
	}
	nested := &stage2bench.Nested{
		ID: 123456789,
		Meta: stage2bench.NestedMeta{
			Version: 17,
			Enabled: true,
		},
		Array:  [4]int64{-11, 22, -33, 44},
		Values: []float64{0.25, -1.5, 3.1415926535, 1000.125, -0.00025, 99.75},
		Items: []stage2bench.NestedItem{
			{Code: 101, Score: 1.25},
			{Code: 202, Score: 2.5},
			{Code: 303, Score: 3.75},
		},
		Tags: []string{"alpha", "escaped \"tag\"", "unicode λ", "omega"},
	}

	makeFixture := func(
		name string,
		want easyjson.Marshaler,
		easyTarget easyjson.Unmarshaler,
		stdTarget any,
	) generatedJSONFixture {
		src, err := easyjson.Marshal(want)
		if err != nil {
			tb.Fatal(err)
		}
		return generatedJSONFixture{
			name:       name,
			src:        src,
			want:       want,
			easyTarget: easyTarget,
			stdTarget:  stdTarget,
		}
	}

	return []generatedJSONFixture{
		makeFixture("SmallFlat", smallFlat, &stage2bench.SmallFlat{}, &stage2bench.SmallFlat{}),
		{
			name:       "LargeStrings",
			src:        largeStringsSrc,
			want:       &largeStrings,
			easyTarget: &stage2bench.LargeStrings{},
			stdTarget:  &stage2bench.LargeStrings{},
		},
		makeFixture("Numeric", numeric, &stage2bench.Numeric{}, &stage2bench.Numeric{}),
		makeFixture("Nested", nested, &stage2bench.Nested{}, &stage2bench.Nested{}),
	}
}

func TestGeneratedJSONFixtures(t *testing.T) {
	for _, fixture := range generatedJSONFixtures(t) {
		t.Run(fixture.name, func(t *testing.T) {
			if err := easyjson.Unmarshal(fixture.src, fixture.easyTarget); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(fixture.src, fixture.stdTarget); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(fixture.easyTarget, fixture.want) {
				t.Fatalf("easyjson decoded %#v, want %#v", fixture.easyTarget, fixture.want)
			}
			if !reflect.DeepEqual(fixture.stdTarget, fixture.want) {
				t.Fatalf("encoding/json decoded %#v, want %#v", fixture.stdTarget, fixture.want)
			}
			t.Logf("%s: %d bytes", fixture.name, len(fixture.src))
		})
	}

	fixtures := generatedJSONFixtures(t)
	if size := len(fixtures[0].src); size < 100 || size > 200 {
		t.Fatalf("SmallFlat is %d bytes, want approximately 150", size)
	}
	if size := len(fixtures[1].src); size < 650 || size > 850 {
		t.Fatalf("LargeStrings is %d bytes, want approximately 750", size)
	}
	if strings.Contains(string(fixtures[2].src), `":"`) {
		t.Fatal("Numeric fixture unexpectedly contains a string value")
	}
}

func BenchmarkJSONStage1GeneratedModels(b *testing.B) {
	for _, fixture := range generatedJSONFixtures(b) {
		blockCount := (len(fixture.src) + BlockSize - 1) / BlockSize
		masks, _ := appendJSONStructuralMasks(make([]Mask, 0, blockCount), fixture.src, JSONState{})
		indexCount := 0
		for _, mask := range masks {
			indexCount += bits.OnesCount64(uint64(mask))
		}

		b.Run(fixture.name+"/stage1_masks", func(b *testing.B) {
			dst := make([]Mask, 0, blockCount)
			b.SetBytes(int64(len(fixture.src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructuralMasks, benchmarkJSONState =
					appendJSONStructuralMasks(dst[:0], fixture.src, JSONState{})
			}
			b.ReportMetric(float64(blockCount), "blocks/op")
			b.ReportMetric(float64(indexCount), "structurals/op")
		})

		b.Run(fixture.name+"/stage1_indexes", func(b *testing.B) {
			dst := make([]uint32, 0, indexCount)
			b.SetBytes(int64(len(fixture.src)))
			b.ReportAllocs()
			for b.Loop() {
				benchmarkJSONStructuralIndexes, benchmarkJSONState =
					AppendJSONStructuralIndexes(dst[:0], fixture.src, JSONState{})
			}
			b.ReportMetric(float64(blockCount), "blocks/op")
			b.ReportMetric(float64(indexCount), "structurals/op")
		})
	}
}
