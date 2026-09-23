---
DOC: "Historial completo — búsqueda semántica nativa en el navegador"
STATUS: congelado — el trabajo pendiente vive en docs/MASTER_PLAN.md, no acá
---

> **Este documento es historia, no un plan.** Es el `MASTER_PLAN.md` completo tal como
> evolucionó entre el 2026-09-16 y el 2026-09-23 — cada decisión de diseño con su argumento
> completo, cada corrección de rumbo, cada postmortem de PR. Se separó del plan vivo el
> 2026-09-23 porque el archivo había crecido a 813 líneas y la mayoría ya no era información
> que alguien necesitara para seguir trabajando — era el registro de cómo se llegó hasta acá.
> `MASTER_PLAN.md` ahora tiene solo las decisiones que todavía rigen y lo que falta. Si una
> sección de acá contradice al plan vivo, **gana el plan vivo** — este archivo no se actualiza.

# Plan — Búsqueda semántica en el navegador, sobre IndexedDB (congelado 2026-09-23)

## 1. Objetivo

**Búsqueda semántica offline total en el navegador.** El corpus —texto y vectores— vive en
IndexedDB, en la máquina del usuario, y la consulta no sale de ahí. Sin dependencia de
ninguna librería JavaScript. La persistencia es `webtyp.com/indexdb`, extendido donde su API
actual no alcanza a expresar el problema.

El flujo, que es lo que gobierna todo el resto del documento:

| Momento | Dónde ocurre | ¿Necesita red? |
|---|---|---|
| Subir un documento | el navegador tokeniza y embebe los chunks, localmente | **no — offline total** |
| Guardar | texto + vectores quedan en IndexedDB (D2) | no |
| **Buscar** | el navegador embebe la consulta y hace el kNN sobre la arena local | **no — offline total** |

**Sin excepción, y sin backend en el camino.** Una versión anterior de este plan mandaba la
subida de documentos al backend "porque es la máquina rápida" — eso contradecía el objetivo
mismo de esta sección: si subir un documento necesita red, no hay offline total, hay offline
*para buscar nada más*. Corregido: los dos caminos —subir y buscar— corren enteros en el
navegador, siempre, incluso con la máquina desconectada. El costo que esto tiene y cómo se
paga (el chunk se achica, no el alcance) está en **D4c**.

El backend/Worker de D4 no desaparece: sigue existiendo para el caso *distinto* de indexar
contenido que el operador de la aplicación posee de antemano (por ejemplo, el contenido
estático de un sitio, indexado una vez en el build) — ahí no hay un usuario esperando en un
navegador, así que la restricción de esta sección no aplica. Lo que este plan elimina es
el backend como paso obligatorio de la subida de un documento **del usuario**.

**La consecuencia que ordena todo el plan:** si todo —consulta y documento— es offline, el
navegador necesita el modelo para **los dos**. Y los vectores de documento y de consulta
tienen que caer en el mismo espacio, así que tienen que ser **los mismos pesos**. No hay
forma de esquivarlo: un transformer que embebe 20 tokens tiene los mismos parámetros que uno
que embebe 300; la entrada corta baja el cómputo, no el tamaño.

Por eso el modelo se elige por el presupuesto del navegador (D5), y por eso hay **una sola
implementación en Go** corriendo en tres targets (D4).

El `MemoryStore` del agente se reescribe una sola vez contra `webtyp.com/orm`, de modo que
el mismo código de memoria corra sobre un backend SQL (servidor) y sobre IndexedDB
(navegador).

## 2. Por qué esto reemplaza al estudio de SQLite

El diseño anterior ([`MEMORY_SQLITE.md`](MEMORY_SQLITE.md)) asumía un único
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

## 3. Decisiones globales de diseño — argumento completo

### D0 — Dimensión de trabajo: primero 384, corregido a 64 (Matryoshka)

Versión original: **`Dim = 384`**, la dimensión nativa de los transformers multilingües
chicos de D5. Arena: `N × 384 × 4` = 1,5 KB/documento; 100 000 documentos = 150 MB, techo que
hacía obligatoria la cuantización int8.

**Corrección 2026-09-23:** `bekko-embedding-v1-a8m` entrena con Matryoshka Representation
Learning — los primeros *k* componentes de su vector de 384 ya son, por construcción, un
embedding válido para *k* ∈ {256, 128, 64}. El plan vivo fija `Dim = 64` — ver
`MASTER_PLAN.md` D0 para la decisión final y las cifras actualizadas.

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
   `Uint8Array`: sin JSON, sin base64, sin boxing, ni `string` (ver §5 más abajo, la nota de
   `jsvalue`).
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

### D2 — Los vectores se persisten por shards; los documentos, por fila

Una fila de `vec_shards` contiene `ShardSize` (1024 por defecto) vectores como un único
blob. El arranque en frío pasa a `N/1024` deserializaciones de structured clone en vez de
`N`, y cada una es un solo `CopyBytesToGo` directo a su offset en la arena.

El texto y los metadatos viven en un object store **separado**, que se lee únicamente para
el top-k final — nunca durante el scoring. En RAM, junto a la arena, vive un arreglo
compacto de cabeceras (`id`, `tags`, `created`, `hits`, `deleted`) usado para el
prefiltrado.

Ojo con no confundir los dos usos de "1024": `ShardSize = 1024` (1024 vectores en una fila,
evita N structured clones al arrancar) vs. el lote de inserción (`k` filas en una
transacción, evita `k` transacciones IDB al indexar documentos) — son cosas distintas que se
parecen. La inserción por lote es para el store de documentos; los vectores ya llegan
agrupados por el shard. Consecuencia: `vectordb` no necesita ninguna API de scan en streaming
de parte de `storage`.

### D3 — IndexedDB no puede indexar un vector. Toda consulta kNN es un barrido completo.

Ningún árbol B sobre un espacio de 384 (hoy 64) dimensiones ayuda. Esto no es una limitación
a sortear en v1; es la forma del problema. El diseño minimiza el costo por candidato (D1, D2)
en vez de intentar evitar candidatos. Los índices aproximados (HNSW/IVF) quedan
explícitamente fuera de alcance hasta que exista un corpus que los necesite.

### D4 — Un modelo, una implementación en Go, tres targets

`embed.Embedder` es el contrato único. Detrás hay una sola implementación —`tokenizer` +
`weights` + el grafo del encoder en `transformer`, todo en Go— corriendo en tres targets de
compilación (navegador TinyGo/WASM, backend Go nativo, Worker de Cloudflare TinyGo/WASM), no
tres implementaciones. Eso es lo que hace que los vectores sean comparables para siempre y
entre instalaciones.

**Lo que esto elimina:** Ollama y el catálogo de Workers AI — evaluados
(`CLOUDFLARE_AI_WORKER.md`), ningún modelo del catálogo es a la vez multilingüe y chico
(`bge-m3`/`qwen3-embedding-0.6b` son ~600M, entran en backend pero no en navegador; un modelo
que solo corre en servidor obliga a un segundo modelo para la consulta, perdiendo el espacio
vectorial común).

### D4b — El cómputo de una consulta es chico, y eso borra WebGPU

WebGPU entró al plan para embeber documentos grandes rápido — resuelto después por D4c
(achicar el chunk), no por acelerar el cómputo.

Argumento original, sobre la consulta (~20 tokens): un forward pass de 20 tokens sobre 12
capas de 384 dims son ~428M MAC ≈ 856M FLOP [calc]. Eso cabe en WASM sobre CPU sin
`navigator.gpu`. **Medido:** `vector` v0.1.1, ~233 ns/op para `Dot` de 384 dims bajo TinyGo
WASM ≈ 3,3 GFLOPS escalares, sin SIMD (confirmando `vector/docs/PLAN.md` §2). 856M FLOP se
pagan en ~259 ms — un piso, no una predicción (`Dot` no incluye softmax/layernorm/GELU).

**Etapa 1 de `transformer`, MEDIDO, v0.1.0:** `BenchmarkEncode_20x12x384` bajo
`tinygo test -target wasm`: 290–343 ms, promedio ~313 ms — banda media, "viable con
reservas". Con el grafo real de Granite (etapa 2, 12 capas reales en vez del arnés
sintético): ~526 ms. Con `bekko-embedding-v1-a8m` (4 capas): estimado ~130 ms [calc], banda
"viable" sin reservas — el argumento directo detrás del cambio de modelo, ver D5.

### D4c — Subir un documento también es offline, y eso fija el tamaño de chunk

Corrección 2026-09-21: una versión anterior mandaba la subida de documentos al backend
("es la máquina rápida"), rompiendo el objetivo de offline total. La subida tiene que ser
tan offline como la búsqueda.

El costo de un chunk largo no escala como el de una consulta — a 8 000 tokens el término
cuadrático de la atención global domina (con la forma real de `bekko-embedding-v1-a8m`, 2 de
4 capas son globales): `FLOP(seq) ≈ 19,3M×seq + 3072×seq²` [calc]. A 8 000 tokens eso son
~351 GFLOP ≈ **~106 s por chunk** — inaceptable. A 256 tokens, ~1,6 s, dominado por el
término lineal.

**Decisión: chunk máximo de 256 tokens** para documentos embebidos en el navegador — sigue
vigente en el plan vivo, ver `MASTER_PLAN.md`.

### D5 — El presupuesto del navegador elige el modelo; el español lo restringe

Candidato inicial (P1b, 2026-09-20): `granite-embedding-97m-multilingual-r2` (28,3M de
cuerpo, 384 dims, 32K contexto, Apache 2.0) — elegido sobre `multilingual-e5-small` y
`paraphrase-multilingual-MiniLM-L12-v2` (peor MTEB), y sobre `bge-m3`/`qwen3-embedding-0.6b`
(demasiado grandes para el navegador). Detalle completo en
[`SMALL_MODEL_FOR_EMBEDING.md`](../SMALL_MODEL_FOR_EMBEDING.md).

**Reabierto y cambiado, 2026-09-21/22:** el forward pass real medido de Granite (526 ms,
etapa 2 de `transformer`) cae en la banda "viable con reservas", y `bekko-embedding-v1-a8m`
(4 capas, ~130 ms estimado) ya estaba nombrado como plan B en el mismo documento. Decisión
directa del usuario: cambio a `bekko-embedding-v1-a8m`. Esto disparó una ola completa de
re-verificación contra el modelo real (no contra Granite de memoria): `tokenizer` ganó un
esquema `Metaspace` plegable (Bekko usa un pretokenizador distinto a Granite — ver
`tokenizer`'s propio historial), `transformer` ganó `Config.Pooling` (Bekko usa mean pooling,
no CLS), y ninguno de los dos repitió trabajo porque el grafo ModernBERT y el motor de merges
BPE ya eran genéricos.

El artifact se embarca cuantizado a int8 con escalas por fila y se cachea en IndexedDB tras
la primera descarga.

### D6 — Licencias

`webtyp/vector-storage` es un fork de `nitaiaharoni1/vector-storage` (MIT). Todo repositorio
Go que porte su lógica —`vectordb` por sobre todo— debe llevar un archivo `NOTICE`
acreditando al autor original y reproduciendo los términos MIT. No es opcional.

### D7 — El isomorfismo vive en `orm` y `ddl`, no en `storage.Conn` crudo

`storage.Executor` es, literalmente, una interfaz de strings SQL. Un backend de navegador no
tiene SQL que poner ahí: `indexdb` contrabandea un `storage.Query` por `args[0]`, `storage/mem`
lee su propio `lastQ`. `storage.Conn` solo es portable si se lo llama con el protocolo exacto
`Compile(q, m) → Plan → Exec(plan.Query, plan.Args...)` — llamarlo de otra forma compila y
falla en runtime, distinto en cada backend. Ese protocolo es `webtyp.com/orm` (DML) +
`webtyp.com/ddl` (schema), ambos con conformance propia y verificados bajo TinyGo `js/wasm`.

Consecuencia: `agent` depende de `orm`/`ddl`, no de `storage` directamente. `vectordb` sí
recibe un `storage.Conn` inyectado (opera sobre tablas propias, necesita control fino de
lote) pero también habla `Compile → Exec`, nunca SQL.

### D8 — El gate de host no prueba nada; `AGENTS.md` es la constitución de cada repositorio

Dos PR de la primera ola (`weights` #1, `agent` #9) salieron con `gotest` verde y sin poder
compilar para el navegador — ningún plan escribió las restricciones. Tres hechos que todo
`AGENTS.md` de este ecosistema tiene que fijar: (1) `GOOS=js GOARCH=wasm go build` usa la
stdlib completa, no implica TinyGo — `tinygo build`/`gotest -tinygo` es lo que decide; (2)
`encoding/json` cuesta ~1 MB de wasm (114 KB → 1 115 KB medido en hello-world); (3) no se
inventa un puerto que ya existe (`weights` declaró `StorageConn`/`Fetcher` propios duplicando
`storage.Conn`/`fetch`).

La tabla de reemplazos (`net/http`→`fetch`, `context`→`webtyp.com/context`,
`encoding/json`→`webtyp.com/json`, `fmt`/`errors`/`strconv`/`strings`→`webtyp.com/fmt`,
`time`→`webtyp.com/time`, `uuid`→`unixid`, `database/sql`→`storage`, sin `map[K]V`) va inline
en cada `AGENTS.md`, no por referencia — el ejecutor no clona `webtyp/devskills`. Modelo a
copiar: [`storage/AGENTS.md`](https://github.com/webtyp/storage/blob/main/AGENTS.md). Esta
regla se aplicó, con el mismo resultado, una tercera vez en `webtyp/agent` (PR #11,
2026-09-22): 8 archivos con `context` de stdlib, 5 con `time`, `net/http`+`encoding/json` en
`mcp_client.go`, `uuid`, y `map[K]V` en `fsm.go`/`mcp_registry.go` — ninguno bloqueaba
`gotest`, todos bloqueaban `tinygo build`.

## 4. Postmortems de PR — por qué cada corrección existe

- **`weights` #1 y `agent` #9 (D8).** Los dos pasaron el gate de host y ninguno compilaba
  para el navegador (`weights` importaba `net/http`; `agent` importaba
  `modernc.org/sqlite`). Cerrado: `weights` #1 volvió con la tabla de reemplazos aplicada
  punto por punto y `gotest -tinygo` en verde; `agent` #9 ya cumplía desde el primer intento,
  solo le faltaba el rebase. Ambos mergeados v0.1.0.
- **El conversor sale de `weights` a `webtyp/weightsc`.** Un `cmd/convert` con
  `os`/`flag`/`log` dentro del módulo rompe `gotest -tinygo` y el build wasm de todo el repo
  (`./...` incluye `cmd/`). Patrón ya establecido: `ormc`/`ddlc`/`sitec`. El writer se queda
  en `weights` (sin dependencia de host, lo necesitan los tests de round-trip); la CLI se va.
- **Un ejecutor que dice "rebasé" puede no haberlo hecho.** El commit de `agent` #9 copió
  `AGENTS.md` a mano en vez de rebasar, perdiendo un borrado que `main` había hecho en la
  misma ventana — `modify/delete conflict` que `gh pr merge` rechazó pero `git merge-tree` de
  plumbing no mostró con claridad. El mismo push pisó el frontmatter de `docs/PLAN.md`
  (`STATUS` volvió a `running`, se perdió `PR:`) por partir de una copia vieja. Se repitió,
  con el mismo síntoma exacto, en `agentmemory` #1 (2026-09-22): un commit automático de
  Jules en respuesta a un comentario de review restauró `STATUS: running` desde una copia
  vieja del archivo. **Verificación que sí lo detecta:** un merge real en un worktree
  descartable, no el resumen del bot ni `git merge-tree` sin más; el fix es reescribir el
  frontmatter a mano y confirmar antes de cerrar el loop.
- **`transformer` #1 reimplementó el producto punto que D8 prohíbe, adentro del propio
  arnés que mide si `Dot` alcanza.** `bench_test.go` hacía el bucle `Q·K` a mano en vez de
  llamar `vector.Dot`. Además el README documentaba `go test -bench` (nativo, ~2,5× más
  rápido, la lectura mentirosa que el propio `AGENTS.md` nombra). Corregido: la llamada pasa
  por `Dot`, el README usa `tinygo test -target wasm -bench`.
- **La ola de P1b: `weightsc`, `tokenizer`, `transformer` etapa 2 — verificados contra el
  modelo real, no contra un paper.** Se leyó directo el header real de `model.safetensors`
  (74 tensores para Granite) y el `tokenizer.json` real. Dos correcciones que ese trabajo
  encontró antes de escribir código: el tokenizador es BPE a nivel de byte, no WordPiece (el
  plan anterior asumía WordPiece); el artifact tiene versiones ONNX cuantizadas publicadas
  por IBM pero usan un esquema asimétrico con zero-point que no cabe en el formato simétrico
  de `weights` — no sirven de atajo.
- **La revisión del PR de `tokenizer` (Scheme plegable) encontró un bug real de
  backtracking en `matchAlt12`** — no reproducía la regex real para caracteres de clase
  compartida (CJK/árabe/devanagari pegados a una mayúscula latina), verificado contra el
  módulo `regex` de Python. Corregido con la búsqueda de `prefixLen` de mayor a menor que
  simula el backtracking real.
- **La revisión del PR de `agentmemory` encontró 5 defectos**: falta de desempate en
  `OrderBy(CreatedAt)` (resolución de 1s), `SearchKnowledge` etiquetando conocimiento global
  con el `sessionID` del que buscó, `limit<=0` cayendo al default de `vectordb` (4) en vez de
  "sin límite", un desbordamiento de `int` sin guardia en el decodificador netstring, y un
  import directo de `webtyp.com/sqlt` que su propio `AGENTS.md` prohíbe (además de no probar
  nada real, porque `storage/mem` ignora el DDL compilado). Los cinco corregidos antes de
  mergear.

## 5. Notas del mapa de repositorios (ola original)

- **`indexdb` antes que `storage` en fase 1.** La conformance de `storage` solo puede exigir
  blobs una vez que existe un backend que los soporte.
- **`webtyp/jsvalue` no está en el mapa, deliberado.** Dueño del códec `[]byte` ↔ JS, tiene
  un bug real de corrupción en su ruta de escritura (ver Riesgos, `MASTER_PLAN.md` §6) pero
  `indexdb` no lo usa para escribir — trae su propio `toJSValue`. Merece su propio plan; no
  bloqueó a este.
- **El chequeo de dimensión lo posee `model`** (`model.ValidateVector`) — `vectordb` lo
  llama, no reimplementa su propia aritmética de `len(b)/4`.
- **`webtyp/webgpu` salió del plan** (D4b) — el plan quedó archivado en
  [`WEBGPU_ENCODER.md`](WEBGPU_ENCODER.md), se desarchiva solo si una medición futura muestra
  que WASM no alcanza y no se quiere bajar a un nivel estático. No fue el caso.

## 6. Decisiones abiertas, todas cerradas

- **O1 — Compatibilidad del espacio vectorial entre entornos.** Un solo modelo, una sola
  implementación en Go, tres targets. Workers AI y Ollama descartados: ningún modelo
  multilingüe de esos catálogos entra en el navegador.
- **O2 — ¿`webtyp/binary` provee un códec little-endian de float32?** No, y `vector` no debe
  depender de él — es un serializador orientado a campos (varints + nombres), no un códec de
  arreglo crudo. `vector` define el suyo, sin dependencias.
- **O3 — Forma del filtrado por metadatos.** `model.RawJSON` opaco + columna de tags
  delimitada (`|a|b|c|`), filtrable con `LIKE` en RAM sobre el arreglo de cabeceras de D2 —
  no un `FieldStringSlice` nuevo, mismo argumento que D1 §4 para vectores.
