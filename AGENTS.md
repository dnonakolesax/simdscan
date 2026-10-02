# AGENTS.md

## Project overview

`simdscan` is a small, allocation-conscious Go library providing SIMD-accelerated primitives for scanning byte slices.

The package is intended to serve as infrastructure for high-performance parsers, protocol implementations, log processing, text processing, serialization formats, and similar workloads.

Typical use cases include:

* finding one delimiter;
* finding any byte from a small set;
* locating bytes inside or outside an ASCII range;
* validating ASCII fast paths;
* producing bit masks for matching bytes;
* collecting all structural-character positions for parsers.

The library is not a JSON, CSV, HTTP, or text parser itself. Keep the package generic.

Primary design goals, in order:

1. Correctness.
2. Portable Go implementation.
3. Predictable zero-allocation hot paths.
4. High performance on large inputs.
5. Competitive performance on small inputs.
6. Small and understandable public API.
7. Minimal architecture-specific code.

---

# Go version

The minimum supported Go version is:

```text
Go 1.27
```

The module must contain:

```go
go 1.27
```

Use the latest available Go 1.27 patch release in CI and development.

Do not lower the minimum Go version for backwards compatibility.

This project intentionally relies on Go 1.27 functionality.

---

# Go 1.27 requirements

Write modern, idiomatic Go targeting Go 1.27.

Agents must be aware of Go 1.27 language and toolchain behavior.

In particular:

* generic methods are available in Go 1.27;
* generic function type inference is improved in Go 1.27;
* `go test` runs the `stdversion` vet check by default;
* `go mod tidy` normalizes dependency blocks for Go 1.27 modules;
* use current Go 1.27 standard-library APIs when they simplify code;
* do not preserve obsolete patterns solely for compatibility with older Go releases.

Do not introduce generics simply because they are available. This library primarily operates on bytes, and concrete APIs are normally preferable in hot paths.

Prefer clear concrete code over generic abstractions when the abstraction does not provide measurable value.

Always format Go source with:

```bash
gofmt -w .
```

Before considering a change complete, run:

```bash
GOEXPERIMENT=simd go test ./...
GOEXPERIMENT=simd go vet ./...
```

and when relevant:

```bash
GOEXPERIMENT=simd go test -race ./...
```

---

# Experimental SIMD requirement

Go 1.27 portable SIMD is experimental.

All SIMD builds require:

```bash
GOEXPERIMENT=simd
```

The project may import:

```go
import "simd"
```

Do not hide this requirement.

Documentation, CI configuration, examples, benchmarks, and development commands must explicitly enable the experiment where necessary.

The public API of `simdscan` itself should not expose Go experimental SIMD types.

For example, avoid public signatures such as:

```go
func Scan(v simd.Uint8s) simd.Mask8s
```

Prefer stable Go types:

```go
func IndexByte(src []byte, c byte) int
```

and:

```go
type Mask uint64
```

This isolates users from changes to the experimental SIMD API.

---

# Portable SIMD first

The primary implementation must use the portable Go 1.27:

```go
simd
```

package.

Do not assume a fixed hardware vector size.

Portable SIMD vectors are intentionally vector-size agnostic.

Never write logic that assumes:

```text
16 bytes
32 bytes
64 bytes
```

per native vector.

Instead derive the native vector length from the SIMD value/API and write loops that remain correct for every supported vector width.

Conceptually:

```go
var v simd.Uint8s
width := v.Len()
```

Use the actual current Go 1.27 SIMD API rather than inventing helper methods based on APIs from C++, Rust intrinsics, old Go proposals, or future Go releases.

---

# Supported execution modes

Correctness must not depend on hardware SIMD support.

Portable `simd` code must work with:

* amd64 SIMD;
* arm64 NEON;
* wasm SIMD;
* Go's portable SIMD emulation.

At minimum, tests must pass in normal mode and forced emulation mode:

```bash
GOEXPERIMENT=simd go test ./...
GOEXPERIMENT=simd GODEBUG=simd=0 go test ./...
```

When testing on hardware that supports specific vector widths, also test appropriate supported modes such as:

```bash
GOEXPERIMENT=simd GODEBUG=simd=128 go test ./...
GOEXPERIMENT=simd GODEBUG=simd=256 go test ./...
GOEXPERIMENT=simd GODEBUG=simd=512 go test ./...
```

Do not blindly require unsupported SIMD widths in CI.

A width-specific mode may fail immediately when the machine does not support the requested features.

---

# Architecture-specific SIMD

Avoid `simd/archsimd` unless portable `simd` cannot efficiently express an operation required by the library.

Architecture-specific implementations are allowed only when all of the following are true:

1. A required primitive is genuinely unavailable from portable `simd`.
2. The primitive materially affects performance.
3. There is a portable fallback.
4. Architecture-specific code is isolated in a small internal package.
5. Correctness is tested against the portable/scalar implementation.
6. Benchmarks demonstrate the benefit.

Do not duplicate the entire scanning implementation for amd64 and arm64 merely to gain small performance improvements.

Preferred architecture:

```text
public scanning algorithm
        |
        +---- portable simd
        |
        +---- tiny internal architecture-specific primitive
```

not:

```text
scanner_amd64.go
scanner_arm64.go
scanner_wasm.go
scanner_generic.go

with four independent scanner implementations
```

Keep architecture-specific surface area minimal.

---

# No assembly by default

Do not add Go assembly unless there is strong benchmark evidence that neither `simd` nor a small `archsimd` helper can provide acceptable performance.

Assembly is the last resort.

Any assembly addition must include:

* justification;
* benchmark comparison;
* scalar reference tests;
* architecture documentation;
* fallback implementation.

Do not use assembly merely because an equivalent implementation exists in another language.

---

# No cgo

The library must remain pure Go.

Do not introduce cgo.

One of the project's primary advantages is providing portable SIMD scanning without requiring C/C++ dependencies or external native libraries.

---

# Public API

Keep the public API deliberately small.

The initial target API is conceptually:

```go
package simdscan

const BlockSize = 64

type Mask uint64

type Set struct {
    // implementation private
}

func MakeSet(chars ...byte) Set

func IndexByte(src []byte, c byte) int
func CountByte(src []byte, c byte) int
func AppendByteIndexes(dst []int, src []byte, c byte) []int

func IndexAny2(src []byte, a, b byte) int
func IndexAny3(src []byte, a, b, c byte) int
func IndexAny4(src []byte, a, b, c, d byte) int

func (s Set) Index(src []byte) int
func (s Set) Count(src []byte) int
func (s Set) AppendIndexes(dst []int, src []byte) []int

func IndexRange(src []byte, lo, hi byte) int
func IndexOutsideRange(src []byte, lo, hi byte) int

func IndexNonASCII(src []byte) int
func AllASCII(src []byte) bool

func MatchByteBlock(src []byte, c byte) Mask
func MatchAnyBlock(src []byte, set Set) Mask
func MatchRangeBlock(src []byte, lo, hi byte) Mask

func (m Mask) Any() bool
func (m Mask) Count() int
func (m Mask) First() int
```

This API is a target, not permission to implement every function prematurely.

Implement functionality incrementally.

---

# API semantics

## Index functions

All `Index*` functions return the zero-based index of the first matching byte.

Return:

```go
-1
```

when no match exists.

Behavior should mirror familiar standard-library search APIs where appropriate.

Examples:

```go
IndexByte([]byte("abc"), 'b') == 1
IndexByte([]byte("abc"), 'x') == -1
```

---

## Count functions

`Count*` functions return the exact number of matching bytes.

They must not allocate.

---

## AppendIndexes functions

Functions such as:

```go
AppendByteIndexes
Set.AppendIndexes
```

append matching source indices to the provided destination.

They must:

* preserve ascending source order;
* preserve existing contents of `dst`;
* reuse `dst` capacity;
* avoid allocation when capacity is sufficient.

Example:

```go
dst := []int{100}

dst = AppendByteIndexes(dst, []byte("a,a,a"), ',')

// []int{100, 1, 3}
```

These functions are especially important for parser workloads.

---

# Set semantics

`Set` represents an immutable set of byte values.

`MakeSet` may perform preprocessing because construction is expected to happen outside the hot scanning loop.

Duplicate input bytes must not alter semantics.

For example:

```go
MakeSet(',', ',', '\n')
```

must behave identically to:

```go
MakeSet(',', '\n')
```

Scanning with an already constructed `Set` must not allocate.

Optimize common small sets first.

Typical parser sets contain approximately 2-8 byte values.

Do not make the common case slower merely to optimize pathological sets containing most of the 256 possible byte values.

---

# Range semantics

Ranges are inclusive.

For:

```go
IndexRange(src, lo, hi)
```

a byte matches when:

```go
lo <= src[i] && src[i] <= hi
```

For:

```go
IndexOutsideRange(src, lo, hi)
```

a byte matches when:

```go
src[i] < lo || src[i] > hi
```

Important use case:

```go
IndexOutsideRange(src, '0', '9')
```

finds the end of an ASCII decimal digit run.

Define and test behavior for `lo > hi`.

Do not leave this accidental or architecture-dependent.

---

# ASCII semantics

`IndexNonASCII` returns the first index whose byte has the high bit set:

```go
src[i] >= 0x80
```

`AllASCII(src)` is equivalent in semantics to:

```go
IndexNonASCII(src) == -1
```

but should use the most efficient implementation.

These functions validate ASCII only.

They do not validate UTF-8.

---

# Logical scan blocks

The library uses a logical block size of:

```go
const BlockSize = 64
```

This does NOT mean the hardware SIMD vector is 64 bytes.

A logical 64-byte block may internally require:

* four 128-bit vector operations;
* two 256-bit vector operations;
* one 512-bit vector operation;
* emulated operations.

Do not conflate:

```text
logical scan block size
```

with:

```text
native SIMD vector width
```

---

# Mask representation

A `Mask` represents matches inside one logical block:

```go
type Mask uint64
```

Bit `i` corresponds to source byte `i`.

For example:

```go
src := []byte{'a', ',', 'b', ',', 'c'}
m := MatchByteBlock(src, ',')
```

must produce a mask equivalent to:

```go
Mask(1<<1 | 1<<3)
```

Bits beyond `len(src)` must always be zero.

Mask ordering must be independent of:

* CPU architecture;
* SIMD vector width;
* endianness.

`Mask` is a logical API representation, not a representation of an architecture-specific SIMD mask register.

---

# MatchBlock input contract

`Match*Block` operates on at most `BlockSize` bytes.

The contract for input larger than `BlockSize` must be explicit and consistent.

Preferred contract:

```text
len(src) must be <= BlockSize
```

and oversized input is programmer misuse.

Do not silently ignore bytes after byte 63.

Higher-level APIs such as `Index*`, `Count*`, and `AppendIndexes` are responsible for processing arbitrarily large slices.

---

# Scalar reference implementation

Every SIMD operation must have an obviously correct scalar equivalent available for testing.

Prefer simple scalar code over clever scalar optimization.

Example:

```go
func indexByteScalar(src []byte, c byte) int {
    for i, b := range src {
        if b == c {
            return i
        }
    }
    return -1
}
```

The scalar implementation is the correctness oracle.

SIMD tests should compare against the scalar implementation over many input shapes rather than relying only on hand-written expected values.

Do not remove reference implementations merely because benchmarks do not call them.

---

# SIMD tails

Tail handling is correctness-critical.

Every function must correctly handle lengths including:

```text
0
1
vectorWidth - 1
vectorWidth
vectorWidth + 1
2*vectorWidth - 1
2*vectorWidth
63
64
65
```

and arbitrary larger values.

Prefer the Go 1.27 partial-load APIs where appropriate.

Never read beyond the valid slice merely because the CPU instruction would tolerate the memory access.

No unsafe out-of-bounds reads.

---

# unsafe

Avoid `unsafe`.

Using `unsafe` requires all of:

* a demonstrated performance requirement;
* an explanation in code;
* no safe equivalent with comparable performance;
* dedicated tests for edge cases;
* benchmark evidence.

Never use `unsafe` merely to avoid writing correct tail handling.

---

# Allocation policy

Scanning operations should allocate zero heap objects.

This applies especially to:

```go
IndexByte
CountByte
IndexAny2
IndexAny3
IndexAny4
Set.Index
Set.Count
IndexRange
IndexOutsideRange
IndexNonASCII
AllASCII
Match*Block
```

`AppendIndexes` may allocate only when the caller-provided destination lacks sufficient capacity.

Use:

```bash
go test
go test -bench=. -benchmem
```

to detect unexpected allocations.

For important hot-path benchmarks, target:

```text
0 allocs/op
```

unless the operation's contract inherently requires allocation.

---

# Performance rules

Do not accept a SIMD implementation simply because it contains SIMD instructions.

It must be benchmarked.

Every performance-sensitive change should compare against:

1. scalar reference implementation;
2. relevant standard-library implementation;
3. previous implementation.

Examples include:

```go
bytes.IndexByte
bytes.Count
```

where applicable.

Benchmark multiple sizes, at minimum around:

```text
0 B
8 B
16 B
32 B
64 B
128 B
256 B
1 KiB
4 KiB
64 KiB
1 MiB
```

Include different match distributions:

```text
match at first byte
match near beginning
match in middle
match at end
no match
rare matches
dense matches
```

For `AppendIndexes`, test both sparse and dense results.

---

# Small-input performance

SIMD initialization and setup overhead can make scalar code faster for short inputs.

Do not force SIMD for every slice length.

A hybrid implementation is allowed and encouraged when benchmarks justify it:

```text
small input -> scalar
large input -> SIMD
```

Thresholds must come from benchmarks.

Do not choose thresholds based solely on intuition.

Keep thresholds named and easy to benchmark.

---

# Benchmark stability

Avoid benchmark code that measures setup work instead of scanning.

Preallocate inputs outside timed loops.

Prevent compiler elimination when required.

Use appropriate benchmark helpers such as:

```go
b.SetBytes(...)
b.ReportAllocs()
```

Prefer sub-benchmarks with descriptive names.

Example:

```text
BenchmarkIndexByte/64/no-match
BenchmarkIndexByte/1024/end
BenchmarkIndexByte/1048576/no-match
```

Never tune an implementation based on a single benchmark size.

---

# Tests

Use table-driven tests when they improve clarity.

Test:

* nil slices;
* empty slices;
* one-byte inputs;
* exact block boundaries;
* exact native SIMD boundaries where testable;
* tails;
* duplicate `Set` elements;
* all 256 possible byte values;
* matches at every possible position;
* no-match inputs;
* dense matches;
* random inputs.

SIMD implementations must be compared against scalar reference implementations.

---

# Fuzzing

Fuzzing is strongly encouraged for scan primitives.

Useful properties include:

```go
SIMD result == scalar result
```

for arbitrary:

```text
src
target byte
set
range
```

Particularly fuzz:

```go
IndexByte
CountByte
Set.Index
Set.Count
IndexRange
IndexOutsideRange
MatchByteBlock
MatchAnyBlock
MatchRangeBlock
```

When fuzzing block APIs, constrain input to the documented block size.

Any fuzz-discovered bug must receive a deterministic regression test.

---

# Race safety

All package-level functions must be safe for concurrent use.

`Set` should be immutable after construction and therefore safe for concurrent scanning without locks.

Do not introduce mutable package-level scratch buffers.

Do not use shared mutable state to avoid allocations.

---

# Dependencies

Prefer zero third-party runtime dependencies.

This package is low-level infrastructure and should remain easy to audit and embed.

Before adding a dependency, determine whether the standard library or a few lines of local code are sufficient.

Benchmark-only or testing-only dependencies also require justification.

---

# Error handling

Most scanning operations should not return errors.

Invalid search results use conventional values such as:

```go
-1
```

Do not introduce errors for normal conditions such as "byte not found."

Programmer contract violations should either:

* be prevented through the API;
* have explicitly documented behavior;
* panic consistently if panic is the chosen contract.

Do not silently produce partial results for invalid block sizes.

---

# Documentation

All exported identifiers must have Go documentation comments.

Comments should explain semantics, not implementation trivia.

Good:

```go
// IndexByte returns the index of the first occurrence of c in src,
// or -1 if c is not present.
```

Avoid:

```go
// IndexByte uses SIMD to quickly scan memory.
```

The implementation may change while the semantic contract should remain stable.

Document experimental build requirements prominently in the README:

```text
Go >= 1.27
GOEXPERIMENT=simd
```

---

# Naming

Follow Go naming conventions.

Prefer:

```go
IndexByte
IndexRange
AllASCII
AppendIndexes
```

over names such as:

```go
SIMDFindByte
VectorizedIndex
FastSearch
ScanBytesAVX
```

SIMD is an implementation technique, not generally part of the public semantic API.

Architecture names must not appear in portable public APIs.

---

# Specialized parser helpers

Do not add format-specific APIs to the core package without strong justification.

Avoid functions such as:

```go
MatchJSONStructural
MatchCSVDelimiters
MatchHTTPWhitespace
```

when callers can express the same operation using:

```go
MakeSet(...)
MatchAnyBlock(...)
```

Format-specific packages may be added separately in the future if they require genuinely specialized algorithms.

Keep the core package generic.

---

# Optimization discipline

Before optimizing:

1. Write or identify the scalar reference.
2. Add correctness tests.
3. Add representative benchmarks.
4. Measure the current implementation.
5. Implement the optimization.
6. Re-run correctness tests.
7. Re-run benchmarks.
8. Check allocations.
9. Inspect generated code when necessary.

Do not optimize based only on expected instruction counts.

Compiler behavior matters.

When a result is surprising, inspect generated assembly rather than guessing.

---

# Compiler and generated-code inspection

For performance investigation it is acceptable to use tools such as:

```bash
go test -gcflags=...
go tool objdump
go build -gcflags=...
```

or appropriate compiler diagnostic flags.

Generated-code inspection is diagnostic evidence, not a replacement for end-to-end benchmarks.

Do not hard-code implementation details based on a particular compiler output unless required and documented.

---

# Changes to experimental SIMD APIs

The Go `simd` API is experimental and is not covered by the Go 1 compatibility guarantee yet.

Before changing SIMD-related implementation code:

1. Check the documentation for the exact Go version being targeted.
2. Do not assume an operation described for a future Go release exists in Go 1.27.
3. Do not copy code written against an earlier experimental proposal without verifying it.
4. Keep experimental API usage concentrated in a small number of implementation files where practical.

When a future Go release adds a portable primitive currently implemented with `archsimd`, prefer migrating to portable `simd` if benchmarks show comparable performance.

---

# Repository structure

Prefer a simple structure.

Example:

```text
.
├── AGENTS.md
├── README.md
├── go.mod
├── scan.go
├── scan_test.go
├── scan_bench_test.go
├── mask.go
├── set.go
├── scalar_test.go
└── internal/
    └── ...
```

Do not create architecture directories or abstraction layers before they are required.

If architecture-specific helpers become necessary, isolate them, for example:

```text
internal/maskbits/
```

rather than spreading architecture checks throughout scanning code.

---

# Implementation order

Prefer implementing the project incrementally in approximately this order:

```text
1. scalar reference implementations
2. Mask semantics
3. MatchByteBlock
4. IndexByte
5. CountByte
6. AppendByteIndexes
7. IndexAny2
8. IndexAny3
9. IndexAny4
10. MatchRangeBlock
11. IndexRange
12. IndexOutsideRange
13. IndexNonASCII / AllASCII
14. Set
15. MatchAnyBlock
16. Set.Index / Set.Count / Set.AppendIndexes
```

Do not implement the entire proposed API in one large change.

Each meaningful stage should have tests and benchmarks.

---

# Definition of done

A change affecting scanning code is complete only when:

```bash
gofmt
```

produces no further changes and the relevant commands succeed:

```bash
GOEXPERIMENT=simd go test ./...
GOEXPERIMENT=simd GODEBUG=simd=0 go test ./...
GOEXPERIMENT=simd go vet ./...
```

For concurrency-sensitive code, also run:

```bash
GOEXPERIMENT=simd go test -race ./...
```

For performance-sensitive code, run:

```bash
GOEXPERIMENT=simd go test -bench=. -benchmem ./...
```

A SIMD optimization must not be merged if it:

* changes public semantics;
* fails under SIMD emulation;
* reads outside valid slice bounds;
* introduces unexplained allocations;
* substantially regresses important small-input workloads;
* substantially regresses supported architectures without justification;
* duplicates architecture-specific implementations unnecessarily.

Correctness takes precedence over benchmark results.

---

# Agent behavior

When modifying this repository:

* inspect existing code before introducing new abstractions;
* preserve API compatibility unless explicitly instructed otherwise;
* prefer small focused changes;
* do not rewrite unrelated code;
* do not introduce dependencies casually;
* do not assume SIMD instruction availability;
* do not assume vector width;
* do not claim a performance improvement without benchmark evidence;
* do not delete scalar reference implementations used for verification;
* do not use future Go SIMD APIs while the project targets Go 1.27;
* verify unfamiliar Go 1.27 SIMD APIs against the actual Go 1.27 documentation before using them.

When correctness and performance conflict, first produce a correct implementation and preserve a benchmarkable path for subsequent optimization.
