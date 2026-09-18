---
PLAN: "feat: webtyp/transformer — grafo del encoder + kernels CPU/WASM"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/transformer (por crear)
---
> ⚠️ **OBSOLETO — hay que reescribirlo antes de crear el repositorio.**
> Se llamaba `nn` — abreviatura no universal, y «neural network» es demasiado ancho para
> un repo que solo corre un encoder. Y está escrito para kernels WGSL sobre WebGPU. La decisión **D4b** del índice
> maestro borró esa necesidad: el navegador solo embebe consultas (~20 tokens), y eso corre
> en WASM sobre CPU. `transformer` subió de la fase 5 a la fase 3 y necesita un plan nuevo — grafo de
> encoder con kernels escalares + SIMD128. Lo que sigue se conserva solo como referencia de
> la estructura del grafo.


> Repositorio nuevo. Se mueve a `transformer/docs/PLAN.md` cuando el repositorio exista.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/MASTER_PLAN.md
> **Fase 5**, y la última pieza. Todo lo demás tiene que estar entregado y funcionando primero.

# Plan — `webtyp/transformer`

## Responsabilidad única

Ejecutar el forward pass de un encoder sentence-transformer. Es dueño de los kernels WGSL,
del grafo de ejecución y de la implementación de referencia en CPU. Obtiene su GPU de
`webtyp/webgpu` y sus parámetros de `webtyp/weights`, y produce un vector de oración
pooleado para que `webtyp/embed` lo normalice y lo pase adelante.

No descarga, no tokeniza, no almacena ni busca.

## Leé esto antes de empezar

Esto es un motor de inferencia real, y la estimación honesta es que es más grande que todos
los demás repositorios de este plan juntos. Existe para reemplazar una función — el
embedder estático de la fase 3 — por una versión de mejor calidad de la misma salida.

Por lo tanto vale la pena hacerlo **solo si** el harness de evaluación de `embed` muestra
que la calidad de recuperación del embedder estático es de hecho insuficiente para el
producto, medida sobre el conjunto de prueba en español (`plans/embed.md` §Evaluación).
Corré esa medición primero. Si el recall@10 es adecuado, este repositorio todavía no debería
construirse, y decirlo es un resultado exitoso de la fase 3, no un fracaso.

## Arquitectura

```
Grafo del encoder (familia BERT, según el modelo elegido en plans/embed.md §2)

  ids de tokens ─► lookup de embeddings ─► + posición ─► LayerNorm
                                                            │
                               ┌────────────────────────────┘
                               ▼
                    ┌─── × N capas ─────────────────────────┐
                    │  proyecciones Q,K,V   (matmul)        │
                    │  atención escalada por producto punto │
                    │    (matmul, softmax, enmascarada)     │
                    │  proyección de salida (matmul)        │
                    │  residual + LayerNorm                 │
                    │  FFN: matmul → GELU → matmul          │
                    │  residual + LayerNorm                 │
                    └───────────────────────────────────────┘
                               │
                               ▼
                mean pooling sobre la máscara de atención ─► vector de oración
```

Kernels necesarios, en orden de dependencia: `matmul` (con tiles, memoria compartida de
workgroup), `layernorm`, `softmax` (por fila, numéricamente estable — restar el máximo de
la fila), `gelu`, `add`, `mul`, `transpose`, `gather` (lookup de embeddings), `meanpool`.

`matmul` es toda la historia de rendimiento. Escribí la versión ingenua primero, hacela
correcta, y después ponele tiles. No empieces con tiles.

## Estrategia de corrección

El código numérico falla en silencio. La estrategia es hacer ruidosa cada falla:

1. **Una implementación de referencia en CPU de cada kernel**, en Go plano, compilada sin
   build tags y testeable con `go test`. Es lenta y está bien — es el oráculo.
2. **Fixtures dorados del modelo de referencia**, generados una vez en Python y commiteados
   en `testdata/`: ids de entrada, y la salida esperada de cada tensor intermedio.
3. **Tests a nivel de kernel antes que a nivel de grafo.** Un softmax incorrecto y una
   máscara de atención incorrecta producen el mismo síntoma a nivel de grafo: una
   recuperación levemente peor. Testeá cada kernel contra el intermedio dorado, y el bug
   tiene una sola ubicación posible.
4. **Una tolerancia documentada.** La aritmética fp32 en GPU no va a coincidir bit a bit
   con Python. Declará la tolerancia (se sugiere 1e-4 relativa en tensores intermedios,
   1e-5 en el vector normalizado final) y verificá contra ella, en vez de descubrir la
   deriva en los números de recall meses después.

## API

```go
type Encoder struct{ ... }

type Config struct {
	Device   *webgpu.Device     // nil → CPU reference path
	Weights  *weights.Artifact
	MaxSeq   int
}

func NewEncoder(cfg Config) (*Encoder, error)

// Forward runs the encoder over a batch of tokenised sequences and writes one
// pooled vector per sequence into dst. Not normalised — embed does that.
func (e *Encoder) Forward(ctx *context.Context, ids [][]int32, dst []float32) error

func (e *Encoder) Dim() int
func (e *Encoder) Close() error
```

Que un `Device` nulo seleccione el camino de CPU no es una comodidad: es cómo corren los
tests dorados en Go estándar sin navegador, y cómo una máquina sin WebGPU igual produce
respuestas correctas (lentas).

## Tests

| Test | Verifica |
|---|---|
| `TestMatmul_CPUMatchesGolden` | contra `testdata/` |
| `TestMatmul_GPUMatchesCPU` | dentro de tolerancia, para varias formas incluyendo no-múltiplos del tamaño de tile |
| `TestLayerNorm_CPUMatchesGolden` | |
| `TestSoftmax_NumericalStability` | una fila que contiene 1e30 no produce NaN |
| `TestSoftmax_GPUMatchesCPU` | |
| `TestGELU_MatchesGolden` | la variante exacta que usa el modelo — la aproximación tanh y erf difieren |
| `TestGather_EmbeddingLookup` | |
| `TestAttention_MaskApplied` | las posiciones de relleno contribuyen exactamente cero |
| `TestMeanPool_IgnoresPadding` | el paso que más comúnmente se arruina en todo el pipeline |
| `TestEncoder_ForwardMatchesGolden` | pasada completa, CPU, contra la salida del modelo de referencia |
| `TestEncoder_GPUMatchesCPU` | pasada completa, dentro de tolerancia |
| `TestEncoder_BatchMatchesSingle` | agrupar en lotes no cambia ningún resultado |
| `TestEncoder_NilDeviceUsesCPU` | el camino de fallback |
| `TestEncoder_VariableLengths` | un lote de secuencias de largos mezclados |

Benchmarks comparando CPU y GPU por secuencia, registrados en el README. Si el camino de
GPU no es significativamente más rápido que el embedder estático de punta a punta, este
repositorio no se ganó su lugar y eso tiene que ser visible en los números.

## Checklist de aceptación

```bash
go vet ./...
gotest                  # camino de referencia en CPU, Go estándar
gotest -tinygo          # camino de GPU, navegador
ls testdata/*.bin       # fixtures dorados commiteados
GOOS=js GOARCH=wasm go build ./...
go run ./cmd/eval       # recall@10 le gana al embedder estático de la fase 3, o esto no se embarca
```
