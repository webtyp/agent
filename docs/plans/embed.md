---
PLAN: "feat: webtyp/embed — generación de embeddings en el navegador"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/embed (por crear)
---

> Repositorio nuevo. Se mueve a `embed/docs/PLAN.md` cuando el repositorio exista.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/PLAN.md — decisiones
> **D4** (dos fases detrás de un puerto) y **D5** (el español manda en el tamaño del modelo).

# Plan — `webtyp/embed`

## Responsabilidad única

Texto → vectores, en el navegador. Es dueño del puerto `Embedder` y del pipeline que lo
implementa: tokenizar, correr el modelo, hacer pooling, normalizar.

**No** es dueño de la tokenización (`webtyp/tokenizer`), ni de la aritmética vectorial
(`webtyp/vector`), ni del almacenamiento, ni —en la fase 5— de la GPU (`webtyp/webgpu`,
`webtyp/transformer`).

## El puerto

Esta interfaz es la juntura sobre la que pivotea toda la arquitectura. Todo lo que está
aguas abajo (`vectordb`, `agent`) depende de esto y de nada más sobre embeddings.

```go
// Embedder turns text into vectors. Implementations run entirely in the browser.
type Embedder interface {
	// Dim is the vector dimension. Constant for the lifetime of the Embedder.
	Dim() int

	// ID identifies the model that produced these vectors, e.g.
	// "potion-multilingual-128M/int8". Vectors from different models are NOT
	// comparable; vectordb stores this and refuses a corpus that disagrees.
	ID() string

	// Embed writes one vector per text into dst, which MUST have length
	// len(texts)*Dim(). Vectors are L2-normalised on the way out (master index D1).
	// The caller owns dst, so batching allocates once.
	Embed(ctx *context.Context, texts []string, dst []float32) error

	Close() error
}
```

`ID()` no es decoración. Mezclar vectores de dos modelos produce resultados que parecen
plausibles y no significan nada — la falla no tiene síntoma. Guardar el id es lo que
convierte eso en un error de arranque.

## Fase 3 — embeddings estáticos (este plan)

Una tabla de embeddings de tokens destilada (familia model2vec / "potion"): tokenizar,
buscar el vector de cada token, promediar, normalizar. Sin atención, sin forward pass, sin
GPU, sin multiplicación de matrices más allá de una búsqueda promediada.

Por qué esto primero, si el destino es un transformer sobre WebGPU: es Go puro, limpio bajo
TinyGo hoy, alrededor de 30 MB cuantizado, y entrega un **pipeline completo funcionando**
antes de que exista código de GPU. La fase 5 después reemplaza esta implementación detrás
de la misma interfaz sin tocar `vectordb`, `indexdb`, `storage` ni `model`. Si la fase 5 se
demora, el producto igual funciona.

La calidad de recuperación queda mediblemente por debajo de un encoder completo. Ese
compromiso se acepta para la fase 3 y se revisa con números, no con opiniones — ver
§Evaluación.

### 2. Selección de modelo — DECISIÓN ABIERTA (índice maestro O1)

Debe resolverse antes de implementar. La restricción de **D5**: los modelos multilingües
cargan un vocabulario de ~250 k tokens, y la tabla de embeddings sola pesa
`250 000 × dim × 4` bytes en fp32 — para un modelo estático, la tabla **es** el modelo.

| Candidato | Dim | Tabla fp32 | Tabla int8 | Español |
|---|---|---|---|---|
| `potion-multilingual-128M` | 256 | ~256 MB | ~64 MB | sí |
| una destilación multilingüe de 128 dims | 128 | ~128 MB | ~32 MB | sí |
| estático solo-inglés (`potion-base-8M`) | 256 | ~32 MB | ~8 MB | **no — descartado por D5** |

Supuesto de trabajo hasta decidir: **multilingüe, 256 dims, tabla int8 con escalas por
fila**. Completá esta tabla con números medidos sobre los artifacts reales antes de elegir;
las cifras de arriba son de orden de magnitud.

Entregable de esta decisión: un conversor offline de una sola vez (una herramienta en
`cmd/`, en Go, que no se embarca al navegador) que transforme el modelo publicado al
formato de artifact de `plans/weights.md`.

### 3. `static.go` — la implementación

```go
type StaticConfig struct {
	Weights   weights.Artifact  // token table + scales + tokenizer vocab
	Tokenizer tokenizer.Tokenizer
	Pooling   Pooling           // MeanPooling default
}

func NewStatic(cfg StaticConfig) (Embedder, error)
```

Bucle caliente por texto: codificar → por cada id de token, acumular su fila
descuantizada en un acumulador → dividir por la cantidad de tokens → `vector.Normalize`.
Un único buffer acumulador, reutilizado en todo el lote: cero allocations por texto después
del calentamiento.

La descuantización int8 es `float32(q) * scale[row]`, fusionada dentro de la acumulación
para que la fila descuantizada nunca se materialice.

### 4. `mock.go` — embedder determinístico para tests aguas abajo

`vectordb` y `agent` tienen que testear sin un modelo. Embarcá un embedder determinístico
derivado de un hash del texto — mismo texto, mismo vector, dentro del paquete, sin artifact
necesario. Exportado, porque es el "toda interfaz externa lleva un mock" de
`DEFAULT_LLM_SKILL.md` §2 aplicado a través de fronteras de repositorio.

### 5. Carga y caché del artifact

La primera carga baja el artifact por HTTP (`webtyp.com/fetch`) y lo guarda en IndexedDB;
las cargas siguientes lo leen de ahí. Una vez por navegador, no una vez por sesión — a
30-100 MB la diferencia es que el producto sea usable o no.

La clave de caché incluye el id del modelo y la versión del artifact, así que una
actualización no sirve pesos viejos. Una descarga parcial no debe cachearse: escribí el
blob recién después de leer el cuerpo completo y verificar su largo contra la cabecera.

## Fase 5 — encoder transformer

Una segunda implementación, `NewTransformer`, sobre `webtyp/transformer` y `webtyp/webgpu`. Misma
interfaz `Embedder`, distinto `ID()`. Está especificada en `plans/transformer.md`; nada de este plan
se bloquea por ella, y nada de acá cambia cuando aterrice salvo agregar un constructor.

## Evaluación

La calidad no es afirmable en un test unitario, así que tiene su propio harness — si no, el
compromiso fase 3 → fase 5 se decide por intuición:

- Un conjunto chico de recuperación en español commiteado como `testdata/` (consultas,
  documentos, juicios de relevancia), unos pocos cientos de entradas.
- `cmd/eval` reporta recall@1, recall@10 y MRR para cualquier `Embedder`.
- Resultados registrados en el README, por id de modelo. La fase 5 tiene que ganarle a la
  fase 3 en este harness para justificar su existencia.

## Tests

| Test | Verifica |
|---|---|
| `TestStatic_DimMatchesArtifact` | |
| `TestStatic_Deterministic` | el mismo texto dos veces da vectores idénticos bit a bit |
| `TestStatic_Normalised` | toda salida tiene norma L2 igual a 1 dentro de 1e-6 |
| `TestStatic_BatchMatchesSingle` | `Embed` sobre 10 textos equivale a 10 llamadas individuales |
| `TestStatic_EmptyText` | un vector definido, no NaN, no pánico |
| `TestStatic_AllUnknownTokens` | una cadena de puro `[UNK]` no divide por cero |
| `TestStatic_WrongDstLength` | error, nunca una escritura parcial |
| `TestStatic_ZeroAllocsPerText` | tras el calentamiento, con `dst` predimensionado |
| `TestStatic_Dequantise` | int8 + escala reproduce la referencia fp32 dentro de tolerancia |
| `TestMock_Deterministic` | el contrato del mock, ya que otros repositorios dependen de él |
| `TestCache_SecondLoadSkipsFetch` | verificado con un fetcher mock |
| `TestCache_PartialDownloadNotCached` | un cuerpo truncado no deja entrada de caché |
| `TestCache_VersionBumpInvalidates` | |

## Checklist de aceptación

```bash
go vet ./...
gotest
gotest -tinygo                            # el camino de caché necesita navegador
GOOS=js GOARCH=wasm go build ./...
go run ./cmd/eval -model static           # recall@10 registrado en el README
grep -rn "webtyp.com/vectordb" .          # → vacío: el puerto no conoce a su consumidor
```
