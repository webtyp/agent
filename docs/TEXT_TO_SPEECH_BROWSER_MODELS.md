# **Evaluación Arquitectónica y Selección de Modelos Text-to-Speech Sub-1B para Inferencia Conversacional en Tiempo Real mediante WebAssembly en Navegadores**

## **Resumen Ejecutivo y Selección del Modelo Óptimo**

La síntesis de voz en el navegador (*in-browser Text-to-Speech*) sin servidor intermedio es fundamental para aplicaciones conversacionales descentralizadas, privadas y de baja latencia1. El objetivo técnico central consiste en identificar un modelo acústico y vocodificador compacto (menos de 1.000 millones de parámetros) optimizado para convertir texto en audio convincente en tiempo real, utilizando únicamente el entorno de ejecución WebAssembly (WASM) en la CPU del cliente1.  
Un análisis comparativo de las arquitecturas ligeras actuales demuestra que la elección óptima para ejecución pura en WebAssembly es **Piper TTS**, un sistema basado en la arquitectura **VITS** (Inferencia Variacional con Aprendizaje Adversarial) con un tamaño paramétrico de entre 15 y 30 millones de parámetros4. Piper alcanza un Factor de Tiempo Real (RTF, por sus siglas en inglés) de 0,066x a 0,35x en CPU mediante WASM con SIMD, lo que equivale a sintetizar el audio entre 3 y 15 veces más rápido que la velocidad de habla humana6. Esta capacidad garantiza una latencia de inicio de reproducción (*Time-to-First-Audio*) de aproximadamente 250 milisegundos mediante segmentación de texto, manteniendo una tasa de transferencia fluida y sin pausas en cualquier navegador moderno sin requerir GPU6.  
Otras alternativas evaluadas presentan inconvenientes específicos para el entorno WASM puro. **Kokoro-82M**, basado en StyleTTS2 con 82 millones de parámetros, ofrece una calidad prosódica superior cercana a un narrador humano1. No obstante, su ejecución en CPU mediante WebAssembly arroja un RTF de 1,28x a 4,5x, operando más lento que el tiempo real y provocando interrupciones de audio a menos que se disponga de aceleración por WebGPU7. Por su parte, **KittenTTS Nano**, con 15 millones de parámetros y un tamaño de 25 MB en cuantización INT812, destaca por su mínimo consumo de memoria RAM7, pero muestra una latencia de síntesis inicial elevada cuando no se implementa un pipeline de micro-segmentación complejo7, además de contar con una disponibilidad reducida de voces en español en comparación con el ecosistema de Piper14.

## **Matriz Comparativa Cuantitativa de Modelos TTS Sub-1B**

La evaluación cuantitativa sintetiza los parámetros métricos, arquitectónicos y de rendimiento obtenidos sobre entornos de ejecución web estandarizados en CPU.

| Modelo | Parámetros | Tamaño ONNX Cuantizado | Arquitectura Neural | Factor Tiempo Real (RTF) en WASM CPU | Latencia FTTS (Primer Audio) | Aceleración WebGPU | Calidad Expresiva y Prosodia | Cobertura en Idioma Español |
| :---- | :---- | :---- | :---- | :---- | :---- | :---- | :---- | :---- |
| **Piper TTS (Medium)** | 15M \- 30M6 | 30 MB \- 60 MB4 | VITS (VAE \+ Flow \+ HiFi-GAN)4 | **0,066x \- 0,35x** \[cite: 6, 7\] | **\~250 ms** (con *chunking*)9 | Opcional / En desarrollo18 | Media-Alta (Clara y natural)4 | Excelente (Variantes regionales)16 |
| **Kokoro-82M** | 82M1 | 86 MB (q8) \- 330 MB (fp32)1 | StyleTTS2 / Non-autoregressive10 | 1,28x \- 4,5x (Más lento que tiempo real)7 | \~600 ms (WebGPU) / \>1200 ms (WASM)9 | Excelente / Recomendado1 | Muy Alta (Calidad de audiolibro)9 | Creciente mediante IPA / G2P21 |
| **KittenTTS Nano** | 15M12 | 25 MB (int8) \- 42 MB7 | Feed-forward ultra-compacto12 | \~0,33x \- 3,1x (Inconsistente en CPU)7 | \>2.000 ms (Sin streaming nativo)7 | Experimental18 | Aceptable (Artefactos en INT8)12 | Muy Limitada14 |
| **Web Speech API** | N/A (Motor SO) | 0 MB (Nativo)9 | Concatenativa o Reglas del SO9 | \< 0,01x9 | \~50 ms9 | N/A | Variable (Sintética / Robótica)9 | Nativa por Sistema Operativo9 |

## **Análisis Arquitectónico de las Opciones Candidatas**

La viabilidad de un modelo de conversión de texto a voz dentro del entorno WebAssembly depende fundamentalmente de la estructura del modelo neural y de la complejidad matemática de sus capas de inferencia.

### **Piper TTS y la Eficiencia End-to-End de VITS**

La arquitectura de Piper TTS se fundamenta en VITS, un modelo generativo end-to-end que unifica el modelado acústico y la vocodificación en un solo pase de inferencia4. VITS combina un autoencodizador variacional (VAE) con flujos de normalización (*normalizing flows*) y un vocodificador basado en HiFi-GAN4. Al no ser un modelo autorregresivo ni requerir un proceso iterativo de difusión, la predicción de la forma de onda de audio a partir de las representaciones fonéticas se realiza en un paso determinista4.  
Trucado para la web mediante onnxruntime-web con extensiones de datos vectoriales SIMD y soporte multihilo (*WebAssembly Threads*), Piper realiza el procesamiento de texto a fonemas (*Grapheme-to-Phoneme* o G2P) utilizando una compilación a WASM del motor eSpeak-NG5. Este pipeline ligero convierte el texto en tokens fonéticos que alimentan al modelo ONNX5. Debido a la baja dimensionalidad de sus capas convolucionales y de atención, el consumo computacional en la CPU del cliente es mínimo, registrando un RTF que fluctúa entre 0,066x y 0,35x según la potencia del procesador6. Esto asegura que la generación de audio aventaje sistemáticamente a la velocidad del habla humana, evitando que el búfer de reproducción se vacíe en medio de un diálogo5.

### **Kokoro-82M y el Cuestión del Cómputo de Estilo**

Kokoro-82M aprovecha los avances de la arquitectura StyleTTS2, la cual utiliza vectores de estilo (*style embeddings*) de 256 dimensiones para derivar la entonación, el ritmo y el timbre del hablante10. La red sustituye los pesados módulos autorregresivos por arquitecturas no autorregresivas optimizadas10. La calidad acústica resultante es excepcional, produciendo una señal de 24 kHz con articulación natural y variaciones de prosodia que superan a la mayoría de modelos sub-1B1.  
Sin embargo, el costo computacional de sus 82 millones de parámetros pasa factura cuando la inferencia se restringe exclusivamente al entorno WebAssembly en CPU1. En pruebas sin aceleración WebGPU, el RTF de Kokoro se degrada a valores entre 1,28x y 4,5x7. Al tardar más tiempo en sintetizar una frase que la duración real del audio generado, se vuelve técnicamente imposible mantener una transmisión de audio continua en tiempo real basándose únicamente en CPU WASM, relegando a Kokoro-82M a escenarios donde WebGPU esté disponible o a procesamiento no conversacional10.

### **KittenTTS Nano y la Compresión Límite**

KittenTTS Nano fue diseñado con el objetivo explícito de reducir al máximo la huella de memoria y almacenamiento12. Al comprimir 15 millones de parámetros en una representación cuantizada INT8 de solo 25 MB, el modelo se puede descargar casi de forma instantánea y requiere una memoria de trabajo mínima (entre 41 MB y 320 MB de RAM)7.  
A pesar de su eficiencia en tamaño, la arquitectura no está optimizada internamente para *streaming* nativo de audio token a token7. En las pruebas comparativas, KittenTTS Nano exige sintetizar la frase completa antes de entregar los datos PCM al motor de audio del navegador, registrando una latencia de primer audio (FTTS) superior a los 10 segundos en oraciones largas si no se fragmenta la entrada manualmente7. Adicionalmente, la cuantización a 8 bits introduce una modulación áspera en las frecuencias agudas y una perdida de expresividad perceptible en comparación con la salida limpia de los modelos VITS de Piper12.

## **Infraestructura de Ejecución en el Navegador y Latencia Conversacional**

Para lograr una conversión de texto a voz fluida en tiempo real dentro del navegador, la arquitectura del software debe aislar los cómputos pesados y organizar el flujo de datos para evitar bloqueos en la interfaz de usuario.

\+---------------------------------------------------------------------------------+  
|                              NAVEGADOR WEB (CLIENTE)                            |  
|                                                                                 |  
|   \+-------------------------+               \+-------------------------------+   |  
|   │   HILO PRINCIPAL (UI)   │               │   WEB WORKER (SEGUNDO HILO)   │   │  
|   │                         │               │                               │   │  
|   │  • Captura de Texto     │               │  • Motor Inferencia ONNX WASM │   │  
|   │  • Interfaz de Usuario  │               │  • Módulo G2P (eSpeak-NG WASM)│   │  
|   │  • Web Audio Context    │               │  • Cola de Fragmentación      │   │  
|   \+------------+------------+               \+---------------+---------------+   |  
|                │                                            │                   │  
|                │            ArrayBuffers Transferibles      │                   │  
|                │\<-------------------------------------------+                   │  
|                │                                                                │  
|                v                                                                │  
|   \+-------------------------+                                                   │  
|   │   AudioBufferSourceNode │ \---\> Altavoces (Salida PCM 22.05 kHz / 24 kHz)    │  
|   \+-------------------------+                                                   │  
\+---------------------------------------------------------------------------------+

### **Aislamiento en Web Workers y Gestión de Memoria**

El motor de inferencia de WebAssembly debe ejecutarse de forma aislada dentro de un **Web Worker** dedicado1. La inferencia de redes neuronales en la CPU exige un uso intensivo de instrucciones vectoriales; si estas operaciones se corren en el hilo principal de JavaScript, congelarán el bucle de eventos (*Event Loop*), bloqueando el renderizado de la interfaz y la respuesta a los eventos del usuario5.  
El hilo principal envía los fragmentos de texto hacia el Worker conversacional5. Una vez que el modelo ONNX en WebAssembly procesa los datos, la señal PCM resultante se empaqueta en un Float32Array y se envía de regreso al hilo principal mediante objetos transferibles (*Transferable Objects*)5. Esto permite transferir el búfer de memoria directamente entre hilos sin incurrir en costos de copia o serialización en JSON5.

### **Estrategia de Descarga e Inicialización (*Cold Start*)**

La experiencia del usuario en aplicaciones web depende del tiempo necesario para descargar e inicializar el motor de síntesis de voz3. Piper TTS requiere la descarga combinada del ejecutable onnxruntime-web, el compilado WASM del fonetizador eSpeak-NG y el archivo de modelo .onnx junto a su configuración .json, lo que suma entre 35 MB y 65 MB de datos4. A través de las API CacheStorage o IndexedDB, el navegador almacena estos componentes de forma permanente en la primera visita9. En ejecuciones subsecuentes, el calentamiento del modelo (*warmup*) en la memoria RAM del Web Worker toma entre 150 ms y 300 ms, permitiendo un inicio de sesión conversacional inmediato9.

### **Fragmentación de Texto y Búfer de Audio Continua**

Sintetizar respuestas conversacionales largas enviando el texto completo al modelo genera demoras inaceptables antes de iniciar la reproducción de voz5. Para contrarrestar este problema, el sistema debe implementar una estrategia de **fragmentación prospectiva (*lookahead chunking*)**5:

> 1. **Segmentación por Puntuación:** El texto entrante (por ejemplo, desde un modelo de lenguaje o un flujo de respuesta) se corta inmediatamente al detectar signos de puntuación fuertes (., ?, \!, ;, \\n)5.  
> 2. **Segmentación por Cláusulas:** Si un bloque supera los 80 caracteres sin puntuación fuerte, se divide utilizando puntuación débil (comas) o conectores gramaticales5.  
> 3. **Límite Mínimo de Longitud:** Para evitar que el modelo genere inflexiones antinaturales debido a la falta de contexto, los segmentos muy cortos se acumulan hasta alcanzar un mínimo de 40 caracteres antes de ser enviados al Worker5.

El hilo principal recibe el primer bloque de audio sintetizado (obtenido en \~250 ms) y lo programa para reproducción a través de la API Web AudioContext utilizando un nodo AudioBufferSourceNode3. Mientras el usuario escucha el bloque actual, el Web Worker sintetiza en segundo plano los bloques subsiguientes5. Al encadenar las horas de inicio de los nodos de audio con precisión de microsegundos (audioContext.currentTime), la reproducción se mantiene continua y libre de pausas o chasquidos5.

## **Evaluación de la Cobertura para el Idioma Español**

Un sistema conversacional en español requiere que el módulo de conversión de texto a fonemas (G2P) procese correctamente la ortografía, la acentuación diacrítica, las diéresis y la estructura fonética del idioma, apoyándose en modelos entrenados con voces naturales16.

### **Voces y Fonetización en Piper TTS**

El repositorio oficial de Piper TTS y sus derivados comunitarios albergan múltiples modelos en español grabados por hablantes nativos y licenciados bajo términos permisivos (MIT, Apache 2.0 y CC-BY)4. Las principales opciones clasificadas por calidad y acento son:

* **es\_ES-carlfm-high (22,05 kHz):** Modelo monohablante de alta calidad26. Presenta una articulación clara del español de España, idóneo para asistentes de voz interactivas que requieren alta inteligibilidad26.  
* **es\_ES-davefx-medium (22,05 kHz):** Voz masculina balanceada con un tamaño de modelo cuantizado de aproximadamente 35 MB4. Es la opción recomendada para optimizar el tiempo de descarga sin comprometer la fluidez4.  
* **es\_MX-ald-medium (22,05 kHz):** Modelo entrenado con acento mexicano neutro, ampliamente utilizado para aplicaciones conversacionales dirigidas al mercado latinoamericano16.  
* **es\_AR-daniela-high (22,05 kHz):** Voz femenina calibrada para la variante dialectal del español rioplatense16.

El módulo eSpeak-NG integrado en Piper resuelve de forma automática la pronunciación fonética de las palabras en español, la expansión de números, fechas y abreviaturas, así como la correcta entonación de las oraciones interrogativas y exclamativas indicadas por los signos de apertura (¿, ¡)5.

### **Soporte Lingüístico en Kokoro-82M y KittenTTS**

Kokoro-82M incluye compatibilidad con el idioma español en sus versiones recientes a través del mapeo de alfabetos fonéticos internacionales (IPA)19. Sin embargo, la integración en navegadores web mediante JavaScript presenta la complicación de requerir módulos fonetizadores externos compatibles con el navegador para transformar el texto en cadenas IPA antes de alimentar los tensores de entrada2. En el caso de KittenTTS Nano, el soporte para el idioma español es prácticamente inexistente en sus repositorios oficiales, estando centrado casi en su totalidad en el idioma inglés14.

## **Conclusiones y Recomendaciones de Integración**

Del análisis técnico de las arquitecturas sub-1B y las limitaciones del entorno de ejecución en navegadores se desprenden las siguientes recomendaciones de ingeniería para el desarrollo del sistema conversacional:

> 1. **Modelo Seleccionado para Entornos WebAssembly Puro:** **Piper TTS (variante medium, 22,05 kHz)** es la solución más sólida, simple y óptima para inferencia exclusiva en CPU mediante WebAssembly4. Su arquitectura VITS garantiza un RTF de 0,066x a 0,35x, lo que permite generar audio varias veces más rápido que el tiempo real sin congelar la interfaz ni depender de procesadores gráficos dedicados6.  
> 2. **Arquitectura de Inferencia Híbrida (Opcional):** Para proyectos que busquen la máxima calidad de voz posible y puedan asumir una lógica de detección de hardware, se recomienda implementar una estrategia condicional. Si la API navigator.gpu está disponible y funcional en el navegador del usuario, el sistema puede cargar **Kokoro-82M** para obtener una calidad prosódica superior1. En caso contrario, el sistema debe alternar automáticamente a **Piper TTS** en WebAssembly para asegurar que la tasa de generación no caiga por debajo del tiempo real1.  
> 3. **Selección de Voz para Español:** Para interacciones en castellano, se recomienda utilizar el modelo es\_ES-davefx-medium o es\_ES-carlfm-high16. Para aplicaciones destinadas a Latinoamérica, el modelo es\_MX-ald-medium ofrece la mejor neutralidad dialectal16.  
> 4. **Diseño del Pipeline de Audio:** El motor debe estructurarse aislando el runtime ONNX dentro de un Web Worker dedicado1. La entrada de texto se debe procesar mediante una cola de fragmentación prospectiva (bloques de 40 a 100 caracteres delimitados por puntuación) y los búferes PCM transferidos deben reproducirse secuencialmente utilizando la API Web AudioContext para ofrecer una respuesta de voz fluida y conversacional3.

#### **Obras citadas**

> 1. GitHub \- rhulha/StreamingKokoroJS: Unlimited text-to-speech in the, [https://github.com/rhulha/StreamingKokoroJS](https://github.com/rhulha/StreamingKokoroJS)  
> 2. Running Kokoro-82M ONNX TTS Model in the Browser, [https://dev.to/emojiiii/running-kokoro-82m-onnx-tts-model-in-the-browser-eeh](https://dev.to/emojiiii/running-kokoro-82m-onnx-tts-model-in-the-browser-eeh)  
> 3. Building a Browser-Based Text-to-Speech System with Piper TTS, [https://dev.to/linmingren/building-a-browser-based-text-to-speech-system-with-piper-tts-ljh](https://dev.to/linmingren/building-a-browser-based-text-to-speech-system-with-piper-tts-ljh)  
> 4. Every Piper Voice, Ranked: The Practical Guide to Piper's voices.json, [https://quick-tts.com/blog/piper-voices-ranked.html](https://quick-tts.com/blog/piper-voices-ranked.html)  
> 5. TTS With Piper & WASM\! \- NickScripts, [https://nickscripts.com/blog/tts-with-piper-wasm](https://nickscripts.com/blog/tts-with-piper-wasm)  
> 6. piper-plus/README\_EN.md at dev · ayutaz/piper-plus \- GitHub, [https://github.com/ayutaz/piper-plus/blob/dev/README\_EN.md](https://github.com/ayutaz/piper-plus/blob/dev/README_EN.md)  
> 7. On-device TTS Comparison: Open-source Benchmark 2026, [https://picovoice.ai/blog/on-device-tts/](https://picovoice.ai/blog/on-device-tts/)  
> 8. clowerweb/piper-tts-web-demo: Local text-to-speech in ... \- GitHub, [https://github.com/clowerweb/piper-tts-web-demo](https://github.com/clowerweb/piper-tts-web-demo)  
> 9. Web Speech API vs Piper vs Kokoro: Browser TTS Compared, [https://quick-tts.com/blog/web-speech-api-vs-piper-vs-kokoro.html](https://quick-tts.com/blog/web-speech-api-vs-piper-vs-kokoro.html)  
> 10. HeadTTS: Free neural text-to-speech (Kokoro) with ... \- GitHub, [https://github.com/met4citizen/HeadTTS](https://github.com/met4citizen/HeadTTS)  
> 11. Self-Hosted TTS with Kokoro ONNX: What CPU-Only Inference, [https://blog.nemesisnet.co.za/self-hosted-tts-with-kokoro-onnx-what-cpu-only-inference-actually-gets-you/](https://blog.nemesisnet.co.za/self-hosted-tts-with-kokoro-onnx-what-cpu-only-inference-actually-gets-you/)  
> 12. KittenTTS \- The Nano TTS | daily.dev, [https://daily.dev/posts/kittentts---the-nano-tts-axa1rlmhe](https://daily.dev/posts/kittentts---the-nano-tts-axa1rlmhe)  
> 13. KittenTTS Nano TTS : 15M Params & 25 MB 8-bit Model, [https://www.geeky-gadgets.com/kittentts-tts-llm-model/](https://www.geeky-gadgets.com/kittentts-tts-llm-model/)  
> 14. text-to-speech-benchmark/README.md at main \- GitHub, [https://github.com/Picovoice/text-to-speech-benchmark/blob/main/README.md](https://github.com/Picovoice/text-to-speech-benchmark/blob/main/README.md)  
> 15. The 6 Best On-Device TTS Models for Voice AI \- GetStream.io, [https://getstream.io/blog/best-on-device-tts-models/](https://getstream.io/blog/best-on-device-tts-models/)  
> 16. TTS models \- sherpa-onnx text-to-speech samples, [https://k2-fsa.github.io/sherpa/onnx/tts/all/](https://k2-fsa.github.io/sherpa/onnx/tts/all/)  
> 17. A Service-Oriented Approach to Low Latency, Context Aware ... \- arXiv, [https://arxiv.org/html/2512.08006v1](https://arxiv.org/html/2512.08006v1)  
> 18. \[Open Source\] 900+ Neural TTS Voices 100% Local In-Browser with, [https://www.reddit.com/r/selfhosted/comments/1mp3rpr/open\_source\_900\_neural\_tts\_voices\_100\_local/](https://www.reddit.com/r/selfhosted/comments/1mp3rpr/open_source_900_neural_tts_voices_100_local/)  
> 19. voirs/examples/KOKORO\_EXAMPLES.md at master \- GitHub, [https://github.com/cool-japan/voirs/blob/master/examples/KOKORO\_EXAMPLES.md](https://github.com/cool-japan/voirs/blob/master/examples/KOKORO_EXAMPLES.md)  
> 20. clowerweb/tts-studio: Test and compare browser-based TTS models\!, [https://github.com/clowerweb/tts-studio](https://github.com/clowerweb/tts-studio)  
> 21. Kokoro-82M TTS on Android — ONNX Runtime \+ APK | Soniqo, [https://soniqo.audio/guides/kokoro/android](https://soniqo.audio/guides/kokoro/android)  
> 22. onnx-community/Kokoro-82M-v1.0-ONNX · Other languages, [https://huggingface.co/onnx-community/Kokoro-82M-v1.0-ONNX/discussions/6](https://huggingface.co/onnx-community/Kokoro-82M-v1.0-ONNX/discussions/6)  
> 23. rhasspy/piper \- Generating speech locally in the web browser \- GitHub, [https://github.com/rhasspy/piper/issues/352](https://github.com/rhasspy/piper/issues/352)  
> 24. GitHub \- KittenML/KittenTTS: State-of-the-art TTS model under 25MB, [https://github.com/KittenML/KittenTTS](https://github.com/KittenML/KittenTTS)  
> 25. piper1-gpl/docs/VOICES.md at main \- GitHub, [https://github.com/OHF-Voice/piper1-gpl/blob/main/docs/VOICES.md](https://github.com/OHF-Voice/piper1-gpl/blob/main/docs/VOICES.md)  
> 26. friyin/vits-piper-es\_ES-carlfm-high \- Hugging Face, [https://huggingface.co/friyin/vits-piper-es\_ES-carlfm-high](https://huggingface.co/friyin/vits-piper-es_ES-carlfm-high)  
> 27. tts-rs \- Kokoro Text-To-Speech Inference on Rust \+ ONNX \- GitHub, [https://github.com/rishiskhare/tts-rs](https://github.com/rishiskhare/tts-rs)  
> 28. How to download the model and specify the local model in kokoro-js?, [https://github.com/hexgrad/kokoro/issues/133](https://github.com/hexgrad/kokoro/issues/133)