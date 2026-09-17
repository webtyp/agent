---
DOC: "Lo que sigue sin decidir"
STATUS: 1 pendiente — una medición, no una decisión
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

# P1 — El forward pass de una consulta en WASM: cuánto tarda

**No es una decisión: es una medición.** Es la única premisa del plan maestro que no está
verificada, y de ella dependen `nn`, `tokenizer`, `weights`, el adaptador de `embed` y la
elección de modelo de D5. Es la puerta de entrada de la fase 3 y está especificada en
[`PLAN.md`](PLAN.md) §6.

## El benchmark

Un forward pass de **20 tokens** sobre **12 capas de 384 dims** en TinyGo `js/wasm`, corrido
en navegador con `gotest`, con y sin SIMD128.

Se puede hacer con un encoder de juguete de pesos aleatorios: mide el kernel, no la calidad,
así que **no hace falta elegir el modelo para correrlo**. Media jornada.

## Por qué decide tanto

El navegador solo embebe **consultas**, no documentos (`PLAN.md` §1 y D4b). El cómputo de un
forward pass es proporcional al largo de la secuencia, así que una consulta de 20 tokens
cuesta del orden de 280M MACs — una fracción de un chunk de 8 000. Si eso corre rápido en
WASM sobre CPU, no hace falta WebGPU en ningún momento del proyecto, y eso ya borró un
repositorio (`webtyp/webgpu`) y convirtió la fase 5 en trabajo opcional.

Lo que no sabemos es el factor constante. La estimación honesta es **150 ms – 3 s** según si
se usa SIMD128, y ese rango es demasiado ancho para construir encima: en un extremo el
diseño es holgado, en el otro no sirve.

## Los tres desenlaces, ya escritos

| Resultado | Qué se construye |
|---|---|
| **< 300 ms** | el transformer chico de D5 es viable. Fase 3 como está escrita. |
| **300 ms – 1 s** | viable con reservas. Hay que decidir si esa latencia se acepta en el cuadro de búsqueda o se baja al nivel estático. **Es el único desenlace que vuelve a requerir una decisión tuya.** |
| **> 1 s** | se baja a la tabla estática de D5 (~32–64 MB, sin atención), `nn` no se construye, y se acepta el recall más bajo. |

Los tres están cubiertos por el plan, así que ninguno es un bloqueo: el peor caso degrada a
una opción escrita, no a una pregunta abierta.

## Antes de correrlo

`plans/nn.md` está escrito para kernels WGSL y **no sirve como está** (`PLAN.md` §5 nota (e)).
Hay que reescribirlo para un grafo de encoder con kernels escalares + SIMD128. El benchmark
no lo necesita —se puede escribir suelto—, pero el repositorio no se crea hasta que ese plan
exista.

---

# Registro de lo ya decidido

Cada uno vive en el plan que lo ejecuta. No repito el contenido acá.

| Decisión | Dónde quedó |
|---|---|
| Flujo: backend embebe documentos, navegador embebe consultas, búsqueda offline total | `PLAN.md` §1 |
| Un modelo, una implementación en Go, tres targets de compilación | `PLAN.md` D4 |
| WebGPU sale del plan; `nn` sube a la fase 3 en versión CPU/WASM | `PLAN.md` D4b, §5 nota (e) |
| Dimensión de trabajo: 384 | `PLAN.md` D0 |
| Modelo: transformer multilingüe chico (~118M, ~120 MB int8), no estático ni de 600M | `PLAN.md` D5 |
| El catálogo de Workers AI / Ollama descartado como fuente del modelo | `PLAN.md` D4 |
| `bge-small-en-v1.5` descartado: solo inglés, y el español es requisito duro | `PLAN.md` D5 |
| La memoria sale de `agent` a `webtyp/agentmemory` | `PLAN.md` §7.2 |
| `MemoryStore` segregado en 4 contratos, compuesto bajo el mismo nombre | `PLAN.md` §7b |
| `Config` no gana un `Embedder`; se inyecta en `agentmemory` | `PLAN.md` §7.5 |
| La conformance de `MemoryStore` se escribe **antes** que `agentmemory` | `PLAN.md` fase 4 |
| `Message.TokenCount` suma tres unidades distintas — defecto a corregir | `PLAN.md` §7.8 |
| `ContextWindowConfig` se queda donde está, con las respuestas del gate | `PLAN.md` §7 |
| `model.ValidateVector` es el dueño del chequeo de forma | `model/docs/PLAN.md`, `vectordb/docs/PLAN.md` |
| `orm` y `ddl` funcionan bajo TinyGo wasm | `PLAN.md` D7 |
| `webtyp/binary` no sirve como códec de vectores | `PLAN.md` O2 |
| Tags como texto delimitado; un `Kind` si aparece un segundo consumidor | `PLAN.md` O3 |
| pgvector / `VectorSearcher`: descartado | — |
