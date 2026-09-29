# Integration Test Scenario: Clinic Assistant

This diagram documents the flow covered by the `//go:build integration` tests in `integration_test.go`.
These tests require llama.cpp's **`llama-server`** running locally on port 8080 with a small
GGUF model (see [IMPLEMENTATION.md](../IMPLEMENTATION.md#real-model-integration-test)). They are
skipped automatically when it is not running.

Linked from: [IMPLEMENTATION.md](../IMPLEMENTATION.md) · [README.md](../../README.md)

---

## Scenario 1: Clinic Hours Query (`TestIntegration_ClinicHours`)

```mermaid
sequenceDiagram
    participant T as Test
    participant A as Agent.Run()
    participant O as Orchestrator (FSM)
    participant L as llama-server (:8080)
    participant M as MemoryStore (mem_memory)

    T->>A: Run(ctx, "session-clinic-1", "¿A qué hora abren los lunes?")
    A->>M: EnsureSession("session-clinic-1")
    A->>M: AppendTurn(role="user", content="¿A qué hora abren los lunes?")
    A->>O: FSM transition: Idle → Reasoning

    O->>M: GetTurns("session-clinic-1", limit=20)
    M-->>O: [user message]
    O->>M: GetSummaries("session-clinic-1", limit=5)
    M-->>O: []

    O->>L: Generate(llm.Request{system="Eres recepcionista...", tools=[clinic_hours]})
    Note over L: Detects tool_use needed
    L-->>O: llm.Response{StopReason=StopToolUse, ToolCalls=[{Name:"clinic_hours"}]}

    O->>O: FSM transition: Reasoning → Acting
    O->>O: Execute clinic_hours tool → "Lunes-Viernes 8h-20h, Sábado 9h-14h"
    O->>M: AppendTurn(role="tool", content="Lunes-Viernes 8h-20h...")
    O->>M: LogToolCall("clinic_hours", input="{}", output="Lunes-Viernes 8h-20h...")
    O->>O: FSM transition: Acting → Reasoning

    O->>L: Generate(llm.Request{messages=[user, tool_result]})
    L-->>O: llm.Response{StopReason=StopEndTurn, Text="La clínica abre de lunes a viernes..."}

    O->>O: FSM transition: Reasoning → Reflecting
    O->>L: Generate(reflectionRequest: "¿Es la respuesta completa y precisa?")
    L-->>O: "SUFFICIENT"
    O->>O: FSM transition: Reflecting → Responding

    O->>M: AppendTurn(role="assistant", content="La clínica abre de lunes a viernes...")
    O->>O: FSM transition: Responding → Idle
    A-->>T: "La clínica abre de lunes a viernes de 8:00 a 20:00..."

    T->>T: assert contains hours, assert language == Spanish
```

---

## Scenario 2: Session Isolation (`TestIntegration_SessionIsolation`)

```mermaid
sequenceDiagram
    participant T as Test (2 goroutines)
    participant A as Agent.Run()
    participant M as MemoryStore (mem_memory)
    participant L as llama-server

    Note over T: Goroutine 1 (Session A)
    T->>A: Run(ctx, "session-A", "Mi nombre es Carlos")
    A->>M: AppendTurn(session-A, role="user", "Mi nombre es Carlos")
    A->>L: Generate(context for session-A)
    L-->>A: "Encantado, Carlos..."
    A->>M: AppendTurn(session-A, role="assistant", "Encantado, Carlos...")

    Note over T: Goroutine 2 (Session B) — concurrent
    T->>A: Run(ctx, "session-B", "¿Cómo me llamo?")
    A->>M: GetTurns("session-B", limit=20)
    M-->>A: [] (empty — session-B has no history)
    A->>L: Generate(llm.Request{messages=[]})
    L-->>A: "No tengo información sobre tu nombre."
    A-->>T: response for session-B

    T->>T: assert session-B response does NOT mention "Carlos"
    T->>T: assert session-A messages NOT in session-B memory
```

---

## Diagram Coverage

| FSM States Exercised | Source |
|----------------------|--------|
| Idle → Reasoning | Both scenarios |
| Reasoning → Acting | Scenario 1 (tool_use) |
| Acting → Reasoning | Scenario 1 |
| Reasoning → Reflecting | Scenario 1 |
| Reflecting → Responding | Scenario 1 |
| Responding → Idle | Scenario 1 |
| Memory isolation | Scenario 2 |

> These integration tests complement (not replace) the DDT unit tests that use mock LLMs.
> They verify real LLM behavior and end-to-end memory isolation with a live `llama-server`.
