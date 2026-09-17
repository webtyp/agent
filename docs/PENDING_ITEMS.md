---
DOC: "Lo que sigue sin decidir"
STATUS: 1 decisión abierta (P1, opción A recomendada) + 1 pregunta
RELATED: docs/PLAN.md
---

> Este documento contiene **solo lo pendiente**. Lo que ya se decidió vive en el plan que lo
> ejecuta, no acá — dos copias de una regla se desincronizan y la vieja es la que alguien
> sigue. El registro de lo resuelto está al final, en una línea cada uno, para que se pueda
> rastrear sin repetir el contenido.
>
> Criterio de evaluación:
> [`devskills/skills/api-design/SKILL.md`](../../devskills/skills/api-design/SKILL.md),
> cuyo núcleo es SOLID.
>
> **Nota de idioma:** la prosa va en español; los bloques de código mantienen sus
> comentarios en inglés.

# P1 — Un solo modelo de embeddings para los tres entornos

**Bloquea D0, D4, D5 y las fases 3 y 5.** Es la decisión más cara de esta lista.

## El requisito

Un solo modelo, de pesos abiertos, que produzca **el mismo vector para el mismo texto** en
todo entorno donde el sistema corra:

1. **El navegador**, que es donde se generan las incrustaciones de los documentos.
2. **El respaldo**, que puede ser Postgres en un servidor local o Cloudflare.
3. **El servidor**, cuando el corpus crezca y la búsqueda tenga que ejecutarse del lado
   servidor sobre esa misma data.

Si cada entorno usa un modelo distinto, los vectores no son comparables y crecer obliga a
re-indexar todo el corpus desde cero. Eso es lo que hay que evitar, y es un requisito de
arquitectura, no una preferencia.

`vec_index.model_id` (D2) ya protege el caso: cargar un corpus cuyo `model_id` no coincide
con el embedder configurado **falla**, en vez de devolver resultados plausibles y falsos. Lo
que P1 decide es cómo no llegar nunca a ese error.

## La distinción que hay que hacer antes de elegir

«Que exista en Cloudflare» son **dos cosas distintas**, y confundirlas es lo que hace
difícil la elección:

| | Qué significa | Quién ejecuta el modelo | Límite que aplica |
|---|---|---|---|
| **En el catálogo de Workers AI** | `@cf/baai/bge-m3` y compañía | la infraestructura GPU de Cloudflare | ninguno nuestro; se paga por token |
| **Corriendo en Cloudflare** | nuestro WASM en un Worker | nuestro código, en el isolate | **128 MB por isolate** |

La segunda columna es la que este ecosistema ya sabe hacer: un Worker ejecuta WebAssembly, y
`webtyp` compila Go a WASM. El **mismo binario** puede correr en el navegador y en un Worker.
Eso satisface «un solo modelo en todos lados» sin depender del catálogo de nadie.

## Por qué el catálogo de Workers AI no resuelve el requisito

Los modelos multilingües del catálogo son grandes, y los chicos son solo-inglés:

| Modelo del catálogo | Params | Dims | Idiomas | ¿Corre en el navegador? |
|---|---|---|---|---|
| `@cf/baai/bge-m3` | ~568M | 1024 | multilingüe | ~568 MB int8 — no con «pocos recursos» |
| `@cf/qwen/qwen3-embedding-0.6b` | 600M | 1024 | multilingüe | igual de pesado |
| `@cf/baai/bge-large-en-v1.5` | 335M | 1024 | **solo inglés** | descartado por D5 |
| `@cf/baai/bge-small-en-v1.5` | ~33M | 384 | **solo inglés** | sí, pero descartado por D5 |

**No hay en el catálogo un modelo que sea multilingüe y chico a la vez.** Ese es el hallazgo
que ordena todo lo demás: el catálogo obliga a elegir entre el español y el navegador.

Y hay un segundo número que cierra la puerta: un Worker tiene **128 MB de memoria por
isolate** (confirmado en la documentación de Cloudflare). Un `bge-m3` self-hosted en WASM no
entra. O sea que ni siquiera podríamos correr nosotros el mismo modelo del catálogo en un
Worker para igualar los vectores.

## Opciones

### A. Un modelo propio, chico y multilingüe, compilado a WASM una vez ← recomendada

No se elige del catálogo: se elige por el presupuesto del entorno más restringido —el
navegador y el isolate de 128 MB— y se implementa una sola vez en Go.

- **Navegador:** TinyGo → WASM. Artifact de ~32–64 MB int8, cacheado en IndexedDB tras la
  primera descarga (D5).
- **Cloudflare:** el **mismo** código Go → WASM en un Worker, con los pesos en R2 o KV. Entra
  en 128 MB si la tabla es de 128 o 256 dims int8.
- **Servidor local con Postgres:** el mismo código Go compilado nativo. Sin WASM, sin
  límites.

Un solo modelo, una sola implementación, tres entornos, vectores siempre comparables. **Nunca
hay que re-indexar.**

Es, además, lo que el plan ya construye: `tokenizer` + `weights` + `embed` de la fase 3. La
única adición es declarar el Worker como tercer target de `embed` — no es un rediseño.

*Lo que cuesta, dicho claro:* un modelo estático multilingüe tiene menor recall que `bge-m3`.
Esa es la moneda con la que se paga «un solo modelo en todos lados». La puerta de la fase 3
—recall@10 contra una referencia— existe para medir si alcanza, y si no alcanza, la fase 5
(encoder completo sobre WebGPU) sigue siendo el camino, con el mismo modelo en los tres
entornos.

### B. `bge-m3` en todos lados, navegador incluido

Reuso total y la mejor calidad. Pero el navegador necesita ~568 MB int8 y WebGPU: la fase 5
deja de ser opcional y pasa a ser bloqueante, y «pocos recursos» queda descartado. En un
Worker directamente no entra en 128 MB, así que el lado servidor quedaría atado al catálogo
de Workers AI y no podría correr en un Postgres local sin una implementación aparte — que es
otra vez dos implementaciones del mismo modelo.

### C. `bge-small-en-v1.5` en todos lados — **DESCARTADA**

33M parámetros: entraría cómodo en el navegador y en un Worker, y está en el catálogo. Habría
sido la respuesta más barata de todas. **Es solo inglés, y el español está confirmado como
requisito duro**, así que viola D5. Queda documentada para que nadie la re-proponga.

## Lo que hay que confirmar antes de ejecutar

El modelo estático multilingüe concreto está sin elegir. `plans/embed.md` §4 tiene los
candidatos; la restricción nueva que P1 agrega es que la tabla int8 debe entrar **en 128 MB
menos el overhead del isolate**, lo que empuja hacia 128 dims (~32 MB) antes que 256
(~64 MB). Esa elección se cierra con el recall@10 de la puerta de fase 3.

# Preguntas abiertas

**1. ¿El Worker de Cloudflare es un target real o hipotético?**
P1.A asume que el mismo WASM corre en un Worker. Si en la práctica el respaldo va a ser
siempre Postgres en un servidor propio —donde el binario Go corre nativo y no hay límite de
128 MB—, entonces la restricción de los 128 MB no aplica y la tabla puede ser de 256 dims en
vez de 128. Cambia la elección del modelo, no la arquitectura.

# Registro de lo ya decidido

Cada uno vive en el plan que lo ejecuta. No repito el contenido acá.

| # | Decisión | Dónde quedó |
|---|---|---|
| — | La memoria sale de `agent` a `webtyp/agentmemory` | `PLAN.md` §7.2 |
| — | `MemoryStore` se segrega en 4 contratos y se compone | `PLAN.md` §7b |
| — | `Config` no gana un `Embedder`; se inyecta en `agentmemory` | `PLAN.md` §7.5 |
| — | `model.ValidateVector` es el dueño del chequeo de forma | `model/docs/PLAN.md`, `plans/vectordb.md` |
| — | `orm` y `ddl` funcionan bajo TinyGo wasm — sin verificación pendiente | `PLAN.md` D7 |
| — | `webtyp/binary` no sirve como códec de vectores | `PLAN.md` O2 |
| — | pgvector / `VectorSearcher`: descartado, no era necesario | — |
| — | Tags como `\|a\|b\|c\|` sobre `FieldText`: se queda así | `PLAN.md` O3 |
| — | La conformance de `MemoryStore` se escribe **antes** que `agentmemory` | `PLAN.md` §7b, fase 4 |
