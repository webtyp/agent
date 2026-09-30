---
PLAN: "feat: preselect tools — the first step already offers the tools that match the message"
TAG: v0.9.0
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> Part of [`AGENT_ECOSYSTEM_MASTER_PLAN.md`](AGENT_ECOSYSTEM_MASTER_PLAN.md) (open decision 1).

# Plan — `webtyp.com/agent`: offer the likely tools on the first step

## 0. Context

`agent` does not show the model every tool. On each step of `Run` it offers `search_tools`
plus the tools the model discovered earlier in that `Run` (`orchestrator.go`: `offered :=
[]llm.ToolDef{searchToolsDef}` in `Run`; `tool_search.go` adds discovered tools). This keeps
the prompt small when an application has many tools, but it costs the model **two hops**:
first search, then call.

**Measured on 2026-09-30** with Qwen3.5-0.8B (the model the ecosystem runs in the browser), the
clinic assistant's real identity, and the message "¿Hasta qué hora atendemos hoy?", 10 seeds
each, first step only:

| Offered on the first step | Calls a tool |
|---|---|
| only `search_tools` (today) | 1–2 of 10; otherwise it greets and answers nothing |
| `search_tools` + the opening-hours tool | 8–9 of 10 |

**The fix:** before the first step, the agent itself asks its `ToolIndex` for the tools that
match the user's message, and offers them together with `search_tools`. When the application
has so few tools that they all fit, it offers all of them.

## Development rules (inline)

- Primary runtime: browser, TinyGo/WASM (`GOOS=js GOARCH=wasm go build ./...`). Never import
  `fmt`, `errors`, `strings`, `strconv` (use `webtyp.com/fmt`), stdlib `context`
  (use `webtyp.com/context`), `encoding/json`, `map[K]V` in non-test files.
- Tests live in `tests/` (package `tests`) and use only the exported API.
- No `TODO`, no commented-out code. `gotest` green.

## Design gate (api-design — five answers)

1. **Prior art.**
   - Anthropic's tool search tool: tools marked `defer_loading` are found through search, while
     the rest are sent up front.
   - LangGraph's "bigtool" retrieves the top-k tools for the query before calling the model.
   - Semantic Kernel's function filtering selects functions by relevance to the request.

   All of them preselect by the request and keep a search for the rest. That is exactly this
   design.
2. **Novice-name test.** `Config.PreselectTools`: "how many tools to preselect for the first
   step". It reads as what it does.
3. **Complexity ledger.**

   ```
   Concepts the developer must learn   +1 (PreselectTools, with a default) / −0
   Files they must touch to do X       +0
   Lines at the call site              +0 (the default works)
   Ways to do the same thing           +0
   ```

4. **Where it belongs.** `agent`, next to tool search: it decides what the first step offers.
   Ranking stays behind the `ToolIndex` port (keywords in `NewMemToolIndex`, meaning in
   `agentmemory`).
5. **What it deletes.** Nothing. It adds capability. The two-hop behavior remains only for tools
   outside the preselection.

## Stage 1 — preselection (`types.go`, `agent.go`, `tool_search.go`, `orchestrator.go`)

1. `Config` gains, after `ToolSearchLimit`:

   ```go
   PreselectTools int // tools offered with search_tools on the first step, by relevance to the message (default 3)
   ```

   In `New`: `0` → `3` (constant `DefaultPreselectTools = 3`, exported, next to the other
   defaults). A negative value is an error `agent: PreselectTools must not be negative`
   (constant `errPreselectNegative`).
2. New unexported `func (a *Agent) preselect(ctx *context.Context, userQuery string) ([]llm.ToolDef, error)`
   in `tool_search.go`:
   - `all := a.registry.getTools()`;
   - if `len(all) <= a.cfg.PreselectTools` → return `append([]llm.ToolDef{searchToolsDef}, all...)`;
   - else `names, err := a.cfg.ToolIndex.SearchTools(ctx, userQuery, a.cfg.PreselectTools)`.
     An error is returned wrapped as `agent: preselect tools: %w`. The result is
     `searchToolsDef` followed by the definition of each found name that exists in `all`, in
     the index's order, without duplicates.
3. `Run` uses `offered, err := a.preselect(ctx, userQuery)` instead of
   `[]llm.ToolDef{searchToolsDef}`. `Confirm` and `Decline` keep `pendingToolsOffered`.
4. A preselected tool counts as offered in every rule that uses `offered`: it may be called,
   and if it modifies data it waits for confirmation (v0.8.0 behavior, unchanged).

## Stage 2 — tests (`tests/preselect_test.go`)

With a recording fake `llm.Client` (it records every `llm.Request`), like `tests/confirm_test.go`:

- **Few tools:** 2 local tools, default config → the first request offers `search_tools` and
  both tools.
- **Many tools, keyword match:** 6 local tools, one named `business_hours` with description
  `Opening hours of the clinic for each day of the week`. The message is
  `What are the opening hours today?` and `NewMemToolIndex()` is used. The first request offers
  `search_tools` plus that tool, at most 3 tools besides `search_tools`, and never the unrelated
  ones that share no word with the message.
- **Direct call works:** in that same setup the model calls `business_hours` in its first step,
  the tool runs, and no "not available" refusal happens.
- **`PreselectTools: -1`** → `New` returns `errPreselectNegative`.
- **Index error:** a `ToolIndex` whose `SearchTools` returns an error → `Run` returns an error
  containing `agent: preselect tools:`.
- Existing tests: `TestRun_OffersOnlySearchToolsFirst`, or whichever test asserts that the
  first step offers only `search_tools`, changes its expectation to the new rule. Keep its
  intent: a tool that does not match is not offered on the first step when there are more tools
  than `PreselectTools`. Give it enough tools for that to hold.

## Stage 3 — docs

- `docs/diagrams/TOOL_SEARCH.md`: the first node becomes `Run starts<br/>offered = search_tools<br/>+ preselected tools`,
  add one sentence explaining the preselection and the measurement of §0 (in the file's
  English), and add the new tests to its branch table.
- `docs/TYPES.md`: `Config` shows `PreselectTools` with its comment.

## Stages

| Stage | Files | Acceptance |
|---|---|---|
| 1 | `types.go`, `agent.go`, `tool_search.go`, `orchestrator.go` | builds |
| 2 | `tests/preselect_test.go` | new tests pass; existing tests green |
| 3 | `docs/diagrams/TOOL_SEARCH.md`, `docs/TYPES.md` | both mention `PreselectTools` |
| all | — | `gotest` green; `GOOS=js GOARCH=wasm go build ./...` |
