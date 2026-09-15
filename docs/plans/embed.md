---
PLAN: "feat: webtyp/embed — in-browser embedding generation"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/embed (to be created)
---

> New repository. Moves to `embed/docs/PLAN.md` once the repository exists.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md — decisions
> **D4** (two phases behind one port) and **D5** (Spanish drives model size).

# Plan — `webtyp/embed`

## Single responsibility

Text → vectors, in the browser. It owns the `Embedder` port and the pipeline that
implements it: tokenise, run the model, pool, normalise.

It does **not** own tokenisation (`webtyp/tokenizer`), vector arithmetic
(`webtyp/vector`), storage, or — in Phase 5 — the GPU (`webtyp/webgpu`, `webtyp/nn`).

## The port

This interface is the seam the whole architecture pivots on. Everything downstream
(`vectordb`, `agent`) depends on this and nothing else about embeddings.

```go
// Embedder turns text into vectors. Implementations run entirely in the browser.
type Embedder interface {
	// Dim is the vector dimension. Constant for the lifetime of the Embedder.
	Dim() int

	// ID identifies the model that produced these vectors, e.g.
	// "potion-multilingual-128M/int8". Vectors from different models are NOT
	// comparable; vectordb stores this and refuses a corpus that disagrees.
	ID() string

	// Embed writes one vector per text into dst, which MUST have length
	// len(texts)*Dim(). Vectors are L2-normalised on the way out (master index D1).
	// The caller owns dst, so batching allocates once.
	Embed(ctx *context.Context, texts []string, dst []float32) error

	Close() error
}
```

`ID()` is not decoration. Mixing vectors from two models produces results that look
plausible and are meaningless — the failure has no symptom. Storing the id is what turns
that into a startup error.

## Phase 3 — static embeddings (this plan)

A distilled token-embedding table (model2vec / "potion" family): tokenise, look up each
token's vector, mean-pool, normalise. No attention, no forward pass, no GPU, no matrix
multiplication beyond an averaged lookup.

Why this first, when the destination is a WebGPU transformer: it is pure Go, TinyGo-clean
today, roughly 30 MB quantised, and it delivers a **complete working pipeline** before any
GPU code exists. Phase 5 then replaces this implementation behind the same interface
without touching `vectordb`, `indexdb`, `storage` or `model`. If Phase 5 slips, the
product still works.

Retrieval quality is measurably below a full encoder. That trade is accepted for Phase 3
and revisited with numbers, not opinions — see §Evaluation.

### 2. Model selection — OPEN DECISION (master index O1)

Must be resolved before implementation. The constraint from **D5**: multilingual models
carry a ~250 k-token vocabulary, and the embedding table alone is
`250 000 × dim × 4` bytes in fp32 — for a static model the table **is** the model.

| Candidate | Dim | fp32 table | int8 table | Spanish |
|---|---|---|---|---|
| `potion-multilingual-128M` | 256 | ~256 MB | ~64 MB | yes |
| a 128-dim multilingual distillation | 128 | ~128 MB | ~32 MB | yes |
| English-only static (`potion-base-8M`) | 256 | ~32 MB | ~8 MB | **no — disqualified by D5** |

Working assumption until decided: **multilingual, 256 dims, int8 table with per-row
scales**. Fill this table with measured numbers for the actual artifacts before choosing;
the figures above are order-of-magnitude.

Deliverable of this decision: a one-off offline converter (a `cmd/` tool, Go, not shipped
to the browser) that turns the published model into the artifact format in
`plans/weights.md`.

### 3. `static.go` — the implementation

```go
type StaticConfig struct {
	Weights   weights.Artifact  // token table + scales + tokenizer vocab
	Tokenizer tokenizer.Tokenizer
	Pooling   Pooling           // MeanPooling default
}

func NewStatic(cfg StaticConfig) (Embedder, error)
```

Hot loop per text: encode → for each token id, accumulate its dequantised row into an
accumulator → divide by token count → `vector.Normalize`. One accumulator buffer, reused
across the batch: zero allocations per text after warm-up.

int8 dequantisation is `float32(q) * scale[row]`, fused into the accumulation so the
dequantised row is never materialised.

### 4. `mock.go` — deterministic embedder for downstream tests

`vectordb` and `agent` must test without a model. Ship a deterministic embedder derived
from a text hash — same text, same vector, in-package, no artifact needed. Exported,
because it is `DEFAULT_LLM_SKILL.md` §2's "every external interface gets a mock" applied
across repository boundaries.

### 5. Artifact loading and caching

First load fetches the artifact over HTTP (`webtyp.com/fetch`) and stores it in
IndexedDB; subsequent loads read it from there. Once per browser, not once per session —
at 30-100 MB the difference is the product being usable or not.

Cache key includes the model id and artifact version, so an upgrade does not serve stale
weights. A partial download must not be cached: write the blob only after the full body
is read and its length checked against the header.

## Phase 5 — transformer encoder

A second implementation, `NewTransformer`, over `webtyp/nn` and `webtyp/webgpu`. Same
`Embedder` interface, different `ID()`. It is specified in `plans/nn.md`; nothing in this
plan blocks on it, and nothing here needs to change when it lands except adding a
constructor.

## Evaluation

Quality is not assertable in a unit test, so it gets its own harness — otherwise the
Phase 3 → Phase 5 trade is decided on vibes:

- A small Spanish retrieval set committed as `testdata/` (queries, documents, relevance
  judgements), a few hundred entries.
- `cmd/eval` reports recall@1, recall@10 and MRR for any `Embedder`.
- Results recorded in the README, per model id. Phase 5 must beat Phase 3 on this
  harness to justify its existence.

## Tests

| Test | Asserts |
|---|---|
| `TestStatic_DimMatchesArtifact` | |
| `TestStatic_Deterministic` | the same text twice gives bit-identical vectors |
| `TestStatic_Normalised` | every output has L2 norm 1 within 1e-6 |
| `TestStatic_BatchMatchesSingle` | `Embed` over 10 texts equals 10 single calls |
| `TestStatic_EmptyText` | a defined vector, not NaN, not a panic |
| `TestStatic_AllUnknownTokens` | a string of nothing but `[UNK]` does not divide by zero |
| `TestStatic_WrongDstLength` | an error, never a partial write |
| `TestStatic_ZeroAllocsPerText` | after warm-up, with `dst` pre-sized |
| `TestStatic_Dequantise` | int8 + scale reproduces the fp32 reference within tolerance |
| `TestMock_Deterministic` | the mock's contract, since other repositories rely on it |
| `TestCache_SecondLoadSkipsFetch` | asserted through a mock fetcher |
| `TestCache_PartialDownloadNotCached` | a truncated body leaves no cache entry |
| `TestCache_VersionBumpInvalidates` | |

## Acceptance checklist

```bash
go vet ./...
gotest
gotest -tinygo                            # cache path needs a browser
GOOS=js GOARCH=wasm go build ./...
go run ./cmd/eval -model static           # recall@10 recorded in README
grep -rn "webtyp.com/vectordb" .          # → empty: the port does not know its consumer
```
