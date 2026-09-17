---
PLAN: "feat: webtyp/webgpu — bindings de navigator.gpu para Go/WASM"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/webgpu (por crear)
---

> Repositorio nuevo. Se mueve a `webgpu/docs/PLAN.md` cuando el repositorio exista.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/PLAN.md
> **Fase 5.** No arrancar antes de que la fase 3 esté entregada — el producto funciona sin esto.

# Plan — `webtyp/webgpu`

## Responsabilidad única

Acceso tipado desde Go a la API de cómputo WebGPU del navegador. Obtención de adapter y
device, buffers, módulos de shader, bind groups, pipelines de cómputo, dispatch y lectura
de resultados.

No sabe nada de machine learning. Sin tensores, sin capas, sin atención — eso es
`webtyp/nn`. Este paquete podría servir igual para procesamiento de imágenes o física, y
mantenerlo así es lo que evita que los bugs de ciclo de vida de GPU y los bugs de modelo se
enreden.

Solo cómputo: sin render pipelines, sin swap chains, sin canvas. El renderizado es otra
preocupación y nada en este proyecto lo necesita.

## Advertencia de alcance

Esta es la pieza más grande y más incierta de todo el esfuerzo, y va deliberadamente
última. El embedder estático de la fase 3 hace funcionar la búsqueda semántica sin una sola
línea de código de GPU. Si este repositorio se traba, el producto no.

Dos restricciones moldean todo lo de abajo:

1. **Todo en WebGPU es asíncrono y basado en promesas.** `requestAdapter`,
   `requestDevice`, `mapAsync` y `onSubmittedWorkDone` son todas promesas.
   `webtyp.com/await` maneja una promesa de un solo disparo, y esa es la primitiva
   correcta — pero bajo TinyGo wasm, una goroutine bloqueada en un canal mientras el event
   loop de JS tiene que correr para resolver la promesa es exactamente la forma de deadlock
   contra la que ya advierte el `docs/LAST_PLAN_EXECUTED.md` de `indexdb`. **Probá que el
   patrón de await funciona contra un shader de cómputo trivial antes de escribir una sola
   superficie de API.** Ese spike es la tarea cero.

2. **Los errores también son asíncronos.** WebGPU reporta errores de validación y de falta
   de memoria a través de `pushErrorScope`/`popErrorScope`, no con excepciones. Un shader
   que falla la validación no lanza nada — produce ceros. Las respuestas silenciosamente
   incorrectas son el modo de falla por defecto, así que los error scopes no son un pulido
   opcional; van en la primera implementación.

## Bosquejo de API

```go
// Available reports whether navigator.gpu exists. Always check: WebGPU is absent
// in Firefox on some platforms, in older Safari, and in every non-secure context.
func Available() bool

func RequestDevice(ctx *context.Context, cfg DeviceConfig) (*Device, error)

type Device struct{ ... }

func (d *Device) Limits() Limits            // maxComputeWorkgroupSize, maxBufferSize, ...
func (d *Device) NewBuffer(size int, usage BufferUsage) (*Buffer, error)
func (d *Device) NewShader(wgsl string) (*Shader, error)
func (d *Device) NewPipeline(sh *Shader, entryPoint string) (*Pipeline, error)
func (d *Device) Close()

func (b *Buffer) Write(src []byte) error              // via CopyBytesToJS
func (b *Buffer) Read(ctx *context.Context, dst []byte) error // mapAsync + CopyBytesToGo
func (b *Buffer) Close()

type Pass struct{ ... }
func (d *Device) Begin() *Pass
func (p *Pass) SetPipeline(pl *Pipeline)
func (p *Pass) SetBuffers(bufs ...*Buffer)
func (p *Pass) Dispatch(x, y, z int)
func (p *Pass) Submit(ctx *context.Context) error     // awaits onSubmittedWorkDone
```

`Buffer.Write`/`Read` usan `js.CopyBytesToJS`/`CopyBytesToGo` — las mismas primitivas de
copia masiva que `indexdb` (índice maestro **D1**), por la misma razón.

Todo `js.Func` creado para un callback de promesa tiene que hacer `Release()`. Un
`js.Func` filtrado en un camino de código por dispatch filtra en cada llamada de
inferencia; convertilo en regla documentada y en ítem de checklist de revisión.

## Detección de capacidad y fallback

Que `Available()` devuelva false es una condición normal, no un error, y el llamador — el
adaptador de `embed` de la fase 5 — cae al embedder estático de la fase 3. Ese camino de
fallback tiene que estar testeado, porque va a ser el camino común durante mucho tiempo.

## Tests

Solo en navegador, `//go:build wasm`, bajo `gotest -tinygo`. Se saltean limpiamente con un
mensaje explícito cuando `Available()` es false, para que un CI sin GPU reporte "salteado",
nunca un falso positivo.

| Test | Verifica |
|---|---|
| `TestAvailable_NoPanic` | en un dispositivo sin WebGPU |
| `TestRequestDevice_Succeeds` | y reporta límites plausibles |
| `TestBuffer_WriteReadRoundTrip` | 1 MB hace round-trip byte a byte |
| `TestShader_TrivialCompute` | un shader que duplica cada elemento de un buffer de 1024 floats produce exactamente eso |
| `TestShader_InvalidWGSLIsError` | **no ceros** — el contrato de error scope |
| `TestDispatch_ExceedsWorkgroupLimit` | error antes del submit, no una pérdida de device |
| `TestBuffer_CloseTwice` | idempotente |
| `TestDevice_FuncsReleased` | un bucle de 1000 dispatches no filtra ningún `js.Func` |
| `TestSubmit_AwaitsCompletion` | los resultados son legibles inmediatamente después de que `Submit` retorna |

## Checklist de aceptación

```bash
GOOS=js GOARCH=wasm go build ./...
gotest -tinygo
grep -rn "js.FuncOf" . | wc -l            # cada uno tiene su Release
grep -rn "pushErrorScope" .               # → presente: los errores async se manejan
grep -rn "webtyp.com/nn\|webtyp.com/embed" .   # → vacío: no se coló conocimiento de ML
```
