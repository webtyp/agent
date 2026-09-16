# Agent
<img src="docs/img/badges.svg">

Autonomous AI Agent system for `tinywasm`.

## Documentation

### Current Plan
*   [**PLAN.md**](docs/PLAN.md) — **master index**: browser-native semantic search on IndexedDB.
    Architecture, shared contracts, build order and phase gates across every repository involved.
*   [Plans for repositories not yet created](docs/plans/) — `vector`, `vectordb`, `tokenizer`,
    `weights`, `embed`, `webgpu`, `nn`. Each moves to its own repository once created.

### Core Guides
*   [System Architecture](docs/ARCHITECTURE.md) - High-level definition and contracts.
*   [Implementation Guide](docs/IMPLEMENTATION.md) - Technical implementation details.
*   [Canonical Types](docs/TYPES.md) - All value types: Message, Episode, LLMRequest/Response, ToolDef, etc.
*   [Custom Agent Research](docs/CUSTOM_AGENT.md) - Research and principles for building agents.
*   [LLM Skill Reference](docs/DEFAULT_LLM_SKILL.md) - Mandatory engineering rules for LLMs working on this project.

### History
*   [SQLite Memory Architecture](docs/history/MEMORY_SQLITE.md) - **superseded**. The SQLite +
    `sqlite-vec` study; kept for its memory categorisation and RRF reasoning, which the current
    plan reuses.
*   [Isomorphic Compatibility Refactor](docs/history/ISOMORPHIC-COMPATIBILITY.md) - executed.
*   [Last Plan Executed](docs/LAST_PLAN_EXECUTED.md) - replace `mcpserve` with `mcp` in tests.

### Architecture Diagrams
*   [System Context](docs/diagrams/SYSTEM_CONTEXT.md)
*   [ReAct + Reflection Flow](docs/diagrams/REACT_FLOW.md)
*   [FSM State Machine](docs/diagrams/FSM_STATE.md)
*   [Memory Schema & Architecture](docs/diagrams/MEMORY_ARCHITECTURE.md)
*   [MCP Client Flow](docs/diagrams/MCP_CLIENT_FLOW.md)
*   [Context Window Logic](docs/diagrams/CONTEXT_WINDOW.md)
*   [Integration Test Scenario](docs/diagrams/INTEGRATION_SCENARIO.md)
