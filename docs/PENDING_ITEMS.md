---
DOC: "Lo que sigue sin decidir"
STATUS: P1 medido (~3,3 GFLOPS) y confirmado en transformer (~313 ms, banda media — decisión de latencia pendiente); P1b resuelto — granite-embedding-97m-multilingual-r2
RELATED: docs/MASTER_PLAN.md
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

`MASTER_PLAN.md` D4b: un forward pass de 20 tokens sobre 12 capas de 384 dims ≈ **856M FLOP**.

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

## El benchmark real de `transformer` — MEDIDO, y no cae donde se esperaba

`webtyp/transformer` v0.1.0 (`BenchmarkEncode_20x12x384`, `tinygo test -target wasm`, arnés
sintético de 12 capas / 384 dims / 6 cabezales / FFN 1536 — la forma de `granite-embedding-97m`):

```
tinygo test -target wasm -bench=BenchmarkEncode_20x12x384 -benchtime=2s/3s
→ 290,8 / 313,3 / 343,2 / 328,4 / 301,3 / 303,6 ms/op   (seis corridas, promedio ~313 ms)
```

No cayó en la banda esperada de 400–800 ms (1,5–3× el piso): quedó **más cerca del piso**
(259 ms) de lo previsto, con varianza corrida a corrida que cruza los 300 ms en ambas
direcciones. Según la tabla de tres desenlaces (`transformer/docs/PLAN.md`), eso es la banda
**media — «viable con reservas», no «< 300 ms, fase 2 como está escrita»**.

**Esto es exactamente el punto que la tabla marca como decisión humana, no de ejecutor:**
¿se acepta ~300–340 ms (con la varianza medida, hasta ~343 ms en el peor caso corrido) como
latencia de un cuadro de búsqueda? P1b ya resolvió el modelo (abajo), así que la fase 2 (el
grafo real del encoder) queda despachable — pero la respuesta a esta pregunta condiciona si
se despacha "como está escrita" o con el pedido explícito de medir apenas el grafo compile,
antes de invertir más.

## P1b — RESUELTO: `granite-embedding-97m-multilingual-r2`

**Decisión directa del usuario (2026-09-20), no la medición de recall@10 especificada
abajo.** Queda anotado así para que nadie confunda esto con el resultado de un corpus real:
si más adelante el recall en español decepciona, la medición de esta sección sigue siendo la
forma de confirmarlo o de justificar un cambio de candidato — sigue siendo barata porque los
tres candidatos comparten los 384 dims (`vector`, `vectordb`, el arena y el códec no cambian).

Con esto elegido: 28,3M de cuerpo, 384 dims, 32K de contexto, Apache 2.0 (**D5**), y es la
forma exacta (12 capas, 6 cabezales, FFN 1536) que `transformer` ya usó para su benchmark de
etapa 1 — el ~313 ms medido (P1, arriba) es el número de este modelo, no una aproximación.
Desbloquea el despacho de la etapa 2 de `transformer` (el grafo real).

La medición de recall@10, si hace falta después: 200–500 documentos representativos en
español, 30–50 consultas reales con el documento correcto anotado a mano, y recall@10 + MRR.
Un día de trabajo, especificada en
[`SMALL_MODEL_FOR_EMBEDING.md`](SMALL_MODEL_FOR_EMBEDING.md) §5.

# Registro de lo ya decidido

Cada uno vive en el plan que lo ejecuta. No repito el contenido acá.

| Decisión | Dónde quedó |
|---|---|
| Flujo: backend embebe documentos, navegador embebe consultas, búsqueda offline total | `MASTER_PLAN.md` §1 |
| Un modelo, una implementación en Go, tres targets de compilación | `MASTER_PLAN.md` D4 |
| WebGPU sale del plan; `transformer` sube a la fase 3 en versión CPU/WASM | `MASTER_PLAN.md` D4b, §5 nota (e) |
| Dimensión de trabajo: 384 | `MASTER_PLAN.md` D0 |
| Modelo: transformer multilingüe chico (~118M, ~120 MB int8), no estático ni de 600M | `MASTER_PLAN.md` D5 |
| El catálogo de Workers AI / Ollama descartado como fuente del modelo | `MASTER_PLAN.md` D4 |
| `bge-small-en-v1.5` descartado: solo inglés, y el español es requisito duro | `MASTER_PLAN.md` D5 |
| La memoria sale de `agent` a `webtyp/agentmemory` | `plans/agent.md` §1.2 |
| `MemoryStore` segregado en 4 contratos, compuesto bajo el mismo nombre | `plans/agent.md` §2 |
| `Config` no gana un `Embedder`; se inyecta en `agentmemory` | `plans/agent.md` §1.5 |
| La conformance de `MemoryStore` se escribe **antes** que `agentmemory` | `MASTER_PLAN.md` fase 4 |
| `Message.TokenCount` suma tres unidades distintas — defecto a corregir | `plans/agent.md` §1.8 |
| `ContextWindowConfig` se queda donde está, con las respuestas del gate | `plans/agent.md` §1 |
| `model.ValidateVector` es el dueño del chequeo de forma | `model/docs/PLAN.md`, `vectordb/docs/PLAN.md` |
| `orm` y `ddl` funcionan bajo TinyGo wasm | `MASTER_PLAN.md` D7 |
| `webtyp/binary` no sirve como códec de vectores | `MASTER_PLAN.md` O2 |
| Tags como texto delimitado; un `Kind` si aparece un segundo consumidor | `MASTER_PLAN.md` O3 |
| pgvector / `VectorSearcher`: descartado | — |
| Modelo elegido: `granite-embedding-97m-multilingual-r2` (P1b, decisión directa, no recall@10 medido) | `PENDING_ITEMS.md` P1b |
