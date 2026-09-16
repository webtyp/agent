---
PLAN: "feat: webtyp/vectordb — almacén de documentos con recuperación kNN"
TAG: v0.1.0
EXECUTOR: unassigned
REVIEWER: none
REPO: webtyp/vectordb (por crear)
---

> Repositorio nuevo. Este archivo vive en `agent/docs/plans/` hasta que `webtyp/vectordb`
> exista; entonces se mueve a `vectordb/docs/PLAN.md` sin modificaciones.
> Índice maestro: https://github.com/webtyp/agent/blob/main/docs/PLAN.md

# Plan — `webtyp/vectordb`

## Responsabilidad única

El almacén: entran documentos, salen documentos rankeados. Es dueño del esquema, del
layout de shards, del índice en memoria, de la política de desalojo y de la cuota. **No**
es dueño de ninguna aritmética (eso es `webtyp/vector`), **ni** de la generación de
embeddings (eso es `webtyp/embed`), **ni** de ningún conocimiento sobre IndexedDB (eso es
`webtyp/indexdb`, alcanzado a través de `storage.Conn`).

Este es el port a Go de `webtyp/vector-storage`. El mapa de port, y la lista de
comportamientos deliberadamente no portados, está en el `docs/PLAN.md` de aquel
repositorio — leelo antes de escribir código acá.

## Obligación de licencia

El original en TypeScript es MIT de Nitai Aharoni. Este repositorio **debe** embarcar un
archivo `NOTICE` acreditando al autor y reproduciendo los términos MIT junto a su propia
licencia. Índice maestro **D6**. No es opcional.

## Dependencias

`webtyp.com/vector`, `webtyp.com/storage`, `webtyp.com/model`, `webtyp.com/embed` (solo la
interfaz del puerto), `webtyp.com/fmt`, `webtyp.com/context`.

**No** importa `webtyp.com/indexdb`. El backend llega como un `storage.Conn` inyectado, que
es lo que hace que el mismo almacén corra en un navegador y en un servidor, y lo que lo
hace testeable contra `storage/mem` en Go plano.

## Esquema

Tres object stores / tablas, declaradas como valores `model.Definition` para que todo
backend las cree igual.

```go
// vec_docs — one row per document. Text and metadata are read ONLY for the final
// top-k, never during scoring (master index D2).
var DocModel = model.Definition{
	Name: "vec_docs",
	Fields: model.Fields{
		{Name: "id",      Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "text",    Type: model.Text(), NotNull: true},
		{Name: "meta",    Type: model.Raw()},                    // opaque JSON payload
		{Name: "tags",    Type: model.Text()},                   // "|a|b|c|", LIKE-filterable
		{Name: "hash",    Type: model.Text(), NotNull: true},    // content hash, dedup
		{Name: "created", Type: model.Int(),  NotNull: true},
		{Name: "hits",    Type: model.Int(),  NotNull: true},
		{Name: "shard",   Type: model.Int(),  NotNull: true},
		{Name: "slot",    Type: model.Int(),  NotNull: true},
	},
}

// vec_shards — one row per ShardSize vectors, as a single blob.
var ShardModel = model.Definition{
	Name: "vec_shards",
	Fields: model.Fields{
		{Name: "id",    Type: model.Int(),  DB: &model.FieldDB{PK: true}},
		{Name: "count", Type: model.Int(),  NotNull: true},
		{Name: "data",  Type: model.Blob(), NotNull: true},
	},
}

// vec_index — exactly one row. Rejects an arena that does not match the corpus.
var IndexModel = model.Definition{
	Name: "vec_index",
	Fields: model.Fields{
		{Name: "id",         Type: model.Text(), DB: &model.FieldDB{PK: true}},
		{Name: "dim",        Type: model.Int(),  NotNull: true},
		{Name: "model_id",   Type: model.Text(), NotNull: true}, // which embedder produced these
		{Name: "shard_size", Type: model.Int(),  NotNull: true},
		{Name: "version",    Type: model.Int(),  NotNull: true},
	},
}
```

`vec_shards.data` es `model.Blob()`, no `model.Vector(dim)`: un shard contiene
`count × dim` floats, no un vector, así que una dimensión fija sería incorrecta. La
concordancia de dimensión la impone `vec_index.dim` contra `len(data)/4/count`.

`model_id` importa más de lo que parece: vectores de dos modelos de embeddings distintos
no son comparables, y mezclarlos produce resultados plausibles en vez de un error. Cargar
un corpus cuyo `model_id` difiere del embedder configurado tiene que **fallar**, con un
mensaje que diga que el corpus necesita reindexarse.

La codificación de `tags` (`|a|b|c|`) es un compromiso de v1 — `model` no tiene un tipo de
campo de slice de strings. Es filtrable con `LIKE '%|tag|%'`, que alcanza para el filtro
sobre el arreglo de cabeceras de más abajo, ya que el filtrado ocurre en RAM de todos
modos. Índice maestro **O3**.

## API

```go
type Config struct {
	Conn      storage.Conn      // required — the backend, injected
	Embedder  embed.Embedder    // required — text → vectors
	IDGen     model.IDGenerator // required — no concrete generator constructed here
	ShardSize int               // default 1024
	MaxDocs   int               // 0 = unbounded; LRU eviction above this
	MaxBytes  int64             // 0 = derive from navigator.storage.estimate()
}

// New opens the store and loads the index into memory. It RETURNS AN ERROR rather
// than racing a background load — the TypeScript original's constructor kicks off an
// un-awaited load, so every early call silently searches an empty corpus.
func New(ctx *context.Context, cfg Config) (*Store, error)

func (s *Store) Add(ctx *context.Context, docs ...Doc) ([]string, error)
func (s *Store) Search(ctx *context.Context, q Query) ([]Match, error)
func (s *Store) Delete(ctx *context.Context, ids ...string) error
func (s *Store) Len() int
func (s *Store) Close() error

type Doc struct {
	ID   string          // empty → IDGen.NewID()
	Text string
	Meta model.RawJSON
	Tags []string
}

type Query struct {
	Text        string    // embedded via Config.Embedder
	Vector      []float32 // pre-computed; takes precedence over Text
	K           int       // default 4, matching the TypeScript original
	IncludeTags []string  // AND
	ExcludeTags []string  // AND NOT
	MinScore    float32
}

type Match struct {
	Doc
	Score float32 // cosine similarity in [-1, 1]
}
```

Notá lo que está ausente: `Search` no devuelve el embedding de la consulta como hace
`similaritySearch`. Nadie lo usaba, y devolverlo obliga a una copia.

## Índice en memoria

```go
type header struct {
	id      string
	tags    string
	created int64
	hits    int32
	deleted bool
}
```

`Store` mantiene una `*vector.Arena` más un `[]header`, paralelos por posición. Ese es todo
el índice de búsqueda. El texto y los metadatos quedan en disco.

El costo residente es `N × Dim × 4` más unos 64 bytes de cabecera por documento: 10 000
documentos a 384 dims son unos 15,6 MB. Pasados los ~100 000 documentos esto requiere
cuantización int8, que es fase 5 — hasta entonces `MaxDocs` es la baranda y excederlo
desaloja.

## Camino de escritura

1. Hashear el texto de cada documento; descartar los cuyo hash ya esté en el corpus. El
   original en TypeScript compara cadenas de texto contra todo el arreglo por inserción —
   O(N·M) para un lote.
2. `Embedder.Embed` para el lote completo, hacia un `[]float32` de trabajo.
3. Agregar a la arena (normalizando en el camino).
4. Asignar shard y posición; marcar los shards tocados como sucios.
5. **Una transacción** (`storage.TxExecutor`, ver §3 del plan de `indexdb`) escribiendo las
   filas de documentos y los blobs de shards sucios juntos. Vectores y texto nunca deben
   ser observables fuera de sincronía.
6. Desalojar si se pasa del presupuesto, antes de confirmar.

Solo se reescriben los shards sucios. El original reescribe el corpus entero en cada
escritura, incluso después de una lectura — `similaritySearch` llama a
`saveToIndexDbStorage()` para persistir los contadores de hits.

## Camino de lectura

1. Embeber la consulta (o tomar `Query.Vector`), normalizar.
2. Construir el closure `keep` sobre el arreglo de cabeceras: saltear borrados, aplicar
   filtros de tags. El filtrado ocurre **antes** del scoring, así que una consulta filtrada
   es más barata, no más cara.
3. `arena.Search(query, keep, topk)` — cero allocations, sin cruzar el puente JS.
4. Cargar solo las k filas ganadoras de `vec_docs` por clave primaria.
5. Incrementar `hits` en el arreglo de cabeceras **en memoria**; volcar de forma diferida
   (en `Close`, en la próxima escritura, o cada N búsquedas). Una lectura no debe disparar
   una reescritura del corpus completo.

## Desalojo y cuota

El orden es el del original y se mantiene: `hits` ascendente, luego `created` ascendente —
menos usado, más viejo primero.

La medición de tamaño no es la del original. `getObjectSizeInMB` serializa el corpus entero
con `JSON.stringify` solo para medirlo, y mide lo que no es. Acá:
- Los bytes de la arena se conocen aritméticamente: `N × Dim × 4`.
- La sobrecarga por fila se estima por documento y se calibra una vez.
- `navigator.storage.estimate()` da el presupuesto real del navegador, detrás de un archivo
  con build tag para que el paquete siga compilando para un target de servidor.
- `navigator.storage.persist()` se solicita en `New` — sin eso el navegador puede desalojar
  la base de datos entera bajo presión, en silencio.

El desalojo es un borrado lógico en la cabecera (`deleted = true`) más una compactación del
shard cuando cae por debajo de la mitad. La compactación renumera posiciones, así que va en
la misma transacción que cualquier otra escritura.

## Tests

La suite corre **dos veces**: contra `storage/mem` en Go estándar, y contra
`webtyp.com/indexdb` en un navegador bajo `gotest -tinygo`. Mismos cuerpos de test, distinta
factory — el patrón que `indexdb/tests/conformance_test.go` ya usa.

Un `MockEmbedder` que devuelva vectores determinísticos a partir de un hash del texto es
obligatorio: los tests del almacén no deben depender de un modelo real.
(`DEFAULT_LLM_SKILL.md` §2 — toda interfaz externa lleva un mock.)

| Test | Verifica |
|---|---|
| `TestAdd_ThenSearchFindsIt` | el round-trip obvio |
| `TestAdd_DeduplicatesByHash` | el mismo texto dos veces da un documento |
| `TestAdd_BatchOneTransaction` | 1024 documentos usan una transacción, verificado con el recorder del mock |
| `TestSearch_RanksByCosine` | vectores conocidos, orden esperado calculado a mano |
| `TestSearch_RespectsK` | incluyendo k > tamaño del corpus |
| `TestSearch_IncludeExcludeTags` | los filtros se aplican antes del scoring |
| `TestSearch_MinScore` | los matches bajo el umbral se descartan |
| `TestSearch_EmptyCorpus` | devuelve vacío, no error, y no hace pánico |
| `TestSearch_DoesNotRewriteCorpus` | una búsqueda emite cero escrituras a la tabla de shards — el test de regresión de la escritura en camino de lectura del original |
| `TestReopen_LoadsIndex` | cerrar, reabrir sobre el mismo `Conn`, la búsqueda sigue funcionando |
| `TestReopen_ModelMismatchFails` | un corpus escrito por el modelo A se rechaza al configurar el modelo B, con "reindexar" en el mensaje |
| `TestReopen_DimMismatchFails` | `vec_index.dim` en desacuerdo con el largo del blob del shard es error |
| `TestDelete_RemovesFromResults` | y sobrevive a una reapertura |
| `TestEvict_LeastUsedOldestFirst` | el contrato de ordenamiento |
| `TestEvict_CompactsShards` | un shard a medio llenar se compacta, las posiciones se renumeran, la búsqueda sigue correcta |
| `TestNew_ReturnsBeforeUse` | una búsqueda inmediatamente después de `New` ve el corpus completo — el test de regresión de la carga sin await del original |

## Checklist de aceptación

```bash
go vet ./...
gotest                 # backend mem, Go estándar
gotest -tinygo         # backend indexdb, navegador
ls NOTICE              # obligación de licencia, índice maestro D6
grep -rn "webtyp.com/indexdb" --include="*.go" . | grep -v _test.go   # → vacío
grep -rn "syscall/js" --include="*.go" . | grep -v _wasm.go           # → solo el código de cuota con build tag
```
