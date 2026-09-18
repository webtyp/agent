---
PLAN: "chore: migrar a webtyp.com y devolver los tests a verde"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
---

> Índice maestro: [`docs/MASTER_PLAN.md`](MASTER_PLAN.md). El alcance completo de este
> repositorio está en [`docs/plans/agent.md`](plans/agent.md); **este plan ejecuta solo su
> punto 1 y sus consecuencias mecánicas.** Todo lo que necesita el repositorio
> `webtyp/agentmemory` —sacar `memory.go`, el esquema por `ddl`, el camino semántico de
> `SearchKnowledge`— queda explícitamente fuera y se despacha después.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus
> comentarios en inglés, como el resto del código fuente.

# Plan — `agent` compila sus tests otra vez, y vive en `webtyp.com`

## Estado de partida, verificado

```
go build ./...   → OK
go vet ./...     → FALLA
```

El código de producción compila. **El test suite no**, y no por culpa de este repositorio:

```
# github.com/tinywasm/mcpserve
.../mcpserve@v0.0.31/executor.go:23: undefined: mcp.CallToolRequest
.../mcpserve@v0.0.31/handler.go:165: undefined: mcp.NewMCPServer
```

`mcpserve@v0.0.31` usa tipos que `mcp` ya removió. Se usa **en un solo archivo**,
`setup_test.go`, para levantar un servidor MCP en los tests de integración.

Ese es el orden obligatorio de este plan: **mientras `mcpserve` esté, no se puede verificar
nada.** La migración va después, no antes.

## 1. Sacar `mcpserve` de `setup_test.go`

Reemplazar el `*mcpserve.Handler` por un `*httptest.Server` que envuelva
`mcp.Server.HandleMessage` directamente. `webtyp.com/mcp` ya expone todo lo necesario:

```
mcp.Server              server.go
Server.HandleMessage    request_handler.go:13 — func (s *Server) HandleMessage(ctx *context.Context, message []byte) JSONRPCMessage
mcp.Tool                tools.go:17
mcp.Request             tools.go:9
mcp.Text(string)        tools.go:53 — devuelve *Result
```

**`docs/LAST_PLAN_EXECUTED.md` tiene un diseño para esto y está DESACTUALIZADO.** Sirve como
referencia de la forma general (httptest envolviendo `HandleMessage`), pero su código de
ejemplo no compila contra el `mcp` de hoy. Estas son las diferencias, verificadas:

| Ese diseño dice | El `mcp` actual tiene |
|---|---|
| `InputSchema: "{...json schema...}"` | **no existe ese campo** — hay `Args model.Fielder`, un modelo generado por ormc; `nil` = sin argumentos |
| `Action: 'r'` (byte) | `Action model.Action`, un tipo, no un carácter |
| `Resource: "calculator"` (string) | `Resource model.Resource`, un tipo |
| — | campo nuevo **`Access model.Access`** |

**El campo `Access` es el que te va a morder.** Su valor cero es `model.AccessGuarded`, que
exige identidad **y** permiso sobre el `Resource`. Un tool de test con `Access` sin setear
es rechazado por `AddTool` con *"is guarded but declares no Resource"*. Para un test de
integración sin autenticación, el tool debe declarar:

```go
Access: model.AccessPublic,   // no identity, no permission — this is a test fixture
```

y entonces `Resource` y `Action` deben quedar **en cero** (`AddTool` los rechaza si
`Access` no es `AccessGuarded`; ver `mcp/server.go` §AddTool).

El tool de prueba puede ser el mismo `calculator` que había: recibe dos números, devuelve la
suma con `mcp.Text(...)`. Si `Args model.Fielder` te obliga a un modelo generado por ormc y
eso es desproporcionado para un fixture, usá `Args: nil` y parseá
`req.Params.Arguments` a mano con `webtyp.com/json` — es un test, no una API pública.

**Verificá las firmas con `go doc` antes de escribir**, no confíes en esta tabla ni en el
documento viejo:

```bash
go doc webtyp.com/mcp Tool
go doc webtyp.com/mcp Server.HandleMessage
go doc webtyp.com/model Access
```

Cuando termine, `docs/LAST_PLAN_EXECUTED.md` se **borra**: su contenido ya fue ejecutado y
dejarlo es exactamente la deuda de "dos documentos diciendo lo mismo, uno desactualizado".

## 2. Migrar el module path

Solo después de que `go vet ./...` pase.

```
module github.com/tinywasm/agent   →   module webtyp.com/agent
```

Ocho sitios de import en código (`agent.go`, `orchestrator.go`, `mcp_client.go`,
`context_window.go`, `mcp_registry.go`, `fsm.go`, `memory.go`, más `setup_test.go` que
para entonces ya no importará `mcpserve`), y doce dependencias en `go.mod`:

| Directa | Indirecta |
|---|---|
| `fmt`, `mcpserve` (se va en §1) | `context`, `dom`, `fetch`, `form`, `html`, `json`, `mcp`, `sse`, `time`, `unixid` |

Todas existen ya bajo `webtyp.com/` —verificado— salvo `mcpserve`, que no existe y no debe
existir: desaparece en §1.

No hagas un `sed` ciego sobre todo el repositorio: `docs/` menciona `tinywasm` en prosa
histórica (`ARCHITECTURE.md`, `DEFAULT_LLM_SKILL.md`, `history/`) y esas menciones cuentan
lo que el proyecto **era**. Migrá `go.mod` y los `.go`; la prosa se trata en §4.

Después: `go mod tidy`, y las versiones de cada dependencia son las últimas publicadas
bajo `webtyp.com` (no las de `github.com/tinywasm`, que están congeladas).

## 3. `Message.TokenCount` tiene tres significados

Hoy el campo se llena de tres formas distintas y `context_window.go` las suma como si
fueran la misma unidad:

| Dónde | Qué guarda |
|---|---|
| `orchestrator.go:25` | `len(userQuery) / 4` — estimación por caracteres |
| `orchestrator.go:89` | `resp.TokensUsed` — el total del **turno entero**, no de ese mensaje. El comentario del propio código dice `// Approximation?` |
| `orchestrator.go:141` | `len(output) / 4` — estimación otra vez |
| `orchestrator.go:179` | `resp.TokensUsed` — mismo problema que :89 |

`prepareContext` suma eso y lo compara contra `MaxTokens × SummarizeAt` para decidir cuándo
resumir. Es decir: **la decisión de resumir se toma sobre una suma de unidades
incompatibles**, y el error crece con la cantidad de turnos porque `TokensUsed` es
acumulativo y se guarda una vez por turno.

**La decisión ya está tomada** (`plans/agent.md` §1.8) y esto es su ejecución:
`Message.TokenCount` significa **los tokens de ese mensaje y nada más**. Quien no pueda
saberlo deja **cero**, no una estimación de otra unidad. Un cero honesto es detectable; un
número inventado no.

Documentá el campo en `types.go` con esa frase. No inventes un tokenizador — no es el
alcance de este plan.

## 4. Prosa y metadatos

- `README.md` dice *"Autonomous AI Agent system for `tinywasm`"*. Es `webtyp` ahora.
- `docs/DEFAULT_LLM_SKILL.md` §1 lista las dependencias permitidas con los paths
  `github.com/tinywasm/*`: actualizalos. **`modernc.org/sqlite` se queda por ahora** — sale
  cuando `memory.go` se mude a `agentmemory`, que es otro plan. No lo saques acá.
- `docs/DEFAULT_LLM_SKILL.md` §1 también dice *"`memory.go` — SQLite MemoryStore
  implementation only"*. Sigue siendo cierto hoy; no lo toques.
- Borrar `docs/LAST_PLAN_EXECUTED.md` (§1).

## Lo que este plan NO hace

Para que no haya ambigüedad sobre el alcance:

- **No** saca `memory.go` ni `schema.go` a `webtyp/agentmemory` — ese repositorio existe
  pero está vacío, y su plan no está escrito.
- **No** segrega `MemoryStore` en cuatro contratos (`plans/agent.md` §2).
- **No** escribe la suite de conformance de `MemoryStore`.
- **No** toca `SearchKnowledge` ni agrega dependencia a `vectordb` o `embed`.
- **No** saca `modernc.org/sqlite`.

Los cinco son el despacho siguiente, y dependen de que este cierre.

## Checklist de aceptación

```bash
go build ./...
go vet ./...            # esto es lo que hoy falla; tiene que pasar
gotest
grep -rn "tinywasm" --include="*.go" .        # → vacío
grep -rn "mcpserve" .                          # → vacío
head -1 go.mod                                 # → module webtyp.com/agent
ls docs/LAST_PLAN_EXECUTED.md                  # → no existe
```

Los tests de `integration_test.go` llevan `//go:build integration` y necesitan Ollama con
`qwen2.5:7b` corriendo (`docs/OLLAMA_TEST.md`). **No son parte de esta puerta**: si no
tenés Ollama, `gotest` los saltea y está bien. Lo que no puede fallar es `go vet ./...`.

Después liberar:

```bash
gopush 'chore: migrate to webtyp.com and restore a compiling test suite'
```
