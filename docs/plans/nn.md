---
PLAN: "feat: webtyp/nn — transformer encoder kernels on WebGPU"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/nn (to be created)
---

> New repository. Moves to `nn/docs/PLAN.md` once the repository exists.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md
> **Phase 5**, and the last piece. Everything else must be shipped and working first.

# Plan — `webtyp/nn`

## Single responsibility

Running a sentence-transformer encoder forward pass. It owns the WGSL kernels, the
execution graph and the CPU reference implementation. It gets its GPU from
`webtyp/webgpu` and its parameters from `webtyp/weights`, and it produces a pooled
sentence vector for `webtyp/embed` to normalise and hand on.

It does not fetch, tokenise, store or search.

## Read this before starting

This is a real inference engine, and the honest estimate is that it is larger than every
other repository in this plan combined. It exists to replace one function — the Phase 3
static embedder — with a better-quality version of the same output.

It is therefore worth doing **only if** `embed`'s evaluation harness shows the static
embedder's retrieval quality is actually insufficient for the product, measured on the
Spanish test set (`plans/embed.md` §Evaluation). Run that measurement first. If recall@10
is adequate, this repository should not be built yet, and saying so is a successful
outcome of Phase 3, not a failure.

## Architecture

```
Encoder graph (BERT-family, per the model chosen in plans/embed.md §2)

  token ids  ──► embedding lookup ──► + position ──► LayerNorm
                                                        │
                            ┌───────────────────────────┘
                            ▼
                    ┌─── × N layers ────────────────────────┐
                    │  Q,K,V projections  (matmul)          │
                    │  scaled dot-product attention          │
                    │    (matmul, softmax, masked)           │
                    │  output projection  (matmul)           │
                    │  residual + LayerNorm                  │
                    │  FFN: matmul → GELU → matmul           │
                    │  residual + LayerNorm                  │
                    └────────────────────────────────────────┘
                            │
                            ▼
                   mean pooling over the attention mask ──► sentence vector
```

Kernels needed, in dependency order: `matmul` (tiled, workgroup-shared memory),
`layernorm`, `softmax` (row-wise, numerically stable — subtract the row max),
`gelu`, `add`, `mul`, `transpose`, `gather` (embedding lookup), `meanpool`.

`matmul` is the whole performance story. Write the naive version first, make it correct,
then tile it. Do not start tiled.

## Correctness strategy

Numerical code fails silently. The strategy is to make every failure loud:

1. **A CPU reference implementation of every kernel**, in plain Go, built with no build
   tags and testable with `go test`. It is slow and that is fine — it is the oracle.
2. **Golden fixtures from the reference model**, generated once in Python, committed to
   `testdata/`: input ids, and the expected output of every intermediate tensor.
3. **Kernel-level tests before graph-level tests.** A wrong softmax and a wrong attention
   mask produce the same symptom at the graph level: slightly worse retrieval. Test each
   kernel against the golden intermediate, and the bug has one possible location.
4. **A documented tolerance.** fp32 GPU arithmetic will not match Python bit-for-bit.
   State the tolerance (suggest 1e-4 relative on intermediate tensors, 1e-5 on the final
   normalised vector) and assert against it, rather than discovering drift in recall
   numbers months later.

## API

```go
type Encoder struct{ ... }

type Config struct {
	Device   *webgpu.Device     // nil → CPU reference path
	Weights  *weights.Artifact
	MaxSeq   int
}

func NewEncoder(cfg Config) (*Encoder, error)

// Forward runs the encoder over a batch of tokenised sequences and writes one
// pooled vector per sequence into dst. Not normalised — embed does that.
func (e *Encoder) Forward(ctx *context.Context, ids [][]int32, dst []float32) error

func (e *Encoder) Dim() int
func (e *Encoder) Close() error
```

A `nil` device selecting the CPU path is not a convenience: it is how the golden tests
run in standard Go without a browser, and how a machine without WebGPU still produces
correct (slow) answers.

## Tests

| Test | Asserts |
|---|---|
| `TestMatmul_CPUMatchesGolden` | against `testdata/` |
| `TestMatmul_GPUMatchesCPU` | within tolerance, for several shapes including non-multiples of the tile size |
| `TestLayerNorm_CPUMatchesGolden` | |
| `TestSoftmax_NumericalStability` | a row containing 1e30 does not produce NaN |
| `TestSoftmax_GPUMatchesCPU` | |
| `TestGELU_MatchesGolden` | the exact variant the model uses — tanh approximation and erf differ |
| `TestGather_EmbeddingLookup` | |
| `TestAttention_MaskApplied` | padded positions contribute exactly zero |
| `TestMeanPool_IgnoresPadding` | the single most commonly botched step in the pipeline |
| `TestEncoder_ForwardMatchesGolden` | full pass, CPU, against the reference model's output |
| `TestEncoder_GPUMatchesCPU` | full pass, within tolerance |
| `TestEncoder_BatchMatchesSingle` | batching changes no result |
| `TestEncoder_NilDeviceUsesCPU` | the fallback path |
| `TestEncoder_VariableLengths` | a batch of mixed-length sequences |

Benchmarks comparing CPU and GPU per sequence, recorded in the README. If the GPU path is
not meaningfully faster than the static embedder end to end, this repository has not
earned its place and that should be visible in the numbers.

## Acceptance checklist

```bash
go vet ./...
gotest                  # CPU reference path, standard Go
gotest -tinygo          # GPU path, browser
ls testdata/*.bin       # golden fixtures committed
GOOS=js GOARCH=wasm go build ./...
go run ./cmd/eval       # recall@10 beats the Phase 3 static embedder, or this does not ship
```
