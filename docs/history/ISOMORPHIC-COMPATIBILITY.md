# Plan: Isomorphic Compatibility Refactor — `context`, `time`, and `agent`

## Context

The isomorphic package policy in `tinywasm/agent` is correct: the agent should use `tinywasm/context` and `tinywasm/time` instead of stdlib. The previous analysis that declared these packages "incompatible" was wrong — the correct fix is to **update the libraries** to expose the APIs needed by the agent package, not to abandon the policy.

Three libraries are involved:
1. **`tinywasm/context`** — missing a backend compatibility layer and missing `context.Context` interface compliance on WASM.
2. **`tinywasm/time`** — missing `Duration` type, time constants (`Second`, `Minute`, `Millisecond`), and helper functions (`UnixSec`, `Since`, `Milliseconds`).
3. **`tinywasm/agent`** — needs to be refactored to use the updated isomorphic packages.

This plan creates a documentation spec (`.md` plan file) inside each library, then executes the agent refactor.

---

## Step 1: Revert incorrect IMPLEMENTATION.md change

**File:** `agent/docs/IMPLEMENTATION.md`

Restore the original isomorphic policy (lines 13 and 51–54). Replace the current incorrect text with:

```
- **Isomorphic Package Policy:** This library runs on both backend and WASM. Always prefer
  tinywasm/ packages over stdlib: github.com/tinywasm/fmt (replaces fmt, errors, strings, strconv),
  github.com/tinywasm/json (replaces encoding/json), github.com/tinywasm/context (replaces context),
  github.com/tinywasm/time (replaces time). Use stdlib directly only for packages without a
  tinywasm/ equivalent (net/http, sync). Allowed external packages: modernc.org/sqlite.
```

Production Dependencies (v1):
```
- github.com/tinywasm/fmt, github.com/tinywasm/context, github.com/tinywasm/time (isomorphic)
- modernc.org/sqlite (pure Go SQLite driver)
- github.com/google/uuid (ID generation)
```

> Note on `tinywasm/json`: Not yet implemented. Until it exists, `encoding/json` is used directly in backend-only files (memory.go, mcp_client.go, mcp_registry.go) — these use SQLite/HTTP which are already backend-only. This will be resolved when `tinywasm/json` is created.

---

## Step 2: Create `context/docs/BACKEND_COMPAT.md`

**Path:** `/home/cesar/Dev/Pkg/tinywasm/context/docs/BACKEND_COMPAT.md`

This file is the implementation plan for the `tinywasm/context` refactor. Full content to create:

```markdown
# Implementation Plan: Backend Compatibility Refactor

## Context

The `tinywasm/context` package currently exposes only a custom WASM-optimized `*Context` struct
that is incompatible with the stdlib `context.Context` interface. Libraries like `tinywasm/agent`
that use SQLite and net/http require stdlib `context.Context` on the backend. This plan adds a
dual build-tag implementation so that on backend, `Context` IS `context.Context` (type alias),
and on WASM, `Context` is a `*tinyCtx` struct that satisfies the `context.Context` interface.

## Breaking Changes

- `*Context` struct is renamed to `*tinyCtx` (unexported). WASM users must use `Context` as
  the type (which is `*tinyCtx` on WASM), not `*Context` directly.
- Method `Value(key string) string` is renamed to `Get(key string) string` to avoid a signature
  conflict with `context.Context.Value(key any) any`.

## File Restructure

Current: single `context.go` (no build tag)
New structure:

```
context/
├── backStlib.go    # //go:build !wasm — backend type alias + wrappers
├── frontWasm.go    # //go:build wasm  — tinyCtx struct + context.Context impl
└── (tests unchanged, already build-tag split)
```

The existing `context.go` is deleted; its content is split between the two new files.

## Backend File: `backStlib.go` (!wasm)

```go
//go:build !wasm

package context

import stdlib "context"

// Context is a type alias for stdlib context.Context on backend (non-WASM) builds.
// On WASM builds, Context is *tinyCtx which also satisfies context.Context.
type Context = stdlib.Context

// CancelFunc is a type alias for stdlib context.CancelFunc on backend builds.
type CancelFunc = stdlib.CancelFunc

// Background returns a non-nil, empty Context. Equivalent to context.Background().
func Background() Context { return stdlib.Background() }

// TODO returns a non-nil, empty Context. Equivalent to context.TODO().
func TODO() Context { return stdlib.TODO() }

// WithCancel returns a copy of parent with a new Done channel.
func WithCancel(parent Context) (Context, CancelFunc) {
    return stdlib.WithCancel(parent)
}

// WithTimeout returns a copy of parent with a timeout set to nanoseconds from now.
// ns is the duration in nanoseconds (e.g., 30 * time.Second = 30_000_000_000).
func WithTimeout(parent Context, ns int64) (Context, CancelFunc) {
    return stdlib.WithTimeout(parent, stdlibtime.Duration(ns))
}

// WithValue returns a copy of parent with the given string key-value pair.
// Only string keys/values are supported (compatible with tinyCtx.Get on WASM).
func WithValue(parent Context, key, value string) Context {
    return stdlib.WithValue(parent, key, value)
}
```

> `WithTimeout` imports stdlib `time` internally (unexported, no conflict with tinywasm/time).

## WASM File: `frontWasm.go` (wasm)

```go
//go:build wasm

package context

import (
    "time"
    "github.com/tinywasm/fmt"
)

// tinyCtx is the minimalist WASM context. No channels, no maps, fixed 16 pairs.
// Satisfies the stdlib context.Context interface.
type tinyCtx struct {
    pairs [16]fmt.KeyValue
    count uint8
}

// Context is a type alias for *tinyCtx on WASM builds.
type Context = *tinyCtx

// CancelFunc is a no-op cancel function on WASM (no goroutines/channels for cancellation).
type CancelFunc = func()

var errCapacityExceeded = fmt.Err("context: max 16 values exceeded")

// Deadline returns zero time and false (no deadlines on WASM).
func (c *tinyCtx) Deadline() (time.Time, bool) { return time.Time{}, false }

// Done returns nil (non-cancellable context on WASM; select on nil channel blocks forever).
func (c *tinyCtx) Done() <-chan struct{} { return nil }

// Err always returns nil (no cancellation on WASM).
func (c *tinyCtx) Err() error { return nil }

// Value returns the value for key if key is a string; otherwise nil.
// Satisfies context.Context interface.
func (c *tinyCtx) Value(key any) any {
    if k, ok := key.(string); ok {
        return c.get(k)
    }
    return nil
}

// Get retrieves the string value for key (reverse search, prioritizes latest).
// Replaces the old Value(key string) string method.
func (c *tinyCtx) Get(key string) string {
    if c == nil {
        return ""
    }
    return c.get(key)
}

func (c *tinyCtx) get(key string) string {
    if c == nil {
        return ""
    }
    for i := int(c.count) - 1; i >= 0; i-- {
        if c.pairs[i].Key == key {
            return c.pairs[i].Value
        }
    }
    return ""
}

// Set adds a key-value pair in-place. Returns error if capacity (16) is exceeded.
func (c *tinyCtx) Set(key, value string) error {
    if c.count >= 16 {
        return errCapacityExceeded
    }
    c.pairs[c.count] = fmt.KeyValue{Key: key, Value: value}
    c.count++
    return nil
}

// Keys returns all keys in the context (including duplicates).
func (c *tinyCtx) Keys() []string {
    if c == nil || c.count == 0 {
        return nil
    }
    keys := make([]string, c.count)
    for i := uint8(0); i < c.count; i++ {
        keys[i] = c.pairs[i].Key
    }
    return keys
}

func Background() Context { return &tinyCtx{} }
func TODO() Context        { return &tinyCtx{} }

func WithCancel(parent Context) (Context, CancelFunc) {
    return parent, func() {}
}

// WithTimeout is a no-op on WASM (returns the parent context unchanged).
// ns is ignored. Cancel is a no-op func.
func WithTimeout(parent Context, ns int64) (Context, CancelFunc) {
    return parent, func() {}
}

func WithValue(parent Context, key, value string) (Context, error) {
    return WithValueCtx(parent, key, value)
}

func WithValueCtx(parent Context, key, value string) (*tinyCtx, error) {
    ctx := &tinyCtx{}
    if parent != nil {
        ctx.pairs = parent.pairs
        ctx.count = parent.count
    }
    if ctx.count >= 16 {
        return nil, errCapacityExceeded
    }
    ctx.pairs[ctx.count] = fmt.KeyValue{Key: key, Value: value}
    ctx.count++
    return ctx, nil
}
```

> Note: On WASM `WithValue` returns an error (capacity constraint). On backend it doesn't.
> The agent only calls `WithValue` on backend (BackStlib.go), so this asymmetry is acceptable.
> Alternatively, drop the error return on WASM and panic or ignore capacity overflow.

## Test Updates

- `ctx_shared_test.go`: rename `ctx.Value("key")` calls to `ctx.Get("key")` (WASM only)
- `ctx_stdlib_test.go`: test `WithTimeout`, `WithCancel`, `WithValue`
- `ctx_wasm_test.go`: test that `Background()` returns a value satisfying `context.Context` interface

## Verification

```bash
cd /home/cesar/Dev/Pkg/tinywasm/context && gotest .
```
All tests must pass. Then publish with `gopush`.
```

---

## Step 3: Create `time/docs/DURATION_SUPPORT.md`

**Path:** `/home/cesar/Dev/Pkg/tinywasm/time/docs/DURATION_SUPPORT.md`

```markdown
# Implementation Plan: Duration Type and Backend Compat Extensions

## Context

The `tinywasm/agent` package (and other backend libraries) need to use `tinywasm/time` instead of
stdlib `time` for timestamp operations and timeouts. Currently `tinywasm/time` is missing:
- A `Duration` type (for timeout specification)
- Time constants: `Second`, `Millisecond`, `Minute`
- `UnixSec()` — current timestamp in Unix seconds (agent uses `time.Now().Unix()`)
- `Since(nanoStart int64) int64` — elapsed nanoseconds (agent uses `time.Since(start)`)
- `Milliseconds(ns int64) int64` — convert nanoseconds to milliseconds

The existing `Now()` function already covers `time.Now().UnixNano()`.

## Design rule: only what MUST differ goes in build-tag files

| Addition | File | Reason |
|----------|------|--------|
| `type Duration` + constants | `backStlib.go` AND `frontWasm.go` | Type IS different: `= stdlib.Duration` vs `int64` |
| `UnixSec()`, `Since()`, `Milliseconds()` | **`api.go`** (shared) | Use existing `Now()` — no platform code needed |

> `api.go` already exposes `Now() int64` (delegates to `provider`). Functions that only call
> `Now()` or do pure math belong there — no duplication across build-tag files.

## Changes: `api.go` (shared — append)

```go
func UnixSec() int64              { return Now() / 1_000_000_000 }
func Since(nanoStart int64) int64 { return Now() - nanoStart }
func Milliseconds(ns int64) int64 { return ns / 1_000_000 }
```

## Changes: `backStlib.go` (backend — append)

```go
type Duration = time.Duration  // type alias — interchangeable with stdlib time.Duration
const (
    Nanosecond  Duration = 1
    Microsecond Duration = 1000 * Nanosecond
    Millisecond Duration = 1000 * Microsecond
    Second      Duration = 1000 * Millisecond
    Minute      Duration = 60 * Second
    Hour        Duration = 60 * Minute
)
```

> `backStlib.go` already imports `"time"` — no new import needed.

## Changes: `frontWasm.go` (WASM — append)

```go
type Duration int64  // independent type — same nanosecond scale as stdlib
const (
    Nanosecond  Duration = 1
    Microsecond Duration = 1000 * Nanosecond
    Millisecond Duration = 1000 * Microsecond
    Second      Duration = 1000 * Millisecond
    Minute      Duration = 60 * Second
    Hour        Duration = 60 * Minute
)
```

## Verification

```bash
cd /home/cesar/Dev/Pkg/tinywasm/time && gotest .
```

All existing tests must still pass. Add tests for the new functions in `backStlib_test.go`
and `frontWasm_test.go` (via shared runner in `data_test.go`).
```

---

## Step 4: Refactor `tinywasm/agent`

After both library plans are executed and published (new versions available), refactor the agent.

### `go.mod` changes

```
require (
    github.com/tinywasm/context v0.0.14  // (or whatever next version is after refactor)
    github.com/tinywasm/fmt     v0.17.4
    github.com/tinywasm/time    v0.3.4   // (or whatever next version is after refactor)
    github.com/google/uuid      v1.6.0
    modernc.org/sqlite          v1.45.0
)
```

### `types.go` — `Config.MCPTimeout` type change

```go
// Before:
MCPTimeout time.Duration

// After:
MCPTimeout int64  // nanoseconds; use twtime.Second for convenience (e.g., 30*twtime.Second)
```

### Import replacements (all source files)

| Old import | New import |
|-----------|-----------|
| `"context"` | `twctx "github.com/tinywasm/context"` |
| `"time"` | `twtime "github.com/tinywasm/time"` |

### API replacements per file

**`interfaces.go`:**
- `context.Context` → `twctx.Context`

**`agent.go`:**
- `context.Background()` → `twctx.Background()`
- `context.WithTimeout(ctx, cfg.MCPTimeout)` → `twctx.WithTimeout(ctx, cfg.MCPTimeout)`
- `cfg.MCPTimeout = 30 * time.Second` → `cfg.MCPTimeout = 30 * twtime.Second`
- `cfg.MCPTimeout = 1 * time.Minute` → `cfg.MCPTimeout = twtime.Minute`

**`orchestrator.go`:**
- `context.Context` → `twctx.Context`
- `time.Now().Unix()` → `twtime.UnixSec()`
- `startTime := time.Now()` → `startTime := twtime.Now()`
- `time.Since(startTime).Milliseconds()` → `twtime.Milliseconds(twtime.Since(startTime))`

**`context_window.go`:**
- `context.Context` → `twctx.Context`
- `time.Now().Unix()` → `twtime.UnixSec()`

**`mcp_client.go`:**
- `context.Context` → `twctx.Context`
- `30 * time.Second` → `twtime.Duration(30 * twtime.Second)` — BUT since `twtime.Duration = time.Duration` on backend (type alias), `http.Client{Timeout: 30 * twtime.Second}` works directly without a cast.
- `time.Now().UnixNano()` → `twtime.Now()`

**`mcp_registry.go`:**
- `context.Context` → `twctx.Context`
- `mcpCaller` interface already uses `context.Context` → change to `twctx.Context`

**`memory.go`:**
- `context.Context` → `twctx.Context`
- `db.ExecContext(ctx, ...)` and `db.QueryContext(ctx, ...)` — works because on backend `twctx.Context = context.Context` (type alias), so stdlib APIs accept it directly.

### Test file replacements

**`setup_test.go`, `orchestrator_test.go`, `mock_memory_test.go`, `mock_mcp_test.go`:**
- `import "context"` → `import twctx "github.com/tinywasm/context"`
- `context.Background()` → `twctx.Background()`
- `context.Context` → `twctx.Context`

**`integration_test.go`:**
- `import "time"` → keep for stdlib Timeout in OllamaClient (or use twtime)
- `import "context"` → `import twctx "github.com/tinywasm/context"`

---

## Critical Files

| File | Action |
|------|--------|
| `context/docs/BACKEND_COMPAT.md` | **Create** |
| `time/docs/DURATION_SUPPORT.md` | **Create** |
| `agent/docs/IMPLEMENTATION.md` | **Modify** — revert isomorphic policy to original |
| `agent/types.go` | **Modify** — MCPTimeout `int64` |
| `agent/interfaces.go` | **Modify** — `twctx.Context` |
| `agent/agent.go` | **Modify** — imports + API replacements |
| `agent/orchestrator.go` | **Modify** — imports + API replacements |
| `agent/context_window.go` | **Modify** — imports + API replacements |
| `agent/mcp_client.go` | **Modify** — imports + API replacements |
| `agent/mcp_registry.go` | **Modify** — imports + API replacements |
| `agent/memory.go` | **Modify** — import only (`twctx.Context`) |
| `agent/go.mod` | **Modify** — add context, time deps |
| All `*_test.go` files | **Modify** — imports + API replacements |

---

## Execution Order

1. Create `context/docs/BACKEND_COMPAT.md`
2. Create `time/docs/DURATION_SUPPORT.md`
3. Revert `agent/docs/IMPLEMENTATION.md` to correct isomorphic policy
4. (Libraries must be implemented and published before step 5 can run)
5. Refactor `tinywasm/agent` (steps above) after library versions are available

---

## Verification

After step 3 only (doc creation):
```bash
# No tests needed — docs only
```

After step 5 (agent refactor):
```bash
cd /home/cesar/Dev/Pkg/tinywasm/agent && gotest .
```
All tests must pass with the new imports.
