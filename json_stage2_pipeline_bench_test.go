package simdscan

import "testing"

var benchmarkJSONPipelineChecksum uint64

func consumeJSONStructuralMasks(masks []Mask) uint64 {
	var checksum uint64
	for _, mask := range masks {
		checksum ^= uint64(mask)
	}
	return checksum
}

func consumeJSONStructuralIndexes(indexes []uint32) uint64 {
	var checksum uint64
	for _, index := range indexes {
		checksum ^= uint64(index)
	}
	return checksum
}

// BenchmarkJSONStage2PipelineDiagnostics separates the materialized-tape
// producer and consumer costs from their coupled producer-to-consumer paths.
// All tape storage is allocated before timing begins.
func BenchmarkJSONStage2PipelineDiagnostics(b *testing.B) {
	want, src := generatedLargeStringsFixture(b)
	blockCount := (len(src) + BlockSize - 1) / BlockSize

	masks, maskState := appendJSONStructuralMasks(make([]Mask, 0, blockCount), src, JSONState{})
	indexes, indexState := AppendJSONStructuralIndexes(nil, src, JSONState{})
	if maskState != (JSONState{}) || indexState != (JSONState{}) {
		b.Fatalf("unexpected stage 1 states: masks=%+v indexes=%+v", maskState, indexState)
	}

	var gotMasks, gotIndexes = want, want
	if err := parseGeneratedStage2Masks(src, masks, &gotMasks); err != nil {
		b.Fatalf("stage 2 masks preflight: %v", err)
	}
	if err := parseGeneratedStage2Indexes(src, indexes, &gotIndexes); err != nil {
		b.Fatalf("stage 2 indexes preflight: %v", err)
	}
	if gotMasks != want || gotIndexes != want {
		b.Fatalf("stage 2 preflight decoded masks=%+v indexes=%+v, want %+v", gotMasks, gotIndexes, want)
	}

	setup := func(b *testing.B) {
		b.Helper()
		b.SetBytes(int64(len(src)))
		b.ReportAllocs()
	}
	report := func(b *testing.B) {
		b.Helper()
		b.ReportMetric(float64(blockCount), "blocks/op")
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

	b.Run("stage1_then_consume_masks_only", func(b *testing.B) {
		dst := make([]Mask, 0, blockCount)
		setup(b)
		for b.Loop() {
			out, state := appendJSONStructuralMasks(dst[:0], src, JSONState{})
			benchmarkJSONPipelineChecksum = consumeJSONStructuralMasks(out)
			benchmarkJSONState = state
		}
		report(b)
	})

	b.Run("stage1_then_stage2_masks", func(b *testing.B) {
		dst := make([]Mask, 0, blockCount)
		setup(b)
		for b.Loop() {
			out, state := appendJSONStructuralMasks(dst[:0], src, JSONState{})
			benchmarkJSONState = state
			benchmarkStage2Error = parseGeneratedStage2Masks(src, out, &benchmarkStage2Record)
		}
		report(b)
	})

	b.Run("stage1_indexes_only", func(b *testing.B) {
		dst := make([]uint32, 0, len(indexes))
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

	b.Run("stage1_then_consume_indexes_only", func(b *testing.B) {
		dst := make([]uint32, 0, len(indexes))
		setup(b)
		for b.Loop() {
			out, state := AppendJSONStructuralIndexes(dst[:0], src, JSONState{})
			benchmarkJSONPipelineChecksum = consumeJSONStructuralIndexes(out)
			benchmarkJSONState = state
		}
		report(b)
	})

	b.Run("stage1_then_stage2_indexes", func(b *testing.B) {
		dst := make([]uint32, 0, len(indexes))
		setup(b)
		for b.Loop() {
			out, state := AppendJSONStructuralIndexes(dst[:0], src, JSONState{})
			benchmarkJSONState = state
			benchmarkStage2Error = parseGeneratedStage2Indexes(src, out, &benchmarkStage2Record)
		}
		report(b)
	})
}
