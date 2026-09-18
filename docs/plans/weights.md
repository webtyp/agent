---
PLAN: "feat: webtyp/weights — formato de artifact de modelo y caché en el navegador"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/weights (por crear)
---

> Repositorio nuevo. Se mueve a `weights/docs/PLAN.md` cuando el repositorio exista.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/MASTER_PLAN.md
> **Fase 3** — `webtyp/embed` lo necesita desde su primera línea de código.

# Plan — `webtyp/weights`

## Responsabilidad única

Llevar los parámetros del modelo desde una URL hasta slices tipados de Go, una vez por
navegador. Es dueño del formato de archivo del artifact, de su lector, del fetch HTTP y del
caché en IndexedDB. No sabe nada de qué significan los números — sin tokenización, sin
inferencia, sin embeddings.

## Por qué un formato propio y no safetensors o GGUF

Ambos son formatos razonables y ambos son el movimiento inicial equivocado acá:

- **safetensors** es una cabecera JSON más tensores crudos — fácil de parsear, pero lleva
  tensores fp32 dispuestos para un framework de entrenamiento. Acá hace falta int8 con
  escalas por fila (**D5**); convertir en tiempo de carga dentro del navegador significa
  descargar 4× los bytes y gastar la memoria del cliente para tirar tres cuartos.
- **GGUF** carga decenas de esquemas de cuantización y un modelo de metadatos muchísimo más
  grande que cualquier cosa que se necesite acá. Implementar lo suficiente como para ser
  correcto es un proyecto en sí.

La conversión ocurre **offline, una vez**, en una herramienta `cmd/` que no se embarca al
navegador. Lo que se embarca son exactamente los bytes que el navegador va a usar, en el
layout en que los va a usar. Un lector de safetensors se puede agregar después si llega un
modelo que lo requiera; no es el punto de partida.

## Formato

```
magic    "WTYPW1\0\0"                  8 bytes
version  uint32 LE                     4
header   uint32 LE largo de la cabecera 4
header   JSON: tensores, dtypes, shapes, offset de escalas, id del modelo, config del tokenizador
tensors  crudos, alineados a 64 bytes, en el orden de la cabecera
```

Puntos de diseño:

- **Alineación a 64 bytes** por tensor, para que se pueda reinterpretar con `unsafe.Slice`
  sin una copia de realineación — el mismo camino sin copia que usa el códec de
  `webtyp/vector`.
- **Little-endian en todo**, coincidiendo con el códec de `webtyp/vector`. Una sola
  decisión de endianness en todo el sistema.
- **La config del tokenizador vive en la cabecera**, no en un archivo aparte. El
  vocabulario, el flag `lowercase` y el flag `strip_accents` son propiedades del modelo;
  separarlos de los pesos es como un tokenizador se desincroniza en silencio del modelo que
  lo entrenó (`plans/tokenizer.md` explica lo que eso cuesta para el español).
- **Largo y checksum en la cabecera**, para detectar una descarga truncada antes de cachear
  nada.

## API

```go
type Artifact struct {
	ID      string  // e.g. "granite-embedding-97m-multilingual-r2/int8"
	Version uint32
	// A slice, not a map: this package compiles to WASM under TinyGo, where the
	// project bans map[K]V (see webtyp.com/context for the house pattern). A
	// transformer artifact holds on the order of a hundred tensors — twelve layers
	// of a handful each, plus the embedding table — so a linear scan by name costs
	// nothing next to the I/O that produced them.
	Tensors []Tensor
	Tokenizer TokenizerConfig
}

// Tensor returns the tensor stored under name, or false. Linear scan, see above.
func (a *Artifact) Tensor(name string) (Tensor, bool)

type Tensor struct {
	Name   string
	DType  DType   // Int8, Float32, Uint8
	Shape  []int
	Data   []byte  // aligned view into the artifact buffer — NOT a copy
	Scales []float32 // per-row dequantisation scales; nil when DType is Float32
}

func (t Tensor) Float32s() ([]float32, error) // zero-copy on LE when DType is Float32
func (t Tensor) Row(i int) []byte

// Open reads an artifact from a byte slice. It does not copy tensor data:
// the returned Artifact aliases src, which must outlive it.
func Open(src []byte) (*Artifact, error)

// Load fetches url, verifies it, caches it in the browser, and opens it. A second
// call for the same id and version reads from the cache and never touches the
// network.
func Load(ctx *context.Context, cfg LoadConfig) (*Artifact, error)

type LoadConfig struct {
	URL     string
	Conn    storage.Conn  // where the cache lives — injected, not constructed
	OnProgress func(done, total int64) // 100 MB deserves a progress bar
}
```

`Conn` se inyecta en vez de que el paquete abra su propia conexión a IndexedDB: la
aplicación ya tiene una, y una segunda conexión a la misma base de datos es fuente de
deadlocks en cambios de versión.

## Reglas de caché

- Clave: `id + "@" + version`. Una actualización nunca sirve pesos viejos.
- Escribir al caché **solo después** de leer el cuerpo completo y verificar que su largo y
  checksum coincidan con la cabecera. Un artifact de 100 MB cacheado a medias que carga sin
  error y produce vectores basura es el peor resultado disponible acá.
- Consultar `navigator.storage.estimate()` antes de escribir; una escritura de caché que
  empuje al origen por encima de la cuota puede desalojar el corpus de documentos del
  usuario.
- `Evict(id)` para limpieza explícita, y una nota documentada de que el caché está sujeto
  al desalojo del navegador como cualquier otro dato de IndexedDB.

## Agregados de la fase 5

El encoder transformer necesita el mismo lector con más dtypes (fp16, int4) y un camino
`cmd/convert` desde safetensors. Aditivo; sin cambio de formato.

## Tests

| Test | Verifica |
|---|---|
| `TestOpen_RoundTrip` | un artifact de fixture escrito por `cmd/convert` se lee con tensores idénticos |
| `TestOpen_BadMagic` | error |
| `TestOpen_TruncatedTensorData` | error, no un slice corto |
| `TestOpen_ChecksumMismatch` | error |
| `TestOpen_Alignment` | el offset de datos de todo tensor está alineado a 64 bytes |
| `TestTensor_Float32sZeroCopy` | el slice devuelto aliasa el buffer fuente |
| `TestArtifact_TensorByName` | encuentra por nombre, y devuelve `false` para uno que no está |
| `TestTensor_Row` | la fila i de una tabla int8 son los bytes correctos |
| `TestLoad_CachesAfterFirstFetch` | la segunda llamada no emite request HTTP (fetcher mock) |
| `TestLoad_PartialBodyNotCached` | un cuerpo truncado deja el caché vacío |
| `TestLoad_VersionBumpRefetches` | |
| `TestLoad_ProgressCallback` | se llama monotónicamente, terminando en el total |

## Checklist de aceptación

```bash
go vet ./...
gotest
gotest -tinygo
GOOS=js GOARCH=wasm go build ./...
grep -rn "map\[" --include="*.go" . | grep -v _test.go        # → vacío
grep -rn '"errors"\|"fmt"' --include="*.go" . | grep -v _test.go  # → vacío
```

`cmd/convert` se testea contra un **artifact sintético** construido en el propio test, no
contra un modelo descargado: el modelo todavía no está elegido (`PENDING_ITEMS.md` P1b) y
este plan no lo necesita. Si tenés un `.safetensors` a mano, `go run ./cmd/convert -in
model.safetensors -out model.wtypw -quantize int8` es una verificación extra, no parte de
la puerta.
