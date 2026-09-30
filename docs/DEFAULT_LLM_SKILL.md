# Agent patterns — `webtyp/agent`

This page holds the **behavioural rules of the orchestrator**: the patterns a change must keep
so the agent stays predictable. It is written for anyone (a person or an LLM) about to change
`orchestrator.go`, `fsm.go` or the tool registry.

The build, import and layout rules are in [AGENTS.md](../AGENTS.md), and the contracts are in
[ARCHITECTURE.md](ARCHITECTURE.md). They are not repeated here.

## The FSM is the only way to move

The agent's steps are states: `Idle`, `Reasoning`, `Acting`, `Reflecting`, `Responding`. Every
move goes through `fsm.transition(to)`, which rejects a move the table does not allow.

```
Idle → Reasoning
Reasoning → Acting | Reflecting | Responding
Acting → Reasoning | Responding
Reflecting → Reasoning | Responding
Responding → Idle
```

A new behaviour is a new transition in `fsm.go` first, then code. A free-form loop that skips
the FSM is never acceptable. Diagram: [FSM_STATE](diagrams/FSM_STATE.md).

## The loop is bounded

- `MaxIterations` (default 10) caps Reasoning→Acting cycles.
- `MaxRetries` (default 3) caps consecutive tool failures.
- A model that stops at its output limit (`llm.StopMaxTokens`) ends the run with an explicit
  error. Treating a cut-off answer as finished would return half a sentence as if it were whole.

## Tool errors are observations, not failures

A failing tool does not stop the agent. Its error is stored as the `RoleTool` message
(`"Error: …"`), so the model can try another tool or other arguments on the next step. Only
`MaxRetries` consecutive failures end the run.

## Primary, Critic and Summarizer

`LLMConfig` routes by task. `Primary` reasons and acts, `Critic` (`llm.Decider`) checks the candidate answer,
and `Summarizer` writes summaries when the conversation is compacted.

## Identity becomes the stable prefix

`Config.Identity` (`agentcontext.Identity`: name, role, instructions, goals) is rendered by
`agentcontext` into the request's `System`. It is the same on every step of a conversation,
which lets a model runtime reuse the work already done on it. Anything that changes per step
(summaries, retrieved knowledge, dates) goes into the messages, never into `System`.

## MCP

- One HTTP endpoint per server, JSON-RPC 2.0, with the method in the body and never in the URL.
- Handshake: `initialize` → `notifications/initialized` (no ID, no response) → `tools/list`.
- Tool arguments are validated against the tool's JSON Schema before execution.

Diagram: [MCP_CLIENT_FLOW](diagrams/MCP_CLIENT_FLOW.md).

## The model is always injected

`agent` ships no model and no provider adapter. A model runtime implements `llm.Client` and
`llm.TokenCounter` in its own repository. In the browser that runtime is Go/TinyGo. On the
developer machine, the integration test uses `llama-server` as a reference model (see
[IMPLEMENTATION.md](IMPLEMENTATION.md)).

## Documentation comes first

- A change to a contract updates [ARCHITECTURE.md](ARCHITECTURE.md) and [TYPES.md](TYPES.md) first.
- A change to a flow updates its diagram in `docs/diagrams/` and its DDT test.
- Every new document is linked from the [README](../README.md).
- Publish with `gopush` only, after `gotest` and `gotest -tinygo` pass.
