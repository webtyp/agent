---
PLAN: "feat!: typed critic (llm.Decider) that never talks to the answer; confirmation before tools that modify; tests in tests/"
TAG: v0.8.0
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 3622357277639513279
PR: https://github.com/webtyp/agent/pull/14
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> Part of
> [`AGENT_ECOSYSTEM_MASTER_PLAN.md`](AGENT_ECOSYSTEM_MASTER_PLAN.md).

# Plan — `webtyp.com/agent`: a critic that decides, and a person who confirms

## 0. Context

`agent` is the orchestrator of the webtyp AI agent: `Run(ctx, sessionID, userQuery)` loops
between a language model and tools until it has an answer (`orchestrator.go`). This plan fixes
two defects that a real evaluation found, and changes the public API for both.

**Defect 1: the critic's feedback leaks into the answer.** After the model writes an answer, a
"reflection" step asks a critic model whether it is good enough (`orchestrator.go`, section
`// Reflection`). When the critic says no, the code stores **two** turns in the session's memory:
the rejected answer, and a *user* message `Reflection feedback: <critique>. Please improve the answer.`
The model then answers the critic instead of the person. Measured with Qwen3.5-0.8B:
*"I understand the feedback. The response is incomplete because…"*. Both turns also stay in the
conversation forever. The critic's free text is parsed with string matching on `SUFFICIENT`.

**The fix:** the critic becomes a **closed question with probabilities**, the new
`llm.Decider` contract (`webtyp.com/llm` v0.2.0, already published):

```go
type Decider interface {
	Decide(ctx *context.Context, q Question) (Decision, error)
}
type Question struct {
	Context string   // what the model reads before the question
	Text    string   // the question itself
	Options []string // 2 to 10 possible answers
}
type Decision struct {
	Choice     int       // index into Question.Options of the most probable option
	Confidence float64   // the probability of Choice
	Probs      []float64 // one per option, in the order given
}
```

A rejected draft is **never stored**, and the critic's words never reach the model. The retry
gets one fixed, internal instruction, and it is not stored either.

**Defect 2: tools that change data run without asking.** An assistant for a clinic can book or
cancel appointments. A wrong or manipulated model decision ("cancel today's appointments", written
by an attacker into a patient's name) must not run until a person confirms it. The fix: a tool
declares whether it only reads. When the model calls one that does not, `Run` **stops and returns
the pending calls**. The application shows them to the person, who confirms or declines. The
pending state is derived from the session's memory, so it survives a page reload.

The MCP server library (`webtyp.com/mcp` v0.2.38, already published) now announces read-only
tools in `tools/list` with the standard MCP annotation `"annotations":{"readOnlyHint":true}`,
set only when the tool's action is exactly `model.Read`.

## Development rules (inline)

- **Primary runtime: browser, TinyGo/WASM.** Every non-test file compiles with
  `GOOS=js GOARCH=wasm go build ./...` and TinyGo.
- **Never import** in non-test files: `fmt`, `errors`, `strings`, `strconv` (use
  `webtyp.com/fmt`), stdlib `context` (use `webtyp.com/context`), `encoding/json` (use
  `webtyp.com/json`), `time` (use `webtyp.com/time`), `net/http`, `os`, `log`, `map[K]V`.
- **No `fmt.Printf` in the library.** Today `orchestrator.go` prints `Reflection failed` and
  `failed to log tool call`. Both become returned errors (stage 3 and 4).
- Every repeated string is a named constant (`prompts.go` for model-facing text, `errors.go` or the
  existing error constants for errors). Quote error messages exactly as written here.
- Update dependencies first: `go get webtyp.com/llm@v0.2.0 webtyp.com/mcp@v0.2.38`.
- No `TODO`, no commented-out code, no fallback that silently restores old behavior.
- `gotest` must be green at the end.

## Design gate (api-design — five answers)

**1. Prior art.**
- *LangGraph* `interrupt()` pauses a graph before a tool node and resumes it with
  `Command(resume=...)`, with the state in a checkpointer. Our `Reply.Pending` + `Confirm`/`Decline`
  is the same idea, with the state in the agent's memory store.
- *OpenAI Agents SDK* marks tools `needs_approval` and returns interruptions from a run, which
  the caller approves or rejects. That matches our `Pending`.
- *MCP tool annotations* (`readOnlyHint`, `destructiveHint`) are the protocol's own way for a
  server to say what a tool does. We read `readOnlyHint`, and a tool without it counts as
  modifying.
- *Constitutional / self-critique loops* (Reflexion) feed the critique text back to the model.
  That is exactly the version that failed here with a small model. Critics as classifiers (reward
  models, LLM-as-judge with fixed options) return a score instead, and that is our `llm.Decider`.

This ecosystem differs because the critic and the model run in the user's browser as a 0.8B
model, so free-text critique is unreliable. Confirmation must also survive a reload without a
server.

**2. Novice-name test.** `Config.Critic` ("the critic"), `Reply.Text`, `Reply.Pending` ("pending
tool calls"), `agent.Confirm(ctx, session)`, `agent.Decline(ctx, session)`, and `Tool.Action()`
(the ecosystem's CRUD verb, `model.Read`). Each reads as the sentence it does.

**3. Complexity ledger.**

```
Concepts the developer must learn   +2 (Reply with Pending; Tool.Action) / −1 (LLMs.Reflector)
Files they must touch to do X       +0
Lines at the call site              +3 (show Pending, call Confirm or Decline) / −0
Ways to do the same thing           −1 (one critic path; free-text reflection deleted)
```

**4. Where it belongs.** The loop, the pause and the resume are orchestration, so they belong in
`agent`. The critic's contract is in `llm` (`Decider`), because a runtime implements it. The
read-only fact comes from the tool itself: `Tool.Action()` locally, `readOnlyHint` for MCP.

**5. What it deletes.** `LLMConfig.Reflector`; `reflectorSystem`, `reflectionPromptFormat` and
`reflectionOutputTokens` in `prompts.go`; the `SUFFICIENT` string matching; storing rejected
drafts and critique turns; both `fmt.Printf` calls; and `integration_test.go`, whose scenarios
now live in `webtyp/agenteval` (`tests/eval_test.go` there).

## Stage 1 — tools declare whether they only read (`interfaces.go`, `mcp_json.go`, `mcp_registry.go`)

1. `Tool` gains one method:

   ```go
   type Tool interface {
   	Name() string
   	Description() string
   	InputSchema() string
   	// Action is the CRUD verb of what the tool does, model.Read for a tool that changes
   	// nothing. Any other value, including zero, makes the agent ask the person before
   	// running it.
   	Action() model.Action
   	Execute(ctx *context.Context, argsJSON string) (string, error)
   }
   ```

   `model` is `webtyp.com/model`. `model.Action` is a bitmask with `model.Create`, `model.Read`,
   `model.Update` and `model.Delete`. A tool is **read-only only when `Action() == model.Read`**.
2. `mcp_json.go`: `toolEntry` gains `ReadOnly bool`, decoded from the raw `annotations` object's
   `readOnlyHint` (decode `annotations` with `r.Raw("annotations")` into a small unexported
   `toolAnnotations` Fielder with one `readOnlyHint` bool). Missing annotations → `false`.
3. `mcp_registry.go`: every tool entry (local and MCP) records whether it is read-only. Add
   `func (r *mcpRegistry) readOnly(name string) bool`. For a local tool it is
   `t.Action() == model.Read`, for an MCP tool `entry.ReadOnly`, and for an unknown name `false`.
   `search_tools` is internal and is always treated as read-only.
4. Update every `Tool` implementation in the tests to declare `Action()`.

## Stage 2 — `Run` returns a `Reply` (`types.go`, `orchestrator.go`)

```go
// Reply is what the agent has for the person after Run, Confirm or Decline.
type Reply struct {
	// Text is the answer to show. While Pending is non-empty it is whatever the model wrote
	// before its tool calls, possibly empty.
	Text string
	// Pending are the tool calls the model wants to make that change data. They have not run.
	// Show them to the person and call Confirm or Decline. Empty when the answer is final.
	Pending []llm.ToolCall
}

func (a *Agent) Run(ctx *context.Context, sessionID, userQuery string) (Reply, error)
func (a *Agent) Confirm(ctx *context.Context, sessionID string) (Reply, error)
func (a *Agent) Decline(ctx *context.Context, sessionID string) (Reply, error)
```

Extract the loop body of today's `Run` into an unexported `func (a *Agent) loop(ctx, sessionID,
userQuery string, offered []llm.ToolDef) (Reply, error)` used by all three. Rules:

1. **Pausing.** When a model step returns tool calls and **any** of them is not read-only
   (`a.registry.readOnly`), store the assistant turn with its tool calls as today, **do not
   execute any call of that step**, and return `Reply{Text: resp.Text, Pending: resp.ToolCalls}`.
   The FSM goes to `StateResponding`, then `StateIdle`. A step whose calls are all read-only runs
   them immediately, as today.
2. **Finding the pending calls.** Unexported `func (a *Agent) pending(ctx, sessionID)
   ([]llm.ToolCall, error)` loads the last turn with `GetTurns(ctx, sessionID, 1)`. The calls are
   pending when that turn is `RoleAssistant` with `ToolCalls` (no tool result can follow a pause,
   because results are appended right after execution).
3. **`Confirm`.** No pending calls → return the error `agent: nothing to confirm in this session`
   (constant `errNothingToConfirm`). Otherwise execute each pending call exactly as the acting
   step does today (tool log, tool turn, `actingFailures`), then continue `loop` with
   `userQuery` = the content of the last user turn. Load it with `GetTurns`: the critic needs it
   (stage 3).
4. **`Decline`.** No pending calls → `agent: nothing to decline in this session`
   (`errNothingToDecline`). Otherwise append, for each pending call, a `RoleTool` turn with
   `ToolCallID` = the call's ID, `ToolName` = its name, and the content
   `The person declined this action; it was not executed.` (constant `declinedToolResult`). Then
   continue `loop` so the model answers the person.
5. **`Run` while calls are pending** (the person typed something instead of confirming): first
   do what `Decline` does (append the declined results), then append the new user turn and
   continue. Never leave an assistant tool-call turn without its results.
6. `offered` for `Confirm`/`Decline`: start from `[searchToolsDef]` plus the definitions of the
   pending tools, so the model can call them again if it needs to.

## Stage 3 — the critic is an `llm.Decider` (`types.go`, `prompts.go`, `critic.go`, `orchestrator.go`)

1. `Config` gains `Critic llm.Decider // optional: checks each answer before it reaches the person; nil = no check`.
   Delete `LLMConfig.Reflector` and its documentation.
2. New file `critic.go`:
   - `func (a *Agent) criticContext(ctx, sessionID, userQuery, answer string) (string, error)`
     builds, in English:

     ```text
     User asked: <userQuery>
     Tool <name> returned: <content>      (one line per RoleTool turn since the last user turn, in order)
     Assistant answered: <answer>
     ```

   - The question and options are constants in `prompts.go`:
     `criticQuestion = "Does the assistant's answer state anything that the tool results do not support?"`,
     `criticOptions = [2]string{"no, everything it says is supported by the tool results", "yes, it states something the tool results do not support"}`.
   - `func (a *Agent) criticRejects(ctx, sessionID, userQuery, answer string) (bool, error)`:
     calls `a.cfg.Critic.Decide`. It rejects when `Choice == 1` and `Confidence >= CriticMinConfidence`
     (exported `const CriticMinConfidence = 0.8`; below it the critic is unsure, and an unsure
     critic does not block an answer). An error from `Decide` is returned wrapped as
     `agent: critic: %w`. Never ignore it.
3. The end-turn branch of the loop, rewritten:
   - `Critic == nil` → store the answer as a `RoleAssistant` turn and return `Reply{Text: resp.Text}`.
   - Otherwise ask `criticRejects` **before storing anything**. Accepted → store and return.
     Rejected, and this `loop` has not retried yet → **store nothing**, and generate again with the
     same request plus one extra message at the end:
     `llm.Message{Role: llm.RoleSystem, Content: criticRetryNote}`, where
     `criticRetryNote = "Your previous draft stated things the tool results do not support. Answer the person again using only the tool results; if you do not have the data, call a tool."`.
     That message is added to the `llm.Request` after `agentcontext.Compile`, never stored in memory.
     The retry's answer goes through the same branch (tool calls are allowed; a second end-turn
     answer is stored and returned **without asking the critic again**). Track this with a
     `retried bool` local to `loop`.
4. Delete the old reflection code, `StateReflecting` stays in the FSM (the critic check runs in
   it), and the two `fmt.Printf` calls go: a failing `LogToolCall` returns
   `agent: failed to log tool call: %w`.

## Stage 4 — tests (`tests/`)

The ecosystem rule, and the owner's standing request: **all tests live in `tests/`**,
`package tests`, and use only the exported API.

1. Move every `*_test.go` of the root into `tests/` with `package tests`, importing
   `webtyp.com/agent`. Shared fakes (`mock_llm_test.go`, `mock_mcp_test.go`, `mock_memory_test.go`,
   `setup_test.go`) move too. A test that reads unexported state (for example the FSM's `current`
   state or `registry` internals) is rewritten to observe the same behavior through `Run`,
   `Confirm`, `Decline` and the requests a recording fake `llm.Client` receives. If one truly
   cannot be observed that way, delete it and write why under `## Executor notes`. Do not export
   anything for tests.
2. Delete `integration_test.go` (its scenario lives in `webtyp/agenteval`).
3. In `AGENTS.md`, replace the section "Test layout — excepción documentada" with one line:
   `All tests live in tests/ (package tests) and use only the exported API.`
4. New tests, each with a recording fake `llm.Client` (records every `llm.Request`, answers from
   a script) and a fake `llm.Decider`:
   - `tests/confirm_test.go`:
     - a `model.Create` tool call → `Reply.Pending` has it, the tool's `Execute` was **not**
       called, and the stored last turn is the assistant tool-call turn;
     - then `Confirm` → `Execute` called once, and the final `Reply.Text` comes from the next
       scripted answer;
     - `Decline` instead → `Execute` never called, and the next request's last message is a
       `RoleTool` message with `declinedToolResult`;
     - `Run` while pending → the declined results precede the new user turn in the next
       request;
     - `Confirm` and `Decline` with nothing pending → the exact errors;
     - a `model.Read` tool runs without pausing;
     - an MCP tool listed with and without `readOnlyHint` (use the existing MCP test server
       helpers, with `model.Read` and `model.Create` tools on `webtyp.com/mcp` v0.2.38): the
       first runs and the second pauses.
   - `tests/critic_test.go`:
     - `Critic` nil → one generation, answer stored;
     - accepting critic → answer returned, and the fake saw the exact context text;
     - rejecting critic with confidence 0.9 → a second generation whose request ends with the
       `criticRetryNote` system message, the rejected draft is **not** in memory
       (`GetTurns`), no stored turn contains `criticRetryNote`, and the second answer is
       returned without a second critic call;
     - rejecting with confidence 0.79 → the first answer is returned;
     - a critic error → `Run` returns an error containing `agent: critic:`.

## Stage 5 — docs

- `docs/TYPES.md`: `Config` without `LLMs.Reflector` and with `Critic`; add `Reply`; `Tool`
  with `Action()`.
- `docs/diagrams/REACT_FLOW.md` and `docs/diagrams/FSM_STATE.md`: the reflecting step is "critic
  decides (llm.Decider)"; add the branch "a tool that modifies → return Pending → Confirm / Decline".
- `docs/ARCHITECTURE.md` gets a section "Confirmation before tools that modify", explaining
  §0's defect 2 in plain words, with a 6-line example of an application handling `Reply.Pending`.
- `README.md`: the usage example uses `Reply`.

## Stages

| Stage | Files | Acceptance |
|---|---|---|
| 1 | `interfaces.go`, `mcp_json.go`, `mcp_registry.go` | `tests/confirm_test.go` read-only cases |
| 2 | `types.go`, `orchestrator.go` | `tests/confirm_test.go` |
| 3 | `critic.go`, `prompts.go`, `orchestrator.go` | `tests/critic_test.go`; `grep -rn "Reflector\|SUFFICIENT\|Printf" --include=*.go .` → empty |
| 4 | `tests/`, `AGENTS.md` | `ls *_test.go` in the root → nothing; `test ! -e integration_test.go` |
| 5 | docs | every doc names `Critic`, `Reply`, `Pending`; none names `Reflector` |
| all | — | `gotest` green; `GOOS=js GOARCH=wasm go build ./...` |

## Executor notes

1. **FSM internal test removal (`fsm_test.go`):** The unexported `fsm` struct and its transition methods are internal implementation details of `Agent`. Per Stage 4 requirements, all tests were moved to `tests/` (`package tests`) to test exclusively through the public exported API of `webtyp.com/agent`. FSM transition validation is fully exercised via `Run`, `Confirm`, and `Decline` in `tests/orchestrator_test.go`, `tests/confirm_test.go`, and `tests/critic_test.go`.
2. **Integration test (`integration_test.go`):** As specified in Stage 4, `integration_test.go` was deleted from this repository (its scenarios live in `webtyp/agenteval`).
