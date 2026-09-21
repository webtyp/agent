---
PLAN: "fix: agent — sacar context/time/net-http/encoding-json/uuid/map[K]V de stdlib, la razón de fondo por la que este repo existe (D8, browser primero)"
TAG: v0.5.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 5537164840897341207
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/MASTER_PLAN.md — D8.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus comentarios
> en inglés.

# Plan — `webtyp/agent` deja de arrastrar la stdlib que el navegador no soporta

## Por qué este plan existe, y por qué es uno solo y no varios

Barrido completo del repo (no una muestra): **8 archivos** importan `context` de stdlib,
**5** importan `time` de stdlib, **1** (`mcp_client.go`) importa `net/http`, **2**
(`mcp_client.go`, `mcp_registry.go`) importan `encoding/json`, **1** (`orchestrator.go`)
importa `github.com/google/uuid`, **2** (`fsm.go`, `mcp_registry.go`) usan `map[K]V`, y hay
**11 archivos de test en la raíz** (`AGENTS.md` dice: más de 5 → todos a `tests/`).

Esto no es deuda nueva — es la deuda que `AGENTS.md` ya documenta ("Known debt") y que los
planes anteriores (incluido el mío, el de segregar `MemoryStore`) siguieron tratando como
"aparte" en vez de cerrarla. **El repo existe para correr en un navegador vía TinyGo — no es
un detalle, es D8 del índice maestro.** Cada import de esta lista compila bajo
`GOOS=js GOARCH=wasm go build` (que usa la stdlib completa) y **no** bajo TinyGo, así que
`gotest` en verde no prueba nada mientras esto no se corrija. Va todo junto porque son la
misma causa (código escrito antes de que este repo tuviera que correr en el navegador) y
porque tocan los mismos archivos varias veces si se hacen por separado.

## Design gate

**1. Prior art.** `webtyp.com/mcp` — repo hermano, mismo ecosistema, ya resuelto: sus
handlers (`server.go`) usan `*context.Context` (el de `webtyp.com/context`) en toda su
superficie pública y **no tienen `Done()`/cancelación en absoluto**. Su `Client`
(`mcp/client.go`) ya hace exactamente lo que `agent/mcp_client.go` reimplementa mal: HTTP vía
`webtyp.com/fetch`, JSON vía `webtyp.com/json`, callback en vez de bloqueo.

**2. Novice-name test.** No hay nombres nuevos — cada símbolo que este plan agrega
(`MCPTimeoutMS`, `NewMemMemory(idGen)`, etc.) ya sigue el patrón que el resto del repo usa
para lo mismo (`Config.IDGen`, sufijo `MS` para milisegundos en vez de `time.Duration`).

**3. Complexity ledger.**
```
Conceptos nuevos                          0 — se borra código, no se agrega
Dependencias de stdlib incompatibles con
  TinyGo, en este repo                    6 → 0 (context, time, net/http, encoding/json,
                                               google/uuid, y el map[K]V que no es un import
                                               pero es la misma categoría de "no compila
                                               liviano bajo TinyGo")
```

**4. Dónde vive.** Acá — es limpieza interna, no cambia ningún contrato que otro repo
consuma (excepto `Config.MCPTimeout`, ver Cambio 3, que es la única ruptura real y está
acotada).

**5. Qué borra.** `mcp_client.go`'s `HTTPMCPClient` completo (se reemplaza por un envoltorio
de `mcp.Client`), los dos `map[K]V` de `fsm.go`/`mcp_registry.go`, y la sección "Known debt"
de `AGENTS.md` correspondiente a estos 5 puntos — una vez hecho, se borra de esa tabla porque
deja de ser deuda.

## Cambio 1 — `context` de stdlib → `webtyp.com/context`, en los 8 archivos

Los 8: `interfaces.go`, `agent.go`, `orchestrator.go`, `mcp_registry.go`, `mcp_client.go`,
`context_window.go`, `mem_memory.go`, `conformance/conformance.go`. Mecánico en la firma —
`ctx context.Context` (stdlib) pasa a `ctx *context.Context` (`webtyp.com/context`, **puntero**,
no valor — así lo usa `mcp` en todos sus métodos) — **salvo un lugar real**:

**`orchestrator.go`, el `select` con `ctx.Done()`/`ctx.Err()`.** `webtyp.com/context.Context`
no tiene `Done()`, `Err()`, `Deadline()` ni equivalente — es una bolsa de valores string, no
un primitivo de cancelación (`context/context.go`, verificado: `Background`, `WithValue`,
`Set`, `Value`, `Keys`, nada más). **Esto no es una laguna a rellenar: es la forma que ya
tiene el ecosistema** — `webtyp.com/mcp` expone handlers con `ctx *context.Context` y **cero**
cancelación cooperativa en su firma pública. Verificado también que **no hay ningún llamador
real** de `Agent.Run` en este repo (`grep -rn "\.Run(ctx" --include="*.go" .` fuera de
`_test.go` da vacío) — la cancelación de hoy es una capacidad de la que nadie depende, ni
siquiera en test. **Borrá el `select`/`case <-ctx.Done()` de `orchestrator.go` sin
reemplazo** — el loop corre hasta terminar, como ya corren los handlers de `mcp`.

## Cambio 2 — `time` de stdlib → `webtyp.com/time`, en los 5 archivos

`agent.go`, `orchestrator.go`, `mcp_client.go`, `context_window.go`, `types.go`.
`webtyp.com/time` no tiene un tipo `Duration` ni `time.Now() time.Time` — tiene
`Now() int64` (unix nanos) y `AfterFunc(milliseconds int, f func()) Timer` (`time/api.go`,
verificado). Dos usos distintos en el repo hoy, dos arreglos distintos:

- **Timestamps** (`CreatedAt: time.Now().Unix()` en `orchestrator.go`, y similares) →
  `webtyp.com/time.Now()` ya devuelve nanos; si el campo es unix **segundos** (`CreatedAt
  int64` en `types.go`, documentado `// unixepoch`), dividí por `1e9` en vez de llamar
  `.Unix()` (que no existe en este paquete).
- **El timeout de MCP** (`agent.go`, `context.WithTimeout(context.Background(),
  cfg.MCPTimeout)`) → no hay equivalente directo a `context.WithTimeout` en
  `webtyp.com/time` ni en `webtyp.com/context` (ninguno de los dos tiene cancelación, Cambio
  1). El timeout se mueve al **punto de la llamada HTTP** (Cambio 3), con
  `time.AfterFunc` + un channel — no a nivel de `context`.

## Cambio 3 — `mcp_client.go`: borrar `HTTPMCPClient`, envolver `mcp.Client`

`net/http` y `encoding/json` de este archivo desaparecen enteros porque **ya existe**
`webtyp.com/mcp.Client` (`mcp/client.go`) que hace lo mismo (HTTP vía `webtyp.com/fetch`,
JSON vía `webtyp.com/json`) — reimplementarlo es la regla de `AGENTS.md` "No se inventa un
puerto que ya existe", violada al revés (un cliente HTTP, no una interfaz).

`mcp.Client.Call(ctx *context.Context, method string, params any, callback func([]byte,
error))` es **callback**, no bloqueante — correcto para el target navegador, donde un fetch
nunca bloquea un hilo. El `mcpCaller` de `agent` (`mcpCaller.Call(ctx, method, params)
(json.RawMessage, error)`) es bloqueante — así que el envoltorio bloquea con un channel, lo
que funciona igual bajo Go nativo (goroutines reales) y bajo TinyGo/wasm (su scheduler
cooperativo soporta channels — es el mismo puente callback→bloqueo que cualquier fetch
envuelto en una goroutine ya necesita):

```go
package agent

import (
	"webtyp.com/context"
	"webtyp.com/fmt"
	"webtyp.com/mcp"
	"webtyp.com/time"
)

// HTTPMCPClient adapts mcp.Client's callback-based Call to mcpCaller's blocking shape,
// with an explicit millisecond timeout (webtyp.com/time.AfterFunc) instead of a cancelable
// context — see docs/PLAN.md Cambio 1/2 for why context can't carry this here.
type HTTPMCPClient struct {
	client    *mcp.Client
	timeoutMS int
}

func NewHTTPMCPClient(url string, timeoutMS int) *HTTPMCPClient {
	return &HTTPMCPClient{client: mcp.NewClient(url, ""), timeoutMS: timeoutMS}
}

func (c *HTTPMCPClient) Call(ctx *context.Context, method string, params any) ([]byte, error) {
	type result struct {
		body []byte
		err  error
	}
	done := make(chan result, 1)
	c.client.Call(ctx, method, params, func(body []byte, err error) {
		done <- result{body, err}
	})

	timedOut := make(chan struct{})
	timer := time.AfterFunc(c.timeoutMS, func() { close(timedOut) })

	select {
	case r := <-done:
		timer.Stop()
		return r.body, r.err
	case <-timedOut:
		return nil, fmt.Err("mcp: call to ", method, " timed out")
	}
}
```

`Timer.Stop() bool` está confirmado (`time/api.go`) — el código de arriba lo usa tal cual. El
tipo de retorno de `Call` cambia de
`json.RawMessage` a `[]byte` — son el mismo tipo subyacente (`json.RawMessage` es
`type RawMessage []byte`), así que **`mcpCaller` pierde su import de `encoding/json`** con
este cambio: actualizá la interfaz en `mcp_registry.go` (Cambio 4) a la vez.

Borrá `jsonRPCRequest`, `jsonRPCResponse`, `rpcError`, `callToolResult` — `mcp.Client` ya
serializa el request, y `callToolResult` lo reemplaza `mcp.ParseResult(raw []byte) (*mcp.Result,
error)` (`mcp/utils.go`, ya expone `Content`/`IsError`) en el call site de "tools/call"
(Cambio 4). `listToolsResult` **no** tiene equivalente exportado en `mcp` — se queda, pero
migra de `encoding/json` a `webtyp.com/json` (Cambio 4).

## Cambio 4 — `mcp_registry.go`: sin `map[K]V`, sin `encoding/json`

**Los dos maps** (`localTools map[string]Tool`, `mcpTools map[string]mcpToolEntry`) pasan a
slices (`[]Tool`, `[]mcpToolEntry`, cada entry con su nombre) recorridos linealmente —
mismo patrón que `vectordb/types.go` (`findShardByID`) y el `containsString` que
`mem_memory.go` ya tiene en este mismo repo. Con la cantidad de tools que un agente registra
(decenas, no miles) un scan lineal no es un problema de performance.

**`listToolsResult` de `encoding/json` a `webtyp.com/json`.** Es una decodificación de dos
pasos porque el array de tools no tiene un lugar natural en el `Fielder` orientado a campos
escalares (`Pointers() []any`) — **copiá el patrón exacto que `webtyp.com/mcp` ya usa para su
propio `listToolsResult`** (`mcp/model_orm.go` línea ~429, no lo reinventes): el campo
`Tools` se decodifica como **string crudo** (`r.Raw("tools")` / `w.Raw("tools", ...)`, no como
slice), y ESE string se vuelve a pasar por `json.Decode` contra un tipo `toolEntry` nuevo
(`Name`, `Description`, `InputSchema` — mismo layout que el `struct` anónimo que
`mcp_client.go` borra) usando el mismo mecanismo de lista (`toolEntryList` con
`Len/At/Append/IsNil` — `mcp/model_orm.go` también tiene un `toolEntry`/`toolEntryList` que
podés copiar como plantilla, ajustando los nombres de campo). Escribí `listToolsResult` y
`toolEntry`/`toolEntryList` a mano en un archivo nuevo, `mcp_json.go` — no dependen de
`ormc`/`model.Definition`, son tipos de protocolo, no de base de datos.

**`resRaw, err := client.Call(ctx, "tools/call", params)` → decodificá con
`mcp.ParseResult(resRaw)`** en vez del `callToolResult` local que Cambio 3 borra.

## Cambio 5 — `fsm.go`: sin `map[K]V`

`allowed` es una tabla fija de 5 estados conocidos en compile-time — ni slice-scan hace
falta, un `switch` sobre `f.current` devolviendo el slice de destinos permitidos es más
simple que mantener un array paralelo a `State`:

```go
func allowedFrom(s State) []State {
	switch s {
	case StateIdle:
		return []State{StateReasoning}
	case StateReasoning:
		return []State{StateActing, StateReflecting, StateResponding}
	case StateActing:
		return []State{StateReasoning, StateResponding}
	case StateReflecting:
		return []State{StateReasoning, StateResponding}
	case StateResponding:
		return []State{StateIdle}
	default:
		return nil
	}
}
```

`transition` cambia `allowed[f.current]` por `allowedFrom(f.current)`. Borrá la variable
`allowed`.

## Cambio 6 — `orchestrator.go`: `uuid.New()` → `model.IDGenerator` inyectado

Mismo argumento que ya se aplicó en `mem_memory.go` (esta misma ola): `model.IDGenerator`'s
propio comentario prohíbe construir un generador concreto adentro de un módulo reusable.
`Config` gana un campo:

```go
IDGen model.IDGenerator // required
```

`New(cfg Config)` valida `cfg.IDGen != nil` junto a los otros campos requeridos (`LLMs.Primary`,
`Memory`). `orchestrator.go` reemplaza cada `uuid.New().String()` por `a.cfg.IDGen.NewID()` (o
el campo que ya tenga acceso al `Agent` en ese método — mirá cómo `a` llega a cada uno de los
5 call sites, algunos pueden estar en un método distinto sin `a.cfg` a mano; si es así, guardá
`idGen` como campo propio de `Agent`, no sólo en `Config`). Borrá el import de
`github.com/google/uuid` de `orchestrator.go` y de `go.mod`.

**Actualizá todo caller de `Config{}` en tests** (`setup_test.go`, `orchestrator_test.go`,
`integration_test.go`, y cualquier otro) para pasar `IDGen: <un unixid.NewUnixID() o un
generador determinístico de test>`.

## Cambio 7 — `Config.MCPTimeout`: de `time.Duration` a milisegundos

```go
MCPTimeoutMS int // default: 30000 (30s)
```

`agent.go`'s default (`if cfg.MCPTimeout == 0 { cfg.MCPTimeout = 30 * time.Second }`) pasa a
`if cfg.MCPTimeoutMS == 0 { cfg.MCPTimeoutMS = 30000 }`. Este es el único campo público que
cambia de forma — si algo fuera de este repo ya lo consume (no hay evidencia de que exista
ese consumidor todavía, `agentmemory`/`embed` no lo tocan), avisá en el PR, no lo asumas
silenciosamente resuelto.

## Cambio 8 — Tests: la excepción documentada, no una decisión en silencio

`AGENTS.md`: "más de 5 archivos de test en la raíz → todos a `tests/`, package `tests`,
consumiendo sólo la API pública." Hoy son 11, y **9 de los 11 son white-box** (`package
agent`, no `agent_test`) — testean `fsm`, `mcpRegistry`, `mcpCaller`, campos no exportados.
Moverlos a `tests/` tal cual violaría la regla al revés: para que compilen ahí, `fsm`,
`mcpRegistry` y sus métodos tendrían que exportarse **sólo para que el test los alcance** —
exactamente el antipatrón de "exponer internals por testeabilidad" que ninguna guía de este
ecosistema pide.

**Esto es una decisión, no una omisión, y se registra en el `AGENTS.md` de este repo** (no
se deja implícita): agregá una sección "Test layout — excepción documentada" en `AGENTS.md`
que diga, en dos líneas, lo que este párrafo dice — que los tests white-box de FSM/registry
se quedan en la raíz porque mover-sin-exportar es imposible y exportar-para-testear es peor,
y que **cualquier test nuevo que sólo necesite la API pública** (como
`mem_memory_test.go`, ya en `package agent_test`) va en `agent_test` o `tests/`, nunca en
`package agent`. No muevas los 9 archivos white-box — dejalos donde están, con la excepción
ahora escrita en vez de callada.

## Checklist de aceptación

```bash
go vet ./...
gotest
gotest -tinygo
GOOS=js GOARCH=wasm go build ./...
tinygo build -target wasm -o /dev/null .
go list -m all | grep -iE "sqlite|google/uuid"    # → vacío
grep -rn '"context"\|"time"\|"net/http"\|"encoding/json"' --include="*.go" . | grep -v _test.go   # → vacío
grep -rn "map\[" --include="*.go" . | grep -v _test.go                                            # → vacío
```

`tinygo build -target wasm -o /dev/null .` — **ésta es la puerta real de este plan.** No pasó
nunca en este repo. Si sigue sin pasar después de estos 8 cambios, algo quedó a medias — no es
un problema pre-existente a documentar y seguir de largo, es la razón por la que este plan
existe.
