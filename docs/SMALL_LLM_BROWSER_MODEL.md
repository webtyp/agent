# Evaluacion Tecnica y Seleccion de Modelos LLM Sub-1B para Inferencia Client-Side en el Navegador (WASM/WebGPU) y Orquestacion de Herramientas MCP

Este documento define la arquitectura y selección de Modelos de Lenguaje Pequeños (Sub-1B) diseñados para ejecutarse en el navegador web cliente mediante **WebAssembly (WASM)** y **WebGPU**. El objetivo principal es actuar como un **agente lingüístico y router de herramientas**, interpretando peticiones en español y generando llamadas estructuradas a herramientas del **Model Context Protocol (MCP)** sin depender de la memoria paramétrica del modelo.

---

## 1. Resumen de Arquitectura y Principios de Diseño

1. **Inferencia Local Isomórfica**: Despliegue 100% client-side dentro del navegador mediante Transformers.js (v3/v4), ONNX Runtime Web o wllama/llama.cpp-WASM, aprovechando WebGPU con fallback transparente a WASM multihilo.
2. **Cero Dependencia de Conocimiento Paramétrico**: La información fáctica proviene de un motor **RAG local** (IndexedDB / VectorDB), mientras que las capacidades operativas las define el registro **MCP**.
3. **Desacoplamiento de Responsabilidades**:
   - **RAG**: Provee contexto fáctico relevante.
   - **Tool Retrieval**: Filtra el catálogo a **3–8 herramientas candidatas** antes de consultar al LLM.
   - **Small LLM (<1B)**: Mapea la intención en español a la herramienta adecuada y extrae parámetros.
   - **RLCD / Decision Model**: Evalúa si es necesario ejecutar la herramienta, valida seguridad y argumentos.
   - **Grammar Engine**: Garantiza 100% de validez sintáctica JSON mediante decodificación restringida por gramática.

---

## 2. Seleccion y Comparativa de Modelos Sub-1B

### Tabla Comparativa de Modelos Evaluados

| Modelo | Parámetros (Totales / No Emb.) | Tamaño Pesos (INT4 / Q4 / Q8) | Memoria RAM/VRAM | Benchmarks Tool-Calling / Razonamiento | Soporte Español | Estado / Recomendación |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Qwen3.5-0.8B** | ~0.80B | ~1019 MB (`Q8_0` GGUF) | ~1.1 GB | Visión, Tool Calling & Razonamiento nativo (Arch `qwen35`) | ★★★★★ | 🥇 **Opción Principal (Prueba Local)** |
| **Qwen3-0.6B** | ~0.60B | ~380 MB | ~0.7 GB | **0.880 Agent Score** (0.700 precisión, 1.000 restraint) [3] | ★★★★☆ | 🥈 **Alternativa Ultra Ligera** |
| **Granite 350M** | ~0.35B | ~220 MB | ~0.5 GB | **27/30** en evaluación multilingüe de tool calling [8] | ★★★☆☆ | 🥈 **Alternativa Alta Velocidad** |
| **FunctionGemma 270M** | 0.27B | ~180 MB | ~0.4 GB | Especialista en extracción de parámetros; requiere fine-tuning [4][5] | ★★☆☆☆ | 🥉 **Especialista para Fine-Tuning** |
| **Qwen2.5-0.5B-Instruct** | 0.49B / 0.36B [2] | ~398 MB [9] | ~0.8 GB [9] | GSM8K: **0.3692** [5]; Formatos estructurados: **0.610** [4] | ★★★★☆ | 🏅 **Fallback Confiable** |
| **SmolLM2-360M** | 0.36B | ~220 MB [7] | ~0.5 GB | GSM8K: 0.0455 [5]; Tool calling solo en versión 1.7B [6] | ★★☆☆☆ | ❌ **Descartado** |
| **Llama-3.2-1B-Instruct** | 1.23B / 1.10B | ~750 MB | ~1.5 GB | BFCL v4: 10.82 [8]; Supera la escala sub-1B | ★★★☆☆ | ❌ **Descartado** |

---

### Análisis Detallado de Candidatos

1. 🥇 **Qwen3.5-0.8B (`Lmstudio-community/Qwen3.5-0.8B...` - Principal en Prueba Local)**:
   - **Archivo**: `Qwen3.5-0.8B-Q8_0.gguf` (~1019.19 MB en disco, cuantización `Q8_0`, formato GGUF, arquitectura `qwen35`).
   - **Capacidades**: Visión, Tool Calling y Razonamiento integrados.
   - Ideal para pruebas locales en navegador/WASM con soporte completo de herramientas y comprensión avanzada en español.

2. 🥈 **Qwen3-0.6B (Alternativa Ultra Ligera)**:
   - Tokenizer y chat template optimizados para `<tools>` y `<tool_call>` [1].
   - Ejecutable directamente en navegador vía WASM con llama.cpp / wllama [2].
   - Soporta alternancia de razonamiento (`enable_thinking = false`) para latencia ultrabaja en ruteo de herramientas.
   - Excelente comprensión del español y capacidad para rehusar llamadas cuando no corresponden (*restraint* score = 1.000) [3].

3. 🥉 **Granite 350M (Prueba A/B - Alta Velocidad)**:
   - Rendimiento sobresaliente (27/30 en prompts multilingües) sin requerir tokens de razonamiento [8].
   - Huella de memoria mínima (~220 MB) y tiempo de carga rápido.

3. 🥉 **FunctionGemma 270M (Candidato para Fine-Tuning Especializado)**:
   - Diseñado por Google para transformar lenguaje natural en invocaciones a funciones [4].
   - No está pensado para diálogo abierto [5], sino como motor ligero de extracción sintáctica (`TuToolModel-270M`) vía destilación/fine-tuning [7][9].

4. 🏅 **Qwen2.5-0.5B-Instruct (Fallback Estable)**:
   - Modelo probado con 18 billones de tokens de preentrenamiento [2].
   - Alta densidad de razonamiento matemático/lógico frente a su escala [5].

---

## 3. Requerimientos de Hardware, Rendimiento y Ejecucion

### Métricas de Rendimiento en Cliente
- **Descarga Inicial**: 180 MB – 398 MB (Q4_K_M / q4f16) a ~1019 MB (Qwen3.5-0.8B `Q8_0` GGUF). Se almacena permanentemente en `CacheStorage` o `IndexedDB` [15].
- **Uso de Memoria**: 0.4 GB – 1.2 GB de VRAM/RAM host.
- **Tasa de Generación**:
  - **WebGPU**: 20 – 30+ tokens/segundo [19].
  - **WASM (Multihilo SIMD)**: 8 – 15 tokens/segundo (suficiente para generación corta de JSON).
- **Modo No-Thinking**: Configurar `enable_thinking = false` para omitir bloques `<think>` y generar llamadas JSON de forma inmediata [1].

---

## 4. Garantia Sintactica: Decodificacion Restringida por Gramatica

Para evitar alucinaciones sintácticas o JSONs malformados en esquemas MCP extensos, la inferencia se combina con **decodificación guiada por gramática** a nivel de motor de inferencia (vía `@huggingface/transformers-structured-output`, `llguidance` o `xgrammar`) [11][21].

El esquema JSON Schema de la herramienta seleccionada se transforma en un Autómata de Estados Finitos (FSM). En cada paso de generación, el `logits_processor` aplica una máscara binaria sobre el vocabulario del tokenizer:

$$P'(t_i | t_{<i}) = \begin{cases} P(t_i | t_{<i}) & \text{si } t_i \text{ es válido en la gramática } G_s \\ 0 & \text{en otro caso} \end{cases}$$

Esto garantiza **100% de adherencia sintáctica al esquema JSON esperable por el servidor MCP** [11][12].

---

## 5. Arquitectura del Agente Client-Side

```text
                             ┌───────────────────┐
                             │  Usuario (Browser)│
                             │  "reserva hora..."│
                             └─────────┬─────────┘
                                       │
                                       ▼
                             ┌───────────────────┐
                             │  RAG + VectorDB   │
                             │  IndexedDB Local  │
                             └─────────┬─────────┘
                                       │
                                       ▼
                             ┌───────────────────┐
                             │ Semantic Router   │
                             │ (Filtrado 3-8     │
                             │  herramientas)    │
                             └─────────┬─────────┘
                                       │
                                       ▼
┌──────────────────────────────────────┴──────────────────────────────────────┐
│ MOTOR DE INFERENCIA NAVEGADOR (Transformers.js / wllama - WASM/WebGPU)      │
│                                                                             │
│   ┌───────────────────────────┐         ┌───────────────────────────────┐   │
│   │   Qwen3-0.6B / Granite    │────────>│ Logits Processor (Grammar FSM)│   │
│   │   (enable_thinking=false) │         │ Masking de esquema JSON MCP   │   │
│   └─────────────┬─────────────┘         └───────────────┬───────────────┘   │
└─────────────────┼───────────────────────────────────────┼───────────────────┘
                  │                                       │
                  └───────────────────┬───────────────────┘
                                      │
                                      ▼
                             ┌───────────────────┐
                             │ Modelo Decisorio  │
                             │     / RLCD        │
                             │ ¿Ejecutar? ¿Safe? │
                             └─────────┬─────────┘
                                       │
                                       ▼
                             ┌───────────────────┐
                             │  Ejecución MCP    │
                             └───────────────────┘
```

---

## 6. Referencias Tecnicas y Enlaces Oficiales

### Modelos y Tokenizers
1. **Qwen3-0.6B Tokenizer Config**: https://huggingface.co/Qwen/Qwen3-0.6B/blob/c916fa4defd319b7d4e4da17604ca7338f4d99f5/tokenizer_config.json
2. **Qwen2.5 LLM Release**: https://qwen.ai/blog?id=qwen2.5-llm
3. **Qwen/Qwen2.5-0.5B-Instruct HF**: https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct
4. **FunctionGemma Model Overview**: https://ai.google.dev/gemma/docs/functiongemma
5. **FunctionGemma Model Card**: https://ai.google.dev/gemma/docs/functiongemma/model_card
6. **SmolLM2-360M README**: https://huggingface.co/HuggingFaceTB/SmolLM2-360M-Instruct/blob/main/README.md
7. **FunctionGemma Fine-Tuning Guide**: https://ai.google.dev/gemma/docs/functiongemma/finetuning-with-functiongemma
8. **modelfix/Qwen2.5-0.5B-Instruct HF**: https://huggingface.co/modelfix/Qwen2.5-0.5B-Instruct
9. **elbruno/Qwen2.5-0.5B Tool Calling HF**: https://huggingface.co/elbruno/Qwen2.5-0.5B-LocalLLMs-ToolCalling

### Benchmarks y Evaluaciones
10. **A Technical Study into 0.5B Reasoning LLMs**: https://arxiv.org/html/2506.13404v2
11. **Behavioral Profile of Qwen 0.5B**: https://medium.com/@suzume1/inside-qwen-0-5b-a-behavioral-profile-across-32-dimensions-1ead43a6ea3c
12. **Qwen2.5 0.5B Math Benchmark Discussion**: https://www.reddit.com/r/LocalLLaMA/comments/1ihcqiq/crazy_that_qwen25_05b_is_so_good_at_math/
13. **Tool Calling Benchmark Repo (MikeVeerman)**: https://github.com/MikeVeerman/tool-calling-benchmark
14. **Mynah SLM Models Comparison**: https://github.com/mynah-org/mynah-slm/blob/main/docs/models.md
15. **5 Tiny Language Models for Tool Calling**: https://ai.plainenglish.io/5-tiny-language-models-for-tool-calling-part-3-ebcda32c2518
16. **BFCL v4 Survey & Information**: https://huggingface.co/datasets/tuandunghcmut/BFCL_v4_information/blob/main/A%20Comprehensive%20Survey%20of%20Benchmarks%20for%20Evaluating%20Tool%20and%20Function%20Calling%20in%20Large%20Language%20Models.md
17. **Deploybase LLM Tool Use Comparison**: https://deploybase.ai/articles/best-llm-for-function-calling-tool-use-comparison

### Execución en Navegador, WASM y Gramáticas Structuradas
18. **llama-cpp-wasm-qwen3 GitHub**: https://github.com/hackur/llama-cpp-wasm-qwen3
19. **Transformers.js Structured Output Issue #1328**: https://github.com/huggingface/transformers.js/issues/1328
20. **Transformers.js Releases**: https://github.com/xenova/transformers.js/releases
21. **Transformers.js Guide**: https://www.developersdigest.tech/blog/transformers-js-guide
22. **Transformers.js v3 WebGPU Blog**: https://huggingface.co/blog/transformersjs-v3
23. **Run AI in Browser Guide (Tighten)**: https://tighten.com/insights/run-ai-in-the-browser-a-practical-guide-to-transformers-js/
24. **Godeltech Browser LLM Applications**: https://www.godeltech.com/transformers-js-and-browser-based-llm-applications/
25. **Transformers.js v4 WebGPU & TypeScript Guide**: https://vadimall.com/posts/transformers-js-v4-webgpu-browser-ai-typescript
26. **OpenAI Community Transformers.js WebGPU**: https://community.openai.com/t/transformers-js-webgpu-run-a-local-llm-in-your-browser-single-page/1370015
27. **Browser Voice AI Agent (LogRocket)**: https://blog.logrocket.com/voice-ai-agent-browser/
28. **MLC xGrammar Structured Generation**: https://blog.mlc.ai/2024/11/22/achieving-efficient-flexible-portable-structured-generation-with-xgrammar
29. **Hugging Face Text Generation Guidance**: https://huggingface.co/docs/text-generation-inference/conceptual/guidance
30. **Transformers.js Demo**: https://alankrantas.github.io/just-another-ai-assistant-huggingface-transformers-js/
