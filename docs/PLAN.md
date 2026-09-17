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
> modificaciones. Los planes de los repositorios que **ya existen** están en su
> `docs/PLAN.md` y §5 enlaza a ellos.
>
> **Los cinco repositorios de las fases 3 y 5 no se crean todavía**: `PENDING_ITEMS.md` P1
> decide si `tokenizer`, `weights`, `embed`, `webgpu` y `nn` existen. Crear un repositorio
> que una decisión abierta puede borrar es deuda, no adelanto.
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

El `MemoryStore` del agente se reescribe una sola vez contra `webtyp.com/orm`, de modo que
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
`storage.Query` en todas partes", que es justamente para lo que se construyeron
`webtyp.com/storage`, `webtyp.com/orm` y `webtyp.com/ddl`.

## 3. Arquitectura

```
                      ┌──────────────────────────────────┐
                      │  agent — orquestador             │
                      │  contrato MemoryStore, 0 storage │
                      └────────────────┬─────────────────┘
                      ┌────────────────▼─────────────────┐
                      │  agentmemory                     │  implementa el
                      │  sobre orm + ddl                 │  contrato
                      └────────────────┬─────────────────┘
                                       │
                      ┌────────────────▼─────────────────┐
                      │  vectordb                        │  documentos, kNN,
                      │  el almacén                      │  filtros, LRU, cuota
                      └──┬───────────┬──────────────┬────┘
                         │           │              │
      ┌──────────────────▼───┐  ┌────▼──────┐  ┌────▼──────────────────┐
      │  vector              │  │  embed    │  │  orm  /  ddl          │
      │  math, arena, top-k, │  │  PUERTO   │  │  DML agnóstico + DDL  │
      │  códec LE            │  │  (fase 2) │  └────┬─────────────┬────┘
      └──────────────────────┘  └─────▲─────┘       │             │
                                      │             │             │
                   adaptador estático (fase 3)      │             │
              ┌───────────────┬───────┘             │             │
      ┌───────▼──────────┐ ┌──▼──────────────┐ ┌────▼─────────┐ ┌─▼─────────────┐
      │  tokenizer       │ │  weights        │ │  storage     │ │ storage       │
      │  texto → ids     │ │  artifact + IDB │ │  + indexdb   │ │ + sqlt/postgr │
      └──────────────────┘ │  caché          │ │  navegador   │ │ servidor      │
                           └──┬──────────────┘ └──────────────┘ └───────────────┘
     ╔════════════════════════▼═══════════════════════════════╗
     ║  FASE 5 — solo si el recall de la fase 3 no alcanza     ║
     ║    webgpu  (navigator.gpu)  →  nn  (encoder en WGSL)    ║
     ║    → un segundo adaptador embed, misma interfaz         ║
     ╚═════════════════════════════════════════════════════════╝
```

Cada flecha es una dependencia de compilación. No hay ciclos y ningún repositorio depende
de `agent`.

Dos aristas que el diagrama hace explícitas porque una versión anterior de este plan las
tenía mal:

- **`vectordb` importa `embed`.** No el adaptador — sólo la interfaz `embed.Embedder`
  (`plans/vectordb.md` §3 y `Config.Embedder`). Por eso el **puerto** `embed` es fase 2 y
  el **adaptador estático** es fase 3. Ver §6.
- **`indexdb` encoda sus propios bytes.** Lee por `jsvalue` (que funciona) y escribe por su
  propio `toJSValue` (porque el writer de `jsvalue` corrompe binario). Ver §5 nota (b) y §8.

## 4. Decisiones globales de diseño

Se deciden **acá**, una sola vez. Los planes por repositorio referencian esta sección en
lugar de volver a argumentarla.

### D0 — Dimensión de trabajo: 256. Toda cifra de este plan se deriva de acá.

> **Condicionada a O1.** Si el corpus se reusa desde el servidor con `@cf/baai/bge-m3`, la
> dimensión pasa a **1024** y las cifras de D1 y D5 se cuadruplican. Ver
> [`PENDING_ITEMS.md`](PENDING_ITEMS.md) P1 antes de fijar nada acá.

La dimensión es la única constante que atraviesa las seis decisiones siguientes, así que
tiene un solo dueño: esta línea. Supuesto de trabajo hasta que O1 cierre: **`Dim = 256`**.

`vector` y `vectordb` son agnósticos de la dimensión y se **testean a 384**, a propósito:
384 es el peor caso creíble y el número que usan los benchmarks de referencia de la
industria, así que el margen medido es conservador. Cuando este plan cita memoria o peso
de artifact, dice a qué dimensión corresponde. Cuando O1 cierre, se actualiza **esta línea
y nada más**.

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
   `GOOS=js GOARCH=wasm` **no hay `recover()`** (`tx.go:getStore` ya lo documenta en ese
   mismo repositorio), así que ese pánico es un crash irrecuperable, no un valor de error.
   Codificar un vector de 256 dims como `[]any` para sortearlo cuesta 256 allocations
   boxeadas y ~2 KB de heap JS por documento, contra 1024 bytes de datos reales. Inviable.

2. **`js.CopyBytesToJS` / `js.CopyBytesToGo` son las únicas primitivas de copia masiva, y
   TinyGo implementa ambas.** Son un `memcpy` entre la memoria lineal WASM y un
   `Uint8Array`. El camino `[]float32` → (reinterpretación O(1)) → `[]byte` → un memcpy →
   `Uint8Array` no tiene representación intermedia: ni JSON, ni base64, ni boxing, **ni
   `string`** (ver §8 R4: convertir a `string` es exactamente el bug que hay que arreglar).

3. **`FieldBlob` ya existe** (`model/field.go:13`) y ya está cableado en `IsZeroPtr`,
   `ValuesFrom` y los codecs. Introducir un `FieldType` nuevo obligaría a editar cada
   `switch` exhaustivo en `model`, `storage/mem`, `sqlt`, `postgres` e `indexdb` — un
   cambio incompatible en seis repositorios para no comprar nada. `model.Vector(dim)` es
   un **`Kind`** sobre ese mismo `FieldBlob`, no un `FieldType`: agrega la dimensión al
   esquema sin tocar un solo `switch`. Ver `model/docs/PLAN.md`.

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

**Costo, dicho con honestidad:** la memoria residente es `N × Dim × 4` bytes. A 256 dims
(D0), 10 000 documentos son 10 MB y 100 000 son 100 MB. A 384 dims son 15 MB y 150 MB
respectivamente. En cualquiera de los dos casos, los ~100 k documentos son donde la
cuantización int8 (÷4) se vuelve obligatoria. La cuantización es **fase 5**, no v1.

### D2 — Los vectores se persisten por shards; los documentos, por fila

Una fila de `vec_shards` contiene `ShardSize` (1024 por defecto) vectores como un único
blob. El arranque en frío pasa a `N/1024` deserializaciones de structured clone en vez de
`N`, y cada una es un solo `CopyBytesToGo` directo a su offset en la arena.

El texto y los metadatos viven en un object store **separado**, que se lee únicamente para
el top-k final — nunca durante el scoring. En RAM, junto a la arena, vive un arreglo
compacto de cabeceras (`id`, `tags`, `created`, `hits`, `deleted`) usado para el
prefiltrado.

Ojo con no confundir los dos usos de "1024", porque se parecen y no son lo mismo:

| | unidad | por qué existe |
|---|---|---|
| `ShardSize = 1024` | 1024 **vectores** en **una** fila | evita `N` structured clones al arrancar |
| lote de inserción | `k` **filas** en **una** transacción | evita `k` transacciones IDB al indexar documentos |

La inserción por lote es para el store de **documentos**, donde sí hay una fila por
documento. Los vectores ya llegan agrupados por el shard.

Una consecuencia que vale señalar: como el camino caliente de lectura es "un puñado de
filas grandes de una tabla", `vectordb` **no necesita ninguna API de scan en streaming**
de parte de `storage`. Eso reduce el cambio en `storage` a conformance de blobs más
inserción por lote.

### D3 — IndexedDB no puede indexar un vector. Toda consulta kNN es un barrido completo.

Ningún árbol B sobre un espacio de 256 dimensiones ayuda. Esto no es una limitación a
sortear en v1; es la forma del problema. Por eso el diseño minimiza el **costo por
candidato** (D1, D2) en vez de intentar evitar candidatos. Los índices aproximados
(HNSW/IVF) quedan explícitamente fuera de alcance hasta que exista un corpus que los
necesite.

### D4 — Los embeddings se producen en el navegador, en dos fases detrás de un mismo puerto

`embed.Embedder` es el contrato único. Se embarcan dos implementaciones detrás de él:

- **Fase 3 — embeddings estáticos.** Una tabla de embeddings de tokens destilada (familia
  model2vec / "potion"): tokenizar, buscar, promediar (mean-pool), normalizar. Sin
  atención, sin forward pass, sin GPU. Go puro, compatible con TinyGo hoy. Peso del
  artifact: ver D5 — **no** lo repitas acá. La calidad de recuperación queda por debajo de
  un encoder completo pero es sólida, y entrega un pipeline funcionando de punta a punta
  antes de que exista una sola línea de código de GPU.
- **Fase 5 — encoder transformer sobre WebGPU.** Inferencia completa de
  sentence-transformer vía `navigator.gpu`, en Go + WGSL de principio a fin.

Ambos corren enteramente en el navegador. La fase 5 es un **adaptador nuevo**, no una
reescritura: `vectordb`, `indexdb`, `storage` y `model` quedan intactos.

El **puerto** `embed.Embedder` (la interfaz, sin implementación, sin dependencias) se
publica en la **fase 2**, porque `vectordb` lo importa. El adaptador estático llega en la
fase 3. `plans/embed.md` divide su propio trabajo por esa línea.

### D5 — El español es una restricción de selección, y domina el tamaño del modelo

`docs/EFFICIENT_SLM.md` pone el español como requisito de primer orden — **confirmado como
requisito duro**, no como preferencia. Eso descarta los encoders solo-inglés
(`all-MiniLM-L6-v2` y familia, y también `bge-small-en-v1.5`, que de otro modo sería la
opción más barata; ver `PENDING_ITEMS.md` P1.C). Los modelos multilingües cargan
un vocabulario de ~250 k tokens, y para un modelo **estático** la tabla de embeddings
**es** el modelo entero:

| dims | tabla fp32 | tabla int8 |
|---|---|---|
| 128 | ~128 MB | **~32 MB** |
| 256 (D0) | ~256 MB | **~64 MB** |
| 384 | ~384 MB | **~96 MB** |

Todas son `250 000 × dim × {4,1}` bytes. La cifra que aplica al supuesto de trabajo es
**~64 MB**; `plans/embed.md` §4 es la tabla de detalle y **manda** sobre cualquier número
suelto en este archivo.

Por lo tanto: **la tabla de embeddings se embarca cuantizada a int8 con escalas por fila**,
y el artifact del modelo se cachea en IndexedDB después de la primera descarga, de modo
que se baja una vez por navegador y no una vez por sesión. Los modelos candidatos y la
elección final se registran en [`docs/plans/embed.md`](plans/embed.md).

Los 64 MB son la cifra que hay que mirar con desconfianza, no con alivio: es una primera
carga larga en una conexión mala. La fila de 128 dims (~32 MB) es el fallback documentado
de §8 R2, y O1 decide entre las dos con datos de recall, no por gusto.

### D6 — Licencias

`webtyp/vector-storage` es un fork de `nitaiaharoni1/vector-storage` (MIT). Todo
repositorio Go que porte su lógica — `vectordb` por sobre todo — debe llevar un archivo
`NOTICE` acreditando al autor original y reproduciendo los términos MIT. No es opcional.

### D7 — El isomorfismo vive en `orm` y `ddl`, no en `storage.Conn` crudo

Esto contradice una versión anterior de este plan y es la corrección más importante de §7,
así que se argumenta acá una sola vez.

`storage.Executor` es, literalmente, una interfaz de strings SQL:

```go
Exec(query string, args ...any) error
QueryRow(query string, args ...any) Scanner
```

Un backend de navegador no tiene SQL que poner ahí. Lo que hace `indexdb` es contrabandear
un `storage.Query` por `args[0]` y el modelo por `args[1]` (`indexdb/adapter.go`,
`func (d *adapter) Exec`); `storage/mem` ni siquiera mira los argumentos: lee `e.lastQ`,
que le dejó su propio `Compile`. Es decir: **`storage.Conn` sólo es portable si se lo llama
con el protocolo exacto `Compile(q, m) → Plan → Exec(plan.Query, plan.Args...)`.** Llamarlo
de cualquier otra forma compila y falla en runtime, distinto en cada backend.

Ese protocolo ya tiene una implementación probada y con conformance: **`webtyp.com/orm`**
(`orm/db.go`, `orm/qb.go`). Escribir `MemoryStore` contra `storage.Conn` "crudo" significa
reimplementar `orm` adentro de `agent`, con un backend de navegador como banco de pruebas
— que es la peor forma posible de descubrir que el protocolo tenía una regla no escrita.

Lo mismo para el esquema: "declarar tablas una vez y que cada backend las materialice" es
**`webtyp.com/ddl`** (`ddl/db.go: New(conn Execer, ddlCompiler Compiler)`, `ddl/schema.go`,
`ddl/sync.go`, y su propia suite `ddl/conformance/`). `agent` declara `model.Definition`
y se lo entrega a `ddl`; no escribe DDL ni crea object stores.

**Consecuencia para §7:** `agent` depende de `orm` y `ddl`, y **no** de `storage`
directamente. `vectordb` sí recibe un `storage.Conn` inyectado, porque opera sobre tablas
que él mismo define y necesita el control fino del lote — pero también habla el protocolo
`Compile → Exec`, nunca SQL.

`orm` y `ddl` bajo TinyGo `js/wasm`: **verificado, funcionan.** No hay condición pendiente
acá.

## 5. Mapa de repositorios

| Repositorio | Estado | Responsabilidad única | Fase | Plan |
|---|---|---|---|---|
| `webtyp/model` | modificar | kind `Vector(dim)` sobre `FieldBlob` + `ValidateVector` | 1 | [`model/docs/PLAN.md`](https://github.com/webtyp/model/blob/main/docs/PLAN.md) |
| `webtyp/indexdb` | modificar | E/S de blobs en los 5 caminos + `TxExecutor` | 1 | [`indexdb/docs/PLAN.md`](https://github.com/webtyp/indexdb/blob/main/docs/PLAN.md) |
| `webtyp/storage` | modificar | cláusulas de conformance de blob y transacción | 1 | [`storage/docs/PLAN.md`](https://github.com/webtyp/storage/blob/main/docs/PLAN.md) |
| `webtyp/vector` | **creado** | math vectorial, arena, top-k, códec LE | 2 | [`vector/docs/PLAN.md`](https://github.com/webtyp/vector/blob/main/docs/PLAN.md) |
| `webtyp/embed` | **nuevo** | puerto `Embedder` (fase 2) + adaptador estático (fase 3) | 2 / 3 | [`docs/plans/embed.md`](plans/embed.md) |
| `webtyp/vectordb` | **creado** | almacén de documentos + kNN + filtros + LRU | 2 | [`vectordb/docs/PLAN.md`](https://github.com/webtyp/vectordb/blob/main/docs/PLAN.md) |
| `webtyp/tokenizer` | **nuevo** | texto → ids de tokens | 3 | [`docs/plans/tokenizer.md`](plans/tokenizer.md) |
| `webtyp/weights` | **nuevo** | formato de artifact + caché en navegador | 3 | [`docs/plans/weights.md`](plans/weights.md) |
| `webtyp/agent` | modificar | contrato `MemoryStore` segregado + conformance | 4 | este repositorio, §7 y §7b |
| `webtyp/agentmemory` | **creado** | implementar `MemoryStore` sobre `orm` + `ddl` | 4 | pendiente — se escribe cuando §7b cierre |
| `webtyp/webgpu` | **nuevo** | bindings de `navigator.gpu` | 5 | [`docs/plans/webgpu.md`](plans/webgpu.md) |
| `webtyp/nn` | **nuevo** | kernels WGSL + grafo del encoder | 5 | [`docs/plans/nn.md`](plans/nn.md) |
| `webtyp/vector-storage` | congelar | referencia histórica JS + mapa de port | 0 | [`vector-storage/docs/PLAN.md`](https://github.com/webtyp/vector-storage/blob/main/docs/PLAN.md) |

Notas, cada una es una decisión que alguien va a querer revertir sin leer el porqué:

- **(a) El orden de la fase 1 cambió: `indexdb` antes que `storage`.** `model` sigue primero
  (los otros dos dependen de su tag). Pero la suite de conformance de `storage` sólo puede
  exigir blobs una vez que existe un backend que los soporta: escribir la cláusula antes que
  el driver deja el repositorio con un test rojo esperando a otro repositorio.
- **(b) `webtyp/jsvalue` no está en esta tabla, y es deliberado.** Es dueño del códec
  `[]byte` ↔ JS y **tiene un bug real de corrupción** (§8 R4), pero este plan no lo necesita:
  el camino de lectura de `jsvalue` ya funciona, y `indexdb/docs/PLAN.md` §1 resuelve el
  camino de escritura con su propio `toJSValue`, sin pasar por `jsvalue`. El bug afecta a
  otros consumidores y merece su propio plan; no bloquea a éste.
- **(c) `vector-storage` pasó a fase 0.** Congelar una referencia histórica en JS no
  desbloquea ninguna persistencia; no pertenece a la fase 1. No bloquea nada.
- **(d) Dueño del chequeo de dimensión: `model`. DECIDIDO.** `vectordb` llama a
  `model.ValidateVector` y borra su aritmética propia de `len(b)/4`. «Una verificación que la
  librería ya hace se llama, nunca se re-implementa en el call site» — DRY, y la fila del
  libro mayor «formas de hacer lo mismo» vuelve a cero. Matiz que queda escrito en el doc
  comment de `ValidateVector`: sobre `vec_shards.data`, que es un `Blob()` multi-vector,
  sólo puede verificar «múltiplo de 4», porque una dimensión fija en esa columna sería
  incorrecta; la concordancia de `dim` real la sigue imponiendo `vec_index`. `ValidateVector`
  reparte el chequeo, no lo unifica, y su documentación no debe prometer más que eso.

## 6. Orden de construcción y puertas de fase

Las fases son estrictamente ordenadas: una fase no arranca hasta que la puerta de la
anterior pase. Dentro de una fase, los repositorios de una misma línea pueden avanzar en
paralelo.

### Fase 0 — Verificar las premisas, antes de escribir un plan de repositorio

Barata, corta, y decide si el resto del plan existe. Ningún `docs/PLAN.md` de fase 1 se
escribe antes de que esto cierre.

1. **Round-trip de bytes.** Un `[]byte` de 1024 bytes que **no** sea UTF-8 válido (incluir
   `0x00`, `0xFF`, `0xC0 0x80`, y un par sustituto mal formado) va a IndexedDB y vuelve.
   Se espera que **falle hoy**; el objetivo es capturar el modo de falla exacto, porque de
   eso depende el diff de `indexdb`.
2. **Auto-commit de transacciones.** Confirmar empíricamente el comportamiento que
   `indexdb/docs/PLAN.md` §3 asume, con un test que haga `add` + `await` + `add` sobre la
   misma transacción y verifique que el segundo `add` tira `TransactionInactiveError`. Es el
   supuesto sobre el que descansa todo el diseño de `BeginTx`.

**Puerta:** los dos resultados escritos en `docs/plans/PHASE0.md`, con el modo de falla
literal de cada uno. Un "funcionó" sin salida pegada no cierra la puerta.

**Ya resuelto — no repetir el trabajo.** `indexdb/docs/PLAN.md` §0 abre pidiendo verificar si
`jsvalue.ScanValue` maneja `Uint8Array`. **Sí lo maneja**: `ScanValue` → `ToGo` →
`decodeBytes` (`jsvalue/codec_wasm.go`) hace `InstanceOf(Uint8Array)` + `js.CopyBytesToGo`.
`mapResult` no necesita cambios y esa §0 se puede cerrar citando esta línea. Lo que **no**
funciona es el camino de escritura de `jsvalue` — ver §8 R4 — y por eso `indexdb` trae su
propio `toJSValue` en vez de delegar.

### Fase 1 — Desbloquear la persistencia (sin ML)
`model` → `indexdb` → `storage`, en ese orden (cada uno depende del release del anterior).

`storage` va último: su suite de conformance sólo puede exigir blobs una vez que existe un
backend que los soporta (§5 nota (a)). `jsvalue` no participa (§5 nota (b)).

**Puerta:**
- Un `[]byte` de 1024 bytes **no-UTF-8** (el mismo vector de bytes de la fase 0) hace
  round-trip por IndexedDB en un navegador real con **igualdad byte a byte**. Este es el
  criterio; "un blob funciona" no lo es, porque un blob ASCII pasa hoy y no prueba nada.
- Insertar `k` **filas de documentos** (k = 1024) usa **una** transacción, verificado
  contando eventos `complete` de transacción, no por tiempo transcurrido.
- La suite de conformance de `storage` pasa en `mem`, `sqlt` e `indexdb`, con el caso de
  blob binario incluido.

Demostrado por `indexdb/tests/` bajo `gotest -tinygo`.

**Restricción de diseño para el contrato de lote — el ejecutor no la va a descubrir solo:**
una transacción de IndexedDB se **auto-comitea** en cuanto el event loop cede el control
sin requests pendientes sobre ella. Un lote escrito como

```go
for _, row := range rows {          // WRONG: commits after the first row
    req := store.Call("add", row)
    await.Request(req)              // yields; the tx commits here
}
```

falla en la segunda iteración con `TransactionInactiveError`. El contrato de lote debe
emitir **todos** los `add()` primero y recién después esperar el evento `complete` **de la
transacción**, no los `success` de cada request:

```go
for _, row := range rows {          // RIGHT: one tx, k requests
    store.Call("add", row)          // fire, do not await
}
await.Event(tx, "complete", "error", "abort")
```

Eso implica que `await` necesita esperar un evento de la transacción, no sólo un request.
Si esa primitiva no existe todavía, es parte del diff de la fase 1.

### Fase 2 — Math, puerto y almacén (sin ML)
`vector` y el **puerto** `embed` en paralelo, luego `vectordb`.

`embed` entra acá, no en la fase 3, porque `vectordb` importa `embed.Embedder` y no
compilaría sin él. Lo que se entrega en esta fase es sólo la interfaz y su `MockEmbedder`:
sin tokenizer, sin pesos, sin ML, sin dependencias. El adaptador estático es fase 3.

**Puerta:** kNN sobre 10 000 vectores sintéticos de 384 dims devuelve el top-k correcto
(verificado contra una implementación de referencia ingenua). El mismo test de corrección
pasa contra `storage/mem` en Go estándar y contra `indexdb` en el navegador.

**Cero allocations por consulta**, medido con `testing.AllocsPerRun`, se exige en las dos
corridas: nativa y navegador. `gotest` compila la suite WASM con el **toolchain de Go** y la
ejecuta en un navegador headless vía `wasmbrowsertest`, y el runtime de Go para `js/wasm`
trae `runtime.ReadMemStats` completo, así que `AllocsPerRun` mide de verdad ahí. La
aserción de allocations es parte de la puerta en ambos targets.

La única excepción es `gotest -tinygo`, donde el conteo de allocations no es comparable
porque el GC es otro. Esa bandera es para los casos que requieren específicamente verificar
el target TinyGo — como los tests de blob de `indexdb` — no para el presupuesto de
allocations de `vector`.

### Fase 3 — Embeddings en el navegador
`tokenizer` y `weights` en paralelo, luego el **adaptador estático** de `embed`.

**Puerta:** un corpus real en español se indexa y se busca de punta a punta en el
navegador, offline después de la primera carga, con un recall@10 documentado contra una
referencia. La cifra de recall es la entrada de la decisión de fase 5 — si no está
escrita, la fase 5 no tiene condición de entrada.

### Fase 4 — Memoria del agente
`agent` (contrato segregado + suite de conformance) → `webtyp/agentmemory` (implementación
sobre `orm` + `ddl`, con `SearchKnowledge` semántico). Ver §7 y §7b.

**Orden dentro de la fase, y no es negociable:** la suite de conformance se escribe
**primero**, antes de una línea de `agentmemory`. Deja de ser una puerta posterior y pasa a
ser la especificación ejecutable de lo que hay que implementar. El skill lo justifica: si el
test con forma de consumidor es incómodo de escribir, la API es incómoda de usar — y eso se
descubre antes de la implementación en vez de después.

**Puerta:** esa suite pasa contra un backend SQL y contra `indexdb`, desde una sola
implementación en `agentmemory`. Más la matriz DDT completa de `DEFAULT_LLM_SKILL.md` §2.

**Puerta adicional, y es la que prueba el punto:** `go list -m all` sobre `webtyp.com/agent`
no menciona ningún motor de base de datos.

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

`webtyp/agent` es el **orquestador**: bucle ReAct, FSM, ventana de contexto y registro de
herramientas. Coordina otras librerías; no implementa ninguna de sus capacidades. La
decisión de fondo de esta sección — **la memoria sale de este repositorio** — se toma acá y
no se vuelve a argumentar en los planes de abajo.

1. **Migración de module path** `github.com/tinywasm/agent` → `webtyp.com/agent`, junto con
   las dependencias `github.com/tinywasm/{fmt,mcpserve,…}` de `go.mod`, que también siguen
   apuntando al path viejo. Todos los repositorios de este plan ya migraron; `agent` no
   puede depender de ellos mientras publica el path viejo sin confundir la actualización de
   módulos dependientes de `gopush`.
   **Va primero: prerrequisito de todo lo demás acá, y no depende de ninguna fase** — se
   puede ejecutar hoy, en paralelo con la fase 0.

2. **`memory.go` y `schema.go` salen a `webtyp/agentmemory`.** No se reescriben acá: se
   mudan. `agent` conserva el contrato (`MemoryStore` y los tipos que lo atraviesan:
   `Message`, `Episode`, `Knowledge`, `ToolLog`) y pierde `modernc.org/sqlite` del `go.mod`.
   `webtyp/agentmemory` implementa ese contrato sobre `webtyp.com/orm` + `webtyp.com/ddl`
   (ver **D7**), de modo que la misma implementación corra sobre SQL y sobre `indexdb`.

   *Por qué mudar y no reescribir en el lugar:* hoy `Config.Memory` es un `MemoryStore`
   inyectado y `DEFAULT_LLM_SKILL.md` §1 exige que «el struct `Agent` sostenga interfaces,
   nunca implementaciones concretas». Esa regla se cumple en `agent.go` y **se rompe en el
   `go.mod`**: quien embeba `agent` en un proyecto con Postgres, o en el navegador, igual
   linkea un motor SQLite que nunca instancia. Es la **D** de SOLID verificada donde importa
   — en el grafo de build, no en la prosa. Arte previo: `database/sql` define
   `driver.Driver` y cada driver vive en su propio módulo; lo mismo hace `storage` con
   `sqlt`/`postgres`/`indexdb`.

   Se ejecuta **junto con el punto 1**, en una sola migración sobre los mismos archivos.

3. **El esquema como valores `model.Definition` materializados por `webtyp.com/ddl`.**
   `agentmemory` declara las `Definition` y `ddl` las convierte en DDL sobre un backend SQL
   y en object stores sobre `indexdb`. Nadie escribe cadenas de DDL: `schema.go` desaparece
   en la mudanza, no se porta.

4. **`SearchKnowledge` gana un camino semántico** a través de `vectordb`. Es un cambio de
   `agentmemory`, **no de `agent`**: la firma
   `SearchKnowledge(ctx, query, sessionID string, limit int)` recibe **texto**, así que el
   orquestador ya no necesita saber nada de vectores.

   FTS5 léxico no existe fuera de SQLite: sobre `indexdb`, la mitad léxica de RRF espera al
   índice BM25 de la fase 5. Hasta entonces es léxico por `LIKE` (soportado en
   `indexdb/execute.go: matchLike` y en `storage.Like`) y semántico por vectores.

5. **`Config` NO gana un `Embedder`.** El `embed.Embedder` se inyecta en el constructor de
   `agentmemory`, que es quien lo usa:

   ```go
   mem, _ := agentmemory.New(agentmemory.Config{Conn: conn, Embedder: emb})
   ag,  _ := agent.New(agent.Config{Memory: mem, LLMs: llms})
   ```

   `agent` no importa `webtyp.com/embed`. Esto revierte lo que anticipaba el estudio
   histórico, y la razón es la misma del punto 2: un `Embedder` en `Config` obliga al
   orquestador a saber que alguna implementación, en algún lado, hace búsqueda vectorial.

6. **`MemoryStore` se segrega en cuatro contratos y se compone.** Ver §7b.

7. Actualizar la lista de dependencias permitidas de `DEFAULT_LLM_SKILL.md`:
   `modernc.org/sqlite` sale — **y no entra nada en su lugar**. `agent` queda sin
   dependencias de almacenamiento. `webtyp.com/orm`, `webtyp.com/ddl`, `webtyp.com/vectordb`
   y `webtyp.com/embed` son dependencias de `agentmemory`, no de `agent`.
   Corregir también la línea de §1 que dice «`memory.go` — SQLite MemoryStore implementation
   only»: ese archivo deja de existir acá.

8. **`Message.TokenCount` tiene tres significados distintos según qué línea lo escribió, y
   `prepareContext` los suma como si fueran la misma unidad.**

   | Dónde | Qué guarda |
   |---|---|
   | `orchestrator.go:25` | `len(userQuery) / 4` — estimación por caracteres |
   | `orchestrator.go:89` | `resp.TokensUsed` — el total del **turno entero** (prompt + completion), no el de ese mensaje. El comentario en el código dice `// Approximation?`, con signo de pregunta |
   | `orchestrator.go:141` | `len(output) / 4` — estimación otra vez |
   | `orchestrator.go:179` | `resp.TokensUsed` — mismo problema que :89 |

   `context_window.go` suma esos valores y los compara contra
   `MaxTokens × SummarizeAt` para decidir cuándo resumir. Es decir: **la decisión de resumir
   se toma sobre una suma de unidades incompatibles**, y el error crece con la cantidad de
   turnos, porque `TokensUsed` es acumulativo y se guarda una vez por turno.

   No es una optimización pendiente: es un presupuesto que no mide lo que dice medir. El
   arreglo mínimo es que `Message.TokenCount` tenga **un** significado documentado —los
   tokens de ese mensaje— y que quien no pueda saberlo lo deje en cero en vez de rellenarlo
   con una estimación de otra unidad. Un cero honesto es mejor que un número inventado,
   porque el cero se puede detectar.

   Va con el punto 1, porque toca los mismos archivos.

9. Corregir `README.md`, que todavía describe el proyecto como "Autonomous AI Agent system
   for `tinywasm`". Va con el punto 1.

**Lo que este plan NO hace con `ContextWindowConfig`, y por qué.** `MaxTokens`,
`SummarizeAt`, `MaxRecentMsgs` y `MaxEpisodes` configuran **una** estrategia de resumen
cableada en `prepareContext` (resumir el 50% más viejo al cruzar el umbral). Extraerla a un
contrato enchufable es tentador y el skill dice que **no**, todavía:

- **Gate 5 — «¿qué borra este cambio?»** Nada. Habría una interfaz nueva y la misma única
  implementación detrás.
- **Gate 3 — el libro mayor.** «Conceptos +1 / −0» sin que ninguna otra fila mejore. Una
  interfaz que sólo agrega tiene que justificarse a los gritos, y acá no hay un segundo
  llamador que la pida.
- **L — sustituibilidad.** Un contrato con una sola implementación no tiene con qué publicar
  una suite de conformance, así que su sustituibilidad sería una promesa, no un hecho. Es
  exactamente lo que la L prohíbe.

La **O** sí queda rozada —agregar una segunda estrategia hoy obliga a editar
`prepareContext`— pero la O se cobra cuando la segunda implementación existe, no antes. Si
aparece, esto se reevalúa con el gate completo. Mientras tanto, lo que hay que arreglar es el
punto 8, que es un defecto y no una decisión de diseño.

## 7b. `MemoryStore`: cuatro contratos, un nombre compuesto

`interfaces.go` declara hoy un solo contrato de once métodos que cubre cuatro dominios.
Toda implementación debe proveer los once, y la búsqueda semántica sólo toca uno.

Con una sola implementación eso costaba poco. Con dos —una SQL y una de navegador— la de
navegador tendría que proveer `LogToolCall` y `GetToolLogs` aunque un agente en el browser
rara vez los use, y la única salida sería un stub que devuelve `nil`. Eso es la **I** de
SOLID: *una interfaz es tan ancha como la necesita su llamador más angosto*, y un stub que
devuelve `nil` es exactamente el fallo silencioso que el harness prohíbe.

```go
// Each contract is what one collaborator of the orchestrator actually needs.
type ConversationStore interface {
	EnsureSession(ctx context.Context, sessionID string) error
	AppendMessage(ctx context.Context, sessionID string, msg Message) error
	GetMessages(ctx context.Context, sessionID string, limit int) ([]Message, error)
	DeleteMessages(ctx context.Context, sessionID string, ids []string) error
}

type EpisodeStore interface {
	SaveEpisode(ctx context.Context, sessionID, summary string, tokenCount int, fromID, toID string) error
	GetEpisodes(ctx context.Context, sessionID string, limit int) ([]Episode, error)
}

type KnowledgeStore interface {
	SaveKnowledge(ctx context.Context, sessionID, content, source string) error
	SearchKnowledge(ctx context.Context, query, sessionID string, limit int) ([]Knowledge, error)
}

type ToolLogStore interface {
	LogToolCall(ctx context.Context, sessionID, toolName, inputJSON, outputText, errText string, durationMS int64) error
	GetToolLogs(ctx context.Context, sessionID, toolName string, limit int) ([]ToolLog, error)
}

// MemoryStore is the composed contract. Config.Memory keeps this type, so no
// call site changes and there is still exactly one way to declare a full memory.
type MemoryStore interface {
	ConversationStore
	EpisodeStore
	KnowledgeStore
	ToolLogStore
}
```

Arte previo: `io.Reader`/`Writer`/`Closer` → `io.ReadWriteCloser`, y `fs.FS` +
`fs.ReadDirFS` + `fs.StatFS`. La fila del libro mayor que nunca debe terminar positiva
—«formas de hacer lo mismo»— queda en cero, porque `MemoryStore` sigue siendo el nombre
compuesto y `Config.Memory` no cambia.

Ganancia concreta para este plan: **el camino semántico sólo necesita `KnowledgeStore`**.
Se puede construir y testear sin tocar mensajes, episodios ni bitácoras.

**Consecuencia de la L de SOLID:** en cuanto exista una segunda implementación de
`MemoryStore` —y este plan crea exactamente eso, SQL y navegador— el repositorio que **posee
el contrato** debe publicar una suite de conformance, igual que `storage/conformance` y
`ddl/conformance`. Esa suite es la puerta de la fase 4, y vive en `agent`, no en
`agentmemory`.

## 8. Riesgos

| Riesgo | Impacto | Mitigación |
|---|---|---|
| `js.ValueOf` hace pánico con `[]byte` en `indexdb.create`, y bajo TinyGo no hay `recover()` | Crash irrecuperable de la página en la primera escritura de vector | `toJSValue` en `indexdb/docs/PLAN.md` §1: `Uint8Array` + `js.CopyBytesToJS`, con rama `default` que devuelve error en vez de crashear |
| `indexdb` descarta columnas blob en silencio en 4 caminos más (`update` ×2, `Scan`, `checkCondition`) | Actualizar cualquier columna **borra el vector**; un blob nunca matchea una condición | `indexdb/docs/PLAN.md` §2: `case FieldBlob` en los cuatro switches **y** una rama `default` que falle ruidosamente |
| **`jsvalue` codifica `[]byte` como JS string — confirmado, no hipotético** | Corrupción silenciosa para cualquier consumidor que encode por `jsvalue` | Fuera del camino crítico de este plan (`indexdb` no usa esa ruta), pero es un bug real: merece su propio plan — ver nota abajo |
| La fase 5 (encoder WebGPU) es un proyecto grande y abierto | La entrega se corre indefinidamente | El embedder estático de la fase 3 hace que el producto funcione sin ella; la fase 5 es un cambio de adaptador detrás de `embed.Embedder` |
| El artifact multilingüe pesa ~64 MB (D5) | Primera carga larga en conexión mala | Tabla de embeddings int8, caché en IndexedDB tras el primer fetch, y el fallback de 128 dims (~32 MB) de D5 como plan B explícito. **Este riesgo desaparece por completo si O1 se resuelve por embeber la consulta en el servidor** — ver `PENDING_ITEMS.md` P1 |
| Una transacción IDB se auto-comitea al ceder el event loop | La inserción por lote falla con `TransactionInactiveError` en la segunda fila | Contrato de lote especificado en la puerta de fase 1: emitir todos los `add()` y esperar `complete` de la transacción |
| La cuota de IndexedDB desaloja el índice | Pérdida silenciosa de datos | `navigator.storage.persist()` al iniciar, `estimate()` antes de escribir, desalojo LRU propio antes que el del navegador |
| La arena excede la memoria del navegador pasados ~100 k docs | OOM | Techo documentado (D1), cuantización int8 en la fase 5 |
| Vectores y texto se desincronizan en una escritura parcial | Resultados corruptos | Una sola transacción abarcando ambos stores; una fila de cabecera `dim`/`model_id` rechaza una arena que no corresponde al cargar |

**Nota sobre `jsvalue`, porque una versión anterior de este plan lo tenía al revés.**
El riesgo estaba escrito como "`jsvalue.ScanValue` podría no manejar `Uint8Array`". Los dos
términos son incorrectos:

- **El lado de lectura ya funciona.** `ScanValue` → `ToGo` → `decodeBytes` (`jsvalue/codec_wasm.go`)
  hace `InstanceOf(Uint8Array)` + `js.CopyBytesToGo`. Exactamente lo que D1 necesita.
- **El lado de escritura está roto, y con certeza, no con probabilidad.** Las tres rutas de
  encode convierten `[]byte` a `string`:
  `ToJS` (`case []byte: return js.ValueOf(string(v))`), `jsObjectWriter.Bytes`
  (`w.obj.Set(name, string(val))`) y `jsArrayWriter.Bytes`. Un Go string cruza a JS como
  UTF-16 decodificado desde UTF-8: **cualquier byte que no forme UTF-8 válido se sustituye
  por U+FFFD**. Un vector de float32 es binario arbitrario, así que la sustitución es la
  regla, no el borde. El dato vuelve del round-trip con el largo y los valores cambiados,
  **sin error y sin pánico** — que es peor que el crash de `js.ValueOf`, porque no se nota.
- **`indexdb` ni siquiera usa esa ruta al escribir:** `execute.go: create` arma un
  `map[string]any` y llama `store.Call("add", data)` directo, o sea `js.ValueOf` sobre los
  valores crudos → pánico irrecuperable con `[]byte` (D1 §1).

**Qué implica para este plan:** nada bloqueante. `indexdb/docs/PLAN.md` §1 no delega el
encode en `jsvalue` — trae su propio `toJSValue`, que construye el `Uint8Array` directo. Es
la decisión correcta: el driver es dueño de su propio borde. Y el decode sí delega en
`jsvalue`, que funciona. `indexdb` queda íntegro sin tocar `jsvalue`.

**Qué implica para `jsvalue`:** sigue roto para todo otro consumidor que encode un `[]byte`.
El arreglo es mecánico — `Uint8ArrayClass.New(len(val))` + `js.CopyBytesToJS` en los tres
writers — y el decode ya lo acepta, así que es compatible hacia atrás con lo ya escrito por
la ruta string. Es un plan aparte, no una fase de éste. Anotado acá para que no se pierda.

## 9. Decisiones abiertas

- **O1. Compatibilidad de espacio vectorial entre servidor y navegador.** Reemplaza a la
  vieja pregunta «qué modelo y qué dimensión», que era la pregunta chica. La grande está en
  [`docs/PENDING_ITEMS.md`](PENDING_ITEMS.md) P1: si el corpus se indexa en el servidor con
  `@cf/baai/bge-m3` (1024 dims) y el navegador tiene que **reusar esos vectores**, el
  embedder del navegador debe producir vectores en **el mismo espacio** — y un modelo
  estático destilado no lo hace. `vec_index.model_id` (D2) ya rechaza la mezcla; lo que hay
  que decidir es cuál de los caminos se toma. Bloquea a D0, D4 y D5.
- **O2. — CERRADA.** *¿`webtyp/binary` provee un códec little-endian de float32?* **No, y
  `vector` no debe depender de él.** `binary` expone `Float(name string, val float64)`, que
  escribe 8 bytes LE, dentro de un formato de mensaje con varints y nombres de campo
  (`binary/codec.go`). Es un serializador orientado a campos, no un códec de arreglo crudo:
  no hay float32, y el framing por campo destruiría justamente la reinterpretación O(1)
  `[]float32` → `[]byte` que compra D1 §2. `vector` define su propio códec LE de float32 —
  unas pocas decenas de líneas, sin dependencias. Ver `plans/vector.md` §4.
- **O3. — CERRADA.** *Forma del filtrado por metadatos.* Queda como está:
  `model.RawJSON` opaco para el payload más una columna de tags delimitada (`|a|b|c|`) y
  filtrable con `LIKE` (soportado en ambos lados: `storage.Like` e `indexdb: matchLike`),
  porque `model` no tiene un tipo de campo de slice de strings — sólo `FieldIntSlice`.

  **Por qué no se agrega un `FieldStringSlice`:** sería el mismo `FieldType` nuevo que D1 §3
  rechaza para vectores — editar cada `switch` exhaustivo en `model`, `storage/mem`, `sqlt`,
  `postgres` e `indexdb`. Usar ese argumento para el vector y no para las etiquetas sería
  incoherente. Y funciona porque el filtrado ocurre **en RAM**, sobre el arreglo de cabeceras
  de D2, no contra la base: el `LIKE` es una búsqueda de subcadena en memoria.

  Si algún día aparece un segundo consumidor que necesite lo mismo, el camino es un `Kind`
  `Tags()` sobre `FieldText` —dueño de su separador y de rechazar una etiqueta que lo
  contenga—, no un `FieldType`. Misma lógica que `Vector(dim)`: cero `switch` tocados.
