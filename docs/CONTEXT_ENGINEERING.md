# Context Engineering para el Agente

Principios de diseño para un agente de nivel producción que corre en el navegador (WASM), con un LLM pequeño (<1B) orientado a tool-calling, RAG/memoria local y tools vía MCP. Prioriza **control determinista**, **aislamiento de datos** y **observabilidad** por sobre frameworks "caja negra".

> Un agente no es un prompt: es un **bucle** (planificar → actuar → observar → repetir) sobre un **estado externo**. El prompt es solo una proyección pequeña de ese estado para cada inferencia.

---

## 1. Idea central: estado ≠ contexto

No se acumula todo en el prompt (conversación + memoria + documentos + todas las tools). Eso crece sin límite. En su lugar, un **context compiler** construye en cada inferencia una *vista de trabajo* mínima:

```text
STATE (grande, fuera del modelo)          WORKING CONTEXT (pequeño)
───────────────────────────────           ─────────────────────────
10.000 mensajes                           identity
3.000 memorias           ── compiler ──►  task + estado mínimo
100 documentos                            ~5 memorias relevantes
500 tools                                 ~2 chunks de documentos
artifacts (GB)                            2–4 tools
                                          input actual
```

- El RAG, la memoria y los documentos **no son el prompt**: son fuentes desde las que se compila.
- El **runtime**, no el LLM, decide cuánto contexto cargar.
- Ciclo: `State → Retrieve → Compile → Infer → Update State`.

Google (ADK) lo describe como *working context* compilado desde *session + memory + artifacts*; Anthropic lo llama *context engineering*; OpenAI recomienda prefijos estables, tools bajo demanda y compactación. [1][2][4][9]

---

## 2. Patrones de razonamiento

### 2.1 ReAct (Reason + Act)
Bucle base: `Input → Thought → Action (tool call) → Observation → repetir`.
- **Stop sequence obligatoria:** el orquestador detiene la generación tras emitir una acción, para que el LLM no alucine la observación.
- Interpretable pero serial; propenso a bucles si las observaciones no son accionables.
- La fiabilidad **cae con cada paso añadido**: minimizar pasos y complejidad.

### 2.2 Plan-and-Execute
Para tareas multipaso ("revisa las facturas A, B y C").
- **Planner:** genera una lista/DAG de pasos, sin ejecutar.
- **Executor:** recibe un prompt mínimo con *solo el paso actual* y sus tools; puede paralelizar.
- Es la forma más eficiente de aislar contexto en cadenas largas.

### 2.3 Reflection
Paso de "crítica" antes de la respuesta final o tras un error de tool. Verifica la salida contra datos recuperados; reduce alucinaciones en tareas de precisión.

---

## 3. Herramientas

### 3.1 Solo dos tools permanentes
El modelo ve siempre únicamente:

```text
search_tools(query)            → busca en el tool registry, devuelve esquemas relevantes
execute_tool(name, arguments)  → ejecuta la tool elegida
```

El resto (`calendar.book`, `github.search`, …) vive fuera del contexto. Flujo: el modelo llama `search_tools("reservar hora")`, el runtime inyecta temporalmente 2–3 esquemas, y el modelo ejecuta. Es el patrón *Tool Search / deferred loading* de Anthropic. [3]

Salida estructurada mínima (ideal para un modelo <1B ajustado para orquestación):

```json
{"action": "search", "query": "reservar hora médica"}
{"action": "execute", "tool": "calendar.book", "arguments": {"doctor": "123", "date": "2026-09-28", "time": "15:00"}}
```

### 3.2 Tool Registry (RAG de tools)
Cada entrada: `name`, `description`, `tags`, `embedding`, `schema` (JSON Schema), `permissions`, `risk` (read/write).
- La **descripción** es lo que guía la decisión del LLM: debe ser precisa.
- Actúa como **adapter**: desacopla la lógica interna de formatos de proveedor (tools OpenAI, XML Anthropic, formato llama).
- Diseño tipo API estricta. Separar tools de **lectura** (bajo riesgo) de las de **escritura** (alto riesgo).

### 3.3 Ciclo de vida de un function call
1. **Definición:** esquema inyectado (solo los descubiertos en este turno).
2. **Detección:** el LLM emite nombre + args JSON; el orquestador detiene la generación.
3. **Ejecución:** parsear, **validar contra el esquema**, ejecutar la función nativa (Go), serializar.
4. **Inyección:** el resultado entra con rol `tool`/`observation`, **pequeño y estructurado** (resultados grandes → artifact con handle).

### 3.4 MCP
MCP separa **Prompts** (cómo comportarse), **Resources** (información) y **Tools** (acciones) sobre JSON-RPC 2.0, lo que permite compartir tools entre agentes. [5][6] MCP **no** implica meter todas las tools en el prompt: el cliente decide qué exponer. La spec recomienda orden determinista en `tools/list` para favorecer caching. [7]

```text
MCP server (500 tools, 100 resources) → agente → tool search → 2–4 tools → LLM
```

---

## 4. Estructura del prompt

### 4.1 Prefijo estático + sufijo dinámico
Lo estable va primero y no cambia (cacheable); lo dinámico va al final. [4]

```text
┌ STATIC (cacheable) ────────────┐
│ identity · rules               │
│ tool protocol · output protocol│
│ core tools (search/execute)    │
├ DYNAMIC (cambia por turno) ────┤
│ task + state                   │
│ memoria relevante              │
│ chunks recuperados             │
│ tools descubiertas             │
│ últimos 2–4 turnos             │
│ input del usuario              │
└────────────────────────────────┘
```

Para estabilizar el comportamiento, la identidad define explícitamente **rol, objetivo y trasfondo** (patrón CrewAI).

### 4.2 Plantilla inicial

```text
<identity>assistant</identity>
<task>{goal}</task>
<state>{minimal_state}</state>
<memory>{relevant_memory}</memory>
<tools>search_tools, execute_tool</tools>
<input>{user_message}</input>
```

Esto es solo el **render** para un modelo concreto, no la estructura interna:

```go
type Context struct {
    Identity Identity
    Task     Task
    State    State
    Memory   []Memory
    Tools    []Tool
    Input    string
}
```

`Context → filtro de presupuesto/relevancia → renderer (chat, llama, …) → LLM`. El agente no debe depender de un formato de prompt específico.

### 4.3 Presupuesto por sección

| Sección  | Tokens   |
|----------|----------|
| identity | 100      |
| task     | 100      |
| state    | 200      |
| memory   | 300      |
| tools    | 500      |
| input    | variable |
| **Total**| **~1.2k** en vez de 20k–100k |

### 4.4 Progressive disclosure
Cada nivel aparece **solo cuando hace falta**: N0 identity + rules + 2 tools → N1 task state + memoria corta → N2 resultados de tool search → N3 esquema de la tool → N4 resultado de la tool → N5 chunk RAG específico. (Modelo usado también por la extensión Skills de MCP. [8])

---

## 5. Documentos y artifacts

**Nunca se inyecta un documento completo.** Al subirlo:

1. **Registro:** `artifact{id: doc_381, type: pdf, name: contrato.pdf, size: 4.8MB}`.
2. **Procesamiento en background:** extracción, chunking semántico, vectorización local (IndexedDB/SQLite-WASM), más resumen, temas, entidades y metadatos.
3. **Lo que ve el prompt:** solo un handle + resumen:
   ```text
   DOCUMENTS
   contrato.pdf (id: doc_381) — Contrato de prestación de servicios...
   Para consultar su contenido usa la tool de búsqueda en documentos.
   ```
4. **Consulta:** ante "¿cuál es la duración del contrato?", el RAG recupera el chunk relevante y se inyecta solo eso: `[source=doc_381#183] ...`.

Patrón *Artifacts + handles* de Google ADK. [2] Aplica igual a CSV, imágenes y resultados grandes de tools.

---

## 6. Memoria

### 6.1 Niveles

| Nivel | Tipo | Contenido | Dónde vive |
|-------|------|-----------|------------|
| L0 | Conversación inmediata | últimos 2–6 turnos (sliding window) | prompt |
| L1 | Estado de tarea | `{goal, status, entities, constraints, last_action}` | estado estructurado |
| L2 | Episódica | "el usuario decidió usar PostgreSQL" | RAG |
| L3 | Semántica | preferencias, hechos, conocimiento (global o por tenant) | RAG vector/keyword |
| L4 | Artifacts | PDF, imágenes, CSV, resultados grandes | fuera del contexto |

El **estado de tarea (L1)** importa mucho más que conservar toda la conversación. Diferenciar estado de ejecución inmediato de memoria de largo plazo evita la *podredumbre del contexto* (context rot).

### 6.2 Aislamiento multi-tenant
Las consultas a memoria episódica/semántica **deben filtrar por `tenant_id` antes** de la búsqueda por similitud. La memoria semántica puede ser global o con namespace por tenant.

### 6.3 Historial y compactación
Al superar el presupuesto: `turnos 1..15 → resumen → task state`, conservando `resumen + últimos 3 turnos + estado actual`. No resumir todo en un solo bloque: separar **resumen de conversación, task state, hechos, decisiones, preferencias e historial de tools**, porque tienen distinta vida útil. La compactación puede invalidar el prefijo cacheable: mantener estable la parte reutilizable. [9]

### 6.4 Estado estructurado (`AgentState`)

```json
{
  "session": "...",
  "task":      {"goal": "...", "status": "waiting_tool"},
  "user":      {"id": "...", "tenant_id": "...", "locale": "es-CL"},
  "memory":    ["..."],
  "artifacts": [{"id": "...", "summary": "..."}],
  "tools":     {"available": [], "loaded": []},
  "turn":      {"input": "..."}
}
```

---

## 7. Orquestación

- **Router:** clasificador de intención liviano que despacha a flujos (procedural "resetear contraseña" vs razonamiento "depurar error").
- **Orquestador:** gestiona el bucle de estado de un agente concreto.
- **Enrutamiento de modelos:** modelos pequeños y rápidos para tareas simples; modelos "frontera" solo para casos que requieren juicio.
- **Pipeline vs autónomo:** elegir siempre la arquitectura **más simple que funcione**; preferir pipelines definidos cuando el flujo es conocido.
- **Máquina de estados (FSM):** reemplaza bucles puramente probabilísticos. Transiciones válidas (`Idle → Reasoning → Executing → Verifying`) y **tools restringidas por estado** (p. ej. sin escritura en `GatheringInfo`).
- **Grafo:** flujos complejos como nodos y aristas, no cadenas lineales (LangGraph).
- **Kernel/Registry:** gestión centralizada y tipada de skills y recursos de memoria por request (Semantic Kernel).

```text
USER → AGENT RUNTIME (browser)
          ├── Memory ───┐
          ├── RAG ──────┼──► CONTEXT COMPILER ──► LLM <1B ──► search_tools / execute_tool
          └── Artifacts ┘            ▲                              │
                                     └──────── update state ◄───────┘
```

---

## 8. Datos, evals, observabilidad y seguridad

### 8.1 Modelo de datos
- Esquema relacional (Sessions, Threads, Messages), no blobs.
- `tool_calls` separado de `messages` para analizar latencia, fallos y uso.
- Esquema concreto: ver `ARCHITECTURE.md` y `history/MEMORY_SQLITE.md`.

### 8.2 Evals y observabilidad
Los agentes son no deterministas: requieren **tests de regresión específicos** (casos de input → tool/args esperados) y **tracing** detallado de cada paso (contexto compilado, tool call, resultado) para depurar en producción.

### 8.3 Seguridad y guardrails
- Tratar **toda entrada del modelo y de tools como potencialmente hostil**.
- Acciones de alto riesgo (escrituras, pagos, reembolsos) validadas **por código**, no por el LLM; confirmación del usuario cuando corresponda.
- Errores de tool: capturar y devolver al LLM como observación estructurada.
- Rate limiting y chequeos de alucinación (reflection).
- Roles tipados (`system`/`user`/`tool`) para separar reglas, peticiones y resultados y mitigar prompt injection.

---

## 9. ¿Existe un estándar?

No hay un formato universal de contexto para agentes, pero sí una convergencia clara:

| Convención | Qué aporta |
|------------|------------|
| Mensajes tipados (system/user/assistant/tool, ChatML) | Separa reglas, peticiones y observaciones; buena para chat, pobre para estado complejo |
| Tool calling estructurado (nombre + JSON args) | Base de todo agente con acciones |
| MCP | Separación prompts/resources/tools y descubrimiento estándar |
| ReAct / Plan-and-Execute | Patrones de razonamiento de facto |

Tendencias compartidas por Anthropic, OpenAI y Google:

1. Estado externo al LLM.
2. Memoria recuperable, no inyectada permanentemente.
3. Artifacts externos referenciados por handle.
4. Tools descubiertas bajo demanda.
5. Contexto reconstruido en cada inferencia.
6. Prefijo estable + sufijo dinámico (caching).
7. Compactación cuando la sesión crece.
8. Resultados de tools pequeños y estructurados.
9. Progressive disclosure.

Para un LLM <1B en WASM esto es clave: la complejidad se traslada al runtime/RAG y el modelo solo necesita saber **qué es, qué intenta hacer, qué sabe ahora y qué puede hacer ahora**.

---

## 10. Roadmap

1. **Motor central (`AgentNode`):** construcción de contexto → llamada al LLM → parseo de tool call → ejecución → bucle.
2. **State Manager:** capa de datos (sesiones, historial, task state) y gestión de ventana (resumen/poda).
3. **Tool Registry + `search_tools`:** registro con embeddings y carga diferida.
4. **Context Compiler:** presupuestos por sección y renderers por formato de modelo.
5. **Router:** despacho a agentes/flujos especializados.
6. **Guardrails:** manejo de errores de tools, validación de acciones de riesgo, rate limiting, reflection.
7. **Evals + tracing.**

---

## Referencias

1. [Anthropic — Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
2. [Google — Architecting efficient context-aware multi-agent framework](https://developers.googleblog.com/architecting-efficient-context-aware-multi-agent-framework-for-production/)
3. [Anthropic — Advanced tool use (Tool Search)](https://www.anthropic.com/engineering/advanced-tool-use)
4. [OpenAI — Prompt caching](https://developers.openai.com/api/docs/guides/prompt-caching)
5. [MCP — Server overview](https://modelcontextprotocol.io/specification/draft/server/index)
6. [MCP — Tools spec](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-06-18/server/tools.mdx)
7. [MCP — Changelog](https://modelcontextprotocol.io/specification/draft/changelog)
8. [MCP Skills Extension](https://skills.extensions.modelcontextprotocol.io/specification/stable/skills)
9. [OpenAI — Compaction](https://developers.openai.com/api/docs/guides/compaction)
