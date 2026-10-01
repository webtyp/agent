# Ecosystem Coding Skill — `webtyp/agent`

Architectural directives and mandatory conventions for agents modifying code in this repository.

---

## The Core Rule

**This repository orchestrates. It does not implement domain models, model runtimes, storage drivers, or retrieval algorithms.**

Every task in this repo must respect the boundaries defined in [docs/AGENT_ECOSYSTEM_MASTER_PLAN.md](AGENT_ECOSYSTEM_MASTER_PLAN.md).

---

## Architectural Principles

### 1. Zero Direct Side Effects in Orchestration
The agent loop (`turn.go`) drives state transitions via the decision model and code guard. It must never directly write to persistent storage or call external APIs outside the ports defined in `interfaces.go`.

### 2. The Code Checks First
Before any model reads a message, the deterministic code guard (`guard.go`) cleans invisible characters, verifies character limits, and checks forbidden phrases and chat template markers.

### 3. The Decision Model Never Writes
All decisions (routing, injection checks, yes/no evaluations, enums, critic validation) are made via `llm.Decider`. Free-form responses are generated strictly by templates or an optional constrained writer model (`llm.Client`).

### 4. Port Isolation
- **`MemoryStore` is a pure interface.** Do not add database connections, drivers, or SQL logic. Reference implementations must be strictly in-memory (`mem_memory.go`).
- **`ToolIndex` is a pure interface.** The keyword index in `mem_tool_index.go` is in-memory only. Vector search lives in `webtyp/agentmemory`.

### 5. WASM / TinyGo Isomorphic Compliance
Every file in root must compile under `GOOS=js GOARCH=wasm` and TinyGo.
- **Never import standard library packages forbidden by `AGENTS.md`**: `fmt`, `errors`, `strconv`, `strings`, `encoding/json`, `context`, `time`, `net/http`, `os`, `log`.
- **Use ecosystem packages instead**: `webtyp.com/fmt`, `webtyp.com/json`, `webtyp.com/context`, `webtyp.com/time`, `webtyp.com/unixid`.
- **No `map[K]V` in non-test code**: use slices.

---

## Testing Directive

All unit and integration tests must reside in `tests/` (`package tests`) and test strictly through the exported public API of `webtyp.com/agent`. Host-only test helpers may use stdlib imports.
