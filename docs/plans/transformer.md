---
PLAN: "feat: webtyp/transformer — grafo del encoder + kernels CPU/WASM"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/transformer (por crear)
---

> Repositorio nuevo. Se mueve a `transformer/docs/PLAN.md` cuando el repositorio exista.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/MASTER_PLAN.md
>
> **Este plan tiene dos etapas y solo la primera está desbloqueada.** La etapa 1 —los
> kernels y el benchmark que decide la fase 3— no depende de qué modelo gane, y se puede
> despachar hoy. La etapa 2 —el grafo real— espera a que `PENDING_ITEMS.md` P1b elija entre
> Granite 97M R2, Bekko a25m y Bekko a8m, porque la arquitectura del grafo depende de cuál
> sea.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus
> comentarios en inglés, como el resto del código fuente.

# Plan — `webtyp/transformer`

## Responsabilidad única

Ids de tokens y pesos entran; **un vector sale**. Es dueño del grafo del encoder y de los
kernels numéricos que lo ejecutan.

**No** es dueño de la tokenización (`webtyp/tokenizer`), **ni** del formato ni la carga de
los pesos (`webtyp/weights`), **ni** del puerto `Embedder` (`webtyp/embed`, que compone a
los tres). No normaliza el vector de salida: eso lo hace `embed`, porque el contrato de
`Embedder.Embed` lo promete y ahí es donde se cumple.

## Por qué este plan reemplaza al anterior

La versión previa de este archivo estaba escrita para kernels **WGSL sobre WebGPU**. La
decisión **D4b** del índice maestro la anuló: el navegador solo embebe **consultas**, no
documentos, y una consulta son ~20 tokens. El cómputo es unos dos órdenes de magnitud menor
que el de un chunk de 8 000, y eso cabe en CPU.

Y ya no es una conjetura. **P1 está medido** (`vector` v0.1.1): `Dot` de 384 dims corre a
**~3,3 GFLOPS escalares bajo TinyGo WASM**. Los ~856M FLOP de un forward pass de 20 tokens
sobre 12 capas de 384 dims se pagan en **~259 ms** derivados.

`webtyp/webgpu` salió del plan maestro. El plan WGSL archivado está en
[`agent/docs/history/WEBGPU_ENCODER.md`](../history/WEBGPU_ENCODER.md) y solo se desarchiva
si la etapa 1 de este plan mide algo inaceptable.

## Dependencias

`webtyp.com/vector` y `webtyp.com/fmt`. **Nada más.**

En particular **no** depende de `weights` ni de `tokenizer`: recibe los tensores ya
decodificados y los ids ya tokenizados. Eso le da la misma propiedad que hace valioso a
`vector` —todo el paquete es testeable con `go test` plano, sin navegador y sin artifact de
200 MB— y es la razón de que sea un repositorio aparte y no parte de `embed`.

---

# Etapa 1 — Los kernels y el número que decide

**Despachable ya.** No necesita que P1b elija modelo.

## La decisión de diseño que ordena todo: `matmul` se construye sobre `vector.Dot`

No reimplementes el producto punto. `vector.Dot` ya existe, ya está desenrollado de a 4, ya
tiene tests contra una referencia ingenua, y **ya es el número que P1 midió**. Un `matmul`
construido encima hereda esa medición: si `Dot` da 3,3 GFLOPS, el forward pass sale de
dividir, y el benchmark de acá solo confirma lo que la aritmética ya predijo.

```go
// MatmulT computes C[m,n] = A[m,k] · Bᵀ[n,k], i.e. B is stored TRANSPOSED so every
// inner product is over contiguous memory — which is what vector.Dot is fast at.
// Storing B transposed is the caller's job (weights does it once, at load).
func MatmulT(dst, a, bT []float32, m, k, n int)
```

El costo de llamar a `Dot` m×n veces es despreciable frente al trabajo que hace: para
20×384 × 384×384 son 7 680 llamadas sobre 2,95M MAC. Medilo igual; si el overhead resulta
visible, desenrollar el bucle externo está permitido, reimplementar `Dot` no.

Esta es la regla del skill de diseño de API aplicada literalmente: *una operación que la
librería ya hace se llama, nunca se re-implementa en el call site.*

## Los kernels

Todos en Go plano, sin build tags, sin `syscall/js`, sin SIMD. **Sin maps** (regla del
proyecto para código que compila a WASM bajo TinyGo). Errores con `webtyp.com/fmt`, nunca
con `errors` ni el `fmt` de stdlib.

```go
// Escribí cada uno PLANO primero. Optimizá solo detrás de un benchmark que lo justifique,
// igual que vector hizo con Dot.

func MatmulT(dst, a, bT []float32, m, k, n int)        // sobre vector.Dot
func LayerNorm(dst, src, gamma, beta []float32, dim int, eps float32)
func RMSNorm(dst, src, gamma []float32, dim int, eps float32)
func Softmax(x []float32)                               // in place, con el truco del máximo
func GELU(x []float32)                                  // in place
func SiLU(x []float32)                                  // in place
func Add(dst, src []float32)                            // residual, in place
func RoPE(q, k []float32, pos, dim, heads int)          // rotary position embedding
```

Dos advertencias que cuestan horas si se descubren tarde:

1. **`Softmax` sin restar el máximo desborda.** Con logits de atención de un modelo real,
   `exp(x)` se va a `+Inf` y el resultado es `NaN` silencioso. Restá el máximo siempre.
2. **La tolerancia de `LayerNorm` importa.** Acumulá la media y la varianza en `float64`
   aunque los datos sean `float32`. Acumular en `float32` sobre 384 elementos introduce
   deriva suficiente para mover el recall sin romper ningún test de kernel.

## Cuantización: qué se desempaqueta y qué no

Los pesos llegan int8 con escalas por fila (**D5**). Este plan toma una posición y la deja
visible para que sea revisable:

| Tensor | Qué hace este plan | Por qué |
|---|---|---|
| **Cuerpo del transformer** (~28,3M params) | desempaquetar a `float32` al cargar | deja los kernels en f32 puro, que es lo que `vector.Dot` hace rápido y lo que P1 midió. Costo: ~113 MB residentes |
| **Tabla de embeddings** (~180k × 384) | **se queda int8**, se desempaqueta solo la fila de cada token | una consulta toca ~20 filas de 180 000. Desempaquetarla entera costaría ~276 MB para usar el 0,01% |

**El número a reportar:** la memoria residente real medida, no estimada. Si los ~113 MB del
cuerpo resultan inaceptables en un navegador, el paso documentado es un kernel de matmul
int8 con la escala fusionada — **no lo escribas ahora**. Es optimización especulativa hasta
que haya una medición que la pida, y complica el kernel que P1 ya validó.

## El benchmark que decide la fase 3

Esta es la entrega principal de la etapa 1. Todo lo demás existe para hacerla posible.

```go
// BenchmarkEncode_20x12x384 runs a full forward pass at the shape of the leading
// candidate: 20 tokens, 12 layers, 384 dims, 6 heads, FFN 1536. Random weights —
// this measures the kernels, not the model.
func BenchmarkEncode_20x12x384(b *testing.B)
```

Corrélo **bajo TinyGo en WASM**, que es el target real:

```bash
tinygo test -target wasm -bench=BenchmarkEncode_20x12x384 -benchtime=2s .
```

`gotest -bench` **no sirve acá**: pasarle argumentos desactiva su etapa wasm y te devuelve
un número nativo, que es ~2,5× más rápido y por lo tanto mentiroso para esta decisión.

Registrá el resultado en el README en **milisegundos**, junto al piso derivado de 259 ms,
para que la diferencia entre lo predicho y lo medido sea visible.

### Los tres desenlaces, ya escritos

| Medido | Qué pasa |
|---|---|
| **< 300 ms** | el transformer es viable. Etapa 2 como está escrita. |
| **300 ms – 1 s** | viable con reservas. **Requiere una decisión humana**: ¿se acepta esa latencia en un cuadro de búsqueda? Pará y preguntá. |
| **> 1 s** | el nivel del medio de D5 no sirve. Se baja a la tabla estática, `transformer` no se construye más allá de esta etapa, y se desarchiva la discusión de WebGPU. |

Esperá que el medido sea **1,5–3× el piso de 259 ms** (o sea 400–800 ms): `Dot` es el mejor
caso posible —MAC secuencial, localidad perfecta— y un forward pass real agrega softmax,
layernorm y GELU, que son trascendentes bastante más caras por elemento. Si medís **menos**
de 259 ms, sospechá del benchmark antes de festejar: probablemente el compilador eliminó
trabajo por no usarse el resultado.

## Tests de la etapa 1

Solo librería estándar, sin paquetes externos de aserciones.

| Test | Verifica |
|---|---|
| `TestMatmulT_MatchesNaive` | contra un triple bucle ingenuo, 1e-5 relativo |
| `TestMatmulT_Dimensions` | dimensiones incompatibles dan error, no corrupción silenciosa |
| `TestSoftmax_SumsToOne` | dentro de 1e-6 |
| `TestSoftmax_LargeLogitsNoNaN` | **logits de ±100 no producen NaN ni Inf** — el bug del máximo |
| `TestSoftmax_Uniform` | entradas iguales dan distribución uniforme |
| `TestLayerNorm_ZeroMeanUnitVar` | salida con media ~0 y varianza ~1 antes de gamma/beta |
| `TestLayerNorm_Float64Accumulation` | contra una referencia en float64, 1e-6 |
| `TestGELU_KnownValues` | valores calculados a mano, incluido 0 y negativos grandes |
| `TestSiLU_KnownValues` | idem |
| `TestRoPE_RotationPreservesNorm` | rotar no cambia la magnitud |
| `TestRoPE_PositionZeroIsIdentity` | la posición 0 no rota nada |
| `TestAdd_InPlace` | el residual no aliasea mal |

## Checklist de aceptación de la etapa 1

```bash
go vet ./...
gotest
GOOS=js GOARCH=wasm go build ./...
tinygo test -target wasm .
tinygo test -target wasm -bench=BenchmarkEncode_20x12x384 -benchtime=2s .   # el número
grep -rn "syscall/js\|webtyp.com/weights\|webtyp.com/tokenizer" .           # → vacío
grep -rn "map\[" --include="*.go" . | grep -v _test.go                      # → vacío
grep -rn '"errors"\|"fmt"' --include="*.go" . | grep -v _test.go            # → vacío
```

---

# Etapa 2 — El grafo real

**Bloqueada por `PENDING_ITEMS.md` P1b.** No empieces hasta que el modelo esté elegido: la
forma del grafo depende de cuál sea, y construirlo contra el candidato equivocado es
trabajo tirado.

## Por qué depende del modelo

`granite-embedding-97m-multilingual-r2` es **ModernBERT**: atención alternada global/local,
RoPE, sin términos de sesgo, activación tipo SiLU. `bekko-embedding-v1-a25m` y `a8m` son
otra familia y no comparten todas esas decisiones. Los **kernels** de la etapa 1 sirven para
cualquiera de los tres; el **orden en que se llaman** no.

Cuando P1b cierre, verificá la forma contra el `config.json` del modelo antes de escribir
nada: `num_hidden_layers`, `hidden_size`, `num_attention_heads`, `intermediate_size`,
`vocab_size`. Son cinco campos en un archivo de 1,2 kB, y fijan toda la aritmética.

## API

```go
// Config is the model's shape, read from its config.json. Nothing here is hardcoded to
// one model: swapping candidates is a Config change plus, at most, one kernel.
type Config struct {
	Layers     int
	Dim        int
	Heads      int
	FFNDim     int
	VocabSize  int
	MaxSeq     int
	Norm       NormKind        // LayerNorm | RMSNorm
	Activation ActivationKind  // GELU | SiLU
	Eps        float32
}

// Tensors is what transformer needs from whoever loaded the weights. Declaring it HERE,
// as the consumer, is what keeps this package free of a dependency on webtyp/weights:
// weights satisfies this, and so does a test fixture built from random numbers.
type Tensors interface {
	// EmbedRow writes the embedding of token id into dst, dequantising on the way.
	EmbedRow(id int32, dst []float32) error
	// Layer returns one layer's weights, already transposed for MatmulT.
	Layer(i int) (LayerWeights, error)
}

type Encoder struct{ ... }

func New(cfg Config, t Tensors) (*Encoder, error)

// Encode runs the forward pass over one tokenised sequence and writes the pooled vector
// into dst, which must have length Dim(). NOT normalised — embed does that.
func (e *Encoder) Encode(ids []int32, dst []float32) error

func (e *Encoder) Dim() int
```

`Tensors` declarado acá y no importado de `weights` es deliberado, y es el mismo movimiento
que hizo `vectordb` con `embed.Embedder`: el consumidor nombra lo que necesita, el proveedor
lo satisface. Sin eso, este paquete no se podría testear sin un artifact real.

## Corrección: el código numérico falla en silencio

Esta estrategia sobrevive intacta del plan anterior, porque no tenía nada que ver con la GPU:

1. **Fixtures dorados del modelo de referencia**, generados una vez con
   `sentence-transformers` u ONNX Runtime y commiteados en `testdata/`: ids de entrada, y la
   salida esperada **de cada tensor intermedio**, no solo del vector final.
2. **Tests de kernel antes que tests de grafo.** Un softmax mal y una máscara de atención
   mal producen el mismo síntoma a nivel de grafo —recuperación levemente peor— y ninguno
   rompe un test de extremo a extremo. Contra los intermedios dorados, el bug tiene una sola
   ubicación posible.
3. **Una tolerancia documentada.** No va a coincidir bit a bit con Python. Declarala
   —sugerido: 1e-4 relativo en intermedios, 1e-5 en el vector final— y verificá contra ella,
   en vez de descubrir la deriva en los números de recall meses después.

Sin los fixtures dorados **no hay forma de saber si el encoder es correcto**, solo si es
rápido. Un encoder rápido y equivocado es peor que ninguno: produce resultados plausibles.

## Puerta de salida de la etapa 2

La del índice maestro, fase 3: el mismo código Go produce **el mismo vector para el mismo
texto** en los tres targets de D4 —navegador (WASM), backend (nativo) y Worker (WASM)—
dentro de la tolerancia documentada. Si los tres no coinciden, no hay un solo espacio
vectorial y el requisito de no re-indexar nunca se cae.
