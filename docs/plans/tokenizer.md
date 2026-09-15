---
PLAN: "feat: webtyp/tokenizer — text to token ids"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/tokenizer (to be created)
---

> New repository. Moves to `tokenizer/docs/PLAN.md` once the repository exists.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md

# Plan — `webtyp/tokenizer`

## Single responsibility

Text in, token ids out. Nothing else. No embeddings, no model weights, no GPU, no
storage. Pure Go, zero dependencies, compiles everywhere, fully testable without a
browser.

It is separate from `embed` because both phases of the embedder need it identically — a
static embedding table and a transformer encoder tokenise exactly the same way — and
because tokenisation is where subtle, hard-to-find bugs live. It deserves its own test
suite and its own version.

## Why it must be exact

A tokeniser that disagrees with the one used to train the model produces embeddings that
are *plausible but wrong*: no crash, no error, just quietly degraded retrieval that looks
like a bad model. Every test in §Tests exists to pin behaviour against the reference
implementation, not to check that the code runs.

## Scope

**v1: WordPiece** (BERT family, which covers the multilingual sentence-transformer
candidates in `plans/embed.md`).

**v2: Unigram/SentencePiece**, if the chosen model needs it. Decide only after
`plans/embed.md` §2 is resolved — implementing both up front is speculative.

BPE is out of scope: no candidate model uses it.

## API

```go
// Tokenizer turns text into model input ids.
type Tokenizer interface {
	// Encode appends token ids for text into dst and returns it. The caller owns
	// dst, so a loop over many texts allocates once.
	Encode(dst []int32, text string) []int32

	// Decode is for debugging and tests only; it is not on any hot path.
	Decode(ids []int32) string

	VocabSize() int
	MaxLen() int
}

type Config struct {
	Vocab        map[string]int32 // loaded from the model artifact
	Lowercase    bool
	StripAccents bool             // note: NOT the same as Lowercase — see below
	MaxLen       int              // truncate; 0 = unlimited
	UnkToken     string           // "[UNK]"
	ClsToken     string           // "[CLS]" — prepended when non-empty
	SepToken     string           // "[SEP]" — appended when non-empty
}

func NewWordPiece(cfg Config) (Tokenizer, error)
```

`Encode` takes a destination slice rather than returning a fresh one because embedding a
batch of 1024 documents would otherwise allocate 1024 slices.

## Implementation notes

The pipeline is: normalise → pre-tokenise on whitespace and punctuation → greedy
longest-match-first WordPiece per word → add special tokens → truncate.

Three details that are usually got wrong:

1. **Accent stripping is not lowercasing.** A multilingual model for Spanish will
   typically have `strip_accents = false`, because *ánimo* and *animo* are different
   words. Getting this backwards silently degrades exactly the language this project
   cares about (master index **D5**). The flag is explicit and separate for that reason,
   and its default must come from the model artifact, never from a constant here.

2. **Unicode without `golang.org/x/text`.** NFD normalisation and category-based
   punctuation splitting need care under TinyGo, where `unicode` tables inflate the
   binary. Measure the WASM binary impact before committing to a table-driven approach;
   a reduced table covering Latin, punctuation and CJK ranges may be the right trade.
   Record the measurement in the README.

3. **Greedy longest-match is over *bytes* after normalisation**, and a word that cannot
   be segmented becomes a single `[UNK]` — not a sequence of `[UNK]` per character.

## Tests

Fixtures are the deliverable here. Generate them once with the reference Python
tokeniser, commit them as `testdata/*.json`, and pin against them forever.

| Test | Asserts |
|---|---|
| `TestEncode_MatchesReferenceFixtures` | every fixture pair encodes identically — the only test that really matters |
| `TestEncode_Spanish` | accented words, `ñ`, `¿¡`, with `StripAccents` both on and off |
| `TestEncode_UnknownWord` | one `[UNK]`, not one per character |
| `TestEncode_SpecialTokens` | `[CLS]`/`[SEP]` placement, and their absence when unset |
| `TestEncode_Truncation` | `MaxLen` truncates and still closes with `[SEP]` |
| `TestEncode_EmptyString` | special tokens only, no panic |
| `TestEncode_ReusesDst` | a second call appends into the caller's slice without reallocating |
| `TestEncode_ZeroAllocsWithCapacity` | `testing.AllocsPerRun` == 0 when `dst` has capacity |
| `TestDecode_RoundTrip` | for text that tokenises cleanly |
| `TestEncode_Emoji` | multi-byte sequences do not split mid-rune |

## Acceptance checklist

```bash
go vet ./...
gotest
ls testdata/*.json                        # fixtures committed
GOOS=js GOARCH=wasm go build ./...
grep -rn "golang.org/x/" .                # → empty, or justified in the README
```
