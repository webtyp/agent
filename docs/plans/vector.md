---
PLAN: "feat: webtyp/vector — math vectorial, arena, top-k"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/vector (por crear)
---

> Repositorio nuevo. Este archivo vive en `agent/docs/plans/` hasta que `webtyp/vector`
> exista; entonces se mueve a `vector/docs/PLAN.md` sin modificaciones.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/PLAN.md

# Plan — `webtyp/vector`

## Responsabilidad única

Números. Aritmética de similitud sobre slices de `float32`, la arena contigua donde viven,
un selector top-k de tamaño fijo, y el códec little-endian que los mueve hacia y desde el
almacenamiento.

Este paquete no sabe nada de documentos, texto, embeddings, almacenamiento ni JavaScript.
Tiene **cero dependencias** (ni siquiera `webtyp.com/fmt` fuera de la construcción de
errores) y compila para todos los targets, lo que significa que todo él es testeable en Go
estándar sin navegador. Esa propiedad es la razón de que sea un repositorio aparte.

## Por qué no es parte de `vectordb`

Si este código vive dentro del almacén, nada de él se puede testear sin un target WASM y
un harness de navegador. El código numérico es exactamente el código que más necesita
iteración rápida con `go test` plano y `testing.AllocsPerRun`. Mantenerlo afuera también
hace exigible el presupuesto de allocations: los tests de este paquete afirman cero
allocations, y ninguna preocupación de storage o JS puede colarse a romperlo.

## Verificar primero

**O2 del índice maestro:** ¿`webtyp/binary` ya provee un códec little-endian de `float32`?
Si lo provee, dependé de él y borrá la §4 de este plan.

```bash
go doc webtyp.com/binary
```

## API

### 1. `arena.go` — dónde viven los vectores

```go
// Arena is a contiguous block of N × Dim float32 values. Vector i occupies
// data[i*dim : (i+1)*dim].
//
// One allocation holds every vector in the corpus. This is not a micro-optimisation:
// TinyGo's conservative GC scans N small slices far more expensively than one large
// one, and a contiguous layout is what makes the dot product walk memory linearly.
type Arena struct {
	data []float32
	dim  int
	n    int
}

func NewArena(dim, capacity int) *Arena
func (a *Arena) Dim() int
func (a *Arena) Len() int
func (a *Arena) Cap() int

// Append copies v into the next slot, normalising it in place, and returns its index.
// v is not retained.
func (a *Arena) Append(v []float32) (int, error)

// At returns a view into the arena — NOT a copy. Writing through it mutates the arena.
func (a *Arena) At(i int) []float32

// Set overwrites slot i, normalising.
func (a *Arena) Set(i int, v []float32) error

// Grow reallocates to at least capacity, preserving contents. The only allocating
// operation on the hot path, and callers are expected to size the arena up front.
func (a *Arena) Grow(capacity int)
```

### 2. `math.go` — la aritmética

```go
// Dot returns the dot product. For L2-normalised vectors this IS the cosine
// similarity: storing normalised removes one sqrt and one division per candidate
// per query.
func Dot(a, b []float32) float32

func Norm(v []float32) float32      // L2 magnitude
func Normalize(v []float32)         // in place; a zero vector is left untouched
func Cosine(a, b []float32) float32 // for un-normalised input; Dot is preferred
```

`Dot` es el bucle caliente de todo el sistema. Escribilo plano primero, con una variante
desenrollada de a 4 detrás de un benchmark — y quedate con la plana si el benchmark no
justifica la desenrollada. No recurras a SIMD de WASM: el soporte de TinyGo es incompleto,
y un intrínseco que no se puede verificar es peor que un bucle correcto en todas partes.

### 3. `topk.go` — seleccionar sin ordenar

```go
// TopK keeps the k highest-scoring ids seen, using a fixed-size min-heap.
// Offer allocates nothing. This replaces "build an array of N pairs, sort it,
// slice the first k", which is what the TypeScript original does.
type TopK struct{ ... }

func NewTopK(k int) *TopK
func (t *TopK) Reset()
func (t *TopK) Offer(id int32, score float32)
func (t *TopK) Results(dst []Match) []Match  // descending; appends into dst

type Match struct {
	ID    int32
	Score float32
}
```

Para k chico (el caso común, k ≤ 100) una inserción lineal en un arreglo fijo ordenado le
gana a un heap. Medí ambos y elegí uno; no embarques los dos.

### 4. `codec.go` — bytes que entran, bytes que salen

```go
// ByteLen returns the encoded size of a dim-element vector: dim*4.
func ByteLen(dim int) int

// Encode writes v into dst as little-endian float32. dst must be at least
// ByteLen(len(v)).
func Encode(dst []byte, v []float32) int

// Decode reads little-endian float32 from src into dst.
func Decode(dst []float32, src []byte) (int, error)

// Bytes returns a zero-copy byte view of the arena's backing store, for handing
// straight to a blob column. Valid until the next Grow.
func (a *Arena) Bytes() []byte

// FromBytes fills the arena from an encoded block, in one copy.
func (a *Arena) FromBytes(src []byte, count int) error
```

Little-endian es el formato de cable porque WASM es little-endian y también lo es todo
target que importa. Usá `unsafe.Slice` para el camino sin copia en arquitecturas
little-endian, protegido por build tag, con un bucle explícito de `math.Float32bits` como
fallback big-endian:

```
codec_le.go   //go:build 386 || amd64 || arm || arm64 || wasm || riscv64 || loong64
codec_be.go   //go:build !(386 || amd64 || ...)
```

El fallback no es teatro de corrección hipotética: un backend del lado del servidor
leyendo un blob escrito por un navegador tiene que decodificarlo idénticamente, y `gotest`
corre sobre lo que sea que provea CI.

### 5. `search.go` — todo junto

```go
// Search scores query against every slot in the arena and offers the results to out.
// keep, when non-nil, filters by slot index BEFORE scoring — this is where deleted
// rows and tag filters are excluded, so a filtered query costs less, not more.
//
// Allocates nothing. query must already be normalised.
func (a *Arena) Search(query []float32, keep func(i int) bool, out *TopK)
```

### 6. `quantize.go` — fase 5, solo declarado por ahora

Declarar la intención, implementar después:

```go
// Quantizer compresses an arena to int8 with a per-vector scale, cutting resident
// memory by 4×. Needed past roughly 100k documents (master index D1).
// NOT IMPLEMENTED — Phase 5.
```

No lo escribas en v1. Un cuantizador sin un corpus contra el cual medir la pérdida de
recall es una adivinanza.

## Tests

Solo librería estándar, sin paquetes externos de aserciones.

| Test | Verifica |
|---|---|
| `TestDot_KnownValues` | productos calculados a mano, incluyendo vectores ortogonales (0) e idénticos (1) |
| `TestDot_EmptyAndMismatched` | largos distintos dan error, no truncamiento silencioso ni pánico |
| `TestNormalize_UnitLength` | `Norm` tras `Normalize` es 1 dentro de 1e-6 |
| `TestNormalize_ZeroVector` | un vector cero no produce NaN |
| `TestCosine_MatchesDotOfNormalised` | ambos caminos coinciden dentro de 1e-6 |
| `TestArena_AppendAtRoundTrip` | `At(i)` devuelve lo que `Append` guardó, normalizado |
| `TestArena_AtIsAView` | escribir a través de `At` muta la arena — fija el contrato de aliasing |
| `TestArena_GrowPreserves` | el contenido sobrevive a un `Grow` |
| `TestCodec_RoundTrip` | 384 floats aleatorios sobreviven encode/decode bit a bit |
| `TestCodec_LittleEndianLayout` | `Encode([1.0])` produce exactamente `00 00 80 3F` — fija el formato de cable contra un cambio accidental |
| `TestCodec_DecodeShortInput` | un blob truncado da error |
| `TestArena_BytesFromBytesRoundTrip` | una arena de 1024 vectores sobrevive `Bytes` → `FromBytes` |
| `TestTopK_OrdersDescending` | empates incluidos |
| `TestTopK_KLargerThanInput` | devuelve todo, sin relleno |
| `TestTopK_Reset` | la reutilización no filtra nada de la consulta anterior |
| `TestSearch_MatchesNaiveReference` | 10 000 vectores aleatorios, 384 dims: resultados idénticos a una implementación ingenua que ordena todo |
| `TestSearch_KeepFilters` | las posiciones filtradas nunca aparecen |
| `TestSearch_ZeroAllocs` | **`testing.AllocsPerRun` == 0** — el presupuesto de D1 del índice maestro, hecho exigible |

Benchmarks: `BenchmarkDot_384`, `BenchmarkSearch_10k_384`, `BenchmarkTopK_Offer`.
`BenchmarkSearch_10k_384` es el número por el que se juzga todo el diseño; registralo en el
README para que una regresión sea visible.

## Checklist de aceptación

```bash
go vet ./...
gotest
go test -run TestSearch_ZeroAllocs -v ./...
GOOS=js GOARCH=wasm go build ./...
grep -rn "syscall/js\|webtyp.com/storage\|webtyp.com/indexdb" .   # → vacío: no se coló ninguna dependencia
```
