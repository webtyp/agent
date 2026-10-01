# Agent
<img src="docs/img/badges.svg">

The AI agent of webtyp: an orchestrator that takes a user's message, lets a language model
decide which tools to use, runs them, and returns an answer. It is written in Go, compiles to
WebAssembly with TinyGo, and is meant to run **inside the browser**. The model, memory and tools
are all injected.

## Getting started

**See it working.** The tests are the runnable example. `orchestrator_test.go` wires a scripted
model, the in-memory store and a real MCP server:

```bash
gotest            # host: vet, race, coverage, wasm
gotest -tinygo    # the browser compiler
```

**Run it against a real model on your machine.** Start llama.cpp's `llama-server` on port 8080
and run the integration scenarios. See
[Implementation → real-model integration test](docs/IMPLEMENTATION.md#real-model-integration-test).

**Use it in an application.** Build a `Config` with a model (`llm.Client` + `llm.TokenCounter`),
its `Budget`, a `MemoryStore` (`webtyp/agentmemory`), an ID generator and your tools, then call
`agent.New(cfg)` and `Run(ctx, sessionID, text)`, which returns a `Reply`. Every field is described in
[Types](docs/TYPES.md).

**Change the agent.** Read [AGENTS.md](AGENTS.md) and [Agent patterns](docs/DEFAULT_LLM_SKILL.md)
first.

## Documentation

### Plans
- [Ecosystem master plan](docs/AGENT_ECOSYSTEM_MASTER_PLAN.md): which repository owns each
  concern (model contract, context, memory, retrieval, voice), the order of the work, and the
  open decisions.

### Guides
- [Architecture](docs/ARCHITECTURE.md): what the agent is, where it runs, the loop, and the contracts.
- [Types](docs/TYPES.md): which library owns each type, and the fields of the ones declared here.
- [Implementation](docs/IMPLEMENTATION.md): files, tests, and the real-model integration test.
- [Agent patterns](docs/DEFAULT_LLM_SKILL.md): FSM, bounded loop, tool errors, identity, MCP.
- [Agent guide](AGENTS.md): build, import and layout rules for any change.

### Diagrams
- [System context](docs/diagrams/SYSTEM_CONTEXT.md)
- [ReAct + reflection flow](docs/diagrams/REACT_FLOW.md)
- [FSM state machine](docs/diagrams/FSM_STATE.md)
- [Memory architecture](docs/diagrams/MEMORY_ARCHITECTURE.md)
- [MCP client flow](docs/diagrams/MCP_CLIENT_FLOW.md)
- [Tool search](docs/diagrams/TOOL_SEARCH.md)
- [Ecosystem map (español): repos, message flow, weights, proposals](docs/diagrams/ECOSYSTEM_MAP.md)
- [Hybrid agent proposal (español)](docs/HYBRID_DESIGN.md) — a decision model drives, code and templates answer ([flow](docs/diagrams/HYBRID_FLOW.md))
- [Integration test scenario](docs/diagrams/INTEGRATION_SCENARIO.md)

### History
- [Isomorphic compatibility refactor](docs/history/ISOMORPHIC-COMPATIBILITY.md): executed (written when the project was named `tinywasm`).

### Moved to their own repositories
Semantic search → [`webtyp/retrieval`](https://github.com/webtyp/retrieval) ·
context engineering → [`webtyp/agentcontext`](https://github.com/webtyp/agentcontext) ·
small browser LLMs → [`webtyp/llm`](https://github.com/webtyp/llm) ·
speech-to-text → [`webtyp/stt`](https://github.com/webtyp/stt) ·
text-to-speech → [`webtyp/tts`](https://github.com/webtyp/tts) ·
SQLite memory study → [`webtyp/agentmemory`](https://github.com/webtyp/agentmemory).
