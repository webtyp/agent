---
DOC: "Lo que sigue sin decidir"
STATUS: 2 mediciones pendientes; la primera se hace en webtyp/vector, fase 2
RELATED: docs/PLAN.md
---

> Este documento contiene **solo lo pendiente**. Lo que ya se decidió vive en el plan que lo
> ejecuta, no acá — dos copias de una regla se desincronizan y la vieja es la que alguien
> sigue. El registro de lo resuelto está al final, en una línea cada uno.
>
> Criterio de evaluación:
> [`devskills/skills/api-design/SKILL.md`](../../devskills/skills/api-design/SKILL.md),
> cuyo núcleo es SOLID.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus
> comentarios en inglés.

# P1 — Cuántos MFLOPS de f32 hace TinyGo en WASM

**No es una decisión: es una medición**, y no necesita ningún repositorio nuevo. Es la única
premisa del plan que no está verificada, y de ella dependen `transformer`, `tokenizer`,
`weights`, el adaptador de `embed` y la elección de modelo de D5.

## Dónde se mide, y por qué ahí

Por **SRP**, en **`webtyp/vector`**: su concern es la aritmética de f32 sobre WASM, ya
existe, ya tiene plan mergeado, y su propia puerta de fase 2 especifica `BenchmarkDot_384`
como «el número por el que se juzga todo el diseño». Solo hay que **registrarlo en MFLOPS**,
no en ns/op.

Por SRP el forward pass completo pertenece a `webtyp/transformer` — es quien posee los
kernels del encoder. Pero ese repositorio no existe, su plan está obsoleto (escrito para
WGSL), y **no hace falta para responder esto**: con los MFLOPS, el tiempo sale por división.

## La aritmética

`PLAN.md` D4b: un forward pass de 20 tokens sobre 12 capas de 384 dims son **~428M MAC ≈
856M FLOP**. Entonces `856 / MFLOPS = segundos`. Eso es todo.

(Corrección de un número que circuló antes: eran ~280M MAC, pero esa cuenta era **solo el
FFN**. Con atención y proyecciones incluidas son ~428M, un 53% más.)

## La contradicción que hay que resolver primero

`vector/docs/PLAN.md` §2 afirma, sobre este mismo target:

> No recurras a SIMD de WASM: el soporte de TinyGo es incompleto, y un intrínseco que no se
> puede verificar es peor que un bucle correcto en todas partes.

El plan maestro venía asumiendo SIMD128 disponible, y **el extremo optimista de la estimación
dependía enteramente de eso**. Si `vector` tiene razón, los 856M FLOP se pagan escalares y el
resultado probablemente caiga en la banda mala.

O sea: **es posible que P1 ya esté decidido en contra del transformer**, y que solo falte
medir para confirmarlo. Confirmar o refutar esa afirmación sobre TinyGo es parte del mismo
benchmark.

## Los tres desenlaces

| Forward pass derivado | Qué se construye |
|---|---|
| **< 300 ms** | el transformer chico de D5 es viable. Fase 3 como está escrita. |
| **300 ms – 1 s** | viable con reservas. Hay que decidir si esa latencia se acepta en el cuadro de búsqueda o se baja al nivel estático. **Es el único desenlace que vuelve a requerir una decisión tuya.** |
| **> 1 s** | se baja a la tabla estática de D5 (~32–64 MB, sin atención), `transformer` no se construye, y se acepta el recall más bajo. |

Los tres están cubiertos por el plan, así que ninguno bloquea: el peor caso degrada a una
opción escrita, no a una pregunta abierta.

Con el número medido se cubren de una vez los tres candidatos de D5, porque solo cambia el
tamaño del cuerpo: **28,3M** (`granite-embedding-97m-multilingual-r2`), **24,9M**
(`bekko-v1-a25m`) y **7,7M** (`bekko-v1-a8m`). Ver
[`SMALL_MODEL_FOR_EMBEDING.md`](SMALL_MODEL_FOR_EMBEDING.md) §5.

## P1b — La segunda medición: recall@10 en español, sobre corpus real

Lo de arriba dice **si corre**. No dice **cuál elegir**. El MTEB multilingüe promedia 18
idiomas y nosotros tenemos uno, así que un modelo de 60.3 promediado puede ser peor en
español que uno de 57.5.

La medición que elige: 200–500 documentos representativos en español, 30–50 consultas reales
con el documento correcto anotado a mano, y recall@10 + MRR por candidato. Un día de trabajo,
especificada en [`SMALL_MODEL_FOR_EMBEDING.md`](SMALL_MODEL_FOR_EMBEDING.md) §5.

Es barata porque los tres candidatos son de 384 dims: `vector`, `vectordb`, el arena y el
códec son idénticos, así que cambiar de candidato es re-embeber el corpus de prueba y nada
más.

# Registro de lo ya decidido

Cada uno vive en el plan que lo ejecuta. No repito el contenido acá.

| Decisión | Dónde quedó |
|---|---|
| Flujo: backend embebe documentos, navegador embebe consultas, búsqueda offline total | `PLAN.md` §1 |
| Un modelo, una implementación en Go, tres targets de compilación | `PLAN.md` D4 |
| WebGPU sale del plan; `transformer` sube a la fase 3 en versión CPU/WASM | `PLAN.md` D4b, §5 nota (e) |
| Dimensión de trabajo: 384 | `PLAN.md` D0 |
| Modelo: transformer multilingüe chico (~118M, ~120 MB int8), no estático ni de 600M | `PLAN.md` D5 |
| El catálogo de Workers AI / Ollama descartado como fuente del modelo | `PLAN.md` D4 |
| `bge-small-en-v1.5` descartado: solo inglés, y el español es requisito duro | `PLAN.md` D5 |
| La memoria sale de `agent` a `webtyp/agentmemory` | `plans/agent.md` §1.2 |
| `MemoryStore` segregado en 4 contratos, compuesto bajo el mismo nombre | `plans/agent.md` §2 |
| `Config` no gana un `Embedder`; se inyecta en `agentmemory` | `plans/agent.md` §1.5 |
| La conformance de `MemoryStore` se escribe **antes** que `agentmemory` | `PLAN.md` fase 4 |
| `Message.TokenCount` suma tres unidades distintas — defecto a corregir | `plans/agent.md` §1.8 |
| `ContextWindowConfig` se queda donde está, con las respuestas del gate | `plans/agent.md` §1 |
| `model.ValidateVector` es el dueño del chequeo de forma | `model/docs/PLAN.md`, `vectordb/docs/PLAN.md` |
| `orm` y `ddl` funcionan bajo TinyGo wasm | `PLAN.md` D7 |
| `webtyp/binary` no sirve como códec de vectores | `PLAN.md` O2 |
| Tags como texto delimitado; un `Kind` si aparece un segundo consumidor | `PLAN.md` O3 |
| pgvector / `VectorSearcher`: descartado | — |
