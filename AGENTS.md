# Agent Guide — `webtyp/agent`

Constraints for agents working on this library. **Read this before any change.**
The master index of the semantic-search work is [docs/MASTER_PLAN.md](docs/MASTER_PLAN.md); the
current work order, when one exists, is [docs/PLAN.md](docs/PLAN.md).

---

## What this library is

`agent` is the **orchestrator** of the webtyp ecosystem: the loop that turns a conversation, a set
of tools and a model into work. It is meant to be dropped into *any* project — a server, a CLI, a
browser tab — and to compose the other webtyp libraries rather than reimplement them.

Two consequences follow, and every rule below is one of them:

1. **It owns no backend.** How an agent stores its memory is a property of the *consuming project*,
   not of this library. `agent` declares the `MemoryStore` port; the implementation lives elsewhere
   (`webtyp/agentmemory`, or the app itself).
2. **It must be able to compile for the browser.** An orchestrator that cannot run next to
   `webtyp/vectordb` in a WASM tab is not agnostic; it is a server library with ambitions.

---

## The builds that define "done"

```bash
go vet ./...
gotest                                  # host: vet + race + cover + wasm
GOOS=js GOARCH=wasm go build ./...
tinygo build -target wasm -o /dev/null .
```

Every file must compile under `GOOS=js GOARCH=wasm` and TinyGo on its own.

---

## Never import these — reach for the ecosystem instead

| Never | Use instead | Why |
|---|---|---|
| `database/sql`, any SQL driver | `webtyp.com/storage` (`storage.Conn`) | `storage` is THE storage port; a driver here nails the orchestrator to one backend and to the host |
| `net/http` | `webtyp.com/fetch` | does not compile under TinyGo; `fetch` is isomorphic |
| `context` (stdlib) | `webtyp.com/context` | the ecosystem's `*context.Context` is what every webtyp API takes |
| `encoding/json` | `webtyp.com/json` | reflection-based `encoding/json` costs **~1 MB of wasm** on its own |
| `fmt`, `errors`, `strconv`, `strings` | `webtyp.com/fmt` | one small package replaces all four |
| `time` | `webtyp.com/time` | |
| `github.com/google/uuid` | `webtyp.com/unixid` | an ID generator already exists in the ecosystem, and this one is a third-party dependency for six lines of work |
| `os`, `log` | inject it | a library never touches the process environment or a global stream |
| `map[K]V` | `fmt.KeyValue` or a slice scanned linearly | TinyGo's map runtime is a size tax on every binary that imports this |

Any remaining third-party module in `go.mod` is a question to answer, not a fact to preserve.

---

## The `MemoryStore` rule

`MemoryStore` is a **port declared here and implemented elsewhere**. This repo may contain:

- the interface,
- the value types that cross it (`Message`, `Summary`, …),
- a **conformance suite** any implementation can run against itself,
- at most an in-memory reference implementation, with no driver and no SQL.

It may **not** contain a SQLite store, a schema string, or a `sql.Open`. Concretely: `memory.go`
and `schema.go` as they stand today belong in `webtyp/agentmemory`, behind the same interface.

The pattern is `webtyp/storage`'s, and it is worth copying exactly: a port, a conformance suite, a
reference `mem` backend, and every real backend in its own repo. That is what lets `orm`, `ddl`,
`indexdb`, `postgres` and `sqlt` coexist without any of them knowing about the others.

---

## Do not invent what the ecosystem already has

Before adding an interface or a helper, search for it. `storage`, `fetch`, `json`, `mcp`, `model`,
`context`, `fmt`, `time`, `unixid`, `crypto` cover most of what an orchestrator needs. An interface
is justified only when the thing it abstracts **varies by consumer** — an LLM provider varies, an
HTTP client does not.

---

## Layout & tests

- Flat hierarchy: Go files in the repo root, no subdirectories for library code.
- Max 500 lines per file; split by domain and rename when exceeded.
- More than 5 test files in the root → move **all** of them to `tests/`, package `tests`, consuming
  only the public API.
- Tests use the standard library only (`net/http/httptest` for a fake LLM endpoint is fine — it is
  host-only test code, and it must live behind the host build).
- Publish with `gopush 'message'` — never `git commit`/`git push` directly.

### Test layout — excepción documentada

Los tests white-box existentes en el paquete raíz (`package agent`) para FSM, registry e internals
se mantienen en la raíz porque moverlos a `tests/` requeriría exportar internals innecesariamente.
Cualquier test nuevo que solo consuma la API pública debe ir en `package agent_test` o en `tests/`, nunca en `package agent`.

---

## Known debt (do not extend, do not "fix" ad hoc)

| Debt | Where it goes |
|---|---|
| `SQLiteMemoryStore` + `schema` in the root package | `webtyp/agentmemory`; this repo keeps the port + conformance suite |

Each of these moves under its own `docs/PLAN.md`, dispatched deliberately. Do not bundle them into
an unrelated PR, and do not leave one half-done.

---

## Common mistakes to avoid

- Adding a concrete backend to the orchestrator "to make it usable out of the box". That is what
  makes it *un*usable everywhere else.
- Believing `gotest` green means the change is done — it never exercises TinyGo here.
- Describing work in a commit message that the diff does not contain. A PR whose commit is empty
  gets closed; if the work could not be finished, say so instead.
