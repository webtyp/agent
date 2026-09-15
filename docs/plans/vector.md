---
PLAN: "feat: webtyp/vector — vector maths, arena, top-k"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/vector (to be created)
---

> New repository. This file lives in `agent/docs/plans/` until `webtyp/vector` exists,
> then moves to `vector/docs/PLAN.md` unchanged.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md

# Plan — `webtyp/vector`

## Single responsibility

Numbers. Similarity arithmetic over `float32` slices, the contiguous arena they live in,
a fixed-size top-k selector, and the little-endian byte codec that moves them to and from
storage.

This package knows nothing about documents, text, embeddings, storage or JavaScript. It
has **zero dependencies** (not even `webtyp.com/fmt` outside error construction) and
compiles for every target, which means the whole of it is testable in standard Go without
a browser. That property is the reason it is a separate repository.

## Why it is not part of `vectordb`

Put this code inside the store and none of it can be tested without a WASM target and a
browser harness. Numerical code is exactly the code that most needs fast, plain
`go test` iteration and `testing.AllocsPerRun`. Keeping it out also makes the allocation
budget enforceable: this package's tests assert zero allocations, and no storage or JS
concern can creep in to break that.

## Verify first

**O2 from the master index:** does `webtyp/binary` already provide a little-endian
`float32` codec? If it does, depend on it and delete §4 from this plan.

```bash
go doc webtyp.com/binary
```

## API

### 1. `arena.go` — where vectors live

```go
// Arena is a contiguous block of N × Dim float32 values. Vector i occupies
// data[i*dim : (i+1)*dim].
//
// One allocation holds every vector in the corpus. This is not a micro-optimisation:
// TinyGo's conservative GC scans N small slices far more expensively than one large
// one, and a contiguous layout is what makes the dot product walk memory linearly.
type Arena struct {
	data []float32
	dim  int
	n    int
}

func NewArena(dim, capacity int) *Arena
func (a *Arena) Dim() int
func (a *Arena) Len() int
func (a *Arena) Cap() int

// Append copies v into the next slot, normalising it in place, and returns its index.
// v is not retained.
func (a *Arena) Append(v []float32) (int, error)

// At returns a view into the arena — NOT a copy. Writing through it mutates the arena.
func (a *Arena) At(i int) []float32

// Set overwrites slot i, normalising.
func (a *Arena) Set(i int, v []float32) error

// Grow reallocates to at least capacity, preserving contents. The only allocating
// operation on the hot path, and callers are expected to size the arena up front.
func (a *Arena) Grow(capacity int)
```

### 2. `math.go` — the arithmetic

```go
// Dot returns the dot product. For L2-normalised vectors this IS the cosine
// similarity: storing normalised removes one sqrt and one division per candidate
// per query.
func Dot(a, b []float32) float32

func Norm(v []float32) float32      // L2 magnitude
func Normalize(v []float32)         // in place; a zero vector is left untouched
func Cosine(a, b []float32) float32 // for un-normalised input; Dot is preferred
```

`Dot` is the hot loop of the entire system. Write it plainly first, with a 4-way unrolled
variant behind a benchmark — and keep the plain one if the benchmark does not justify the
unrolled one. Do not reach for WASM SIMD: TinyGo's support is incomplete, and an
unverifiable intrinsic is worse than a loop that is correct everywhere.

### 3. `topk.go` — selecting without sorting

```go
// TopK keeps the k highest-scoring ids seen, using a fixed-size min-heap.
// Offer allocates nothing. This replaces "build an array of N pairs, sort it,
// slice the first k", which is what the TypeScript original does.
type TopK struct{ ... }

func NewTopK(k int) *TopK
func (t *TopK) Reset()
func (t *TopK) Offer(id int32, score float32)
func (t *TopK) Results(dst []Match) []Match  // descending; appends into dst

type Match struct {
	ID    int32
	Score float32
}
```

For small k (the common case, k ≤ 100) a linear insert into a sorted fixed array beats a
heap. Benchmark both and pick one; do not ship both.

### 4. `codec.go` — bytes in, bytes out

```go
// ByteLen returns the encoded size of a dim-element vector: dim*4.
func ByteLen(dim int) int

// Encode writes v into dst as little-endian float32. dst must be at least
// ByteLen(len(v)).
func Encode(dst []byte, v []float32) int

// Decode reads little-endian float32 from src into dst.
func Decode(dst []float32, src []byte) (int, error)

// Bytes returns a zero-copy byte view of the arena's backing store, for handing
// straight to a blob column. Valid until the next Grow.
func (a *Arena) Bytes() []byte

// FromBytes fills the arena from an encoded block, in one copy.
func (a *Arena) FromBytes(src []byte, count int) error
```

Little-endian is the wire format because WASM is little-endian and so is every target
that matters. Use `unsafe.Slice` for the zero-copy path on little-endian architectures,
guarded by a build tag, with an explicit `math.Float32bits` loop as the big-endian
fallback:

```
codec_le.go   //go:build 386 || amd64 || arm || arm64 || wasm || riscv64 || loong64
codec_be.go   //go:build !(386 || amd64 || ...)
```

The fallback is not hypothetical correctness theatre: a server-side backend reading a
blob written by a browser must decode it identically, and `gotest` runs on whatever CI
provides.

### 5. `search.go` — putting it together

```go
// Search scores query against every slot in the arena and offers the results to out.
// keep, when non-nil, filters by slot index BEFORE scoring — this is where deleted
// rows and tag filters are excluded, so a filtered query costs less, not more.
//
// Allocates nothing. query must already be normalised.
func (a *Arena) Search(query []float32, keep func(i int) bool, out *TopK)
```

### 6. `quantize.go` — Phase 5, stubbed now

Declare the intent, implement later:

```go
// Quantizer compresses an arena to int8 with a per-vector scale, cutting resident
// memory by 4×. Needed past roughly 100k documents (master index D1).
// NOT IMPLEMENTED — Phase 5.
```

Do not write it in v1. A quantiser without a corpus to measure recall loss against is a
guess.

## Tests

Standard library only, no external assertion package.

| Test | Asserts |
|---|---|
| `TestDot_KnownValues` | hand-computed products, including orthogonal (0) and identical (1) vectors |
| `TestDot_EmptyAndMismatched` | length mismatch is an error, not a silent truncation or a panic |
| `TestNormalize_UnitLength` | `Norm` after `Normalize` is 1 within 1e-6 |
| `TestNormalize_ZeroVector` | a zero vector does not produce NaN |
| `TestCosine_MatchesDotOfNormalised` | the two paths agree within 1e-6 |
| `TestArena_AppendAtRoundTrip` | `At(i)` returns what `Append` stored, normalised |
| `TestArena_AtIsAView` | writing through `At` mutates the arena — pins the aliasing contract |
| `TestArena_GrowPreserves` | contents survive a `Grow` |
| `TestCodec_RoundTrip` | 384 random floats survive encode/decode bit-exactly |
| `TestCodec_LittleEndianLayout` | `Encode([1.0])` produces exactly `00 00 80 3F` — pins the wire format against an accidental change |
| `TestCodec_DecodeShortInput` | a truncated blob is an error |
| `TestArena_BytesFromBytesRoundTrip` | a 1024-vector arena survives `Bytes` → `FromBytes` |
| `TestTopK_OrdersDescending` | ties included |
| `TestTopK_KLargerThanInput` | returns everything, no padding |
| `TestTopK_Reset` | reuse leaks nothing from the previous query |
| `TestSearch_MatchesNaiveReference` | 10 000 random vectors, 384 dims: identical results to a naive sort-everything implementation |
| `TestSearch_KeepFilters` | filtered slots never appear |
| `TestSearch_ZeroAllocs` | **`testing.AllocsPerRun` == 0** — the budget from master index D1, enforced |

Benchmarks: `BenchmarkDot_384`, `BenchmarkSearch_10k_384`, `BenchmarkTopK_Offer`.
`BenchmarkSearch_10k_384` is the number the whole design is judged by; record it in the
README so a regression is visible.

## Acceptance checklist

```bash
go vet ./...
gotest
go test -run TestSearch_ZeroAllocs -v ./...
GOOS=js GOARCH=wasm go build ./...
grep -rn "syscall/js\|webtyp.com/storage\|webtyp.com/indexdb" .   # → empty: no deps leaked in
```
