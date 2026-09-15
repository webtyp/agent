# Plan: Eliminar dependencia de mcpserve, usar tinywasm/mcp directamente

## Contexto

`mcpserve` fue reemplazado completamente por `tinywasm/mcp` y debe ser archivado.
`agent` lo usa únicamente en `setup_test.go` para levantar un servidor MCP en tests de integración.
`mcpserve@v0.0.31` ya no compila porque usa tipos removidos de `mcp` (`CallToolRequest`, `CallToolResult`, `NewMCPServer`, etc.).

## Objetivo

Reemplazar el uso de `mcpserve` en `setup_test.go` con un servidor HTTP mínimo que use
`mcp.Server.HandleMessage` directamente, usando `net/http/httptest`.

## Cambios requeridos

### 1. `setup_test.go` — reescribir sin mcpserve

Reemplazar `*mcpserve.Handler` por `*httptest.Server` wrapeando `mcp.Server`:

```go
package agent

import (
    stdfmt "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "os"
    "testing"

    "github.com/tinywasm/context"
    tinyfmt "github.com/tinywasm/fmt"
    "github.com/tinywasm/json"
    "github.com/tinywasm/mcp"
)

var testMemory MemoryStore
var testServer *httptest.Server

type testToolProvider struct{}

func (p testToolProvider) Tools() []mcp.Tool {
    return []mcp.Tool{
        {
            Name:        "calculator",
            Description: "Calculates sum of two numbers",
            InputSchema: `{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`,
            Resource:    "calculator",
            Action:      'r',
            Execute: func(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
                // parse a and b from req.Params.Arguments (JSON string)
                var args struct {
                    A float64 `json:"a"`
                    B float64 `json:"b"`
                }
                json.Decode([]byte(req.Params.Arguments), &args)
                return mcp.Text(stdfmt.Sprintf("%v", args.A+args.B)), nil
            },
        },
    }
}

func TestMain(m *testing.M) {
    var err error
    testMemory, err = NewSQLiteMemory(":memory:")
    if err != nil {
        panic(err)
    }

    srv, err := mcp.NewServer(
        mcp.Config{Name: "test", Version: "1.0.0", Auth: mcp.OpenAuthorizer()},
        []mcp.ToolProvider{testToolProvider{}},
    )
    if err != nil {
        panic(err)
    }

    testServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        var ctx context.Context
        body, _ := io.ReadAll(r.Body)
        resp := srv.HandleMessage(&ctx, body)
        w.Header().Set("Content-Type", "application/json")
        var out []byte
        if enc, ok := resp.(tinyfmt.Encodable); ok {
            json.Encode(enc, &out)
        }
        w.Write(out)
    }))

    code := m.Run()
    testServer.Close()
    os.Exit(code)
}
```

### 2. `orchestrator_test.go` — reemplazar `testHandler.URL()` con `testServer.URL`

```
grep -n "testHandler.URL()" orchestrator_test.go
# Reemplazar todas las ocurrencias con testServer.URL
```

### 3. `go.mod` — remover mcpserve, agregar mcp como directo

```
go get github.com/tinywasm/mcp@latest
go mod tidy
```

Verificar que `mcpserve` desaparece del `go.mod` y `go.sum`.

## Problema pendiente de investigar

El agente llama `tools/list` directamente sin inicializar la sesión MCP primero.
El protocolo MCP requiere `initialize` antes de `tools/list`.
Opciones:
- A) El agente debe enviar `initialize` antes de `tools/list` en `addMCPClient` (fix en `mcp_registry.go`)
- B) El servidor `mcp.HandleMessage` debe tolerar `tools/list` sin `initialize` previo

Verificar cuál aplica examinando el comportamiento de `mcp.Server.HandleMessage` con `tools/list`
sin `initialize` previo (con `OpenAuthorizer` el auth pasa, pero puede haber otro guard).

## Comandos de validación

```bash
go vet ./...
gotest
gopush 'fix: replace mcpserve with mcp.Server in tests'
```
