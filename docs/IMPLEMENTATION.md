# Implementation — `webtyp/agent`

> **STATUS (remove this note when agent v0.6.0 is published):** the file list and tests
> describe the target of [PLAN.md](PLAN.md). Until then, `context_window.go` still exists and
> the integration test still targets Ollama.

This page is for someone about to change the code. It covers where each piece lives, how it is
tested, and how to run the real-model test. The *why* is in [ARCHITECTURE.md](ARCHITECTURE.md),
the types in [TYPES.md](TYPES.md), and the rules every change must follow in
[AGENTS.md](../AGENTS.md).

## Files

```
agent/
├── agent.go            New(cfg): validation, defaults, tool wiring — the only DI point
├── interfaces.go       memory ports + Tool + MCPServer
├── types.go            Agent, Config, LLMConfig, Knowledge, ToolLog
├── errors.go           error message constants
├── prompts.go          reflection prompt constants
├── orchestrator.go     Run: ReAct + reflection loop
├── turn.go             newTurn (ID, tokens, time) and request (load → compact → compile)
├── fsm.go              states and valid transitions
├── mcp_client.go       JSON-RPC 2.0 client over webtyp/mcp
├── mcp_json.go         MCP wire helpers
├── mcp_registry.go     merges local tools, MCP handlers, remote MCP servers
├── mem_memory.go       in-memory reference MemoryStore
├── conformance/        the suite every MemoryStore implementation runs
└── *_test.go           unit, DDT and integration tests
```

## Testing

Run everything with `gotest` (vet, race, coverage, WASM) and `gotest -tinygo` (the real browser
compiler).

### Diagram-driven tests

Every flow in `docs/diagrams/` has a test that walks each branch.

| Test | Diagram |
|---|---|
| `TestReAct_ToolCallThenAnswer` | [REACT_FLOW](diagrams/REACT_FLOW.md) |
| `TestReAct_ReflectionApproved` | REACT_FLOW |
| `TestReAct_ReflectionRetry` | REACT_FLOW |
| `TestReAct_ToolErrorSelfCorrect` | REACT_FLOW |
| `TestReAct_MaxIterationsGuard` | [FSM_STATE](diagrams/FSM_STATE.md) |
| `TestFSM_Transitions` | FSM_STATE |
| `TestRun_CompactsAndSummarizesOldestTurns` | `agentcontext` context-window diagram |
| `TestRun_StopMaxTokensReturnsError` | FSM_STATE |
| `TestMCPClient_Discovery`, `TestMCPClient_CallTool` | [MCP_CLIENT_FLOW](diagrams/MCP_CLIENT_FLOW.md) |

Unit tests use mocks (`mock_llm_test.go`, `mock_memory_test.go`, `mock_mcp_test.go`) and a
`quarterCounter` (`len(text)/4`) as the token counter. Each test uses `t.Name()` as its session
ID, so tests never see each other's memory.

### Memory conformance

`conformance.Run(t, conformance.Factory{…})` is the contract test for `MemoryStore`. It runs
here against `mem_memory.go` and in `webtyp/agentmemory` against its real backends. A new
implementation is only valid once it passes this suite.

### Real-model integration test

`integration_test.go` (build tag `integration`, host only) runs the two clinic scenarios of
[INTEGRATION_SCENARIO](diagrams/INTEGRATION_SCENARIO.md) against a real model served by
**llama.cpp's `llama-server`** on the developer machine. The test contains a small
OpenAI-compatible client that implements `llm.Client` and `llm.TokenCounter` (through
`/tokenize`, so budgets use the model's real tokenizer). It is test code and is not shipped.

```bash
# 1. Start llama-server with a small GGUF model (llama.cpp build: see the
#    Resources/LLM/LLamaCPP install notes on the developer machine).
llama-server -m <model>.gguf --port 8080 --jinja

# 2. Run the scenarios (skipped automatically when :8080 is not answering /health).
go test -tags integration -run TestIntegration -v -timeout 300s ./...
```

`--jinja` makes `llama-server` apply the model's own chat template, which tool calling needs.
llama.cpp is **not** the runtime of the product. In the browser, the model runs in Go/TinyGo
(see the [master plan](AGENT_ECOSYSTEM_MASTER_PLAN.md), decision D2). Here it is only a
reference model to test the loop against.
