---
PLAN: "feat: webtyp/webgpu — navigator.gpu bindings for Go/WASM"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/webgpu (to be created)
---

> New repository. Moves to `webgpu/docs/PLAN.md` once the repository exists.
> Master index: https://github.com/webtyp/agent/blob/main/docs/PLAN.md
> **Phase 5.** Do not start before Phase 3 ships — the product works without this.

# Plan — `webtyp/webgpu`

## Single responsibility

Typed Go access to the browser's WebGPU compute API. Adapter and device acquisition,
buffers, shader modules, bind groups, compute pipelines, dispatch, readback.

It knows nothing about machine learning. No tensors, no layers, no attention — those are
`webtyp/nn`. This package could equally serve image processing or physics, and keeping it
that way is what stops GPU lifetime bugs and model bugs from tangling.

Compute only: no render pipelines, no swap chains, no canvas. Rendering is a separate
concern and nothing in this project needs it.

## Scope warning

This is the largest and least certain piece of the whole effort, and it is deliberately
last. Phase 3's static embedder makes semantic search work without a single line of GPU
code. If this repository stalls, the product does not.

Two constraints shape everything below:

1. **Everything in WebGPU is async and promise-based.** `requestAdapter`,
   `requestDevice`, `mapAsync`, `onSubmittedWorkDone` are all promises.
   `webtyp.com/await` handles a one-shot promise, and that is the right primitive — but
   under TinyGo wasm a goroutine blocking on a channel while the JS event loop must run
   to resolve the promise is exactly the deadlock shape `indexdb`'s
   `docs/LAST_PLAN_EXECUTED.md` already warns about. **Prove the await pattern works
   against a trivial compute shader before writing any API surface.** That spike is task
   zero.

2. **Errors are asynchronous too.** WebGPU reports validation and out-of-memory errors
   through `pushErrorScope`/`popErrorScope`, not exceptions. A shader that fails
   validation does not throw — it produces zeros. Silent wrong answers are the default
   failure mode, so error scopes are not optional polish; they are in the first
   implementation.

## API sketch

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

`Buffer.Write`/`Read` use `js.CopyBytesToJS`/`CopyBytesToGo` — the same bulk-copy
primitives as `indexdb` (master index **D1**), for the same reason.

Every `js.Func` created for a promise callback must be `Release()`d. A leaked `js.Func`
in a per-dispatch code path leaks on every inference call; make it a documented rule and
a review checklist item.

## Feature detection and fallback

`Available()` returning false is a normal condition, not an error, and the caller — the
Phase 5 `embed` adapter — falls back to the Phase 3 static embedder. That fallback path
must be tested, because it will be the common path for a long time.

## Tests

Browser-only, `//go:build wasm`, under `gotest -tinygo`. Skip cleanly with an explicit
message when `Available()` is false, so CI without a GPU reports "skipped", never a false
pass.

| Test | Asserts |
|---|---|
| `TestAvailable_NoPanic` | on a device without WebGPU |
| `TestRequestDevice_Succeeds` | and reports plausible limits |
| `TestBuffer_WriteReadRoundTrip` | 1 MB round-trips byte for byte |
| `TestShader_TrivialCompute` | a shader that doubles every element of a 1024-float buffer produces exactly that |
| `TestShader_InvalidWGSLIsError` | **not zeros** — the error-scope contract |
| `TestDispatch_ExceedsWorkgroupLimit` | an error before submission, not a device loss |
| `TestBuffer_CloseTwice` | idempotent |
| `TestDevice_FuncsReleased` | a dispatch loop of 1000 iterations leaks no `js.Func` |
| `TestSubmit_AwaitsCompletion` | results are readable immediately after `Submit` returns |

## Acceptance checklist

```bash
GOOS=js GOARCH=wasm go build ./...
gotest -tinygo
grep -rn "js.FuncOf" . | wc -l            # every one has a matching Release
grep -rn "pushErrorScope" .               # → present: async errors are handled
grep -rn "webtyp.com/nn\|webtyp.com/embed" .   # → empty: no ML knowledge leaked in
```
