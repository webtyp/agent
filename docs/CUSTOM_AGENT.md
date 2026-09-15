# Custom Agentic AI: Architectural Paradigms & Design Principles

## 1. Core Philosophy
This document aggregates the "bare metal" design principles for building a custom, multi-tenant Agentic AI system. It prioritizes **deterministic control**, **data isolation**, and **observability** over the convenience of "black box" frameworks. The goal is surgical precision in state management and tool orchestration.

## 2. Cognitive Architectures (Reasoning Patterns)

### 2.1 The ReAct Pattern (Reasoning + Acting)
The standard loop for general-purpose agents. It combines reasoning traces with action execution.
*   **Flow:** `Input -> Thought (Reasoning) -> Action (Tool Call) -> Observation (Result) -> Repeat`.
*   **Critical Constraint:** The **Stop Sequence**. The orchestrator must strictly halt generation after an "Action" is emitted to prevent the LLM from hallucinating the "Observation".
*   **Trade-offs:** High interpretability but serial execution (slower). Prone to loops if observations are not actionable.

### 2.2 Plan-and-Execute
For complex, multi-step queries (e.g., "Check invoices A, B, and C").
*   **Planner:** Generates a DAG or list of steps (no execution).
*   **Executor:** Processes the steps, potentially in parallel.
*   **Benefit:** Prevents losing context in long chains; optimizes latency via parallelism.

### 2.3 Reflection & Self-Correction
*   **Mechanism:** A "Critique" step before the final answer or after tool errors.
*   **Value:** Reduces hallucinations in high-precision tasks (math, data retrieval) by verifying output against retrieved data.

## 3. Tool Interaction Protocols

### 3.1 The Function Call Lifecycle
Tool usage is a structured text-processing protocol, not magic.
1.  **Definition:** Tools are injected via JSON Schema. The **Description** field is critical—it drives the LLM's decision logic.
2.  **Detection:** LLM emits a structured "Tool Call" (name + JSON args). Orchestrator halts text generation.
3.  **Execution:** System parses JSON, validates against schema, executes the native function (Go), and serializes output.
4.  **Context Injection:** Output is appended to history with role `tool` or `observation`. LLM generates the final response based on this new context.

### 3.2 Protocol Agnosticism & MCP
*   **Adapter Pattern:** Use a **Tool Registry** to decouple internal logic from provider-specific formats (OpenAI Tools vs. Anthropic XML).
*   **Model Context Protocol (MCP):** Treat tools as distinct servers using standardized JSON-RPC 2.0. This allows sharing tools across different agents and simplifies scaling.

## 4. Multi-Tenancy & Memory Isolation

### 4.1 Hierarchical Memory
State is not monolithic; it is segmented for security and context window management.
1.  **Short-Term (Working):** Active session context (FIFO/Sliding Window). Ephemeral.
2.  **Episodic (Long-Term):** vector-based retrieval of past interactions. **Strict Isolation:** Queries *must* filter by `tenant_id` *before* similarity search.
3.  **Semantic (Knowledge):** RAG base. Can be global or tenant-specific (Namespaced).

### 4.2 Data Model Principles
*   **Relational Backbone:** Use meaningful schemas (Sessions, Threads, Messages) rather than blob storage.
*   **Auditability:** Separate `tool_calls` from `messages` to enable granular analysis (latency, failure rates, usage patterns).
*   **See also:** `ARCHITECTURE.md` and `history/MEMORY_SQLITE.md` for the concrete database schema.

## 5. Orchestration Logic

### 5.1 Orchestrator vs. Router
*   **Router:** Lightweight intent classifier. Directs traffic to specific flows (e.g., "Procedural/Reset Password" vs. "Reasoning/Debug Error").
*   **Orchestrator:** Manages the active state loop of a specific agent.

### 5.2 Finite State Machines (FSM)
To ensure enterprise reliability, replace purely probabilistic loops with FSMs.
*   **Concept:** Define valid transitions (e.g., `Idle -> Reasoning -> Executing -> Verifying`).
*   **Constraint:** Restrict available tools/actions based on current state (e.g., no "Write" actions in "GatheringInfo" state).

## 6. Development Phases (Roadmap)

1.  **Central Engine:** `AgentNode` class. Handles Prompt construction -> LLM Call -> Tool parsing -> Execution loop.
2.  **State Manager:** Database layer. Implement Sessions, History retrieval, and Token Window management (summarization/pruning).
3.  **Router:** Dispatch logic to specialized agents (e.g., Sales vs Technical).
4.  **Guardrails:** Tool error handling (try/catch -> notify LLM), Rate Limiting, and Hallucination Checks (Reflection).

## 7. Key Framework Concepts to Adopt
*   **Graph Orchestration:** (From LangGraph) Model complex flows as nodes and edges rather than linear chains.
*   **Role-Based Systems:** (From CrewAI) Explicitly define "Role", "Goal", and "Backstory" in system prompts to stabilize behavior.
*   **Kernel/Registry:** (From Semantic Kernel) Centralized, typed management of all skills and memory resources for a request.
