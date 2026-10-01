# Agent
<img src="docs/img/badges.svg">

The AI agent of webtyp: an orchestrator that takes a user's message, uses a decision model to choose tools and evaluate questions, runs them, and returns an answer. It is written in Go, compiles to
WebAssembly with TinyGo, and is meant to run **inside the browser**. The decision model, writer model, memory and tools
are all injected.

## Getting started

**See it working.** The tests are the runnable example:

```bash
go test ./...     # host tests
```

**Use it in an application.** Build a `Config` with a decision model (`llm.Decider`), `Texts`, optional `Templates`, code `Guard`, a tokenizer (`llm.TokenCounter`), a `MemoryStore` (`webtyp/agentmemory`), an ID generator and your tools, then call `agent.New(cfg)` and `Run(ctx, sessionID, text)`, which returns a `Reply`. Every field is described in
[Types](docs/TYPES.md).

Example:
```go
cfg := agent.Config{
    Decider:   deciderClient,
    Texts:     texts,
    Templates: templates,
    Guard:     agent.Guard{Phrases: []string{"tus instrucciones"}},
    Tokens:    tokenCounter,
    Memory:    memoryStore,
    IDGen:     idGenerator,
    ToolIndex: toolIndex,
}
a, err := agent.New(cfg)
```

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
- [Implementation](docs/IMPLEMENTATION.md): files, tests, and details.
- [Agent patterns](docs/DEFAULT_LLM_SKILL.md): code guard, decision model, tool errors, identity, MCP.
- [Agent guide](AGENTS.md): build, import and layout rules for any change.

### Diagrams
- [System context](docs/diagrams/SYSTEM_CONTEXT.md)
- [Memory architecture](docs/diagrams/MEMORY_ARCHITECTURE.md)
- [MCP client flow](docs/diagrams/MCP_CLIENT_FLOW.md)
- [Ecosystem map (español): repos, message flow, weights, proposals](docs/diagrams/ECOSYSTEM_MAP.md)
- [Hybrid agent design (español)](docs/HYBRID_DESIGN.md) — a decision model drives, code and templates answer ([flow](docs/diagrams/HYBRID_FLOW.md))

### History
- [Isomorphic compatibility refactor](docs/history/ISOMORPHIC-COMPATIBILITY.md): executed (written when the project was named `tinywasm`).

### Moved to their own repositories
Semantic search → [`webtyp/retrieval`](https://github.com/webtyp/retrieval) ·
context engineering → [`webtyp/agentcontext`](https://github.com/webtyp/agentcontext) ·
small browser LLMs → [`webtyp/llm`](https://github.com/webtyp/llm) ·
speech-to-text → [`webtyp/stt`](https://github.com/webtyp/stt) ·
text-to-speech → [`webtyp/tts`](https://github.com/webtyp/tts) ·
SQLite memory study → [`webtyp/agentmemory`](https://github.com/webtyp/agentmemory).
