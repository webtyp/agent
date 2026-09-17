fecha de investigacion: 2026-09-17

# pregunta:
cuel es el mejor modelo para embedin de 384 dimenciones multiliguue o inges español, o el mejor para ambos idiomas (ing, spa) pero con mejor rendimiento,probado ya en produccion..la idea es hacer embeding en el navegador directamente (no me interza codigo, ni libreias al respecto eso es otroa apartado) solo nos interaz el mejor modelo pqueño acteul para ese proposito.(que se pudea usar con codgo libre)

# gemini:
Selección de Modelos de Embedding Multilingües de 384 Dimensiones para Despliegue Local en Navegador: Análisis Técnico y Benchmark de RendimientoEl despliegue de modelos de representación vectorial (text embeddings) directamente en el entorno de ejecución del navegador mediante tecnologías como WebAssembly y ONNX Runtime marca una transformación sustancial en el diseño de arquitecturas para búsqueda semántica y recuperación aumentada por generación (Retrieval-Augmented Generation, RAG). Esta modalidad de cómputo en el borde (edge computing) elimina la dependencia de llamadas a API externas, garantiza la privacidad total de los datos procesados en la sesión del cliente y anula la latencia de red en operaciones periódicas de vectorización. Sin embargo, la viabilidad práctica de este enfoque en producción exige satisfacer restricciones operativas rigurosas. El modelo seleccionado debe limitar su espacio vectorial a exactamente 384 dimensiones para minimizar la huella de memoria en bases de datos vectoriales locales como IndexedDB, mantener un recuento reducido de parámetros activos para evitar la sobrecarga de la CPU del cliente, ofrecer licencias abiertas verdaderamente comerciales y exhibir una precisión excepcional en tareas monolingües y cross-lingual centradas en español e inglés.Identificación del Modelo Líder en Producción: IBM Granite Embedding 97M Multilingual R2A la luz de los resultados recientes en benchmarks estandarizados como MTEB (Massive Text Embedding Benchmark) y MMTEB, el modelo ibm-granite/granite-embedding-97m-multilingual-r2 desarrollado por IBM Research se posiciona como la mejor alternativa actual probada en producción para vectorización local en navegador en 384 dimensiones. Publicado bajo la licencia permisiva Apache 2.0, este modelo supera significativamente a cualquier otro competidor de código abierto dentro de la categoría sub-100M. Construido sobre la arquitectura ModernBERT, granite-embedding-97m-multilingual-r2 genera embeddings nativos de 384 dimensiones registrando una puntuación de 60.3 en la tarea de recuperación multilingüe de MTEB (Multilingual Retrieval sobre 18 idiomas), aventajando por más de 9.4 puntos al estándar histórico en este segmento, multilingual-e5-small (50.9).El perfil de eficiencia de granite-embedding-97m-multilingual-r2 resulta idóneo para entornos de navegador debido a su diseño de parámetros asimétrico. Aunque posee 97 millones de parámetros totales, la red de cómputo del Transformer integra únicamente 28.3 millones de parámetros activos en sus capas contextuales, lo que reduce drásticamente las operaciones de punto flotante por segundo (FLOPs) requeridas durante la inferencia en CPU. Asimismo, su peso en disco comprimido en formato ONNX cuantizado asciende a tan solo 98 MB, lo que facilita descargas iniciales fluidas y un almacenamiento en caché transparente en el navegador, superando con creces la huella de 471 MB observada en modelos tradicionales como paraphrase-multilingual-MiniLM-L12-v2. Adicionalmente, el modelo expande la ventana de contexto estándar de 512 a 32,768 tokens y ofrece cobertura optimizada para 52 idiomas principales —con fuerte alineación en español e inglés— junto con más de 200 lenguajes adicionales y código de programación.Evaluación Comparativa de Rendimiento y Características TécnicasLa selección de un modelo para el entorno del navegador exige ponderar la precisión del modelo frente al tamaño del archivo descargado y la carga de cómputo impuesta al procesador. La siguiente tabla detalla la evaluación comparativa entre las principales opciones de 384 dimensiones optimizadas para procesamiento multilingüe.ModeloParámetros ActivosDimensiónMTEB Multilingüe (Retrieval)Ventana de ContextoPeso ONNX CuantizadoLicencia ComercialEstado en Producciónibm-granite/granite-embedding-97m-multilingual-r2[cite: 3, 5]28.3M38460.332,768 tokens~98 MBApache 2.0Líder Recomendadogranite-embedding-311m-multilingual-r2 (Matryoshka @ 384d)110M384 (de 768)63.832,768 tokens~300 MBApache 2.0Máxima Precisión / Mayor Pesobekko-embedding-v1-a25m[cite: 1, 9, 10]24.9M38457.58,192 tokens~60 MBMITAlternativa Ultra-Ligerabekko-embedding-v1-a8m[cite: 1, 9, 10]7.7M38456.28,192 tokens~25 MBMITUltra-Rápido / Baja Cómputointfloat/multilingual-e5-small[cite: 1, 3, 11]21.6M38450.9512 tokens~118 MBMITEstándar Legadoparaphrase-multilingual-MiniLM-L12-v2[cite: 1, 3]22.0M38436.6512 tokens~150 MBApache 2.0Obsoleto en RecuperaciónEl análisis cuantitativo evidencia que la arquitectura Granite R2 establece un nuevo techo de rendimiento en el grupo de modelos compactos. Las soluciones históricas como multilingual-e5-small y paraphrase-multilingual-MiniLM-L12-v2, aunque ampliamente extendidas en el ecosistema, presentan limitaciones severas tanto en capacidad de recuperación como en la longitud de contexto admitida, quedando relegadas ante opciones más modernas que incorporan técnicas avanzadas de destilación y poda estructural.Innovaciones Arquitectónicas y Mecanismos de OptimizaciónLa marcada superioridad técnica de granite-embedding-97m-multilingual-r2 responde a innovaciones estructurales derivadas del paradigma ModernBERT. En lugar de emplear el esquema de atención cuadrática tradicional de XLM-RoBERTa, el modelo implementa un mecanismo de atención alternante que intercala capas de atención global y local, acelerando drásticamente el procesamiento de secuencias largas en CPU mediante la reducción de la complejidad computacional. La integración de incrustaciones de posición rotatoria (Rotary Position Embeddings, RoPE) permite extender la ventana contextual hasta los 32,768 tokens sin recurrir a técnicas de interpolación posicional que suelen mermar la calidad semántica en textos extensos. Además, la adopción de funciones de activación SiLU y la eliminación de términos de sesgo unilineales optimizan el flujo de datos a nivel de microinstrucciones en los motores de ejecución WebAssembly.El proceso de compresión ideado por IBM combina la poda de capas (layer pruning) con la optimización del tokenizador. Mediante la destilación de conocimiento desde modelos maestros de mayor escala, la arquitectura original de 22 capas se redujo a 12 capas esenciales, preservando la densidad de representación vectorial. Para resolver el cuello de botella que representan las matrices de incrustación iniciales en la memoria del navegador, se diseñó un tokenizador multilingüe compacto de 180,000 tokens que mantiene una cobertura profunda en español, inglés y código fuente. Este ajuste reduce la huella de parámetros del vocabulario a una tercera parte en comparación con los tokenizadores genéricos de más de 250,000 entradas. Finalmente, a diferencia de la familia e5, que requiere anteponer prefijos de tarea como "query: " y "passage: " para funcionar adecuadamente, Granite 97M R2 opera como una red simétrica nativa, simplificando la interfaz de ejecución en el navegador al permitir codificaciones directas mediante similitud de coseno.Rendimiento en Contextos Bilingües y Tareas Cross-LingualEn escenarios de producción con usuarios hispanohablantes e angloparlantes, la calidad de la representación vectorial no solo depende del rendimiento en un idioma aislado, sino de la alineación semántica en tareas de recuperación cruzada (cross-lingual retrieval). Granite 97M R2 fue sometido a un entrenamiento contrastivo especializado sobre conjuntos de datos curados en 52 idiomas principales, incorporando pares de entrenamiento con muestras negativas complejas (hard negatives) explícitamente balanceadas en español e inglés. Como resultado, una consulta formulada en español ("instrucciones para configurar la autenticación de dos factores") proyecta una representación vectorial en el espacio de 384 dimensiones extremadamente cercana a su equivalente en inglés ("how to enable two-factor authentication"), asegurando altas tasas de acierto en índices vectoriales heterogéneos.Esta capacidad de alineación bilingüe se complementa con un desempeño sólido en tareas de recuperación de documentos extensos y contenido técnico. El modelo alcanza una puntuación de 65.6 en el benchmark LongEmbed y 60.4 en recuperación de código de programación (CoIR). Esta versatilidad resulta de particular valor en aplicaciones web donde la documentación técnica o empresarial suele coexistir en ambos idiomas y mezclar pasajes de texto explicativo con fragmentos de código fuente o estructurado.Análisis de Alternativas Especializadas para Escenarios LímiteAunque ibm-granite/granite-embedding-97m-multilingual-r2 constituye la recomendación de propósito general por su equilibrio entre tamaño y calidad, determinados requisitos operacionales pueden orientar la elección hacia otras alternativas del espectro de 384 dimensiones.Para aplicaciones donde el ancho de banda o la memoria del dispositivo cliente impongan restricciones severas, la familia bekko-embedding-v1 representa una opción de alta eficiencia. Su variante bekko-embedding-v1-a25m opera con solo 24.9 millones de parámetros activos y logra un puntaje de 57.5 en MTEB Multilingüe con un peso de descarga reducido a ~60 MB, mientras que la versión ultra-compacta bekko-embedding-v1-a8m reduce la red a 7.7 millones de parámetros activos ofreciendo una puntuación de 56.2. Ambos modelos incorporan licencias MIT y soporte nativo para la técnica Matryoshka, lo que permite truncar sus vectores de 384 dimensiones a 256, 128 o 64 dimensiones si se requiere minimizar drásticamente el espacio consumido por los índices en IndexedDB.En el extremo opuesto, si el objetivo prioritario es maximizar la tasa de precisión sin importar que el peso del archivo ONNX aumente a aproximadamente 300 MB, la alternativa superior consiste en emplear ibm-granite/granite-embedding-311m-multilingual-r2. Este modelo de 311 millones de parámetros cuenta con soporte de aprendizaje de representación Matryoshka nativo. Al truncar sus vectores originales de 768 dimensiones a un espacio objetivo de 384 dimensiones, obtiene una puntuación de 63.8 en recuperación multilingüe, conservando la mayor parte de la precisión semántica de su variante de tamaño completo.ConclusionesLa evolución técnica en el campo de los modelos de representación vectorial compactos permite hoy ejecutar tareas complejas de búsqueda semántica y recuperación información directamente en el navegador web con niveles de precisión profesionales. Para aplicaciones enfocadas en los idiomas español e inglés que requieran una dimensión vectorial fija de 384 elementos, ibm-granite/granite-embedding-97m-multilingual-r2 se consolida como el modelo líder en producción. Su combinación de arquitectura ModernBERT, ventana contextual de 32k tokens, tamaño comprimido de 98 MB en ONNX, rendimiento de 60.3 en MTEB Multilingual Retrieval y licencia abierta Apache 2.0 lo convierten en la solución más sólida y avanzada disponible actualmente para el ecosistema ejecutable en el cliente.

# chat gtp
Sí. Busqué específicamente modelos **pequeños, multilingües, de 384 dimensiones**, pensando en tu caso: **embeddings directamente en el navegador**, inglés + español, código/modelo abierto y algo que tenga evidencia real de uso y benchmarks.

### Mi elección actual: `bekko-embedding-v1-a25m`

Hay un candidato nuevo que cambia bastante la respuesta tradicional de `multilingual-e5-small`: **Bekko Embedding**.

[Bekko Embedding — modelo y benchmark](https://huggingface.co/blog/hotchpotch/bekko-embedding?utm_source=chatgpt.com)

| Modelo                | Dimensiones | Parámetros activos | Contexto | MMTEB Multilingual v2 — retrieval | Licencia |
| --------------------- | ----------: | -----------------: | -------: | --------------------------------: | -------- |
| **Bekko a25m**        |     **384** |         **24.9 M** |   **8K** |                          **57.5** | MIT      |
| **Bekko a8m**         |     **384** |          **7.7 M** |   **8K** |                          **56.2** | MIT      |
| multilingual-e5-small |         384 |          ~96–118 M |      512 |                            ~50.9* | MIT      |
| BGE-M3                |        1024 |             ~568 M |       8K |                                 — | MIT      |

*El benchmark de IBM muestra 50.9 para multilingual-e5-small en esa evaluación. ([GitHub][1])

### Lo interesante de Bekko

El **a25m** tiene solamente **24.93 millones de parámetros activos**, produce vectores de **384 dimensiones**, admite **8.192 tokens** y fue entrenado específicamente para recuperación multilingüe con alrededor de **1.1 mil millones de pares**. Además, está entrenado con **Matryoshka**, por lo que puede reducirse posteriormente a 256/128/64 dimensiones. ([Hugging Face][2])

Y hay un dato especialmente relevante para tu caso:

> **Ya existe una demo que carga Bekko directamente en el navegador y ejecuta el modelo allí.** ([Hugging Face][2])

Eso lo convierte en un candidato mucho más interesante que simplemente mirar MTEB.

---

## ¿Y `multilingual-e5-small`?

Sigue siendo una **excelente referencia**, y durante bastante tiempo habría sido mi respuesta.

`intfloat/multilingual-e5-small`:

* 384 dimensiones
* 512 tokens
* ~118M parámetros
* 100 idiomas aproximadamente
* MIT
* muy utilizado
* soporte ONNX/Web existente
* muchísimo ecosistema
* probado extensamente en producción

[multilingual-e5-small en Hugging Face](https://huggingface.co/intfloat/multilingual-e5-small?utm_source=chatgpt.com)

La propia documentación confirma que utiliza un backbone `Multilingual-MiniLM-L12-H384` y que fue entrenado con grandes cantidades de datos multilingües, incluyendo pares de traducción NLLB, Wikipedia, CC News, Reddit, StackExchange, etc. ([Hugging Face][3])

Además, Microsoft reporta **384 dimensiones y 12 capas** para multilingual-e5-small. ([GitHub][4])

Pero para un proyecto nuevo en **2026**, hay una diferencia importante:

**Bekko consigue bastante más rendimiento con un modelo mucho más pequeño.**

---

# Para tu caso concreto

Yo separaría la decisión así:

### 🥇 Calidad/rendimiento manteniendo 384 dimensiones

**Bekko `a25m`**

Es el que probaría primero.

**384 dimensiones + 24.9M parámetros activos + 8K contexto + MMTEB multilingual retrieval 57.5** es una combinación extraordinariamente buena para algo que quieres ejecutar localmente.

### 🥈 Si quieres minimizar brutalmente el tamaño

**Bekko `a8m`**

Solamente **7.67M parámetros activos**, 384 dimensiones y 8K de contexto.

Y aun así obtiene **56.2** en los 18 benchmarks de retrieval multilingüe de MMTEB v2. ([Hugging Face][2])

Para navegador esto es particularmente atractivo.

### 🥉 Si quieres máxima madurez/ecosistema

**`multilingual-e5-small`**

Es la opción conservadora.

Tiene muchos años de adopción, documentación, conversiones ONNX y herramientas alrededor. Incluso existe una versión ONNX específicamente preparada para ejecución web. ([Hugging Face][5])

---

## ¿Qué pasa con BGE?

`BAAI/bge-small-en-v1.5` tiene **384 dimensiones**, pero es **inglés**, por lo que no lo elegiría para tu requisito inglés + español. BGE-M3 sí es multilingüe, pero sube a **1024 dimensiones y ~568M parámetros**, justamente lo contrario de lo que buscas para navegador. ([Hugging Face][6])

Tampoco escogería `jina-embeddings-v3` para este caso: aunque es muy bueno multilingüe, tiene **570M parámetros y 1024 dimensiones**. Jina misma señala que modelos posteriores son preferibles para proyectos nuevos. ([Jina][7])

---

# Mi conclusión

Si tu requisito es exactamente:

> **"Quiero el mejor embedding pequeño que pueda ejecutar en el navegador, inglés + español, 384 dimensiones, open-source/open-weight y con evidencia de rendimiento real."**

mi shortlist de 2026 sería:

**1. Bekko a25m — mi candidato principal**
**2. Bekko a8m — si el tamaño/latencia es prioridad absoluta**
**3. multilingual-e5-small — si priorizas madurez y ecosistema sobre rendimiento**

Y hay algo particularmente interesante para tu proyecto: **Bekko no solamente es pequeño; fue diseñado precisamente alrededor de la pregunta "¿hasta dónde podemos reducir un modelo de retrieval multilingüe sin perder utilidad?"**. Sus resultados sugieren que llegó bastante lejos. ([Hugging Face][2])

Para **inglés ↔ español**, además, no necesitas necesariamente un modelo especializado en esos dos idiomas: el objetivo de Bekko es precisamente el retrieval **cross-lingual**, es decir, que una consulta en un idioma pueda recuperar contenido en otro. ([Hugging Face][2])

**Si tuviera que elegir hoy uno para investigar seriamente para tu arquitectura WASM/browser, empezaría por `bekko-embedding-v1-a25m`, no por E5.**

[1]: https://github.com/ibm-granite/granite-embedding-models/blob/main/README.md?utm_source=chatgpt.com "granite-embedding-models/README.md at main · ibm-granite/granite-embedding-models · GitHub"
[2]: https://huggingface.co/blog/hotchpotch/bekko-embedding?utm_source=chatgpt.com "Bekko Embedding: how small can a multilingual retrieval model be?"
[3]: https://huggingface.co/intfloat/multilingual-e5-small/blob/main/README.md?utm_source=chatgpt.com "README.md · intfloat/multilingual-e5-small at main"
[4]: https://github.com/microsoft/unilm/blob/master/e5/README.md?utm_source=chatgpt.com "unilm/e5/README.md at master · microsoft/unilm · GitHub"
[5]: https://huggingface.co/Xenova/multilingual-e5-small?utm_source=chatgpt.com "Xenova/multilingual-e5-small · Hugging Face"
[6]: https://huggingface.co/BAAI/bge-m3/blob/refs%2Fpr%2F117/README.md?utm_source=chatgpt.com "README.md · BAAI/bge-m3 at refs/pr/117"
[7]: https://jina.ai/es/models/jina-embeddings-v3/?utm_source=chatgpt.com "jina-embeddings-v3 - Search Foundation Models"


# pregunta:
por que dejaste fuera a ibm-granite/granite-embedding-97m-multilingual-r2?

# respuesta chat gtp:
Sí. **Tienes razón en cuestionarlo**: después de revisar la documentación actual de IBM, **`ibm-granite/granite-embedding-97m-multilingual-r2` debió estar en mi shortlist y, para tus requisitos, probablemente por delante de Bekko**.

La razón de que lo omitiera fue que mi búsqueda anterior no capturó adecuadamente la generación R2 de Granite, publicada en 2026. Eso cambia bastante la comparación.

### Lo importante

El **Granite 97M R2** encaja casi exactamente con lo que estás buscando:

| Característica              |                              Granite 97M R2 |
| --------------------------- | ------------------------------------------: |
| Parámetros                  |                                     **97M** |
| Dimensión                   |                                     **384** |
| Idiomas                     |           **200+**, 52 con soporte mejorado |
| Español                     |            **Sí, explícitamente entrenado** |
| Inglés                      |                                      **Sí** |
| Contexto                    |                           **32.768 tokens** |
| MTEB Multilingual Retrieval |                                    **60.3** |
| Código                      | Python, Go, Java, JS, PHP, Ruby, SQL, C/C++ |
| Licencia                    |                              **Apache 2.0** |
| ONNX                        |                                      **Sí** |
| OpenVINO                    |                                          Sí |
| GGUF                        |                                          Sí |
| Arquitectura                |                                  ModernBERT |
| Lanzamiento                 |                             abril/mayo 2026 |

IBM lo describe explícitamente como su modelo compacto para workloads de baja latencia, edge deployment y alto throughput. ([Hugging Face][1])

Y hay un dato que cambia bastante la comparación:

**Granite 97M R2 obtiene 60.3 en MTEB Multilingual Retrieval**, frente a **50.9 de multilingual-e5-small**. Además, IBM reporta que es el mayor resultado entre los modelos multilingües abiertos por debajo de 100M de parámetros. ([Hugging Face][1])

### Granite vs Bekko

Aquí está la comparación que realmente deberíamos hacer para tu caso:

|                       |      **Granite 97M R2** |             **Bekko a25m** |
| --------------------- | ----------------------: | -------------------------: |
| Dimensión             |                 **384** |                        384 |
| Parámetros            |                     97M |           **~25M activos** |
| Contexto              |                 **32K** |                         8K |
| MTEB ML Retrieval     |                **60.3** |                       57.5 |
| Idiomas               | **200+ / 52 mejorados** |                multilingüe |
| Español               |       **sí, explícito** |                         sí |
| Apache 2.0            |                  **sí** |                        MIT |
| ONNX                  |                  **sí** |                         sí |
| ModernBERT            |                  **sí** |                         no |
| Producción/enterprise |                 **IBM** | proyecto más pequeño/nuevo |

Los números publicados por IBM ponen a Granite claramente por encima de E5 y, en su benchmark, también por encima del Bekko que mencionamos anteriormente. ([GitHub][2])

Hay además una particularidad muy importante:

**Granite 97M tiene 28M parámetros activos según la tabla de IBM**, aunque el modelo tenga 97M parámetros totales. ([Hugging Face][3])

Eso es bastante relevante si tu objetivo final es **ejecutarlo en el navegador**.

### Y hay otra ventaja enorme para tu caso

El contexto máximo es de **32K tokens**.

Eso es muy superior a `multilingual-e5-small`, que está limitado a 512 tokens, y también superior a Bekko (8K). ([GitHub][2])

Para tu idea de hacer búsqueda semántica local en el navegador, eso permite trabajar con documentos/párrafos considerablemente más grandes sin tener que fragmentarlos tan agresivamente.

---

## Pero hay una distinción importante

Si tu criterio fuera:

> **"Quiero el modelo con mejor relación calidad/tamaño absoluta para correr en navegador."**

entonces **Bekko a8m/a25m sigue siendo muy interesante**, porque 8M/25M parámetros es muchísimo menos que 97M.

Pero si tu criterio es:

> **"Quiero el mejor modelo pequeño de 384 dimensiones para inglés + español, con evidencia fuerte de benchmark, licencia permisiva y una opción razonablemente seria para producción."**

entonces **Granite 97M R2 es actualmente un candidato mucho más fuerte**.

Y específicamente para **inglés ↔ español**, me gusta más la evidencia de Granite porque IBM declara explícitamente que **español está dentro de los 52 idiomas que reciben entrenamiento específico de retrieval y cross-lingual**, no simplemente que el tokenizer pueda procesarlo. ([Hugging Face][1])

### Mi shortlist corregida

Para lo que tú estás buscando, yo investigaría ahora en este orden:

**Granite 97M R2 → Bekko a25m → Bekko a8m → multilingual-e5-small**

No porque sea un ranking universal, sino porque esos cuatro representan cuatro puntos diferentes del espacio **calidad ↔ tamaño ↔ madurez ↔ navegador**.

Y **Granite 97M R2 sería mi referencia principal de calidad** para las pruebas que hagas en inglés/español.

Hay una última cuestión que considero **crítica antes de elegirlo para tu proyecto**: que tenga 97M parámetros no significa que necesariamente sea el más conveniente **descargado y ejecutado dentro del navegador**. El tamaño real de los pesos, cuantización, formato ONNX/WebGPU/WASM y **tiempo de primera carga** pueden cambiar completamente la decisión.

Si quieres, puedo hacer una búsqueda específica de **Granite 97M R2 vs Bekko vs E5 enfocada exclusivamente en ejecución client-side/browser (tamaño descargado, ONNX, WebGPU, WASM, memoria y velocidad en CPU/GPU)**, sin entrar todavía en código ni librerías.

[1]: https://huggingface.co/ibm-granite/granite-embedding-97m-multilingual-r2?utm_source=chatgpt.com "ibm-granite/granite-embedding-97m-multilingual-r2 · Hugging Face"
[2]: https://github.com/ibm-granite/granite-embedding-models/?utm_source=chatgpt.com "GitHub - ibm-granite/granite-embedding-models · GitHub"
[3]: https://huggingface.co/blog/ibm-granite/granite-embedding-multilingual-r2?utm_source=chatgpt.com "Granite Embedding Multilingual R2: Open Apache 2.0 Multilingual Embeddings with 32K Context — Best Sub-100M Retrieval Quality"
