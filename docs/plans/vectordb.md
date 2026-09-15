---
PLAN: "feat: webtyp/vectordb — document store with kNN retrieval"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/vectordb (to be created)
---

> New repository. This file lives in `agent/docs/plans/` until `webtyp/vectordb` exists,
> then moves to `vectordb/docs/PLAN.md` unchanged.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md

# Plan — `webtyp/vectordb`

## Single responsibility

The store: documents in, ranked documents out. It owns the schema, the shard layout, the
in-memory index, eviction policy and quota. It owns **no** arithmetic (that is
`webtyp/vector`), **no** embedding generation (that is `webtyp/embed`) and **no**
knowledge of IndexedDB (that is `webtyp/indexdb`, reached through `storage.Conn`).

This is the Go port of `webtyp/vector-storage`. The port map, and the list of behaviours
deliberately not ported, is in that repository's `docs/PLAN.md` — read it before writing
code here.

## Licence obligation

The TypeScript original is MIT by Nitai Aharoni. This repository **must** ship a `NOTICE`
file crediting the author and reproducing the MIT terms alongside its own licence.
Master index **D6**. Not optional.

## Dependencies

`webtyp.com/vector`, `webtyp.com/storage`, `webtyp.com/model`, `webtyp.com/embed`
(the port interface only), `webtyp.com/fmt`, `webtyp.com/context`.

It does **not** import `webtyp.com/indexdb`. The backend arrives as an injected
`storage.Conn`, which is what makes the same store run in a browser and on a server, and
what makes it testable against `storage/mem` in plain Go.

## Schema

Three object stores / tables, declared as `model.Definition` values so every backend
creates them the same way.

```go
// vec_docs — one row per document. Text and metadata are read ONLY for the final
// top-k, never during scoring (master index D2).
var DocModel = model.Definition{
	Name: "vec_docs",
	Fields: model.Fields{
		{Name: "id",      Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "text",    Type: model.Text(), NotNull: true},
		{Name: "meta",    Type: model.Raw()},                    // opaque JSON payload
		{Name: "tags",    Type: model.Text()},                   // "|a|b|c|", LIKE-filterable
		{Name: "hash",    Type: model.Text(), NotNull: true},    // content hash, dedup
		{Name: "created", Type: model.Int(),  NotNull: true},
		{Name: "hits",    Type: model.Int(),  NotNull: true},
		{Name: "shard",   Type: model.Int(),  NotNull: true},
		{Name: "slot",    Type: model.Int(),  NotNull: true},
	},
}

// vec_shards — one row per ShardSize vectors, as a single blob.
var ShardModel = model.Definition{
	Name: "vec_shards",
	Fields: model.Fields{
		{Name: "id",    Type: model.Int(),  DB: &model.FieldDB{PK: true}},
		{Name: "count", Type: model.Int(),  NotNull: true},
		{Name: "data",  Type: model.Blob(), NotNull: true},
	},
}

// vec_index — exactly one row. Rejects an arena that does not match the corpus.
var IndexModel = model.Definition{
	Name: "vec_index",
	Fields: model.Fields{
		{Name: "id",         Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "dim",        Type: model.Int(),  NotNull: true},
		{Name: "model_id",   Type: model.Text(), NotNull: true}, // which embedder produced these
		{Name: "shard_size", Type: model.Int(),  NotNull: true},
		{Name: "version",    Type: model.Int(),  NotNull: true},
	},
}
```

`vec_shards.data` is `model.Blob()`, not `model.Vector(dim)`: a shard holds
`count × dim` floats, not one vector, so a fixed dimension would be wrong. Dimension
agreement is enforced by `vec_index.dim` against `len(data)/4/count`.

`model_id` matters more than it looks: vectors from two different embedding models are
not comparable, and mixing them produces plausible-looking nonsense rather than an error.
Loading a corpus whose `model_id` differs from the configured embedder must **fail**, with
a message saying the corpus needs re-indexing.

The `tags` encoding (`|a|b|c|`) is a v1 compromise — `model` has no string-slice field
type. It is `LIKE '%|tag|%'`-filterable, which is enough for the header-array filter below
since filtering happens in RAM anyway. Master index **O3**.

## API

```go
type Config struct {
	Conn      storage.Conn      // required — the backend, injected
	Embedder  embed.Embedder    // required — text → vectors
	IDGen     model.IDGenerator // required — no concrete generator constructed here
	ShardSize int               // default 1024
	MaxDocs   int               // 0 = unbounded; LRU eviction above this
	MaxBytes  int64             // 0 = derive from navigator.storage.estimate()
}

// New opens the store and loads the index into memory. It RETURNS AN ERROR rather
// than racing a background load — the TypeScript original's constructor kicks off an
// un-awaited load, so every early call silently searches an empty corpus.
func New(ctx *context.Context, cfg Config) (*Store, error)

func (s *Store) Add(ctx *context.Context, docs ...Doc) ([]string, error)
func (s *Store) Search(ctx *context.Context, q Query) ([]Match, error)
func (s *Store) Delete(ctx *context.Context, ids ...string) error
func (s *Store) Len() int
func (s *Store) Close() error

type Doc struct {
	ID   string          // empty → IDGen.NewID()
	Text string
	Meta model.RawJSON
	Tags []string
}

type Query struct {
	Text        string    // embedded via Config.Embedder
	Vector      []float32 // pre-computed; takes precedence over Text
	K           int       // default 4, matching the TypeScript original
	IncludeTags []string  // AND
	ExcludeTags []string  // AND NOT
	MinScore    float32
}

type Match struct {
	Doc
	Score float32 // cosine similarity in [-1, 1]
}
```

Note what is absent: `Search` does not echo the query embedding back to the caller the
way `similaritySearch` does. Nobody used it, and returning it forces a copy.

## In-memory index

```go
type header struct {
	id      string
	tags    string
	created int64
	hits    int32
	deleted bool
}
```

`Store` holds `*vector.Arena` plus `[]header`, parallel by slot. That is the entire
search index. Text and metadata stay on disk.

Resident cost is `N × Dim × 4` plus roughly 64 bytes of header per document: 10 000
documents at 384 dims is about 15.6 MB. Past ~100 000 documents this needs int8
quantisation, which is Phase 5 — until then `MaxDocs` is the guard rail and exceeding it
evicts.

## Write path

1. Hash each document's text; drop any whose hash is already in the corpus. The
   TypeScript original compares text strings across the whole array per insert — O(N·M)
   for a batch.
2. `Embedder.Embed` for the whole batch, into a scratch `[]float32`.
3. Append to the arena (normalising on the way in).
4. Assign shard and slot; mark the touched shards dirty.
5. **One transaction** (`storage.TxExecutor`, see the `indexdb` plan §3) writing the
   document rows and the dirty shard blobs together. Vectors and text must never be
   observable out of sync.
6. Evict if over budget, before committing.

Only dirty shards are rewritten. The original rewrites the entire corpus on every write,
including after a read — `similaritySearch` calls `saveToIndexDbStorage()` to persist hit
counters.

## Read path

1. Embed the query (or take `Query.Vector`), normalise.
2. Build the `keep` closure over the header array: skip deleted, apply tag filters.
   Filtering happens **before** scoring, so a filtered query is cheaper, not dearer.
3. `arena.Search(query, keep, topk)` — zero allocations, no JS boundary crossing.
4. Load only the k winning rows from `vec_docs` by primary key.
5. Increment `hits` in the header array **in memory**; flush lazily (on `Close`, on the
   next write, or every N searches). A read must not trigger a full corpus rewrite.

## Eviction and quota

Ordering is the original's and is kept: `hits` ascending, then `created` ascending —
least used, oldest first.

The size measurement is not the original's. `getObjectSizeInMB` serialises the entire
corpus with `JSON.stringify` just to measure it, and measures the wrong thing. Here:
- Arena bytes are known arithmetically: `N × Dim × 4`.
- Row overhead is estimated per document and calibrated once.
- `navigator.storage.estimate()` gives the real browser budget, behind a build-tagged
  file so the package still builds for a server target.
- `navigator.storage.persist()` is requested at `New` — without it the browser may
  evict the whole database under pressure, silently.

Eviction is a soft delete in the header (`deleted = true`) plus a shard compaction when a
shard drops below half full. Compaction renumbers slots, so it takes the same transaction
as any other write.

## Tests

The suite runs **twice**: against `storage/mem` in standard Go, and against
`webtyp.com/indexdb` in a browser under `gotest -tinygo`. Same test bodies, different
factory — the pattern `indexdb/tests/conformance_test.go` already uses.

A `MockEmbedder` returning deterministic vectors from a text hash is mandatory: the store's
tests must not depend on a real model. (`DEFAULT_LLM_SKILL.md` §2 — every external
interface gets a mock.)

| Test | Asserts |
|---|---|
| `TestAdd_ThenSearchFindsIt` | the obvious round trip |
| `TestAdd_DeduplicatesByHash` | the same text twice yields one document |
| `TestAdd_BatchOneTransaction` | 1024 documents use one transaction, asserted through the mock recorder |
| `TestSearch_RanksByCosine` | known vectors, hand-computed expected order |
| `TestSearch_RespectsK` | including k > corpus size |
| `TestSearch_IncludeExcludeTags` | filters apply before scoring |
| `TestSearch_MinScore` | below-threshold matches are dropped |
| `TestSearch_EmptyCorpus` | returns empty, not an error, and does not panic |
| `TestSearch_DoesNotRewriteCorpus` | a search issues zero writes to the shard table — the regression test for the original's read-path write |
| `TestReopen_LoadsIndex` | close, reopen on the same `Conn`, search still works |
| `TestReopen_ModelMismatchFails` | a corpus written by model A rejected when configured with model B, with "re-index" in the message |
| `TestReopen_DimMismatchFails` | `vec_index.dim` disagreeing with the shard blob length is an error |
| `TestDelete_RemovesFromResults` | and survives a reopen |
| `TestEvict_LeastUsedOldestFirst` | the ordering contract |
| `TestEvict_CompactsShards` | a half-empty shard is compacted, slots renumbered, search still correct |
| `TestNew_ReturnsBeforeUse` | a search immediately after `New` sees the full corpus — the regression test for the original's un-awaited load |

## Acceptance checklist

```bash
go vet ./...
gotest                 # mem backend, standard Go
gotest -tinygo         # indexdb backend, browser
ls NOTICE              # licence obligation, master index D6
grep -rn "webtyp.com/indexdb" --include="*.go" . | grep -v _test.go   # → empty
grep -rn "syscall/js" --include="*.go" . | grep -v _wasm.go           # → only build-tagged quota code
```
