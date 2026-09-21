---
PLAN: "feat: agent — MemoryStore segregado en 4 contratos + backend mem + suite de conformance"
TAG: v0.4.0
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 9716843894652945299
PR: https://github.com/webtyp/agent/pull/10
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/MASTER_PLAN.md — fase 4,
> D7, D8. Reemplaza y cierra [`docs/plans/agent.md`](plans/agent.md) — ese archivo queda como
> historial, no lo edites; lo que manda es este.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus comentarios
> en inglés.

# Plan — `webtyp/agent`, la memoria sale del repositorio

## Qué ya está hecho — no lo repitas

`docs/plans/agent.md` traía 9 puntos; 3 ya están resueltos en `main` y este plan **no los
toca**:

- **Migración de module path.** `go.mod` ya dice `webtyp.com/agent` — hecho en el PR #9.
- **`Message.TokenCount`.** Los 5 call sites de `orchestrator.go` ya escriben `TokenCount: 0`
  (no una estimación de otra unidad) — verificado, `grep -n "/ 4" orchestrator.go` da vacío.
- **`README.md`.** Ya dice "for `webtyp`", no "for `tinywasm`".

Lo que sigue es lo que falta: los puntos 2, 6 y 7 del plan original, más la suite de
conformance que ese plan dejaba pendiente ("vive en `agent`, no en `agentmemory`") sin decir
qué tests son.

## Design gate

**1. Prior art.** `io.Reader`/`Writer`/`Closer` → `io.ReadWriteCloser`; `fs.FS` + `fs.StatFS`
+ `fs.ReadDirFS`. Interno: `webtyp.com/storage` ya tiene exactamente esta forma —
`storage.Conn` como contrato compuesto, `storage/conformance` como la suite que cualquier
backend corre contra sí mismo, `storage/mem` como la referencia sin dependencias. Este plan
porta ese mismo patrón a `MemoryStore`.

**2. Novice-name test.** `ConversationStore`, `EpisodeStore`, `KnowledgeStore`,
`ToolLogStore` — cada nombre es el dominio que ya existía como grupo de métodos en la
interfaz plana; no hay nombre nuevo que aprender, sólo un corte donde ya había una costura.
`conformance.Run(t, conformance.Factory{...})` calca la firma de
`storage/conformance.Run`.

**3. Complexity ledger.**
```
Conceptos nuevos para el desarrollador   +1 (4 contratos en vez de 1 — pero cada uno ya
                                             existía como sub-grupo de métodos, sólo se nombra)
Formas de implementar MemoryStore         2 (antes: 1, SQLite hardcodeada en este repo) —
                                             mem (acá, referencia) y la real (agentmemory)
Dependencias de storage en este repo      0 (hoy: 1, modernc.org/sqlite)
```

**4. Dónde vive.** Acá. `agent` es dueño del contrato que sus colaboradores consumen —
exactamente como `storage` es dueño de `storage.Conn` y no de `sqlt`/`postgres`/`indexdb`.

**5. Qué borra.** `memory.go`, `schema.go`, `modernc.org/sqlite` de `go.mod`. Un orquestador
que puede correr en un Worker de 128 MB no puede linkear un motor SQLite completo que nunca
instancia — es la **D** de SOLID verificada en el grafo de build, no en la prosa (`docs/plans/agent.md` punto 2 ya lo argumenta, no se repite acá).

## Cambio 1 — `interfaces.go`: `MemoryStore` se segrega en cuatro contratos

Reemplazá el bloque `type MemoryStore interface { ... }` (los 11 métodos de hoy) por:

```go
// Each contract is what one collaborator of the orchestrator actually needs. See
// docs/MASTER_PLAN.md D7/D8 and docs/plans/agent.md §2 for the argument.
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

// KnowledgeStore's SearchKnowledge takes TEXT, never a vector — the caller (agentmemory)
// owns embedding it. sessionID == "" scopes to global knowledge; a non-empty sessionID must
// see its own session's knowledge PLUS global — never another session's. See conformance
// tests TestKnowledge_GlobalVisibleFromAnySession / TestKnowledge_SessionScopedNotVisibleFromOtherSession.
type KnowledgeStore interface {
	SaveKnowledge(ctx context.Context, sessionID, content, source string) error
	SearchKnowledge(ctx context.Context, query, sessionID string, limit int) ([]Knowledge, error)
}

type ToolLogStore interface {
	LogToolCall(ctx context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
	GetToolLogs(ctx context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

// MemoryStore is the composed contract. Config.Memory keeps this type — structurally
// identical to the old 11-method interface, so no call site changes.
type MemoryStore interface {
	ConversationStore
	EpisodeStore
	KnowledgeStore
	ToolLogStore
}
```

Nada más cambia en `interfaces.go` — `LLMClient`, `MCPServer`, `Tool` se quedan igual.

## Cambio 2 — Borrar `memory.go` y `schema.go`

Los dos archivos desaparecen enteros. No se "mudan" a `agentmemory` como código — la
responsabilidad se muda, pero `agentmemory` la reimplementa sobre `orm`+`ddl` (otro plan,
otro repo, ver `docs/plans/agentmemory.md` cuando exista). Nada de este repo debe seguir
importando `modernc.org/sqlite` o `github.com/google/uuid` **desde estos dos archivos**
(`orchestrator.go` sí sigue usando `uuid` — no lo toques, es deuda separada, ver
`AGENTS.md` "Known debt").

## Cambio 3 — `mem_memory.go`: implementación de referencia, en memoria, sin SQL

`AGENTS.md` ya permite esto explícitamente: *"This repo may contain: the interface, the
value types, a conformance suite, at most an in-memory reference implementation, with no
driver and no SQL."* Esto reemplaza a `NewSQLiteMemory(":memory:")` como backend de
`testMemory` (`setup_test.go`) y como el primer backend que la suite de conformance corre
contra sí misma.

```go
package agent

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
)

// NewMemMemory returns an in-memory MemoryStore with no external dependency — the reference
// implementation any other MemoryStore is checked against via conformance.Run. Not for
// production use: SearchKnowledge does a case-insensitive substring match, not semantic
// search (that requires an embedder, which this package deliberately does not depend on —
// see MASTER_PLAN.md D7/§5). Safe for concurrent use.
func NewMemMemory() MemoryStore {
	return &memMemory{}
}

type memMemory struct {
	mu        sync.Mutex
	sessions  []string // membership only — see note below on why this isn't a map
	messages  []Message
	episodes  []Episode
	knowledge []Knowledge
	toolLogs  []ToolLog
}
```

**Ni `sessions` ni nada más acá es un `map[K]V` — cero excepciones.** `AGENTS.md` prohíbe
`map[K]V` sin salvedad de tamaño ni de uso ("Never: `map[K]V` | Use instead: a slice scanned
linearly"), y el checklist de este mismo plan lo verifica con
`grep -rn "map\[" --include="*.go" . | grep -v _test.go` → vacío. `sessions` es
`[]string`, y un chequeo de membresía es un loop lineal (`for _, s := range sessions { if s
== sessionID { ... } }`) — con la cantidad de sesiones concurrentes que maneja un agente
(decenas, no miles) esto no es un problema de performance, es la misma regla que
`vectordb.Store.hashes`/`docIDByHash` ya aplica (`vectordb/types.go`, comentario "No maps").
Este repo no compila bajo TinyGo hoy (`modernc.org/sqlite` lo bloqueaba — con este plan deja
de bloquearlo), así que la regla empieza a aplicar de verdad acá en cuanto este archivo se
escribe.

Implementá los 11 métodos sobre esos 4+1 slices:

- `EnsureSession`: si `sessionID` no está en `sessions` (loop lineal), agregalo; idempotente.
- `AppendMessage`/`GetMessages`/`DeleteMessages`: filtran `messages` por `SessionID`.
  `GetMessages` devuelve los `limit` más recientes por `CreatedAt` (si `CreatedAt` es 0 para
  todos porque el caller no lo puso, preservá el orden de inserción — no falles).
- `SaveEpisode`/`GetEpisodes`: igual, sobre `episodes`.
- `SaveKnowledge`: agrega a `knowledge` con `ID: uuid.New().String()`, `CreatedAt` si no se
  provee un reloj inyectado usá `0` — este backend no necesita tiempo real para ser correcto,
  sólo orden estable.
- `SearchKnowledge(ctx, query, sessionID, limit)`: filtra `knowledge` donde
  `k.SessionID == "" || k.SessionID == sessionID`, y entre esos, donde
  `strings.Contains(strings.ToLower(k.Content), strings.ToLower(query))`. Sin ese filtro de
  sesión, `TestKnowledge_SessionScopedNotVisibleFromOtherSession` (Cambio 4) falla — es la
  parte no negociable de este método.
- `LogToolCall`/`GetToolLogs`: igual que mensajes, con filtro adicional por `toolName` si no
  es `""`.

## Cambio 4 — `conformance/conformance.go`: la suite que agentmemory tiene que pasar

**Excepción deliberada a "sin subdirectorios" de `AGENTS.md`.** Esa regla es para código de
librería (la lógica del orquestador); `conformance` es un paquete de soporte de testing,
importable desde **otro repositorio** (`agentmemory`), así que no puede ser un archivo
`_test.go` en la raíz — un `_test.go` no se puede importar. `storage/conformance` es el mismo
caso, en el mismo ecosistema: no lo "arregles" fusionándolo a la raíz.

```go
package conformance

import (
	"context"
	"testing"

	"webtyp.com/agent"
)

// Factory builds a fresh, empty agent.MemoryStore for ONE test. Called once per subtest —
// no state bleeds between them.
type Factory struct {
	Name string
	New  func(t *testing.T) agent.MemoryStore
}

func Run(t *testing.T, f Factory) {
	if f.New == nil {
		t.Fatal("conformance: Factory.New is required")
	}
	t.Run("conversation_ensure_session_idempotent", func(t *testing.T) { conversationEnsureSessionIdempotent(t, f) })
	t.Run("conversation_append_and_get_messages", func(t *testing.T) { conversationAppendAndGetMessages(t, f) })
	t.Run("conversation_get_messages_respects_limit", func(t *testing.T) { conversationGetMessagesRespectsLimit(t, f) })
	t.Run("conversation_delete_removes_only_specified", func(t *testing.T) { conversationDeleteRemovesOnlySpecified(t, f) })
	t.Run("conversation_session_isolation", func(t *testing.T) { conversationSessionIsolation(t, f) })
	t.Run("episode_save_and_get", func(t *testing.T) { episodeSaveAndGet(t, f) })
	t.Run("episode_get_respects_limit", func(t *testing.T) { episodeGetRespectsLimit(t, f) })
	t.Run("episode_session_isolation", func(t *testing.T) { episodeSessionIsolation(t, f) })
	t.Run("knowledge_search_finds_exact_content", func(t *testing.T) { knowledgeSearchFindsExactContent(t, f) })
	t.Run("knowledge_global_visible_from_any_session", func(t *testing.T) { knowledgeGlobalVisibleFromAnySession(t, f) })
	t.Run("knowledge_session_scoped_not_visible_from_other_session", func(t *testing.T) { knowledgeSessionScopedNotVisibleFromOtherSession(t, f) })
	t.Run("knowledge_search_respects_limit", func(t *testing.T) { knowledgeSearchRespectsLimit(t, f) })
	t.Run("toollog_log_and_get", func(t *testing.T) { toolLogLogAndGet(t, f) })
	t.Run("toollog_filter_by_tool_name", func(t *testing.T) { toolLogFilterByToolName(t, f) })
	t.Run("toollog_session_isolation", func(t *testing.T) { toolLogSessionIsolation(t, f) })
}
```

Cada función (`conversationEnsureSessionIdempotent`, etc.) es un test normal que recibe
`(t *testing.T, f Factory)`, llama `mem := f.New(t)`, y hace las llamadas + asserts. Dos
ejemplos completos — el resto sigue el mismo molde, adaptado al método que testea:

```go
func conversationSessionIsolation(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.EnsureSession(ctx, "session-a"); err != nil {
		t.Fatalf("EnsureSession(a): %v", err)
	}
	if err := mem.EnsureSession(ctx, "session-b"); err != nil {
		t.Fatalf("EnsureSession(b): %v", err)
	}
	if err := mem.AppendMessage(ctx, "session-a", agent.Message{ID: "m1", SessionID: "session-a", Role: "user", Content: "hello from a"}); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	gotB, err := mem.GetMessages(ctx, "session-b", 10)
	if err != nil {
		t.Fatalf("GetMessages(b): %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("session-b sees %d messages from session-a, want 0", len(gotB))
	}
}

func knowledgeSessionScopedNotVisibleFromOtherSession(t *testing.T, f Factory) {
	ctx := context.Background()
	mem := f.New(t)

	if err := mem.SaveKnowledge(ctx, "session-a", "the secret project codename is condor", "agent"); err != nil {
		t.Fatalf("SaveKnowledge: %v", err)
	}

	results, err := mem.SearchKnowledge(ctx, "condor", "session-b", 10)
	if err != nil {
		t.Fatalf("SearchKnowledge: %v", err)
	}
	for _, r := range results {
		if r.Content == "the secret project codename is condor" {
			t.Fatalf("session-b's search found session-a's private knowledge")
		}
	}
}
```

Escribí las 13 restantes con el mismo nivel de precisión — cada una asertando UNA propiedad
del contrato, con nombres de sesión/contenido que dejan claro qué se está aislando. No
testees implementación (por ejemplo, no asumas orden de slice interno) — sólo el contrato
público.

## Cambio 5 — `mem_memory_test.go`: `mem` corre su propia suite

```go
package agent

import (
	"testing"

	"webtyp.com/agent/conformance"
)

func TestMemMemoryConformance(t *testing.T) {
	conformance.Run(t, conformance.Factory{
		Name: "mem",
		New:  func(t *testing.T) MemoryStore { return NewMemMemory() },
	})
}
```

Import cycle: `conformance` importa `agent`, y `agent`'s test binary importa `conformance` —
eso es válido en Go (el ciclo lo rompe el hecho de que `_test.go` compila en un binario de
test separado, no en el paquete `agent` en sí). Si `go vet`/`go build` se quejaran de un
ciclo real, es señal de que `conformance` terminó importado desde un archivo NO `_test.go` de
`agent` — no debería pasar si seguiste este plan tal cual.

## Cambio 6 — `setup_test.go` e `integration_test.go`: apuntar a `mem`

`NewMemMemory` (Cambio 3) no devuelve `error` — cada call site pierde su chequeo de error,
no lo reemplaces por un `_`.

**`setup_test.go`**, dentro de `TestMain` — hoy:

```go
var err error
testMemory, err = NewSQLiteMemory(":memory:")
if err != nil {
	panic(err)
}
```

pasa a:

```go
testMemory = NewMemMemory()
```

(el `var err error` de arriba puede seguir existiendo si el resto de `TestMain` lo usa para
otra cosa — chequealo, no lo borres a ciegas).

**`integration_test.go`**, dos call sites — el primero (hoy con chequeo de error):

```go
mem, err := NewSQLiteMemory(":memory:")
if err != nil {
	t.Fatalf("failed to create memory: %v", err)
}
```

pasa a:

```go
mem := NewMemMemory()
```

El segundo (hoy descarta el error):

```go
mem, _ := NewSQLiteMemory(":memory:")
```

pasa a:

```go
mem := NewMemMemory()
```

`memory_test.go` se borra entero (Cambio 2 ya lo cubre, repetido acá para que no se pierda:
sus tests eran de la implementación SQLite que ya no existe).

## Cambio 7 — `go.mod`: sale `modernc.org/sqlite`

Borrá la línea `modernc.org/sqlite v1.45.0` del bloque `require` y corré `go mod tidy` —
eso también se lleva sus dependencias indirectas (`modernc.org/libc`, `modernc.org/mathutil`,
`modernc.org/memory`, `github.com/ncruces/go-strftime`, `github.com/remyoudompheng/bigfft`,
`golang.org/x/exp`, `golang.org/x/sys` — verificá con `go mod tidy` cuáles quedan huérfanas
en vez de borrarlas a mano, alguna puede seguir siendo necesaria por otro import).
`github.com/google/uuid` **se queda** — lo sigue usando `orchestrator.go`.

## Cambio 8 — `docs/DEFAULT_LLM_SKILL.md`

Tres correcciones puntuales:

1. La tabla de "Production Code (v1)" en "Allowed External Dependencies" — borrá la línea
   `modernc.org/sqlite — pure Go SQLite driver...`. No agregues nada en su lugar.
2. La lista de propósito de archivos en "Single Responsibility Principle" —
   `memory.go — SQLite MemoryStore implementation only` pasa a
   `mem_memory.go — in-memory reference MemoryStore only (no SQL, no driver)`.
3. El bloque de ejemplo de `setup_test.go` en "Shared Setup" — reemplazá
   `testMemory = agent.NewSQLiteMemory(":memory:")` por `testMemory = agent.NewMemMemory()`,
   y el comentario `// SQLite :memory: — no disk writes...` por uno que describa el backend
   `mem` (sin disco, sin dependencias, igual de rápido).

## Tests

Además de la suite de conformance (Cambio 4/5) y los tests existentes que ya cubren FSM/ReAct/
contexto (no los toques, siguen pasando tal cual — la matriz DDT de
`docs/DEFAULT_LLM_SKILL.md` §2 no cambia, esto no toca ningún diagrama):

| Test | Verifica |
|---|---|
| `TestMemMemoryConformance` | `mem` pasa las 15 subpruebas de `conformance.Run` |
| Los 3 tests nuevos de aislamiento citados arriba, dentro de la suite | session isolation en Conversation/Episode/Knowledge/ToolLog |

## Checklist de aceptación

```bash
go vet ./...
gotest
GOOS=js GOARCH=wasm go build ./...
tinygo build -target wasm -o /dev/null .
go list -m all | grep -i sqlite   # → vacío — la puerta adicional de MASTER_PLAN.md fase 4
grep -rn "map\[" --include="*.go" . | grep -v _test.go   # → vacío
```

`tinygo build -target wasm -o /dev/null .` **tiene que pasar por primera vez en este repo** —
hoy falla porque `memory.go` importa `modernc.org/sqlite`, que no compila bajo TinyGo. Si
sigue fallando después de este plan, algo del Cambio 2/7 quedó a medias — no es un problema
pre-existente a ignorar, es la puerta que este plan existe para abrir (D8, MASTER_PLAN.md).
