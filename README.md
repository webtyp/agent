# Agent
<img src="docs/img/badges.svg">

Autonomous AI Agent system for `webtyp`.

## Documentation

### Current Plan
*   [**PLAN.md**](docs/PLAN.md) — the dispatchable plan for THIS repository's next change.
    Created and consumed by codejob; absent between dispatches.
*   [**MASTER_PLAN.md**](docs/MASTER_PLAN.md) — **master index**: browser-native semantic search on IndexedDB.
    Architecture, shared contracts, and what's still pending across every repository involved.
*   [**docs/history/**](docs/history/) — how the design got here: every correction, every PR
    postmortem, every closed decision. Frozen; MASTER_PLAN.md is the live document.

### Core Guides
*   [System Architecture](docs/ARCHITECTURE.md) - High-level definition and contracts.
*   [Implementation Guide](docs/IMPLEMENTATION.md) - Technical implementation details.
*   [Canonical Types](docs/TYPES.md) - All value types: Message, Episode, LLMRequest/Response, ToolDef, etc.
*   [Custom Agent Research](docs/CUSTOM_AGENT.md) - Research and principles for building agents.
*   [LLM Skill Reference](docs/DEFAULT_LLM_SKILL.md) - Mandatory engineering rules for LLMs working on this project.

### Research
*   [Small embedding models](docs/SMALL_MODEL_FOR_EMBEDING.md) - **consolidated**: the 384-dim
    multilingual candidates (Granite 97M R2, Bekko a25m/a8m), what "active parameters"
    actually means for browser compute, and the two measurements that pick the winner.
    This document governs PLAN D5.
*   [Cloudflare Workers AI](docs/CLOUDFLARE_AI_WORKER.md) - embedding and LLM model costs on
    Workers AI; why the catalogue was ruled out as the model source (PLAN D4).

### History
*   [SQLite Memory Architecture](docs/history/MEMORY_SQLITE.md) - **superseded**. The SQLite +
    `sqlite-vec` study; kept for its memory categorisation and RRF reasoning, which the current
    plan reuses.
*   [Isomorphic Compatibility Refactor](docs/history/ISOMORPHIC-COMPATIBILITY.md) - executed.
*   [WebGPU Encoder](docs/history/WEBGPU_ENCODER.md) - **archived, not cancelled**. The
    browser only embeds queries, which runs on CPU/WASM (PLAN D4b), so no GPU is needed.
    Unarchive only if the phase-3 benchmark says otherwise.

### Architecture Diagrams
*   [System Context](docs/diagrams/SYSTEM_CONTEXT.md)
*   [ReAct + Reflection Flow](docs/diagrams/REACT_FLOW.md)
*   [FSM State Machine](docs/diagrams/FSM_STATE.md)
*   [Memory Schema & Architecture](docs/diagrams/MEMORY_ARCHITECTURE.md)
*   [MCP Client Flow](docs/diagrams/MCP_CLIENT_FLOW.md)
*   [Context Window Logic](docs/diagrams/CONTEXT_WINDOW.md)
*   [Integration Test Scenario](docs/diagrams/INTEGRATION_SCENARIO.md)
