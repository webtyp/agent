---
PLAN: "feat: agent como orquestador — la memoria sale del repositorio"
TAG: v0.3.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/agent
---

> El diff concreto de **este** repositorio. El índice maestro —arquitectura, contratos
> compartidos, orden de construcción y puertas— es [`docs/PLAN.md`](../PLAN.md), y las
> decisiones **D4**, **D5** y **D7** de ahí son la justificación de lo de abajo: no se
> vuelven a argumentar acá.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus
> comentarios en inglés.

# Plan — `agent` queda sin almacenamiento

## 1. Alcance

`webtyp/agent` es el **orquestador**: bucle ReAct, FSM, ventana de contexto y registro de
herramientas. Coordina otras librerías; no implementa ninguna de sus capacidades. La
decisión de fondo de esta sección — **la memoria sale de este repositorio** — se toma acá y
no se vuelve a argumentar en los planes de abajo.

1. **Migración de module path** `github.com/tinywasm/agent` → `webtyp.com/agent`, junto con
   las dependencias `github.com/tinywasm/{fmt,mcpserve,…}` de `go.mod`, que también siguen
   apuntando al path viejo. Todos los repositorios de este plan ya migraron; `agent` no
   puede depender de ellos mientras publica el path viejo sin confundir la actualización de
   módulos dependientes de `gopush`.
   **Va primero: prerrequisito de todo lo demás acá, y no depende de ninguna fase** — se
   puede ejecutar hoy, en paralelo con la fase 0.

2. **`memory.go` y `schema.go` salen a `webtyp/agentmemory`.** No se reescriben acá: se
   mudan. `agent` conserva el contrato (`MemoryStore` y los tipos que lo atraviesan:
   `Message`, `Episode`, `Knowledge`, `ToolLog`) y pierde `modernc.org/sqlite` del `go.mod`.
   `webtyp/agentmemory` implementa ese contrato sobre `webtyp.com/orm` + `webtyp.com/ddl`
   (ver **D7**), de modo que la misma implementación corra sobre SQL y sobre `indexdb`.

   *Por qué mudar y no reescribir en el lugar:* hoy `Config.Memory` es un `MemoryStore`
   inyectado y `DEFAULT_LLM_SKILL.md` §1 exige que «el struct `Agent` sostenga interfaces,
   nunca implementaciones concretas». Esa regla se cumple en `agent.go` y **se rompe en el
   `go.mod`**: quien embeba `agent` en un proyecto con Postgres, o en el navegador, igual
   linkea un motor SQLite que nunca instancia. Es la **D** de SOLID verificada donde importa
   — en el grafo de build, no en la prosa. Arte previo: `database/sql` define
   `driver.Driver` y cada driver vive en su propio módulo; lo mismo hace `storage` con
   `sqlt`/`postgres`/`indexdb`.

   Se ejecuta **junto con el punto 1**, en una sola migración sobre los mismos archivos.

3. **El esquema como valores `model.Definition` materializados por `webtyp.com/ddl`.**
   `agentmemory` declara las `Definition` y `ddl` las convierte en DDL sobre un backend SQL
   y en object stores sobre `indexdb`. Nadie escribe cadenas de DDL: `schema.go` desaparece
   en la mudanza, no se porta.

4. **`SearchKnowledge` gana un camino semántico** a través de `vectordb`. Es un cambio de
   `agentmemory`, **no de `agent`**: la firma
   `SearchKnowledge(ctx, query, sessionID string, limit int)` recibe **texto**, así que el
   orquestador ya no necesita saber nada de vectores.

   FTS5 léxico no existe fuera de SQLite: sobre `indexdb`, la mitad léxica de RRF espera al
   índice BM25 de la fase 5. Hasta entonces es léxico por `LIKE` (soportado en
   `indexdb/execute.go: matchLike` y en `storage.Like`) y semántico por vectores.

5. **`Config` NO gana un `Embedder`.** El `embed.Embedder` se inyecta en el constructor de
   `agentmemory`, que es quien lo usa:

   ```go
   mem, _ := agentmemory.New(agentmemory.Config{Conn: conn, Embedder: emb})
   ag,  _ := agent.New(agent.Config{Memory: mem, LLMs: llms})
   ```

   `agent` no importa `webtyp.com/embed`. Esto revierte lo que anticipaba el estudio
   histórico, y la razón es la misma del punto 2: un `Embedder` en `Config` obliga al
   orquestador a saber que alguna implementación, en algún lado, hace búsqueda vectorial.

6. **`MemoryStore` se segrega en cuatro contratos y se compone.** Ver §2.

7. Actualizar la lista de dependencias permitidas de `DEFAULT_LLM_SKILL.md`:
   `modernc.org/sqlite` sale — **y no entra nada en su lugar**. `agent` queda sin
   dependencias de almacenamiento. `webtyp.com/orm`, `webtyp.com/ddl`, `webtyp.com/vectordb`
   y `webtyp.com/embed` son dependencias de `agentmemory`, no de `agent`.
   Corregir también la línea de §1 que dice «`memory.go` — SQLite MemoryStore implementation
   only»: ese archivo deja de existir acá.

8. **`Message.TokenCount` tiene tres significados distintos según qué línea lo escribió, y
   `prepareContext` los suma como si fueran la misma unidad.**

   | Dónde | Qué guarda |
   |---|---|
   | `orchestrator.go:25` | `len(userQuery) / 4` — estimación por caracteres |
   | `orchestrator.go:89` | `resp.TokensUsed` — el total del **turno entero** (prompt + completion), no el de ese mensaje. El comentario en el código dice `// Approximation?`, con signo de pregunta |
   | `orchestrator.go:141` | `len(output) / 4` — estimación otra vez |
   | `orchestrator.go:179` | `resp.TokensUsed` — mismo problema que :89 |

   `context_window.go` suma esos valores y los compara contra
   `MaxTokens × SummarizeAt` para decidir cuándo resumir. Es decir: **la decisión de resumir
   se toma sobre una suma de unidades incompatibles**, y el error crece con la cantidad de
   turnos, porque `TokensUsed` es acumulativo y se guarda una vez por turno.

   No es una optimización pendiente: es un presupuesto que no mide lo que dice medir. El
   arreglo mínimo es que `Message.TokenCount` tenga **un** significado documentado —los
   tokens de ese mensaje— y que quien no pueda saberlo lo deje en cero en vez de rellenarlo
   con una estimación de otra unidad. Un cero honesto es mejor que un número inventado,
   porque el cero se puede detectar.

   Va con el punto 1, porque toca los mismos archivos.

9. Corregir `README.md`, que todavía describe el proyecto como "Autonomous AI Agent system
   for `tinywasm`". Va con el punto 1.

**Lo que este plan NO hace con `ContextWindowConfig`, y por qué.** `MaxTokens`,
`SummarizeAt`, `MaxRecentMsgs` y `MaxEpisodes` configuran **una** estrategia de resumen
cableada en `prepareContext` (resumir el 50% más viejo al cruzar el umbral). Extraerla a un
contrato enchufable es tentador y el skill dice que **no**, todavía:

- **Gate 5 — «¿qué borra este cambio?»** Nada. Habría una interfaz nueva y la misma única
  implementación detrás.
- **Gate 3 — el libro mayor.** «Conceptos +1 / −0» sin que ninguna otra fila mejore. Una
  interfaz que sólo agrega tiene que justificarse a los gritos, y acá no hay un segundo
  llamador que la pida.
- **L — sustituibilidad.** Un contrato con una sola implementación no tiene con qué publicar
  una suite de conformance, así que su sustituibilidad sería una promesa, no un hecho. Es
  exactamente lo que la L prohíbe.

La **O** sí queda rozada —agregar una segunda estrategia hoy obliga a editar
`prepareContext`— pero la O se cobra cuando la segunda implementación existe, no antes. Si
aparece, esto se reevalúa con el gate completo. Mientras tanto, lo que hay que arreglar es el
punto 8, que es un defecto y no una decisión de diseño.

## 2. `MemoryStore`: cuatro contratos, un nombre compuesto

`interfaces.go` declara hoy un solo contrato de once métodos que cubre cuatro dominios.
Toda implementación debe proveer los once, y la búsqueda semántica sólo toca uno.

Con una sola implementación eso costaba poco. Con dos —una SQL y una de navegador— la de
navegador tendría que proveer `LogToolCall` y `GetToolLogs` aunque un agente en el browser
rara vez los use, y la única salida sería un stub que devuelve `nil`. Eso es la **I** de
SOLID: *una interfaz es tan ancha como la necesita su llamador más angosto*, y un stub que
devuelve `nil` es exactamente el fallo silencioso que el harness prohíbe.

```go
// Each contract is what one collaborator of the orchestrator actually needs.
type ConversationStore interface {
	EnsureSession(ctx context.Context, sessionID string) error
	AppendMessage(ctx context.Context, sessionID string, msg Message) error
	GetMessages(ctx context.Context, sessionID string, limit int) ([]Message, error)
	DeleteMessages(ctx context.Context, sessionID string, ids []string) error
}

type EpisodeStore interface {
	SaveEpisode(ctx context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error
	GetEpisodes(ctx context.Context, sessionID string, limit int) ([]Episode, error)
}

type KnowledgeStore interface {
	SaveKnowledge(ctx context.Context, sessionID, content, source string) error
	SearchKnowledge(ctx context.Context, query, sessionID string, limit int) ([]Knowledge, error)
}

type ToolLogStore interface {
	LogToolCall(ctx context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
	GetToolLogs(ctx context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

// MemoryStore is the composed contract. Config.Memory keeps this type, so no
// call site changes and there is still exactly one way to declare a full memory.
type MemoryStore interface {
	ConversationStore
	EpisodeStore
	KnowledgeStore
	ToolLogStore
}
```

Arte previo: `io.Reader`/`Writer`/`Closer` → `io.ReadWriteCloser`, y `fs.FS` +
`fs.ReadDirFS` + `fs.StatFS`. La fila del libro mayor que nunca debe terminar positiva
—«formas de hacer lo mismo»— queda en cero, porque `MemoryStore` sigue siendo el nombre
compuesto y `Config.Memory` no cambia.

Ganancia concreta para este plan: **el camino semántico sólo necesita `KnowledgeStore`**.
Se puede construir y testear sin tocar mensajes, episodios ni bitácoras.

**Consecuencia de la L de SOLID:** en cuanto exista una segunda implementación de
`MemoryStore` —y este plan crea exactamente eso, SQL y navegador— el repositorio que **posee
el contrato** debe publicar una suite de conformance, igual que `storage/conformance` y
`ddl/conformance`. Esa suite es la puerta de la fase 4, y vive en `agent`, no en
`agentmemory`.

