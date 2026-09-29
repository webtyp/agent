# Plan — tool search: the model sees one tool and discovers the rest

> Second plan of the execution queue in [PLAN.md](PLAN.md). It runs **after**
> [PLAN_LLM_AGENTCONTEXT.md](PLAN_LLM_AGENTCONTEXT.md) and assumes its result: `llm.ToolDef`,
> `llm.ToolCall`, `agentcontext`, the `turn.go` / `request` split. This plan is dispatched via
> the CodeJob workflow. See skill: agents-workflow.

## 0. Context

Every tool the agent can run is today sent to the model on every step: its name, description
and JSON Schema, in `llm.Request.Tools`. With 50 tools that is thousands of tokens the model
reads before it reads the question. The first in-browser model (`Qwen3.5-0.8B`, running in
Go/TinyGo) has a small window and reads roughly 1 token per few milliseconds on a CPU, so
every token of tool description is paid in context space and in waiting time.

**Tool search** fixes this with progressive disclosure:

1. On every step the model is offered **one** permanent tool, `search_tools(query)`, plus the
   tools it has already discovered in this `Run`.
2. When the model calls `search_tools`, the agent asks a `ToolIndex` for the tools that match
   the query, adds them to the offered set, and answers with their names and descriptions.
3. On the next step the model calls a discovered tool **directly**, with its real JSON Schema
   in `Request.Tools`. The model runtime can then constrain the arguments to that schema.

The `ToolIndex` is a port, the same pattern as the memory ports. This plan ships a keyword
reference implementation (`NewMemToolIndex`) so the agent works out of the box. The semantic
implementation (by meaning, over `webtyp/retrieval`) is `webtyp/agentmemory`'s job, in its
own plan.

## Development rules (inline)

Same as [PLAN_LLM_AGENTCONTEXT.md](PLAN_LLM_AGENTCONTEXT.md) § Development rules. In short:
TinyGo/WASM-compatible; no `net/http`, stdlib `context`, `encoding/json`, `fmt`/`errors`/
`strings`/`strconv`, `time`, `map[K]V`; error messages and the tool definition are constants;
tests with `testing` only; no `gopush`/`codejob`.

## Design gate (api-design — five answers)

1. **Prior art.** **Anthropic tool search** (`tool_search_tool`, deferred loading): tools are
   marked deferred and a search tool returns references that make them callable. **OpenAI
   Agents SDK / MCP clients** filter a large tool list per turn before sending it. **LangGraph
   "bigtool"** retrieves tools by embedding similarity and binds only the matches. All three
   discover tools, then call the discovered tool directly with its own schema. We do the same.
   We do not use a generic `execute_tool(name, args)` wrapper, because its arguments would have
   no fixed schema, and the runtime needs one to constrain the model's output.
2. **Novice-name test.** `ToolIndex.IndexTools(ctx, tools)` / `ToolIndex.SearchTools(ctx, query, limit)`
   read as "index these tools" / "search the tools for this query". The model-facing name
   `search_tools` says what it does. `Config.ToolIndex` and `Config.ToolSearchLimit` say what
   they configure.
3. **Complexity ledger.**
   ```
   Concepts the developer must learn   +2 (ToolIndex, search_tools) / −0
   Files they must touch to do X       +0 / −0   (tools are still registered the same three ways)
   Lines at the call site              +1 / −0   (Config.ToolIndex)
   Ways to do the same thing           +0 / −1   (the "send every tool every step" path is deleted)
   ```
   `ToolIndex` is **required**. There is no fallback that sends every tool when it is nil,
   because that would be a second way to offer tools.
4. **Where it belongs.** Deciding what the model is offered is orchestration, so it lives
   here. Ranking tools by meaning is retrieval, so it sits behind a port that `agentmemory`
   implements over `retrieval`. The keyword reference implementation lives here, like
   `mem_memory.go`.
5. **What it deletes.** Sending `registry.getTools()` (every tool) in each request. Afterwards
   `getTools()` only feeds `ToolIndex.IndexTools` at construction and the lookup of discovered
   definitions.

## Stage 1 — the port and the reference index

**`interfaces.go`**, add:

```go
// ToolIndex finds, among every tool the agent can run, the ones that match what the model
// is looking for. webtyp/agentmemory implements it by meaning; NewMemToolIndex by keywords.
type ToolIndex interface {
	// IndexTools replaces the indexed set with tools.
	IndexTools(ctx *context.Context, tools []llm.ToolDef) error
	// SearchTools returns the names of at most limit tools, most relevant first; empty when none match.
	SearchTools(ctx *context.Context, query string, limit int) ([]string, error)
}
```

**`mem_tool_index.go`** (new): `func NewMemToolIndex() ToolIndex`. Behaviour:
- `IndexTools` stores a copy of the slice.
- `SearchTools`: lower-case the query and split it into words (a word is a run of letters or
  digits; ASCII + Latin-1 accented letters count as letters). For each tool, the score is how
  many **distinct** query words appear as substrings of the lower-cased `Name + " " + Description`.
  Return tools with score > 0, sorted by score descending, then by indexing order (stable
  insertion sort, no `sort` package), truncated to `limit`.
- Use `webtyp.com/fmt` for lower-casing. No `strings`.

## Stage 2 — config and constructor

**`types.go`** `Config`, add:

```go
	ToolIndex       ToolIndex // required: finds tools for search_tools (NewMemToolIndex for keywords)
	ToolSearchLimit int       // tools returned per search (default 5)
```

**`agent.go`** `New`:
- `if cfg.ToolIndex == nil` → `agent: ToolIndex is required` (constant in `errors.go`).
- default `ToolSearchLimit` 5.
- After all tools are registered: if any registered tool is named `search_tools`, return
  `agent: a tool named search_tools is reserved for tool search`. Then
  `cfg.ToolIndex.IndexTools(ctx, registry.getTools())`, wrapping its error as `agent: indexing tools: %w`.

## Stage 3 — the orchestrator

**`tool_search.go`** (new):

```go
const searchToolsName = "search_tools"

// searchToolsDef is the one tool offered on every step.
var searchToolsDef = llm.ToolDef{
	Name:        searchToolsName,
	Description: "Find the tools that can do what you need. Describe the task in a few words; the matching tools become available for you to call.",
	InputSchema: `{"type":"object","properties":{"query":{"type":"string","description":"the task you need a tool for"}},"required":["query"]}`,
}
```

- A per-`Run` value `offered` (a slice of `llm.ToolDef`, starting as `[searchToolsDef]`).
- `func (a *Agent) searchTools(ctx, sessionID string, call llm.ToolCall, offered *[]llm.ToolDef) string`:
  decode `call.Input` with `webtyp.com/json` into a small type with a `DecodeFields` reading
  `r.String("query")` (same pattern as `mcp_json.go`). An empty query returns
  `"Error: search_tools needs a query"`. Otherwise call `SearchTools(ctx, query, cfg.ToolSearchLimit)`.
  Append each found tool's `llm.ToolDef` (from `registry.getTools()`) to `*offered` unless it is
  already there. Return one line per found tool, `"<name>: <description>\n"`, or
  `"No tools matched. Try other words."` when none.
- In `request` (from `turn.go`), pass `offered` as `agentcontext.Input.Tools` instead of `registry.getTools()`.
- In the Acting branch of `orchestrator.go`, for each `call`:
  - `call.Name == searchToolsName` → the result of `searchTools`, stored and logged like any tool
    (`LogToolCall` with the same duration measurement);
  - the name is not in `offered` → **do not execute**; the tool result is
    `"Error: tool <name> is not available; call search_tools first"` and it counts as an Acting
    failure (same `MaxRetries` rule);
  - otherwise → `registry.execute` as today.

## Stage 4 — tests

| Test | Asserts |
|---|---|
| `TestMemToolIndex_RanksByMatchedWords` | tools `clinic_hours` ("opening hours of the clinic"), `book_appointment` ("book an appointment"), `weather`; query `"clinic opening hours"` → `[clinic_hours]`; query `"book appointment clinic"` → `[book_appointment, clinic_hours]` |
| `TestMemToolIndex_LimitAndNoMatch` | limit 1 returns one; query `"zzz"` → empty, no error |
| `TestNew_RequiresToolIndex` | nil → `agent: ToolIndex is required` |
| `TestNew_ReservesSearchToolsName` | a local tool named `search_tools` → the reserved-name error |
| `TestRun_OffersOnlySearchToolsFirst` | the mock LLM's first recorded request has `Tools == [searchToolsDef]` |
| `TestRun_DiscoveredToolBecomesCallable` | mock replies: (1) call `search_tools {"query":"clinic hours"}`, (2) call `clinic_hours {}`, (3) end turn → request 2's `Tools` contains `clinic_hours`; the local tool ran once; the answer is returned |
| `TestRun_UndiscoveredToolIsRefused` | mock calls `clinic_hours` first → it is **not** executed, and the stored tool result is the "not available" error |

Add `docs/diagrams/TOOL_SEARCH.md` coverage: these tests are its DDT.

## Stage 5 — docs

Remove the `STATUS` notes that mention tool search in `docs/ARCHITECTURE.md` and
`docs/diagrams/TOOL_SEARCH.md`. The documents were written before this plan and describe the
result.
