---
DOC: "Elección del modelo de embeddings"
FECHA_INVESTIGACION: 2026-09-17
STATUS: consolidado; la elección se cierra con las dos mediciones de §5
RELATED: docs/PLAN.md D5, docs/PENDING_ITEMS.md P1
---

> Consolidación de una investigación en línea (Gemini + ChatGPT, 2026-09-17). Las fuentes
> están en §6; la investigación cruda no se conserva — lo que sobrevivió de ella está acá.
>
> Marcas de procedencia: **[inv]** dato de la investigación, sin verificar en esta sesión ·
> **[calc]** aritmética hecha sobre esos datos · **[ver]** verificado contra código o
> documentación de plataforma.

# 1. Requisito

El mejor modelo de embeddings **de 384 dimensiones**, multilingüe con foco español + inglés,
de pesos abiertos, ejecutable **dentro del navegador**. Contexto en [`PLAN.md`](PLAN.md) D4:
una sola implementación en Go corriendo en tres targets, y el navegador es el más apretado.

# 2. Candidatos **[inv]**

| Modelo | Params | Cuerpo (cómputo) | Dims | Contexto | ONNX int8 | MTEB ML retr. | Lic. | MRL |
|---|---|---|---|---|---|---|---|---|
| **granite-embedding-97m-multilingual-r2** | 97M | **28,3M** | 384 | **32 K** | ~98 MB | **60.3** | Apache 2.0 | no |
| **bekko-embedding-v1-a25m** | ~60M [calc] | 24,9M | 384 | 8 K | ~60 MB | 57.5 | MIT | **sí** |
| **bekko-embedding-v1-a8m** | ~25M [calc] | **7,7M** | 384 | 8 K | **~25 MB** | 56.2 | MIT | **sí** |
| multilingual-e5-small | 118M | 21,6M | 384 | 512 | ~118 MB | 50.9 | MIT | no |
| paraphrase-multilingual-MiniLM-L12-v2 | 118M | 22,0M | 384 | 512 | ~150 MB | 36.6 | Apache 2.0 | no |
| granite-embedding-311m-multilingual-r2 | 311M | 110M | 768→384 | 32 K | ~300 MB | 63.8 | Apache 2.0 | sí |

Las dos investigaciones coinciden en todas las cifras y no se contradicen en ninguna.

**Esto corrige a `PLAN.md` D5**, que proponía `multilingual-e5-small` y
`paraphrase-multilingual-MiniLM-L12-v2` — los dos peores de la tabla. El segundo está
descrito como obsoleto para recuperación.

Descartados y por qué, para que no se re-propongan: `bge-small-en-v1.5` y familia BGE chica
(solo inglés), `bge-m3` y `qwen3-embedding-0.6b` (1024 dims, ~600M — solo corren en
servidor, lo que obliga a un segundo modelo para la consulta y rompe D4),
`jina-embeddings-v3` (570M, 1024 dims).

# 3. Qué significa «parámetros activos», y por qué decide

Ambas fuentes repiten que Granite 97M tiene «28,3M activos de 97M» y lo dejan ahí.
**ModernBERT es denso, no MoE**, así que «activos» no es enrutamiento. La aritmética lo
aclara **[calc]**:

```
tokenizador de 180 000 tokens × 384 dims = 69,1M   ← tabla de embeddings (lookup)
                    cuerpo del transformer = 28,3M   ← cómputo real
                                      total ≈ 97,4M   ✓ coincide con los 97M
```

**El 71% del modelo es una tabla de búsqueda, no capas a ejecutar.** Eso parte el presupuesto
del navegador en dos ejes que veníamos tratando como uno:

| Eje | Lo determina | A quién le duele |
|---|---|---|
| **Descarga** | params totales | una vez por navegador, cacheado en IndexedDB |
| **Cómputo por consulta** | **solo el cuerpo** | cada búsqueda — es lo que mide §5.1 |

Consecuencia, e invierte la intuición: Granite 97M y Bekko a25m cuestan **casi lo mismo por
consulta** (28,3M vs 24,9M, 14% de diferencia) aunque pesen 98 MB y 60 MB. Elegir a25m «para
aliviar el forward pass» compra poco; compra 38 MB menos de descarga. El único que cambia el
orden de magnitud es **a8m (7,7M)**, ~3,7× menos trabajo.

También confirma la calibración del benchmark de `PLAN.md` §6 —20 tokens, 12 capas, 384
dims—: Granite 97M es exactamente 12 capas de 384 tras su poda de 22 **[inv]**.

# 4. Lo que no pude verificar

1. **Mi corte de conocimiento es mayo de 2026.** Granite R2 se publicó abril/mayo 2026, justo
   en el borde; **Bekko no lo conozco.** Toda la tabla de §2 es de la investigación.
2. **La aritmética de Granite cierra sola** (69,1 + 28,3 ≈ 97), buena señal sobre esos datos.
   Bekko no publica desglose de vocabulario: sus totales en §2 son inferencia mía desde el
   peso ONNX **[calc]**, no dato publicado.
3. **Procedencia.** Bekko es un proyecto chico y nuevo (`hotchpotch`); Granite tiene IBM
   detrás. Para un sistema que debe durar años eso pesa como mantenimiento, no como técnica.
   A favor de Bekko: MIT, y **ya existe una demo corriéndolo en el navegador [inv]**, que es
   evidencia directa de lo único que nos importa.

# 5. Cómo elegir con datos propios

**El MTEB es la columna en la que menos hay que confiar acá.** Promedia 18 idiomas y nosotros
tenemos uno: un modelo de 60.3 promediado puede ser peor en español que uno de 57.5, y
ninguna fuente desglosa por idioma. Dos mediciones: la primera descarta, la segunda elige.

### 5.1 ¿Corre? — se responde por aritmética, en `webtyp/vector`

No hace falta un repositorio nuevo. `vector` mide `BenchmarkDot_384` **en MFLOPS** en su
puerta de fase 2, y el forward pass sale por división: `PLAN.md` D4b lo cifra en ~428M MAC ≈
**856M FLOP** para 20 tokens sobre 12 capas de 384 dims, así que `856 / MFLOPS = segundos`.

El mismo número cubre los tres candidatos, porque entre ellos solo cambia el tamaño del
cuerpo — 28,3M, 24,9M y 7,7M — y el costo escala con eso.

**Advertencia:** `vector/docs/PLAN.md` §2 afirma que el soporte de SIMD de TinyGo es
incompleto y desaconseja usarlo. Si es así, los 856M FLOP se pagan escalares y el resultado
puede caer en la banda mala. Confirmar o refutar esa afirmación es parte del benchmark.

Umbrales y desenlaces, en [`PENDING_ITEMS.md`](PENDING_ITEMS.md) P1.

### 5.2 ¿Sirve en español? — la que realmente elige

Con los sobrevivientes de 5.1, sobre **el corpus real**:

- 200–500 documentos representativos en español, troceados como los trocearía el sistema
- 30–50 consultas reales, con el documento correcto anotado a mano
- indexar con cada candidato, medir **recall@10 y MRR**

Un día de trabajo, y produce el único número que importa. Es barato porque **los tres son de
384 dims**: `vector`, `vectordb`, el arena y el códec son idénticos, así que cambiar de
candidato es re-embeber el corpus de prueba y nada más.

# 6. Recomendación

**Orden de prueba: Granite 97M R2 → Bekko a25m → Bekko a8m.** El argumento no es el MTEB:

1. **Contexto 32 K contra 8 K y 512.** Irrelevante en el navegador —las consultas son de 20
   tokens— pero manda en el backend, que es donde se embeben los documentos. Con 512 (`e5`)
   habría que trocear agresivamente y cada corte parte una idea al medio.
2. **Es simétrico:** no necesita los prefijos `query:` / `passage:` de la familia e5 **[inv]**.
   Ese prefijo habría que replicarlo idéntico en los tres targets de D4, y una discrepancia
   ahí produce vectores de distinto espacio **sin ningún error visible** — el defecto más caro
   que tiene este diseño. Menos superficie para él.
3. **Apache 2.0 con IBM detrás**, mantenimiento previsible.

**Bekko a25m** segundo, y su argumento fuerte no es el tamaño de cómputo (§3) sino
**Matryoshka**: truncar 384→256→128 recorta la arena de `PLAN.md` D0 a la mitad o a un cuarto
sin cambiar de modelo. Si el techo de ~100 k documentos aprieta, eso vale más que 2,8 puntos
de MTEB. Granite 97M no lo ofrece.

**Bekko a8m** es el plan B real si 5.1 sale mal — y es mejor opción que bajar a una tabla
estática: 56.2 contra el recall bastante más bajo de un model2vec.

**Confianza:** alta en §3 y §5, que son aritmética y método. Baja en §2, que no pude
verificar. Si un dato resulta estar mal cambia el orden de prueba, no el método.

# 7. Fuentes

- [granite-embedding-97m-multilingual-r2](https://huggingface.co/ibm-granite/granite-embedding-97m-multilingual-r2) · [anuncio R2](https://huggingface.co/blog/ibm-granite/granite-embedding-multilingual-r2) · [repo y benchmarks](https://github.com/ibm-granite/granite-embedding-models/)
- [Bekko Embedding: how small can a multilingual retrieval model be?](https://huggingface.co/blog/hotchpotch/bekko-embedding)
- [multilingual-e5-small](https://huggingface.co/intfloat/multilingual-e5-small) · [ONNX para web](https://huggingface.co/Xenova/multilingual-e5-small) · [E5 en microsoft/unilm](https://github.com/microsoft/unilm/blob/master/e5/README.md)
- [bge-m3](https://huggingface.co/BAAI/bge-m3) · [jina-embeddings-v3](https://jina.ai/es/models/jina-embeddings-v3/)
