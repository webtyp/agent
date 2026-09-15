---
PLAN: "feat: webtyp/weights — model artifact format and browser cache"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/weights (to be created)
---

> New repository. Moves to `weights/docs/PLAN.md` once the repository exists.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md
> **Phase 3** — `webtyp/embed` needs this from its first line of code.

# Plan — `webtyp/weights`

## Single responsibility

Getting model parameters from a URL into typed Go slices, once per browser. It owns the
artifact file format, its reader, the HTTP fetch and the IndexedDB cache. It knows
nothing about what the numbers mean — no tokenisation, no inference, no embeddings.

## Why a custom format rather than safetensors or GGUF

Both are reasonable formats and both are the wrong first move here:

- **safetensors** is a JSON header plus raw tensors — easy to parse, but it carries fp32
  tensors laid out for a training framework. The static embedder needs an int8 table with
  per-row scales; converting at load time in the browser means downloading 4× the bytes
  and spending the client's memory to throw three quarters of it away.
- **GGUF** carries dozens of quantisation schemes and a metadata model far larger than
  anything needed here. Implementing enough of it to be correct is a project.

The conversion happens **offline, once**, in a `cmd/` tool that does not ship to the
browser. What ships is exactly the bytes the browser will use, in the layout it will use
them in. A safetensors reader can be added later if a model arrives that needs one; it is
not the starting point.

## Format

```
magic    "WTYPW1\0\0"                  8 bytes
version  uint32 LE                     4
header   uint32 LE header length       4
header   JSON: tensors, dtypes, shapes, scales offset, model id, tokenizer config
tensors  raw, 64-byte aligned, in header order
```

Design points:

- **64-byte alignment** per tensor, so a tensor can be handed to a GPU buffer or
  reinterpreted with `unsafe.Slice` without a realignment copy.
- **Little-endian throughout**, matching `webtyp/vector`'s codec. One endianness
  decision across the system.
- **Tokeniser config lives in the header**, not in a side file. The vocabulary, the
  `lowercase` flag and the `strip_accents` flag are model properties; separating them
  from the weights is how a tokeniser silently drifts out of sync with the model that
  trained it (`plans/tokenizer.md` explains what that costs for Spanish).
- **Length and checksum in the header**, so a truncated download is detected before
  anything is cached.

## API

```go
type Artifact struct {
	ID      string            // "potion-multilingual-128M/int8"
	Version uint32
	Tensors map[string]Tensor
	Tokenizer TokenizerConfig
}

type Tensor struct {
	Name   string
	DType  DType   // Int8, Float32, Uint8
	Shape  []int
	Data   []byte  // aligned view into the artifact buffer — NOT a copy
	Scales []float32 // per-row dequantisation scales; nil when DType is Float32
}

func (t Tensor) Float32s() ([]float32, error) // zero-copy on LE when DType is Float32
func (t Tensor) Row(i int) []byte

// Open reads an artifact from a byte slice. It does not copy tensor data:
// the returned Artifact aliases src, which must outlive it.
func Open(src []byte) (*Artifact, error)

// Load fetches url, verifies it, caches it in the browser, and opens it. A second
// call for the same id and version reads from the cache and never touches the
// network.
func Load(ctx *context.Context, cfg LoadConfig) (*Artifact, error)

type LoadConfig struct {
	URL     string
	Conn    storage.Conn  // where the cache lives — injected, not constructed
	OnProgress func(done, total int64) // 100 MB deserves a progress bar
}
```

`Conn` is injected rather than the package opening its own IndexedDB connection: the
application already has one, and a second connection to the same database is a source of
version-change deadlocks.

## Cache rules

- Key: `id + "@" + version`. An upgrade never serves stale weights.
- Write to the cache **only after** the full body is read and its length and checksum
  match the header. A partially cached 100 MB artifact that loads without error and
  produces garbage vectors is the worst outcome available here.
- Check `navigator.storage.estimate()` before writing; a cache write that pushes the
  origin over quota can evict the user's document corpus.
- `Evict(id)` for explicit cleanup, and a documented note that the cache is subject to
  browser eviction like any other IndexedDB data.

## Phase 5 additions

The transformer encoder needs the same reader with more dtypes (fp16, int4) and a
`cmd/convert` path from safetensors. Additive; no format change.

## Tests

| Test | Asserts |
|---|---|
| `TestOpen_RoundTrip` | a fixture artifact written by `cmd/convert` reads back with identical tensors |
| `TestOpen_BadMagic` | error |
| `TestOpen_TruncatedTensorData` | error, not a short slice |
| `TestOpen_ChecksumMismatch` | error |
| `TestOpen_Alignment` | every tensor's data offset is 64-byte aligned |
| `TestTensor_Float32sZeroCopy` | the returned slice aliases the source buffer |
| `TestTensor_Row` | row i of an int8 table is the right bytes |
| `TestLoad_CachesAfterFirstFetch` | second call issues no HTTP request (mock fetcher) |
| `TestLoad_PartialBodyNotCached` | a truncated body leaves the cache empty |
| `TestLoad_VersionBumpRefetches` | |
| `TestLoad_ProgressCallback` | called monotonically, ending at total |

## Acceptance checklist

```bash
go vet ./...
gotest
gotest -tinygo
GOOS=js GOARCH=wasm go build ./...
go run ./cmd/convert -in model.safetensors -out model.wtypw -quantize int8
```
