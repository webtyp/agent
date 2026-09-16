---
PLAN: "feat: búsqueda semántica nativa en el navegador — índice maestro"
TAG: v0.2.0
EXECUTOR: unassigned
REVIEWER: none
---

> Este es el **plan índice**. No le pertenece código. Cada repositorio listado en §5 lleva
> su propio `docs/PLAN.md` con el diff concreto; este archivo es dueño de la arquitectura,
> los contratos compartidos, el orden de construcción y los criterios de aceptación que
> cruzan repositorios.
>
> Los planes de los repositorios que **todavía no existen** viven en [`docs/plans/`](plans/)
> hasta que el repositorio se cree; entonces el archivo se mueve a su `docs/PLAN.md` sin
> modificaciones.
>
> **Nota de idioma:** la prosa va en español. Los bloques de código se mantienen con
> comentarios en inglés porque son código literal destinado a los repositorios, cuyos
> comentarios de fuente son en inglés.

# Plan — Búsqueda semántica en el navegador, sobre IndexedDB

## 1. Objetivo

Ejecutar búsqueda semántica **enteramente en el navegador**: generación de embeddings,
almacenamiento de vectores y recuperación por k-vecinos más cercanos, sin viaje al
servidor en tiempo de consulta y sin dependencia de ninguna librería JavaScript. La
persistencia es `webtyp.com/indexdb`, extendido donde su API actual no alcanza a expresar
el problema.

El `MemoryStore` del agente se reescribe una sola vez contra `storage.Conn`, de modo que
el mismo código de memoria corra sobre un backend SQL (servidor) y sobre IndexedDB
(navegador).

## 2. Por qué esto reemplaza al estudio de SQLite

El diseño anterior ([`docs/history/MEMORY_SQLITE.md`](history/MEMORY_SQLITE.md)) asumía un
único motor de almacenamiento en todas partes: SQLite en el servidor, el mismo SQLite
compilado a WASM en el navegador, con `sqlite-vec` para la búsqueda vectorial. Dos
premisas fallaron:

- `modernc.org/sqlite` es Go puro pero **no** compila bajo TinyGo para
  `GOOS=js GOARCH=wasm`. Es una dependencia exclusiva de backend.
- `sqlite-vec` es una extensión en C. Usarla en el navegador significa embarcar un segundo
  runtime WASM más una capa VFS en JavaScript — exactamente la dependencia JS que este
  ecosistema existe para evitar, y una segunda copia de cada byte almacenado.

Lo que sobrevive de ese estudio y se **reutiliza tal cual**: la categorización de memoria
(corto plazo / episódica / semántica / de acciones), la regla de alcance de conocimiento
global por `session_id IS NULL`, y Reciprocal Rank Fusion como estrategia eventual de
recuperación híbrida.

Lo que lo reemplaza: el navegador ya trae un almacén persistente, transaccional y
estructurado — IndexedDB — y este ecosistema ya tiene un driver para él. El isomorfismo
sube un nivel: en vez de "el mismo motor en todas partes", pasa a ser "el mismo contrato
`storage.Conn` en todas partes", que es justamente para lo que se construyó
`webtyp.com/storage`.

## 3. Arquitectura

```
                          ┌──────────────────────────────┐
                          │  agent  (MemoryStore)        │
                          │  reescrito sobre storage.Conn│
                          └──────────────┬───────────────┘
                                         │
                          ┌──────────────▼───────────────┐
                          │  vectordb                    │  documentos, kNN,
                          │  el almacén                  │  filtros, LRU, cuota
                          └──┬──────────┬──────────┬─────┘
                             │          │          │
         ┌───────────────────▼──┐  ┌────▼─────┐  ┌─▼─────────────────┐
         │  vector              │  │  embed   │  │  storage          │
         │  math, arena, top-k, │  │  puerto +│  │  contrato Conn    │
         │  códec LE            │  │  adaptad.│  └──┬─────────────┬──┘
         └──────────────────────┘  └─┬──┬──┬──┘     │             │
                                     │  │  │        │             │
              ┌──────────────────────┘  │  └────────┼─────┐       │
              │              ┌──────────┘           │     │       │
    ┌─────────▼────────┐  ┌──▼──────────────┐  ┌────▼─────▼───┐ ┌─▼─────────────┐
    │  tokenizer       │  │  weights        │  │  indexdb     │ │ sqlt/postgres │
    │  texto → ids     │  │  artifact + IDB │  │  navegador   │ │ servidor      │
    └──────────────────┘  │  caché          │  └──────────────┘ └───────────────┘
                          └──┬──────────────┘
                             │
        ╔════════════════════▼═══════════════════════════════════╗
        ║  FASE 5 — solo si el recall de la fase 3 no alcanza     ║
        ║    webgpu  (navigator.gpu)  →  nn  (encoder en WGSL)    ║
        ║    → un segundo adaptador embed, misma interfaz         ║
        ╚════════════════════════════════════════════════════════╝
```

Cada flecha es una dependencia de compilación. No hay ciclos y ningún repositorio depende
de `agent`.

## 4. Decisiones globales de diseño

Se deciden **acá**, una sola vez. Los planes por repositorio referencian esta sección en
lugar de volver a argumentarla.

### D1 — Un vector es `[]byte` en disco y una porción de una arena compartida en memoria

En disco el tipo canónico es `model.Blob()` → `[]byte` → un `Uint8Array` de JS bajo
structured clone. En Go, los vectores **nunca** son un `[]float32` por documento: viven en
una única arena contigua `[]float32` de `N × Dim`, donde el documento *i* ocupa
`arena[i*Dim : (i+1)*Dim]`.

Justificación, en el orden que importa:

1. **`js.ValueOf` no puede transportar un vector, y fallar cuesta la aplicación entera.**
   `execute.go:create` arma un `map[string]any` y se lo entrega a `store.Call("add", …)`,
   que pasa por `js.ValueOf`. `js.ValueOf` acepta `[]any` pero **ni `[]byte` ni
   `[]float32`**, y hace pánico con cualquier otra cosa. Bajo TinyGo
   `GOOS=js GOARCH=wasm` **no hay `recover()`** (`getStore` ya lo documenta en ese mismo
   repositorio), así que ese pánico es un crash irrecuperable, no un valor de error.
   Codificar un vector de 384 dims como `[]any` para sortearlo cuesta 384 allocations
   boxeadas y ~3 KB de heap JS por documento, contra 1536 bytes de datos reales. Inviable.

2. **`js.CopyBytesToJS` / `js.CopyBytesToGo` son las únicas primitivas de copia masiva, y
   TinyGo implementa ambas.** Son un `memcpy` entre la memoria lineal WASM y un
   `Uint8Array`. El camino `[]float32` → (reinterpretación O(1)) → `[]byte` → un memcpy →
   `Uint8Array` no tiene representación intermedia: ni JSON, ni base64, ni boxing.

3. **`FieldBlob` ya existe** (`model/field.go:13`) y ya está cableado en `IsZeroPtr`,
   `ValuesFrom` y los codecs. Introducir un `FieldType` nuevo obligaría a editar cada
   `switch` exhaustivo en `model`, `storage/mem`, `sqlt`, `postgres` e `indexdb` — un
   cambio incompatible en seis repositorios para no comprar nada.

4. **`[]byte` es la única representación isomórfica.** Mapea a `BLOB` (SQLite), `BYTEA`
   (Postgres) y `Uint8Array` (IndexedDB). Un `Float32Array` sería marginalmente más rápido
   en el navegador y no tiene contraparte SQL.

5. **La arena es lo que realmente hace rápidas las consultas.** El scoring lee solamente
   memoria lineal WASM, así que una consulta cruza el puente JS **cero veces**. No asigna
   nada por candidato. El GC conservador de TinyGo ve un objeto grande en vez de N slices
   pequeños — esto pesa mucho más bajo TinyGo que bajo Go estándar. Y el producto punto
   recorre memoria contigua, con localidad de caché óptima.

6. **Los vectores se guardan normalizados L2**, lo que convierte la similitud coseno en un
   producto punto simple. Esto elimina un `sqrt` y una división por documento por consulta.
   (El original en JS precomputa la magnitud pero igual divide N veces por búsqueda.)

**Costo, dicho con honestidad:** la memoria residente es `N × Dim × 4` bytes. 10 000
documentos a 384 dims son 15 MB — aceptable. 100 000 documentos son 150 MB, que es donde
la cuantización int8 (÷4) se vuelve obligatoria. La cuantización es **fase 5**, no v1.

### D2 — Los vectores se persisten por shards; los documentos, por fila

Una fila de `vec_shards` contiene `ShardSize` (1024 por defecto) vectores como un único
blob. El arranque en frío pasa a `N/1024` deserializaciones de structured clone en vez de
`N`, y cada una es un solo `CopyBytesToGo` directo a su offset en la arena.

El texto y los metadatos viven en un object store **separado**, que se lee únicamente para
el top-k final — nunca durante el scoring. En RAM, junto a la arena, vive un arreglo
compacto de cabeceras (`id`, `tags`, `created`, `hits`, `deleted`) usado para el
prefiltrado.

Una consecuencia que vale señalar: como el camino caliente de lectura es "un puñado de
filas grandes de una tabla", `vectordb` **no necesita ninguna API de scan en streaming**
de parte de `storage`. Eso reduce el cambio en `storage` a conformance de blobs más
inserción por lote.

### D3 — IndexedDB no puede indexar un vector. Toda consulta kNN es un barrido completo.

Ningún árbol B sobre un espacio de 384 dimensiones ayuda. Esto no es una limitación a
sortear en v1; es la forma del problema. Por eso el diseño minimiza el **costo por
candidato** (D1, D2) en vez de intentar evitar candidatos. Los índices aproximados
(HNSW/IVF) quedan explícitamente fuera de alcance hasta que exista un corpus que los
necesite.

### D4 — Los embeddings se producen en el navegador, en dos fases detrás de un mismo puerto

`embed.Embedder` es el contrato único. Se embarcan dos implementaciones detrás de él:

- **Fase 3 — embeddings estáticos.** Una tabla de embeddings de tokens destilada (familia
  model2vec / "potion"): tokenizar, buscar, promediar (mean-pool), normalizar. Sin
  atención, sin forward pass, sin GPU. Go puro, compatible con TinyGo hoy, ~30 MB
  cuantizado. La calidad de recuperación queda por debajo de un encoder completo pero es
  sólida, y entrega un pipeline funcionando de punta a punta antes de que exista una sola
  línea de código de GPU.
- **Fase 5 — encoder transformer sobre WebGPU.** Inferencia completa de
  sentence-transformer vía `navigator.gpu`, en Go + WGSL de principio a fin.

Ambos corren enteramente en el navegador. La fase 5 es un **adaptador nuevo**, no una
reescritura: `vectordb`, `indexdb`, `storage` y `model` quedan intactos.

### D5 — El español es una restricción de selección, y domina el tamaño del modelo

`docs/EFFICIENT_SLM.md` pone el español como requisito de primer orden, lo que descarta
los encoders solo-inglés (`all-MiniLM-L6-v2` y familia). Los modelos multilingües cargan
un vocabulario de ~250 k tokens, y la tabla de embeddings sola pesa
`250 000 × 384 × 4 ≈ 384 MB` en fp32 — muchísimo más que el cuerpo del transformer.

Por lo tanto: **la tabla de embeddings se embarca cuantizada a int8 con escalas por fila**
(~96 MB, y ~30 MB para un modelo estático de 256 dims), y el artifact del modelo se
cachea en IndexedDB después de la primera descarga, de modo que se baja una vez por
navegador y no una vez por sesión. Los modelos candidatos y la elección final se registran
en [`docs/plans/embed.md`](plans/embed.md).

### D6 — Licencias

`webtyp/vector-storage` es un fork de `nitaiaharoni1/vector-storage` (MIT). Todo
repositorio Go que porte su lógica — `vectordb` por sobre todo — debe llevar un archivo
`NOTICE` acreditando al autor original y reproduciendo los términos MIT. No es opcional.

## 5. Mapa de repositorios

| Repositorio | Estado | Responsabilidad única | Fase | Plan |
|---|---|---|---|---|
| `webtyp/model` | modificar | kind `Vector(dim)` sobre `FieldBlob` | 1 | [`model/docs/PLAN.md`](https://github.com/webtyp/model/blob/main/docs/PLAN.md) |
| `webtyp/storage` | modificar | conformance de blob + contrato de inserción por lote | 1 | [`storage/docs/PLAN.md`](https://github.com/webtyp/storage/blob/main/docs/PLAN.md) |
| `webtyp/indexdb` | modificar | E/S de blobs, una transacción por lote | 1 | [`indexdb/docs/PLAN.md`](https://github.com/webtyp/indexdb/blob/main/docs/PLAN.md) |
| `webtyp/vector` | **nuevo** | math vectorial, arena, top-k, códec LE | 2 | [`docs/plans/vector.md`](plans/vector.md) |
| `webtyp/vectordb` | **nuevo** | almacén de documentos + kNN + filtros + LRU | 2 | [`docs/plans/vectordb.md`](plans/vectordb.md) |
| `webtyp/tokenizer` | **nuevo** | texto → ids de tokens | 3 | [`docs/plans/tokenizer.md`](plans/tokenizer.md) |
| `webtyp/weights` | **nuevo** | formato de artifact + caché en navegador | 3 | [`docs/plans/weights.md`](plans/weights.md) |
| `webtyp/embed` | **nuevo** | puerto `Embedder` + adaptador estático | 3 | [`docs/plans/embed.md`](plans/embed.md) |
| `webtyp/agent` | modificar | `MemoryStore` sobre `storage.Conn` | 4 | este repositorio, §7 |
| `webtyp/webgpu` | **nuevo** | bindings de `navigator.gpu` | 5 | [`docs/plans/webgpu.md`](plans/webgpu.md) |
| `webtyp/nn` | **nuevo** | kernels WGSL + grafo del encoder | 5 | [`docs/plans/nn.md`](plans/nn.md) |
| `webtyp/vector-storage` | congelar | referencia histórica JS + mapa de port | 1 | [`vector-storage/docs/PLAN.md`](https://github.com/webtyp/vector-storage/blob/main/docs/PLAN.md) |

## 6. Orden de construcción y puertas de fase

Las fases son estrictamente ordenadas: una fase no arranca hasta que la puerta de la
anterior pase. Dentro de una fase, los repositorios de una misma línea pueden avanzar en
paralelo.

### Fase 1 — Desbloquear la persistencia (sin ML)
`model` → `storage` → `indexdb`, en ese orden (cada uno depende del release del anterior).

**Puerta:** un `[]byte` de 1536 bytes hace round-trip por IndexedDB en un navegador real
con igualdad byte a byte, e insertar 1024 filas usa **una** transacción. Demostrado por
`indexdb/tests/` bajo `gotest -tinygo` y por la suite de conformance de `storage` pasando
en `mem`, `sqlt` e `indexdb`.

### Fase 2 — Math y almacén (sin ML)
`vector` → `vectordb`.

**Puerta:** kNN sobre 10 000 vectores sintéticos devuelve el top-k correcto (verificado
contra una implementación de referencia ingenua) con **cero allocations por consulta**,
medido con `testing.AllocsPerRun`. El mismo test pasa contra `storage/mem` en Go estándar
y contra `indexdb` en el navegador.

### Fase 3 — Embeddings en el navegador
`tokenizer` y `weights` en paralelo, luego `embed`.

**Puerta:** un corpus real en español se indexa y se busca de punta a punta en el
navegador, offline después de la primera carga, con un recall@10 documentado contra una
referencia.

### Fase 4 — Memoria del agente
`agent`: `MemoryStore` reescrito sobre `storage.Conn`, `SearchKnowledge` gana su camino
semántico.

**Puerta:** la matriz DDT completa de `DEFAULT_LLM_SKILL.md` §2 pasa contra un backend SQL
y contra `indexdb`, desde una sola implementación.

### Fase 5 — Encoder WebGPU y optimización
`webgpu` → `nn` → un segundo adaptador de `embed`. Después cuantización int8 en `vector`,
después BM25 léxico + fusión RRF.

**Condición de entrada, no solo dependencia:** la fase 5 arranca únicamente si el harness
de evaluación de `embed` demuestra que el embedder estático de la fase 3 no alcanza en el
conjunto de prueba en español. Si el recall@10 es adecuado, **no** construir `nn` es el
resultado correcto. Ver [`docs/plans/nn.md`](plans/nn.md).

**Puerta:** el encoder produce vectores que coinciden con la implementación de referencia
dentro de una tolerancia documentada, y `vectordb` no cambia en nada por su llegada.

## 7. Cambios que le pertenecen a este repositorio (`agent`)

Alcance, a detallar cuando la fase 2 libere:

1. **`memory.go` → `memory_storage.go`.** Se saca `modernc.org/sqlite`. Una sola
   implementación de `MemoryStore` escrita contra `storage.Conn`, de modo que el backend
   pasa a ser una dependencia inyectada — que es lo que `DEFAULT_LLM_SKILL.md` §1 ya exige
   y lo que la implementación actual con SQL directo viola.
2. **El esquema como valores `model.Definition`**, no como cadenas de DDL, para que
   `indexdb` pueda crear los object stores desde la misma declaración que un backend SQL
   convierte en DDL.
3. **`SearchKnowledge` gana un camino semántico** a través de `vectordb`, con un
   `embed.Embedder` inyectado. FTS5 léxico no existe fuera de SQLite: sobre `indexdb`, la
   mitad léxica de RRF espera al índice BM25 de la fase 5. Hasta entonces `SearchKnowledge`
   es léxico por `LIKE` y semántico por vectores.
4. **`LLMConfig` gana un `Embedder` opcional**, como ya anticipaba el estudio histórico.
5. **Migración de module path** `github.com/tinywasm/agent` → `webtyp.com/agent`. Todos los
   demás repositorios de este plan ya migraron; `agent` no puede depender de
   `webtyp.com/storage` mientras publica el path viejo sin confundir la actualización de
   módulos dependientes de `gopush`. Es un prerrequisito de la fase 4, no parte de ella.
6. Actualizar la lista de dependencias permitidas de `DEFAULT_LLM_SKILL.md`:
   `modernc.org/sqlite` sale del código de producción; entran `webtyp.com/storage`,
   `webtyp.com/vectordb` y `webtyp.com/embed`.

## 8. Riesgos

| Riesgo | Impacto | Mitigación |
|---|---|---|
| La fase 5 (encoder WebGPU) es un proyecto grande y abierto | La entrega se corre indefinidamente | El embedder estático de la fase 3 hace que el producto funcione sin ella; la fase 5 es un cambio de adaptador detrás de `embed.Embedder` |
| El artifact multilingüe pesa 100 MB+ | Primera carga inusable | Tabla de embeddings int8, caché en IndexedDB tras el primer fetch, y un fallback más chico documentado |
| La cuota de IndexedDB desaloja el índice | Pérdida silenciosa de datos | `navigator.storage.persist()` al iniciar, `estimate()` antes de escribir, desalojo LRU propio antes que el del navegador |
| `jsvalue.ScanValue` podría no manejar `Uint8Array` | Fase 1 bloqueada | **Verificar primero** — es la tarea de apertura del plan de `indexdb` |
| La arena excede la memoria del navegador pasados ~100 k docs | OOM | Techo documentado, cuantización int8 en la fase 5 |
| Vectores y texto se desincronizan en una escritura parcial | Resultados corruptos | Una sola transacción abarcando ambos stores; una fila de cabecera `dim`/`model_id` rechaza una arena que no corresponde al cargar |

## 9. Decisiones abiertas

- **O1.** Modelo y dimensión exactos de embeddings para la fase 3 (ver `plans/embed.md`
  §2). Restringido por D5. Supuesto de trabajo hasta decidir: 256 dims, estático
  multilingüe.
- **O2.** ¿`webtyp/binary` ya provee un códec little-endian de float32? Si lo hace,
  `vector` depende de él en vez de definir el suyo. **Verificar antes de escribir
  `vector`.**
- **O3.** Forma del filtrado por metadatos. Supuesto v1: `model.RawJSON` opaco para el
  payload más una columna de tags delimitada y filtrable con `LIKE`, porque `model` no
  tiene un tipo de campo de slice de strings. Revisar si el filtrado se vuelve camino
  caliente.
- **O4.** Si `vectordb` debería exponer una capacidad opcional `VectorSearcher` para que
  `postgres` delegue en pgvector del lado del servidor. Diferido: no cambia nada del código
  del navegador.
