> First plan of the execution queue in [PLAN.md](PLAN.md). This plan is dispatched via the
> CodeJob workflow. See skill: agents-workflow.
>
> **Phase 3** of [`AGENT_ECOSYSTEM_MASTER_PLAN.md`](AGENT_ECOSYSTEM_MASTER_PLAN.md).
> **Blocked until `webtyp.com/llm` v0.1.0 and `webtyp.com/agentcontext` v0.1.0 are published.**
> `webtyp/agentmemory` (phase 3b) waits for this tag.

# Plan — `webtyp/agent` becomes the orchestrator only

## 0. Context

`webtyp/agent` runs the loop "ask the model → run the tools it asked for → ask again",
guarded by a finite-state machine (FSM). Two jobs that are not orchestration still live here:

1. **The contract with the model**: `LLMClient`, `LLMRequest`, `LLMResponse`, `ToolDef`,
   `ToolCall`, and the model-facing half of `Message`. These now live in `webtyp.com/llm`.
2. **Deciding what the model sees**: `context_window.go`. That logic now lives in
   `webtyp.com/agentcontext` as pure functions (`Compile`, `Compact`, `SummaryRequest`).

This plan deletes both copies here and wires the two libraries in. The orchestrator keeps
every piece of I/O: reading and writing memory, calling the models, assigning IDs and
timestamps.

It also fixes behaviour the extraction exposed. The tests in Stage 5 pin each one:
- the context size (8192) was sent to the model as its **output** limit,
- summaries were rendered newest-first because the store returns them oldest-first,
- a model that stops at the output limit (`llm.StopMaxTokens`) was not handled, and the loop
  spun until `MaxIterations`,
- every stored message had `TokenCount: 0`, so the budget ignored the conversation entirely.

Finally, the real-model integration test moves from Ollama to **llama.cpp's `llama-server`**,
the local tool this project uses to run models on the developer machine.

## Development rules (inline)

- **Orchestrator only.** No model runtime, no storage backend, no prompt-building logic.
  Prompts other than reflection come from `agentcontext`.
- Every file compiles under `GOOS=js GOARCH=wasm` and TinyGo. Test files behind the
  `integration` build tag are host-only and may use `net/http`.
- **Never import** (outside `integration_test.go`):

| Never | Use instead | Why |
|---|---|---|
| `database/sql`, any SQL driver | the `MemoryStore` port | the backend belongs to the consuming project |
| `net/http` | `webtyp.com/fetch` | does not compile under TinyGo |
| `context` (stdlib) | `webtyp.com/context` | |
| `encoding/json` | `webtyp.com/json` | reflection JSON costs ~1 MB of wasm |
| `fmt`, `errors`, `strconv`, `strings` | `webtyp.com/fmt` | |
| `time` | `webtyp.com/time` | |
| `github.com/google/uuid` | `webtyp.com/unixid` via `model.IDGenerator` | |
| `os`, `log` | inject it | |
| `map[K]V` | a slice | TinyGo's map runtime is a size tax |

- **No hardcoded strings in logic:** error messages and the reflection prompt are named
  constants in `errors.go` / `prompts.go`.
- Max 500 lines per file. Tests: `testing` only. Do **not** run `gopush`/`codejob`.

## Design gate (api-design — five answers)

1. **Prior art.** **LangGraph**: the agent graph calls a model interface it does not define.
   **Google ADK**: agent, model (`BaseLlm`) and context processors are separate modules.
   **Semantic Kernel**: the kernel orchestrates, and connectors implement a chat-completion
   interface from a separate package. All three keep "the orchestrator" apart from "the model
   contract" and "the context builder". Before this plan, agent held all three.
2. **Novice-name test.** The consumer-facing names change as follows. `Config.Identity` is now
   an `agentcontext.Identity`. `Config.Tokens` is "the tokenizer of the primary model".
   `Config.Budget` is "the token limits of the primary model". `RecentTurns` and
   `RecentSummaries` say how much history is loaded. The memory port methods are
   `AppendTurn` / `GetTurns` / `DeleteTurns` / `SaveSummary` / `GetSummaries`, all named
   after the types they move.
3. **Complexity ledger.**
   ```
   Concepts the developer must learn   +2 (Tokens, Budget) / −4 (LLMClient, LLMRequest, LLMResponse, ContextWindowConfig — now llm/agentcontext, one name each)
   Files they must touch to do X       +0 / −1   (context_window.go gone)
   Lines at the call site              +2 / −0   (Tokens and Budget in Config)
   Ways to do the same thing           +0 / −2   (agent.LLMRequest vs llm.Request; context_window.go vs agentcontext)
   ```
   `Tokens` and `Budget` are required. There is no silent 8192 default, because the context
   size is a fact of the model and cannot be guessed.
4. **Where it belongs.** The loop, the FSM, the tool registry and the memory ports stay here.
   The model contract is in `llm`, and context compilation is in `agentcontext`. The memory
   ports stay here (the port-owner pattern of `webtyp/storage`). Their value types come from
   `agentcontext`, which is the library that reads them.
5. **What it deletes.** `context_window.go`, `context_window_test.go`; from `types.go`:
   `Message`, `Episode`, `LLMRequest`, `LLMResponse`, `ToolDef`, `ToolCall`,
   `ContextWindowConfig` (including the unused `BufferTokens`), `IdentityConfig`; from
   `interfaces.go`: `LLMClient`, `EpisodeStore`. The Ollama client in `integration_test.go`.

## Stage 1 — dependencies

```bash
go get webtyp.com/llm@v0.1.0 webtyp.com/agentcontext@v0.1.0
```

## Stage 2 — types and ports

**`types.go`**: delete the types listed in gate answer 5. Keep `Agent`, `Knowledge`, `ToolLog`,
`LLMConfig`. Change:

```go
type Config struct {
	Identity agentcontext.Identity
	LLMs     LLMConfig           // required: LLMs.Primary != nil
	Tokens   llm.TokenCounter    // required: the tokenizer of LLMs.Primary
	Budget   agentcontext.Budget // required: the token limits of LLMs.Primary
	Memory   MemoryStore         // required
	IDGen    model.IDGenerator   // required

	RecentTurns     int // turns loaded per reasoning step (default 20)
	RecentSummaries int // summaries loaded per reasoning step (default 5)

	LocalTools  []Tool
	MCPHandlers []MCPServer
	MCPServers  []string

	MaxIterations int // default 10
	MaxRetries    int // default 3
	MCPTimeoutMS  int // default 30000
}

type LLMConfig struct {
	Primary    llm.Client // required
	Reflector  llm.Client // optional, defaults to Primary
	Summarizer llm.Client // optional, defaults to Primary
}
```

**`interfaces.go`**: delete `LLMClient` and `EpisodeStore`. Replace:

```go
type ConversationStore interface {
	EnsureSession(ctx *context.Context, sessionID string) error
	AppendTurn(ctx *context.Context, sessionID string, t agentcontext.Turn) error
	GetTurns(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Turn, error) // the `limit` most recent
	DeleteTurns(ctx *context.Context, sessionID string, ids []string) error
}

type SummaryStore interface {
	SaveSummary(ctx *context.Context, sessionID string, s agentcontext.Summary) error
	GetSummaries(ctx *context.Context, sessionID string, limit int) ([]agentcontext.Summary, error) // the `limit` most recent
}

type MemoryStore interface {
	ConversationStore
	SummaryStore
	KnowledgeStore
	ToolLogStore
}
```

The store assigns nothing: `Turn.ID`, `Summary.ID` and `CreatedAt` arrive filled by the
orchestrator. Order of the returned slices is not part of the contract, because
`agentcontext` sorts by `CreatedAt`.

**`mcp_registry.go`**: `ToolDef` → `llm.ToolDef`, `ToolCall` → `llm.ToolCall`. No behaviour change.

## Stage 3 — constructor (`agent.go`)

- Required checks, in this order, with these messages (constants in `errors.go`):
  `agent: LLMs.Primary is required`, `agent: Tokens is required`, `agent: Memory is required`,
  `agent: IDGen is required`, then `if err := cfg.Budget.Validate(); err != nil { return nil, fmt.Errf("agent: Budget: %w", err) }`.
- Defaults: `RecentTurns` 20, `RecentSummaries` 5, `MaxIterations` 10, `MaxRetries` 3,
  `MCPTimeoutMS` 30000. **Delete** the `ContextWindow` defaults (8192 / 0.8 / 20 / 5).

## Stage 4 — orchestrator

**Delete `context_window.go`.** Create **`turn.go`** with the I/O the compiler does not do:

```go
// newTurn stamps a message with an ID, its token count and the current time.
func (a *Agent) newTurn(msg llm.Message) agentcontext.Turn {
	return agentcontext.Turn{ID: a.idGen.NewID(), Message: msg, Tokens: a.cfg.Tokens.CountTokens(msg.Content), CreatedAt: time.Now() / 1e9}
}

// request loads history, compacts it if needed, and compiles the request for this step.
func (a *Agent) request(ctx *context.Context, sessionID string) (llm.Request, error)
```

`request` does exactly this:
1. `summaries := GetSummaries(ctx, sessionID, cfg.RecentSummaries)`, `turns := GetTurns(ctx, sessionID, cfg.RecentTurns)`.
2. `in := agentcontext.Input{Identity: cfg.Identity, Summaries: summaries, Turns: turns, Tools: a.registry.getTools()}`.
3. `if old := agentcontext.Compact(in, cfg.Budget, cfg.Tokens); old != nil`:
   `resp, err := summarizer.Generate(ctx, agentcontext.SummaryRequest(old, cfg.Budget))` (summarizer = `LLMs.Summarizer` or `Primary`);
   `SaveSummary(ctx, sessionID, agentcontext.Summary{ID: idGen.NewID(), Text: resp.Text, Tokens: cfg.Tokens.CountTokens(resp.Text), FromTurnID: old[0].ID, ToTurnID: old[len(old)-1].ID, CreatedAt: now})`;
   `DeleteTurns(ctx, sessionID, ids of old)`; then reload both lists and rebuild `in` (repeat step 1–2 once).
4. `return agentcontext.Compile(in, cfg.Budget, cfg.Tokens)`.

**`orchestrator.go`**:
- Every `Message{…}` literal becomes `a.newTurn(llm.Message{…})` and `AppendMessage` becomes
  `AppendTurn`. Roles use `llm.RoleUser`, `llm.RoleAssistant` and `llm.RoleTool`, never string literals.
- Replace `a.prepareContext(...)` + `req.Tools = a.registry.getTools()` with `req, err := a.request(ctx, sessionID)`.
- `resp.StopReason == "tool_use"` → `llm.StopToolUse`; `"end_turn"` → `llm.StopEndTurn`.
- **New branch** for `llm.StopMaxTokens`: transition to `StateResponding` then `StateIdle`
  and return `"", fmt.Err(errOutputTruncated)` where
  `errOutputTruncated = "agent: the model stopped at Budget.OutputTokens before finishing; raise OutputTokens"`.
- Any other `StopReason`: return `fmt.Errf("agent: unknown stop reason %q", resp.StopReason)`.
- Reflection: build `llm.Request{System: reflectorSystem, Messages: []llm.Message{{Role: llm.RoleUser, Content: prompt}}, MaxOutputTokens: reflectionOutputTokens}`.
  Move the reflection strings to **`prompts.go`** as constants (`reflectorSystem`,
  `reflectionPromptFormat`) and `const reflectionOutputTokens = 100`. The text stays
  byte-for-byte the same.

## Stage 5 — tests

- **`mock_llm_test.go`**: the mock implements `llm.Client`. Add, in the same file,
  `type quarterCounter struct{}` (`CountTokens(s) = len(s)/4`) used by every test `Config`.
- **`mock_memory_test.go`**, **`mem_memory.go`**: rename to the new port methods and types.
  `memMemory` stores `[]agentcontext.Turn` and `[]agentcontext.Summary` keyed by session in a
  slice of structs (no map).
- **`conformance/conformance.go`**: rename every subtest and call to the new methods
  (`conversation_append_and_get_turns`, `summary_save_and_get`, …). Turns are built as
  `agentcontext.Turn{ID: "m1", Message: llm.Message{Role: llm.RoleUser, Content: "hello"}, CreatedAt: 1}`.
  Summaries are built as `agentcontext.Summary{ID: "e1", Text: "summary text", Tokens: 100, FromTurnID: "m1", ToTurnID: "m5", CreatedAt: 1}`.
  Add `summary_round_trips_all_fields`: every field of the saved `Summary` comes back unchanged.
- **Delete `context_window_test.go`.** Add to `orchestrator_test.go`:

| Test | Asserts |
|---|---|
| `TestRun_SendsOutputLimitNotContextSize` | `Budget{ContextTokens: 8192, OutputTokens: 256}` → the mock's recorded request has `MaxOutputTokens == 256` |
| `TestRun_CompactsAndSummarizesOldestTurns` | a session pre-loaded with 30 turns of 100 tokens and `ContextTokens: 2000` → the summarizer mock is called once, `SaveSummary` receives `FromTurnID` = the oldest turn, and those turns are gone from memory |
| `TestRun_StopMaxTokensReturnsError` | the mock answers `StopMaxTokens` → the error text equals `errOutputTruncated`; FSM ends in `StateIdle` |
| `TestRun_TurnsCarryTokenCounts` | after one `Run`, every stored turn has `Tokens == len(Content)/4` |
| `TestNew_RequiresTokensAndBudget` | missing `Tokens` → `agent: Tokens is required`; zero `Budget` → error starting `agent: Budget:` |

- **`integration_test.go`** (build tag `integration`, host-only). Delete the Ollama client.
  Create `llamaServer` implementing `llm.Client` and `llm.TokenCounter` against llama.cpp's
  `llama-server`:
  - constants `llamaServerURL = "http://localhost:8080"`, `llamaHealthPath = "/health"`,
    `llamaChatPath = "/v1/chat/completions"`, `llamaTokenizePath = "/tokenize"`;
  - `llamaServerAvailable()`: `GET llamaServerURL + llamaHealthPath` returns 200, and the tests
    call `t.Skip("llama-server not running on :8080")` otherwise;
  - `Generate` maps `llm.Request` to the OpenAI-compatible body. The system message comes
    first, then the messages, tools as `{"type":"function","function":{name,description,parameters}}`,
    and `max_tokens = MaxOutputTokens`. `finish_reason` maps as `"tool_calls"` → `StopToolUse`,
    `"length"` → `StopMaxTokens`, anything else → `StopEndTurn`;
  - `CountTokens`: `POST /tokenize {"content": text}` → `len(tokens)`.
  - The two clinic scenarios (`TestIntegration_ClinicHours`, `TestIntegration_SessionIsolation`)
    keep their assertions, with `Budget{ContextTokens: 4096, OutputTokens: 512}`.

## Stage 6 — documentation

Remove every `STATUS (remove this note when …)` line in `docs/ARCHITECTURE.md`, `docs/TYPES.md`,
`docs/IMPLEMENTATION.md` and `README.md` that refers to this plan. The documents were written
for the target state before this plan and must not change otherwise.

## Stages

| Stage | Files | Acceptance |
|---|---|---|
| 1 | `go.mod` | requires `llm` v0.1.0 and `agentcontext` v0.1.0 |
| 2 | `types.go`, `interfaces.go`, `mcp_registry.go` | `grep -rn "LLMRequest\|LLMResponse\|LLMClient\|Episode\|ContextWindowConfig\|IdentityConfig\|BufferTokens" --include=*.go .` → empty |
| 3 | `agent.go`, `errors.go` | `TestNew_RequiresTokensAndBudget` passes |
| 4 | `turn.go`, `orchestrator.go`, `prompts.go`; `context_window.go` deleted | `grep -rn '"user"\|"assistant"\|"tool"\|"tool_use"\|"end_turn"' --include=*.go . \| grep -v _test.go` → empty |
| 5 | tests, `mem_memory.go`, `conformance/conformance.go` | `gotest`, `gotest -tinygo`, `GOOS=js GOARCH=wasm go build ./...`; with `llama-server` running, `go test -tags integration ./...` passes |
| 6 | docs | `grep -rn "STATUS (remove" docs README.md` → empty |
