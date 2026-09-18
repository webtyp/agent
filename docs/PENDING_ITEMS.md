---
DOC: "Lo que sigue sin decidir"
STATUS: P1 medido (~3,3 GFLOPS); queda P1b, que necesita corpus real
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

# P1 — MEDIDO: ~3,3 GFLOPS bajo TinyGo WASM

**Resuelto.** Medido en `webtyp/vector` v0.1.1, donde quedó registrado en el README para que
una regresión sea visible.

## El número

```
tinygo test -target wasm -bench=BenchmarkDot_384 -benchtime=2s
→ 229,5 / 231,4 / 238,1 ns/op   (tres corridas, ~233 ns/op)
```

Un `Dot` de 384 dims son 384 multiplicaciones + 383 sumas ≈ **768 FLOP**, así que
`768 / 233e-9` ≈ **3,3 GFLOPS**. Escalar, sin SIMD — lo que confirma la afirmación de
`vector/docs/PLAN.md` §2 que el plan maestro venía asumiendo sin verificar.

Referencia: Go nativo en la misma máquina da ~8,3 GFLOPS, o sea que **WASM corre ~2,5×
más lento**. Esa proporción es la parte reutilizable para cualquier presupuesto futuro.

## La división

`PLAN.md` D4b: un forward pass de 20 tokens sobre 12 capas de 384 dims ≈ **856M FLOP**.

```
856M FLOP / 3,3 GFLOPS ≈ 259 ms
```

Por tamaño de cuerpo (D5), escalando proporcionalmente:

| Candidato | Cuerpo | Forward pass derivado |
|---|---|---|
| `granite-embedding-97m-multilingual-r2` | 28,3M | ~259 ms |
| `bekko-embedding-v1-a25m` | 24,9M | ~228 ms |
| `bekko-embedding-v1-a8m` | 7,7M | ~70 ms |

## Cómo leer esto, sin adornos

Los tres caen bajo los 300 ms, que es la banda «viable, fase 3 como está escrita». **Pero
259 ms es un piso optimista, no una predicción**, y la diferencia importa:

`Dot` es el mejor caso posible — multiplicar-acumular secuencial, localidad perfecta,
desenrollado de a 4. Un forward pass real agrega softmax, layernorm y GELU/SiLU, que son
funciones trascendentes bastante más caras por elemento que un MAC, más los accesos
con stride de la atención. Un factor de 1,5–3× sobre el piso es lo esperable, lo que
ubica el número real en **~400–800 ms**: a caballo entre «viable» y «viable con reservas».

**Lo que esto sí decide:** el transformer **no está muerto**, que era el riesgo vivo cuando
apareció la contradicción de SIMD. No hay que bajar a tabla estática por falta de cómputo.

**Lo que no decide:** si 400–800 ms es aceptable en un cuadro de búsqueda. Eso se confirma
con el benchmark real de `transformer` cuando exista, no con esta división.

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
