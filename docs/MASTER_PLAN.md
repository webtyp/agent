---
PLAN: "feat: búsqueda semántica nativa en el navegador — índice maestro"
TAG: v0.3.0
EXECUTOR: unassigned
REVIEWER: none
---

> Este es el **plan índice** — arquitectura, decisiones compartidas, y **sobre todo, lo que
> falta**. El *cómo se llegó hasta acá* (cada corrección de rumbo, cada postmortem de PR, las
> decisiones ya cerradas) vive en
> [`docs/history/SEMANTIC_SEARCH_WAVE.md`](history/SEMANTIC_SEARCH_WAVE.md) — congelado, no se
> actualiza. Este archivo sí se actualiza, y se mantiene corto a propósito: si una sección de
> acá ya no describe algo pendiente o una regla que todavía rige, se va al histórico.
>
> **Nota de idioma:** la prosa va en español. Los bloques de código se mantienen con
> comentarios en inglés porque son código literal destinado a los repositorios.

# Plan — Búsqueda semántica en el navegador, sobre IndexedDB

## 1. Objetivo

**Búsqueda semántica offline total en el navegador** — subir un documento, guardarlo y
buscarlo corren enteros en el navegador, sin red, con el mismo modelo Go compilado a TinyGo
en los tres targets (D4). El corpus vive en IndexedDB. Detalle y argumento completo:
[`history/SEMANTIC_SEARCH_WAVE.md`](history/SEMANTIC_SEARCH_WAVE.md) §1.

## 2. Arquitectura

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

   Navegador embebe CONSULTAS Y DOCUMENTOS (chunks ≤256 tokens, D4c) — siempre, sin red.
   MISMO código Go, MISMOS pesos, MISMO Dim=64 (Matryoshka, D0) → un solo espacio vectorial.
```

Cada flecha es una dependencia de compilación. No hay ciclos; ningún repositorio depende de
`agent`. `vectordb` importa solo la interfaz `embed.Embedder`, nunca el adaptador — por eso el
puerto es fase 2 y el adaptador (`StaticEmbedder`) es fase 3.

## 3. Decisiones que todavía rigen

Argumento completo de cada una: [`history/SEMANTIC_SEARCH_WAVE.md`](history/SEMANTIC_SEARCH_WAVE.md) §3.
Acá, solo la forma final.

- **D0 — `Dim = 64`, no 384.** `bekko-embedding-v1-a8m` entrena con Matryoshka: los primeros
  64 componentes de su vector nativo de 384 son, por construcción, un embedding válido y
  completo. `transformer.Encode` sigue devolviendo 384 siempre — truncar a 64 y renormalizar
  L2 es trabajo del adaptador `embed` (`StaticEmbedder`), antes de que el vector salga de ahí.
  Arena: `N × 64 × 4` = 256 bytes/doc; 100 000 documentos = 25 MB. **Sin confirmar todavía:**
  cuánto degrada `recall@10` a 64 dims contra 128 — no hay cifra publicada, es la fase 3 la
  que tiene que medirlo (§5).
- **D1 — Un vector es `[]byte` en disco, una porción de arena compartida en memoria.**
  Nunca `[]float32` por documento — `js.ValueOf` no transporta vectores y TinyGo no tiene
  `recover()`. Normalizados L2 siempre (coseno = producto punto).
- **D2 — Vectores por shards (1024/fila), documentos por fila.** Insertar documentos usa
  batching de transacción IndexedDB; los vectores ya llegan agrupados por shard.
- **D3 — Sin índice aproximado.** IndexedDB no puede indexar un vector; toda consulta kNN es
  un barrido completo sobre la arena. HNSW/IVF fuera de alcance hasta que un corpus real lo
  pida.
- **D4/D4b — Un modelo, una implementación Go, tres targets.** Sin WebGPU (el cómputo de una
  consulta, y ahora también de un chunk de 256 tokens, entra en WASM/CPU puro).
- **D4c — Chunk máximo de 256 tokens** para documentos subidos por el usuario en el
  navegador — a esa escala el costo es ~1,6 s/chunk, dominado por el término lineal, no el
  cuadrático de la atención. El troceo es responsabilidad de quien sube el documento
  (`agentmemory.KnowledgeStore.SaveKnowledge` o el flujo que lo llame), no de `vectordb` ni de
  `embed`.
- **D5 — Modelo: `bekko-embedding-v1-a8m`.** Multilingüe, 4 capas, mean pooling, MIT,
  soporte Matryoshka (de donde sale D0). Elegido sobre `granite-embedding-97m-multilingual-r2`
  (forward pass real medido en ~526 ms, banda "viable con reservas") por su latencia
  estimada ~4× menor. Candidatos y método: [`SMALL_MODEL_FOR_EMBEDING.md`](SMALL_MODEL_FOR_EMBEDING.md).
- **D6 — Licencias.** `vectordb` (fork de `nitaiaharoni1/vector-storage`, MIT) lleva
  `NOTICE` acreditando al autor original.
- **D7 — El isomorfismo vive en `orm`/`ddl`, nunca en `storage.Conn` crudo.** `agent`
  depende de `orm`/`ddl`, no de `storage` directamente. `vectordb` recibe un `storage.Conn`
  inyectado pero solo lo llama vía `Compile → Exec`, nunca SQL cruda.
- **D8 — `AGENTS.md` es la constitución de cada repositorio.** `GOOS=js GOARCH=wasm go
  build` no implica TinyGo — `gotest -tinygo` / `tinygo build -target wasm` deciden.
  `encoding/json` cuesta ~1 MB de wasm. No se inventa un puerto que ya existe. La tabla de
  reemplazos (`net/http`→`fetch`, `context`→`webtyp.com/context`, `encoding/json`→
  `webtyp.com/json`, `fmt`/`errors`/`strconv`/`strings`→`webtyp.com/fmt`, `time`→
  `webtyp.com/time`, `uuid`→`unixid`, `database/sql`→`storage`, sin `map[K]V`) va inline en
  cada `AGENTS.md`, nunca por referencia.

## 4. Estado de los repositorios

| Repositorio | Fase | Estado |
|---|---|---|
| `model`, `indexdb`, `storage` | 1 | publicado |
| `vector`, `vectordb` | 2 | publicado |
| `embed` (puerto + `MockEmbedder`) | 2 | publicado |
| `tokenizer` | 3 | publicado v0.2.0 — `Scheme` plegable (Granite + Bekko) |
| `weights` | 3 | publicado v0.1.0 |
| `weightsc` | 3 | publicado v0.1.1 — probado contra los archivos reales de `bekko-embedding-v1-a8m` |
| `transformer` | 3 | publicado v0.1.3 — grafo ModernBERT + `Config.Pooling`, Granite y Bekko a8m |
| `embed` (`StaticEmbedder`, el adaptador) | 3 | **en curso** — ver §5 |
| `agent` | 4 | publicado v0.5.0 — `MemoryStore` segregado, sin stdlib prohibida |
| `agentmemory` | 4 | publicado v0.1.0 |
| `vector-storage` | 0 | congelado (referencia histórica) |

## 5. Pendiente

Esto es lo que falta — el resto del documento es contexto para hacer esto, no una lista
aparte.

1. **`webtyp/embed` — `StaticEmbedder`.** Despachado (`docs/PLAN.md` de ese repo,
   `codejob`), en revisión. Compone `tokenizer` + `weights` + `transformer` para
   `bekko-embedding-v1-a8m`, trunca a 64 dims (D0) y renormaliza. Es lo único que falta para
   que el resto de esta ola sirva de punta a punta.

2. **Puerta de salida de fase 3 (bloqueada por el punto 1):**
   - El mismo texto produce el mismo vector, bit a bit o con tolerancia documentada, en los
     tres targets de D4 (navegador WASM, backend nativo, Worker WASM).
   - Un corpus real en español, indexado offline en el navegador, con `recall@10`
     documentado — el número que confirma o descarta si 64 dims (D0) alcanza.

3. **Artifact real de producción.** `weightsc` ya corrió contra los 3 archivos reales de
   `bekko-embedding-v1-a8m` (`model.safetensors`, `config.json`, `tokenizer.json`) y produjo
   un `.wtypw` de 109 MB + `.merges` de 5,8 MB, verificados con `weights.Open` — la cadena de
   conversión funciona. **Falta decidir dónde se hostea ese artifact** para que el navegador
   lo descargue (D5: se cachea en IndexedDB tras la primera descarga) — no hay plan todavía
   para esa pieza de despliegue; no es código de ningún repositorio de esta lista.

4. **`jsvalue` — bug de corrupción en su ruta de escritura.** Confirmado, no bloquea nada de
   esta ola (`indexdb` no delega el encode), pero sigue roto para cualquier otro consumidor.
   Arreglo mecánico conocido (`Uint8ArrayClass.New(len)` + `CopyBytesToJS` en los tres
   writers) — sin plan propio despachado todavía.

5. **Fase 5 — optimización, ninguna bloqueante, ninguna con condición de entrada cumplida
   todavía:**
   - Cuantización int8 en `vector` — entra cuando un corpus real se acerque al techo de D0
     (ahora mucho más lejos, a 64 dims: ~1M documentos antes de los 150 MB).
   - BM25 léxico + fusión RRF — completa la mitad léxica que hoy cubre `LIKE`.
   - Encoder sobre WebGPU — descartado salvo que la puerta de fase 3 mida que WASM no
     alcanza. Plan archivado en [`history/WEBGPU_ENCODER.md`](history/WEBGPU_ENCODER.md).

## 6. Riesgos abiertos

| Riesgo | Impacto | Mitigación |
|---|---|---|
| `jsvalue` corrompe `[]byte` en su ruta de escritura (confirmado) | Corrupción silenciosa para cualquier consumidor que no sea `indexdb` | Fuera del camino crítico de esta ola; arreglo mecánico conocido, sin plan despachado — ver §5.4 |
| `recall@10` a 64 dims sin medir | Si degrada demasiado, hay que subir a 128 y re-medir arena/costo | Es la puerta de salida de fase 3 (§5.2) — se mide, no se asume |
| El artifact (109 MB) no tiene dónde hostearse todavía | Bloquea la primera descarga real en un navegador | Pendiente, no es código — ver §5.3 |
| La cuota de IndexedDB desaloja el índice | Pérdida silenciosa de datos | `navigator.storage.persist()` al iniciar, `estimate()` antes de escribir, LRU propio antes que el del navegador |
| Vectores y texto se desincronizan en una escritura parcial | Resultados corruptos | Una sola transacción abarcando ambos stores; cabecera `dim`/`model_id` rechaza una arena que no corresponde |
