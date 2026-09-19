---
PLAN: "feat: búsqueda semántica nativa en el navegador — índice maestro"
TAG: v0.2.0
EXECUTOR: unassigned
REVIEWER: none
---

> Este es el **plan índice**. No le pertenece código — ni siquiera el de este repositorio,
> que vive en [`docs/plans/agent.md`](plans/agent.md) como el de cualquier otro. Cada
> repositorio de §5 lleva su propio plan con el diff concreto; este archivo es dueño de la
> arquitectura, los contratos compartidos, el orden de construcción y los criterios de
> aceptación que cruzan repositorios.
>
> **Regla de extensión:** si una sección de acá explica *cómo* se hace algo en un repositorio,
> está en el archivo equivocado. El índice dice qué, quién, en qué orden y con qué puerta.
>
> Los planes de los repositorios que **todavía no existen** viven en [`docs/plans/`](plans/)
> hasta que el repositorio se cree; entonces el archivo se mueve a su `docs/PLAN.md` sin
> modificaciones. Los planes de los repositorios que **ya existen** están en su
> `docs/PLAN.md` y §5 enlaza a ellos.
>
> **`tokenizer`, `weights`, `embed` y `transformer` se crean recién cuando la fase 3 abra**, y su
> puerta de apertura es un benchmark (§6). Crear un repositorio antes de saber si su diseño
> es viable es deuda, no adelanto.
>
> **Nota de idioma:** la prosa va en español. Los bloques de código se mantienen con
> comentarios en inglés porque son código literal destinado a los repositorios, cuyos
> comentarios de fuente son en inglés.

# Plan — Búsqueda semántica en el navegador, sobre IndexedDB

## 1. Objetivo

**Búsqueda semántica offline total en el navegador.** El corpus —texto y vectores— vive en
IndexedDB, en la máquina del usuario, y la consulta no sale de ahí. Sin dependencia de
ninguna librería JavaScript. La persistencia es `webtyp.com/indexdb`, extendido donde su API
actual no alcanza a expresar el problema.

El flujo, que es lo que gobierna todo el resto del documento:

| Momento | Dónde ocurre | ¿Necesita red? |
|---|---|---|
| Subir un documento | el backend tokeniza, embebe los chunks y **devuelve los vectores al cliente** | **sí** |
| Guardar | texto + vectores quedan en IndexedDB (D2) | no |
| **Buscar** | el navegador embebe la consulta y hace el kNN sobre la arena local | **no — offline total** |

El backend no está ahí porque el navegador no pueda embeber: está porque es la máquina rápida
y no hace esperar al usuario mientras se procesan diez chunks de 8 000 tokens. La data
termina siempre del lado del cliente.

**La consecuencia que ordena todo el plan:** si la búsqueda es offline, el navegador necesita
el modelo — no para los documentos, para la **consulta**. Y el vector de la consulta tiene
que caer en el mismo espacio que los de los documentos, así que tienen que ser **los mismos
pesos**. No hay forma de esquivarlo: un transformer que embebe 20 tokens tiene los mismos
parámetros que uno que embebe 8 000; la entrada corta baja el cómputo, no el tamaño.

Por eso el modelo se elige por el presupuesto del navegador (D5), y por eso hay **una sola
implementación en Go** corriendo en tres targets (D4).

El `MemoryStore` del agente se reescribe una sola vez contra `webtyp.com/orm`, de modo que
el mismo código de memoria corra sobre un backend SQL (servidor) y sobre IndexedDB
(navegador).

## 2. Por qué esto reemplaza al estudio de SQLite

El diseño anterior ([`history/MEMORY_SQLITE.md`](history/MEMORY_SQLITE.md)) asumía un único
motor en todas partes: SQLite en el servidor, el mismo compilado a WASM en el navegador, con
`sqlite-vec` para la búsqueda vectorial. Dos premisas fallaron: `modernc.org/sqlite` es Go
puro pero **no** compila bajo TinyGo para `js/wasm`, y `sqlite-vec` es una extensión en C —
usarla en el navegador significa un segundo runtime WASM más una capa VFS en JavaScript,
exactamente la dependencia JS que este ecosistema existe para evitar.

**Sobrevive y se reutiliza tal cual:** la categorización de memoria (corto plazo / episódica
/ semántica / de acciones), el alcance de conocimiento global por `session_id IS NULL`, y RRF
como estrategia eventual de recuperación híbrida.

**Lo reemplaza:** el navegador ya trae un almacén persistente y transaccional —IndexedDB— y
este ecosistema ya tiene driver. El isomorfismo sube un nivel: de «el mismo motor en todas
partes» a «el mismo contrato `storage.Query` en todas partes», que es para lo que se
construyeron `storage`, `orm` y `ddl`.

## 3. Arquitectura

```
   NAVEGADOR (TinyGo → WASM)                    BACKEND (Go nativo) / WORKER (WASM)
   ─────────────────────────                    ───────────────────────────────────
   ┌──────────────────────────────────┐
   │  agent — orquestador             │
   │  contrato MemoryStore, 0 storage │
   └────────────────┬─────────────────┘
   ┌────────────────▼─────────────────┐
   │  agentmemory                     │  implementa el contrato sobre orm + ddl
   └────────────────┬─────────────────┘
   ┌────────────────▼─────────────────┐
   │  vectordb    documentos, kNN,    │
   │              filtros, LRU, cuota │
   └──┬──────────┬──────────────┬─────┘
      │          │              │
 ┌────▼──────┐ ┌─▼──────────┐ ┌─▼───────────────┐
 │  vector   │ │  embed     │ │  orm  /  ddl    │
 │  arena,   │ │  puerto +  │ │  DML + DDL      │
 │  top-k,   │ │  adaptador │ └─┬───────────┬───┘
 │  códec LE │ └─┬────────┬─┘   │           │
 └───────────┘   │        │     │           │
        ┌────────┘        │     │           │
 ┌──────▼─────┐ ┌─────────▼──┐ ┌▼─────────┐ ┌▼──────────────┐
 │ tokenizer  │ │transformer │ │ storage  │ │ storage       │
 │ texto→ids  │ │ encoder,   │ │ +indexdb │ │ +sqlt/postgres│
 └──────┬─────┘ │ CPU/WASM   │ └──────────┘ └───────────────┘
        │       └─────┬──────┘
        │       ┌─────▼──────┐
        └───────│ weights    │  artifact int8 + caché IDB
                └────────────┘

   El navegador embebe CONSULTAS (~20 tokens, D4b).
   El backend embebe DOCUMENTOS (chunks de hasta 8 K).
   MISMO código Go, MISMOS pesos → un solo espacio vectorial (D4).
```

Cada flecha es una dependencia de compilación. No hay ciclos y ningún repositorio depende
de `agent`.

Dos aristas que el diagrama hace explícitas porque una versión anterior de este plan las
tenía mal:

- **`vectordb` importa `embed`.** No el adaptador — sólo la interfaz `embed.Embedder`
  (`plans/vectordb.md` §3 y `Config.Embedder`). Por eso el **puerto** `embed` es fase 2 y
  el adaptador es fase 3. Ver §6.
- **`indexdb` encoda sus propios bytes.** Lee por `jsvalue` (que funciona) y escribe por su
  propio `toJSValue` (porque el writer de `jsvalue` corrompe binario). Ver §5 nota (b) y §8.

## 4. Decisiones globales de diseño

Se deciden **acá**, una sola vez. Los planes por repositorio referencian esta sección en
lugar de volver a argumentarla.

### D0 — Dimensión de trabajo: 384. Toda cifra de este plan se deriva de acá.

La dimensión es la única constante que atraviesa las decisiones siguientes, así que tiene un
solo dueño: esta línea. **`Dim = 384`**, que es la dimensión nativa de los transformers
multilingües chicos de D5 y la que usan los benchmarks de `vector`.

Consecuencias aritméticas, para no repetirlas en cada sección: la arena residente son
`N × 384 × 4` = **1,5 KB por documento**. 10 000 documentos son 15 MB; 100 000 son 150 MB, y
ahí la cuantización int8 (÷4) se vuelve obligatoria.

`vector` y `vectordb` son agnósticos de la dimensión — 384 es el valor de configuración, no
una constante compilada. Si D5 termina eligiendo un modelo de otra dimensión, se actualiza
**esta línea y nada más**.

### D1 — Un vector es `[]byte` en disco y una porción de una arena compartida en memoria

En disco el tipo canónico es `model.Blob()` → `[]byte` → un `Uint8Array` de JS bajo
structured clone. En Go, los vectores **nunca** son un `[]float32` por documento: viven en
una única arena contigua `[]float32` de `N × Dim`, donde el documento *i* ocupa
`arena[i*Dim : (i+1)*Dim]`.

Por qué, en el orden que importa:

1. **`js.ValueOf` no puede transportar un vector, y fallar cuesta la aplicación entera.**
   Acepta `[]any` pero **ni `[]byte` ni `[]float32`**, y hace pánico con cualquier otra cosa.
   Bajo TinyGo `GOOS=js GOARCH=wasm` **no hay `recover()`** (`indexdb/tx.go:getStore` ya lo
   documenta), así que ese pánico es un crash irrecuperable, no un error. Y codificar 384
   dims como `[]any` para sortearlo cuesta 384 allocations boxeadas contra 1536 bytes de
   datos reales.
2. **`js.CopyBytesToJS` / `CopyBytesToGo` son las únicas primitivas de copia masiva**, y
   TinyGo implementa ambas. `[]float32` → (reinterpretación O(1)) → `[]byte` → un memcpy →
   `Uint8Array`: sin JSON, sin base64, sin boxing, **ni `string`** (§8).
3. **`[]byte` es la única representación isomórfica:** `BLOB` (SQLite), `BYTEA` (Postgres),
   `Uint8Array` (IndexedDB). Un `Float32Array` no tiene contraparte SQL.
4. **`FieldBlob` ya existe** (`model/field.go:13`) y ya está cableado. Un `FieldType` nuevo
   obligaría a editar cada `switch` exhaustivo en seis repositorios para no comprar nada.
   `model.Vector(dim)` es un **`Kind`** sobre ese mismo `FieldBlob`, no un `FieldType`.
5. **La arena es lo que hace rápidas las consultas.** El scoring lee solo memoria lineal
   WASM: cero cruces del puente JS por consulta, cero allocations por candidato, y el GC
   conservador de TinyGo ve un objeto grande en vez de N slices chicos.
6. **Los vectores se guardan normalizados L2**, lo que convierte la similitud coseno en un
   producto punto y elimina un `sqrt` y una división por documento por consulta.

**Costo:** ya está en D0 — 1,5 KB por documento, con el techo en ~100 k. La cuantización
int8 que corre ese techo es trabajo posterior a la v1; ver §6, Fase 5.

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

Ningún árbol B sobre un espacio de 384 dimensiones ayuda. Esto no es una limitación a
sortear en v1; es la forma del problema. Por eso el diseño minimiza el **costo por
candidato** (D1, D2) en vez de intentar evitar candidatos. Los índices aproximados
(HNSW/IVF) quedan explícitamente fuera de alcance hasta que exista un corpus que los
necesite.

### D4 — Un modelo, una implementación en Go, tres targets

`embed.Embedder` es el contrato único. Detrás hay **una sola implementación**, no dos:
`tokenizer` + `weights` + el grafo del encoder en `transformer`, todo en Go.

Lo que cambia entre entornos es el target de compilación, no el código ni los pesos:

| Target | Cómo compila | Qué embebe | Presupuesto |
|---|---|---|---|
| Navegador | TinyGo → WASM | **consultas** (~20 tokens) | descarga del artifact + un forward pass |
| Backend | Go nativo | **documentos** (chunks de hasta 8 K) | la CPU del servidor |
| Worker de Cloudflare | TinyGo → WASM | documentos, en despliegues en la nube | 128 MB por isolate |

Eso es lo que hace que los vectores sean comparables **para siempre y entre instalaciones**:
no hay dos modelos que puedan divergir, porque no hay dos implementaciones.

**Lo que esto elimina:** no hace falta Ollama ni el catálogo de Workers AI. Se evaluaron
(`docs/CLOUDFLARE_AI_WORKER.md`) y el problema es que ningún modelo del catálogo es a la vez
multilingüe y chico: `bge-m3` y `qwen3-embedding-0.6b` son de ~600M parámetros, así que
entrarían en el backend pero no en el navegador ni en un isolate de 128 MB. Un modelo que
solo corre en el servidor obliga a un segundo modelo para la consulta, y con eso se pierde
el espacio vectorial común, que es justamente el requisito.

**Lo que esto borra del plan anterior:** `webtyp/webgpu` y la fase 5 como proyecto abierto.
El razonamiento está en D4b.

### D4b — El cómputo de una consulta es chico, y eso borra WebGPU

WebGPU entró al plan para embeber **documentos** rápido. El navegador no embebe documentos.

Un forward pass cuesta en proporción al largo de la secuencia. Una consulta de 20 tokens
sobre 12 capas de 384 dims son **~428M MAC ≈ 856M FLOP** [calc]:

```
por capa:  QKV        3 × 20 × 384 × 384  =  8,85M      atención  2 × 20 × 20 × 384 = 0,31M
           proyección     20 × 384 × 384  =  2,95M      FFN   2 × 20 × 384 × 1536 = 23,60M
                                                              ≈ 35,7M MAC/capa × 12 = 428M
```

Eso cabe en WASM sobre CPU sin `navigator.gpu`, sin WGSL, sin bindings de GPU. Por lo tanto
`transformer` sube de la fase 5 a la **fase 3** en versión CPU/WASM, y `webtyp/webgpu` sale
del plan.

**MEDIDO.** `vector` v0.1.1: ~233 ns/op para un `Dot` de 384 dims bajo TinyGo WASM, o sea
**~3,3 GFLOPS** escalares. Los 856M FLOP se pagan en **~259 ms** — un piso optimista, porque
`Dot` no incluye softmax, layernorm ni GELU. Detalle y lectura completa en
[`PENDING_ITEMS.md`](PENDING_ITEMS.md) P1. La contradicción de abajo quedó **confirmada**:
es un número escalar, no hay SIMD en juego. `vector/docs/PLAN.md` §2 decía:

> No recurras a SIMD de WASM: el soporte de TinyGo es incompleto, y un intrínseco que no se
> puede verificar es peor que un bucle correcto en todas partes.

Tenía razón, y medirlo era barato: no hizo falta construir `transformer` para responderlo.
El número salió de `BenchmarkDot_384`, que `vector` ya corría en su propia puerta de fase 2 —
solo faltaba registrarlo en MFLOPS en vez de ns/op.

**Qué queda abierto:** los ~259 ms son el piso, no el techo. El benchmark real del encoder,
cuando `transformer` exista, es el que confirma si la latencia sirve en un cuadro de
búsqueda. Lo que la medición ya descartó es el escenario malo — que el cómputo obligara a
bajar a la tabla estática de D5.

### D5 — El presupuesto del navegador elige el modelo; el español lo restringe

Dos restricciones, en este orden:

1. **El navegador tiene que poder descargar el artifact y correr un forward pass** (D4b).
   Fija el techo de tamaño, porque es el más apretado de los tres targets.
2. **El español es requisito duro.** Descarta los encoders solo-inglés, incluido el caso
   doloroso: `bge-small-en-v1.5` son 33M parámetros y está en el catálogo de Cloudflare, o
   sea que habría sido la opción más barata de todas.

**Elegido: un transformer multilingüe chico de 384 dims** — atención real, muy por encima de
una tabla estática, y sin GPU para el único trabajo que hace en el navegador (D4b). El nivel
grande (`bge-m3`, `qwen3-embedding-0.6b`: 1024 dims, ~600M) queda descartado porque solo
corre en el servidor, y eso obliga a un segundo modelo para la consulta.

Candidatos, cifras, el significado de «parámetros activos» y el método de elección están en
[`SMALL_MODEL_FOR_EMBEDING.md`](SMALL_MODEL_FOR_EMBEDING.md), **que manda sobre este
párrafo**. Primer candidato: `granite-embedding-97m-multilingual-r2` (28,3M de cuerpo, 384
dims, 32 K de contexto, Apache 2.0).

Lo único que este índice necesita fijar: el artifact se embarca **cuantizado a int8 con
escalas por fila** y se cachea en IndexedDB tras la primera descarga — se baja una vez por
navegador, no una vez por sesión.

### D6 — Licencias

`webtyp/vector-storage` es un fork de `nitaiaharoni1/vector-storage` (MIT). Todo
repositorio Go que porte su lógica — `vectordb` por sobre todo — debe llevar un archivo
`NOTICE` acreditando al autor original y reproduciendo los términos MIT. No es opcional.

### D7 — El isomorfismo vive en `orm` y `ddl`, no en `storage.Conn` crudo

Esto contradice una versión anterior de este plan y es la corrección más importante de `plans/agent.md`,
así que se argumenta acá una sola vez.

`storage.Executor` es, literalmente, una interfaz de strings SQL (`Exec(query string, ...)`,
`QueryRow(query string, ...)`). Un backend de navegador no tiene SQL que poner ahí: `indexdb`
contrabandea un `storage.Query` por `args[0]` y el modelo por `args[1]`; `storage/mem` ni
mira los argumentos y lee su propio `lastQ`. Es decir, **`storage.Conn` solo es portable si
se lo llama con el protocolo exacto `Compile(q, m) → Plan → Exec(plan.Query, plan.Args...)`**.
Llamarlo de otra forma compila y falla en runtime, distinto en cada backend.

Ese protocolo ya tiene implementación probada y con conformance: **`webtyp.com/orm`**
(`orm/db.go`, `orm/qb.go`). Escribir contra `storage.Conn` crudo es reimplementar `orm` con un
backend de navegador como banco de pruebas — la peor forma de descubrir que el protocolo
tenía una regla no escrita. Lo mismo para el esquema: «declarar tablas una vez y que cada
backend las materialice» es **`webtyp.com/ddl`**, con su propia suite de conformance.

**Consecuencia para `plans/agent.md`:** `agent` depende de `orm` y `ddl`, y **no** de `storage`
directamente. `vectordb` sí recibe un `storage.Conn` inyectado, porque opera sobre tablas
que él mismo define y necesita el control fino del lote — pero también habla el protocolo
`Compile → Exec`, nunca SQL.

`orm` y `ddl` bajo TinyGo `js/wasm`: **verificado, funcionan.** No hay condición pendiente
acá.

### D8 — El gate de host no prueba nada; `AGENTS.md` es la constitución de cada repositorio

Dos PR de esta ola (`weights` #1, `agent` #9) salieron con `gotest` verde y sin poder compilar para
el navegador. No fue descuido del ejecutor: **ningún plan escribió las restricciones**, y un plan
que lista comandos sin decir cuál manda deja que el ejecutor elija el que pasa.

Tres hechos que todo repositorio de este plan tiene que tener escritos en su propio `AGENTS.md`,
porque es el archivo que un agente lee antes de tocar código:

1. **`GOOS=js GOARCH=wasm go build ./...` no implica TinyGo.** Ese target usa la stdlib **completa**
   de Go; TinyGo usa un **subconjunto**. `net/http` es el caso testigo: compila para `js/wasm` y
   falla bajo TinyGo. El comando que decide es `tinygo build -target wasm` / `gotest -tinygo`.
2. **`encoding/json` cuesta ~1 MB de wasm.** Medido sobre un hello-world TinyGo: 114 KB sin él,
   1 115 KB con él. No rompe el build, se paga en cada descarga. `webtyp.com/json` existe por esto.
3. **No se inventa un puerto que ya existe.** Una interfaz se justifica cuando lo que abstrae
   **varía según el consumidor**: `storage.Conn` varía (indexdb / sqlt / postgres / mem), un cliente
   HTTP no. `weights` declaró `StorageConn` y `Fetcher` en vez de usar `storage.Conn` y
   `webtyp.com/fetch`, y el resultado es que la aplicación no puede pasarle la conexión que ya
   tiene abierta.

La tabla de reemplazos —`net/http`→`fetch`, `context`→`webtyp.com/context`,
`encoding/json`→`webtyp.com/json`, `fmt`/`errors`/`strconv`/`strings`→`webtyp.com/fmt`,
`time`→`webtyp.com/time`, `uuid`→`unixid`, `database/sql`→`storage`, y nada de `map[K]V`— va
**inline en cada `AGENTS.md`**, no por referencia: el ejecutor no clona `webtyp/devskills`.

Eso es duplicación deliberada y acotada. Lo que **no** se duplica es el *porqué*: cada `AGENTS.md`
es dueño de la misión de su repositorio y de sus trampas propias, y la doctrina genérica se mantiene
corta a propósito. El modelo a copiar es
[`storage/AGENTS.md`](https://github.com/webtyp/storage/blob/main/AGENTS.md).

Ya escritos: [`weights`](https://github.com/webtyp/weights/blob/main/AGENTS.md),
[`agent`](https://github.com/webtyp/agent/blob/main/AGENTS.md),
[`transformer`](https://github.com/webtyp/transformer/blob/main/AGENTS.md). `tokenizer`,
`weightsc` y `agentmemory` llevan el suyo **antes** de su primer despacho, no después del primer PR
fallido.

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
| `webtyp/weights` | **creado** | formato de artifact int8 + caché en navegador | 3 | [`weights/docs/PLAN.md`](https://github.com/webtyp/weights/blob/main/docs/PLAN.md) — PR #1 devuelto, ver (f) |
| `webtyp/weightsc` | **nuevo** | conversor offline safetensors → artifact (host-only) | 3 | pendiente — se escribe al corregir `weights`, ver (g) |
| `webtyp/transformer` | **creado** | grafo del encoder + kernels CPU/WASM | 3 | [`transformer/docs/PLAN.md`](https://github.com/webtyp/transformer/blob/main/docs/PLAN.md) — etapa 1 en ejecución; la 2 sigue en [`docs/plans/transformer.md`](plans/transformer.md), espera P1b |
| `webtyp/agent` | modificar | contrato `MemoryStore` segregado + conformance | 4 | [`docs/plans/agent.md`](plans/agent.md) |
| `webtyp/agentmemory` | **creado** | implementar `MemoryStore` sobre `orm` + `ddl` | 4 | pendiente — se escribe cuando `plans/agent.md` §2 cierre |
| `webtyp/vector-storage` | congelar | referencia histórica JS + mapa de port | 0 | [`vector-storage/docs/PLAN.md`](https://github.com/webtyp/vector-storage/blob/main/docs/PLAN.md) |

Notas, cada una es una decisión que alguien va a querer revertir sin leer el porqué:

- **(a) `indexdb` va antes que `storage` en la fase 1.** `model` sigue primero (los otros dos
  dependen de su tag), pero la conformance de `storage` solo puede exigir blobs una vez que
  existe un backend que los soporta; al revés queda un test rojo esperando a otro repo.
- **(b) `webtyp/jsvalue` no está en la tabla, y es deliberado.** Es dueño del códec
  `[]byte` ↔ JS y **tiene un bug real de corrupción** (§8), pero `indexdb` no lo usa para
  escribir: trae su propio `toJSValue`. Merece su propio plan; no bloquea a éste.
- **(c) `vector-storage` es fase 0.** Congelar una referencia histórica en JS no desbloquea
  ninguna persistencia.
- **(d) El chequeo de dimensión lo posee `model`.** `vectordb` llama a `model.ValidateVector`
  y borra su aritmética propia de `len(b)/4` — una verificación que la librería ya hace se
  llama, no se re-implementa. Alcance exacto en `model/docs/PLAN.md`: sobre un blob
  multi-vector solo verifica «múltiplo de 4»; la concordancia de `dim` la impone `vec_index`.
- **(e) `webtyp/webgpu` salió del plan; `plans/transformer.md` ya fue reescrito.** D4b borró
  la necesidad de GPU y el plan quedó reescrito para kernels CPU/WASM, **en dos etapas**: la
  1 (kernels + el benchmark que decide la fase) no depende del modelo y es despachable ya; la
  2 (el grafo) espera a que P1b elija, porque la arquitectura depende de cuál gane. El plan
  de WebGPU quedó en [`history/WEBGPU_ENCODER.md`](history/WEBGPU_ENCODER.md) y se desarchiva
  solo si la etapa 1 mide algo inaceptable.

- **(f) `weights` y `agent` volvieron de su PR con la misma falla de raíz, y es de los planes.**
  Los dos pasaron el gate de host —`gotest` verde, cobertura razonable— y ninguno compila para
  el navegador. `weights` importa `net/http`, que TinyGo no provee; `agent` importa
  `modernc.org/sqlite` desde el paquete raíz. Además `weights` declaró un `StorageConn` y un
  `Fetcher` propios, duplicando `storage.Conn` y `webtyp.com/fetch`. Ningún plan de esta ola
  escribió las restricciones; el checklist listaba los comandos pero nada fijaba *por qué*. La
  corrección está en **D8**.
- **(g) El conversor sale de `weights` a `webtyp/weightsc`.** Un `cmd/convert` con `os`, `flag` y
  `log` dentro del módulo rompe `gotest -tinygo` y `GOOS=js GOARCH=wasm go build ./...` de todo el
  repo, porque `./...` incluye `cmd/`. El ecosistema ya tiene el patrón para herramientas de
  build: `ormc` para `orm`, `ddlc` para `ddl`, `sitec` para `site`. El **writer** se queda en
  `weights` —no tiene dependencia de host y los tests de round-trip lo necesitan—; lo que se va es
  la CLI.

### Estado de la ola — 2026-09-19

| Repositorio | Fase | Estado |
|---|---|---|
| `model`, `indexdb`, `storage` | 1 | **publicado** |
| `vector`, `embed` (puerto), `vectordb` | 2 | **publicado** — `vector` v0.1.1 entregó los 3,3 GFLOPS de la puerta |
| `agent` (migración a `webtyp.com`) | — | PR [#9](https://github.com/webtyp/agent/pull/9) **cumple su alcance**, en espera: falta rebase para traer `AGENTS.md`. El intento previo (#8) se cerró por commit vacío |
| `weights` | 3 | PR [#1](https://github.com/webtyp/weights/pull/1) **devuelto con correcciones** (f) |
| `transformer` | 3 | etapa 1 **en ejecución** — sesión 18091215561444816771 |
| `tokenizer` | 3 | plan escrito, sin despachar |
| `weightsc` | 3 | sin plan (g) |
| `agent` (`MemoryStore` segregado), `agentmemory` | 4 | sin despachar |

Lo único que **no puede avanzar sin el usuario** sigue siendo **P1b** (recall@10 en español sobre
un corpus real): elige entre Granite 97M R2, Bekko a25m y Bekko a8m, y de eso depende la etapa 2
de `transformer`. Ver [`PENDING_ITEMS.md`](PENDING_ITEMS.md).

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

**Restricción de diseño que el ejecutor no va a descubrir solo:** una transacción de
IndexedDB se **auto-comitea** en cuanto el event loop cede sin requests pendientes, así que
un lote que espera cada `add()` falla en la segunda fila con `TransactionInactiveError`. Hay
que emitir todos los `add()` y esperar el `complete` **de la transacción**. El patrón, con el
código malo y el bueno lado a lado, está en
[`indexdb/docs/PLAN.md`](https://github.com/webtyp/indexdb/blob/main/docs/PLAN.md) §3.

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

**Entregable extra de esta fase, y es el que desbloquea la fase 3:** `BenchmarkDot_384`
corrido en navegador tiene que quedar registrado **en MFLOPS**, no solo en ns/op. Es el
número del que la fase 3 deriva el costo del forward pass (D4b), así que registrarlo en la
unidad equivocada obliga a correr todo de nuevo. Con y sin la variante desenrollada, y
anotando si el SIMD de TinyGo está disponible o no — `vector/docs/PLAN.md` §2 afirma que no
lo está, y esa afirmación es la que hay que confirmar o refutar acá.

La única excepción es `gotest -tinygo`, donde el conteo de allocations no es comparable
porque el GC es otro. Esa bandera es para los casos que requieren específicamente verificar
el target TinyGo — como los tests de blob de `indexdb` — no para el presupuesto de
allocations de `vector`.

### Fase 3 — El embedder: un modelo, tres targets

**Puerta de entrada: ABIERTA.** La fase 2 entregó el número (`vector` v0.1.1):
**~3,3 GFLOPS** escalares bajo TinyGo WASM, de donde los 856M FLOP de D4b salen en
**~259 ms** derivados. Eso descarta el desenlace malo —que el cómputo obligara a bajar a la
tabla estática— y deja el transformer en pie. Detalle y salvedades en
[`PENDING_ITEMS.md`](PENDING_ITEMS.md) P1.

Los 259 ms son un **piso**, no una predicción: `Dot` no incluye softmax, layernorm ni GELU.
El número que manda es el que mida `transformer` con su propio benchmark, y sus tres
desenlaces siguen tabulados en P1.

**Construcción:** `tokenizer` y `weights` en paralelo, luego
`transformer`, luego el adaptador de `embed` que los compone. Toda pieza de esta fase lleva su
`AGENTS.md` **antes** del despacho (**D8**): es la fase que más código compilado a WASM produce y
donde un gate de host verde engaña más. Antes de crear `transformer`
su plan ya está reescrito para CPU/WASM y viene en dos etapas (§5 nota (e)): la etapa 1
—kernels y el benchmark que decide esta fase— **no depende de qué modelo gane P1b**, así que
puede arrancar antes que `tokenizer` y `weights`.

`transformer` confirma con su propio benchmark lo que la aritmética predijo — un forward pass
real de 20 tokens. Si el medido se aparta más de 2× del derivado, el que manda es el medido y
la decisión se revisa.

**Puerta de salida:** el mismo código Go produce **el mismo vector para el mismo texto** en
los tres targets de D4 — navegador (WASM), backend (nativo) y Worker (WASM) — con igualdad
bit a bit o dentro de una tolerancia documentada. Y un corpus real en español se indexa en el
backend, viaja al cliente, y se busca **offline** con un recall@10 documentado.

Esa igualdad entre targets es el criterio que hace válido todo el plan: si los tres no
coinciden, no hay un solo espacio vectorial y el requisito de no re-indexar nunca se cae.

### Fase 4 — Memoria del agente
`agent` (contrato segregado + suite de conformance) → `webtyp/agentmemory` (implementación
sobre `orm` + `ddl`, con `SearchKnowledge` semántico). Ver [`plans/agent.md`](plans/agent.md).

**Orden dentro de la fase, y no es negociable:** la suite de conformance se escribe
**primero**, antes de una línea de `agentmemory`. Deja de ser una puerta posterior y pasa a
ser la especificación ejecutable de lo que hay que implementar. El skill lo justifica: si el
test con forma de consumidor es incómodo de escribir, la API es incómoda de usar — y eso se
descubre antes de la implementación en vez de después.

**Puerta:** esa suite pasa contra un backend SQL y contra `indexdb`, desde una sola
implementación en `agentmemory`. Más la matriz DDT completa de `DEFAULT_LLM_SKILL.md` §2.

**Puerta adicional, y es la que prueba el punto:** `go list -m all` sobre `webtyp.com/agent`
no menciona ningún motor de base de datos.

### Fase 5 — Optimización, si hace falta

Ya no hay encoder de GPU acá: D4b lo borró y `webtyp/webgpu` salió del plan (§5 nota (e)).
Lo que queda es trabajo de optimización, cada pieza con su propia condición de entrada y
ninguna bloqueante:

- **Cuantización int8 en `vector`**, que corre el techo de ~100 k documentos de D0. Entra
  cuando exista un corpus que lo pida.
- **BM25 léxico + fusión RRF**, que completa la mitad léxica que hoy cubre `LIKE`.
- **Un encoder sobre WebGPU**, solo si la puerta de la fase 3 midió que el forward pass en
  WASM no alcanza *y* se decidió no bajar al nivel estático. El plan archivado está en
  [`docs/history/WEBGPU_ENCODER.md`](history/WEBGPU_ENCODER.md) y se desarchiva si ese caso
  llega. Hoy no es el camino esperado.

## 7. Cambios que le pertenecen a este repositorio (`agent`)

`webtyp/agent` es el **orquestador**: bucle ReAct, FSM, ventana de contexto y registro de
herramientas. Coordina otras librerías; no implementa ninguna de sus capacidades.

La decisión de fondo —**la memoria sale de este repositorio** a `webtyp/agentmemory`— se
argumenta en **D7**. El diff concreto, la segregación de `MemoryStore` en cuatro contratos y
los defectos a corregir de paso están en [`docs/plans/agent.md`](plans/agent.md), igual que
el de cualquier otro repositorio de §5. Este archivo es el índice: no le pertenece código.

## 8. Riesgos

| Riesgo | Impacto | Mitigación |
|---|---|---|
| `js.ValueOf` hace pánico con `[]byte` en `indexdb.create`, y bajo TinyGo no hay `recover()` | Crash irrecuperable de la página en la primera escritura de vector | `toJSValue` en `indexdb/docs/PLAN.md` §1: `Uint8Array` + `js.CopyBytesToJS`, con rama `default` que devuelve error en vez de crashear |
| `indexdb` descarta columnas blob en silencio en 4 caminos más (`update` ×2, `Scan`, `checkCondition`) | Actualizar cualquier columna **borra el vector**; un blob nunca matchea una condición | `indexdb/docs/PLAN.md` §2: `case FieldBlob` en los cuatro switches **y** una rama `default` que falle ruidosamente |
| **`jsvalue` codifica `[]byte` como JS string — confirmado, no hipotético** | Corrupción silenciosa para cualquier consumidor que encode por `jsvalue` | Fuera del camino crítico de este plan (`indexdb` no usa esa ruta), pero es un bug real: merece su propio plan — ver nota abajo |
| **El forward pass de una consulta en WASM es más lento de lo estimado** | El nivel del medio de D5 no sirve en el navegador | **Es la única premisa sin medir.** La fase 3 abre con un benchmark y tiene tres desenlaces escritos, uno de ellos «bajar al nivel estático». No es un riesgo a mitigar: es una medición a hacer antes de construir |
| Los tres targets de D4 producen vectores distintos por diferencias de punto flotante | Se pierde el espacio vectorial común y hay que re-indexar — el requisito central se cae | Es la puerta de salida de la fase 3: igualdad bit a bit entre navegador, backend y Worker, o tolerancia documentada. `vec_index.model_id` detecta la divergencia al cargar |
| El artifact del transformer chico pesa ~120 MB (D5) | Primera carga larga en conexión mala | Tabla de embeddings int8 con escalas por fila, caché en IndexedDB tras el primer fetch —se baja una vez por navegador, no por sesión—, y el nivel estático de D5 (~32–64 MB) como plan B medido, no supuesto |
| Una transacción IDB se auto-comitea al ceder el event loop | La inserción por lote falla con `TransactionInactiveError` en la segunda fila | Contrato de lote especificado en la puerta de fase 1: emitir todos los `add()` y esperar `complete` de la transacción |
| La cuota de IndexedDB desaloja el índice | Pérdida silenciosa de datos | `navigator.storage.persist()` al iniciar, `estimate()` antes de escribir, desalojo LRU propio antes que el del navegador |
| La arena excede la memoria del navegador pasados ~100 k docs | OOM | Techo documentado (D0), cuantización int8 en la fase 5 |
| Vectores y texto se desincronizan en una escritura parcial | Resultados corruptos | Una sola transacción abarcando ambos stores; una fila de cabecera `dim`/`model_id` rechaza una arena que no corresponde al cargar |

**Nota sobre `jsvalue`, porque una versión anterior de este plan lo tenía al revés.** El
riesgo decía «`ScanValue` podría no manejar `Uint8Array`»; los dos términos son incorrectos.
**Leer ya funciona** (`decodeBytes` en `jsvalue/codec_wasm.go` hace `InstanceOf(Uint8Array)` +
`CopyBytesToGo`). **Escribir está roto con certeza**: las tres rutas de encode —`ToJS`,
`jsObjectWriter.Bytes`, `jsArrayWriter.Bytes`— hacen `string(val)`, y un Go string cruza a JS
decodificado desde UTF-8, así que todo byte que no forme UTF-8 válido se sustituye por
U+FFFD. Un float32 es binario arbitrario: la sustitución es la regla, no el borde, y vuelve
**sin error y sin pánico**, que es peor que el crash de `js.ValueOf`.

No bloquea este plan: `indexdb` trae su propio `toJSValue` y no delega el encode (§5 nota
(b)). Sigue roto para cualquier otro consumidor, y el arreglo es mecánico —
`Uint8ArrayClass.New(len)` + `CopyBytesToJS` en los tres writers, compatible hacia atrás
porque el decode ya acepta ambos. Es un plan aparte; anotado acá para que no se pierda.

## 9. Decisiones abiertas

- **O1. — CERRADA.** *Compatibilidad del espacio vectorial entre entornos.* Un solo modelo,
  una sola implementación en Go, tres targets de compilación — ver **D4**, **D4b** y **D5**.
  El catálogo de Workers AI y Ollama quedaron descartados como fuente del modelo porque
  ningún modelo multilingüe de ahí entra en el navegador, y un modelo que solo corre en el
  servidor obliga a un segundo modelo para la consulta, que es justamente lo que rompe el
  requisito. Lo único que queda por medir es el forward pass en WASM: puerta de entrada de
  la fase 3.
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
