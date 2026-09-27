pregunta:
cual modelo llm  de menos de 1b de parametros, mas simple, optimo para hacer inferencia usando webassembly en el navegador como objetivo manejo de herramientas, segun peticiones. (correcto español) se decea que sea inteligente pero no requerie conocimientos.la idea es que use herramientas adecuadamente segun contexto. puede complementarce con un modelo rlcd para toma de deciciones. el proyecto ya contara con rag,y herramientas mcp..peor falta el modelo pequeño que contexte a los requerimientos@Busca en la web 

respuesta:

Sí. Para el escenario que describes —**LLM <1B, ejecutándose en el navegador mediante WASM, español, sin necesidad de conocimiento factual propio, y especializado en decidir cuándo/cómo usar herramientas MCP**— la respuesta cambia bastante respecto de buscar simplemente el mejor “LLM pequeño”.

## Mi candidato principal: Qwen3-0.6B

Alibaba Cloud **Qwen3-0.6B** es actualmente el punto de partida que probaría primero.

Tiene ~600M parámetros y, lo más importante para tu arquitectura, **su tokenizer/chat template ya contempla explícitamente tool calling**: recibe las herramientas en `<tools>` y genera llamadas estructuradas `<tool_call>` con nombre y argumentos JSON. ([Hugging Face][1])

Además, existe evidencia específica de que **Qwen3-0.6B funciona directamente en navegador mediante WebAssembly**, incluso sin WebGPU; hay implementaciones que lo ejecutan con wllama/llama.cpp-WASM. ([GitHub][2])

Y hay un benchmark reciente específicamente orientado a tu problema: **decidir si llamar o no llamar una herramienta**, no simplemente producir JSON válido. En esa evaluación, Qwen3-0.6B obtuvo **0,880 Agent Score**, con **0,700 en acciones correctas, 1,000 en restraint y 0 herramientas incorrectas**. ([GitHub][3])

### Pero hay una alternativa extremadamente interesante: FunctionGemma 270M

Google DeepMind **FunctionGemma 270M** está diseñado específicamente para esto.

Son solamente **270M parámetros**, y Google lo entrenó específicamente para transformar lenguaje natural en llamadas a funciones. Google lo describe explícitamente como un modelo para construir agentes locales rápidos y privados. ([Google AI for Developers][4])

Pero hay una diferencia fundamental:

> **FunctionGemma es un especialista en tool calling, no un pequeño chatbot general.**

Google dice explícitamente que **no está pensado como modelo de diálogo directo** y que debe ser fine-tuneado para conseguir buen rendimiento en una tarea específica. ([Google AI for Developers][5])

Y eso, curiosamente, podría encajar **muy bien con tu arquitectura**.

---

# Cómo veo las opciones para tu proyecto

| Modelo            | Parámetros | Tool calling | Español | WASM/browser | Razonamiento/decisión | Mi lectura                      |
| ----------------- | ---------: | ------------ | ------- | ------------ | --------------------- | ------------------------------- |
| **Qwen3-0.6B**    |      ~600M | ★★★★☆        | ★★★★☆   | ★★★★★        | ★★★★☆                 | **Principal**                   |
| **FunctionGemma** |       270M | ★★★★★        | ★★☆☆☆   | ★★★★☆        | ★★★☆☆                 | **Especialista**                |
| SmolLM2-360M      |       360M | ★★☆☆☆        | ★★☆☆☆   | ★★★★☆        | ★★★☆☆                 | No lo elegiría                  |
| Qwen2.5-0.5B      |       490M | ★★★☆☆        | ★★★★☆   | ★★★★★        | ★★★☆☆                 | Buen fallback                   |
| Granite 350M      |      ~350M | ★★★★☆        | ★★★☆☆   | ★★★★☆        | ★★★★☆                 | **Muy interesante para probar** |

Hay que hacer una distinción importante: **SmolLM2-360M parece atractivo por tamaño, pero su propia documentación indica que el soporte de function calling corresponde al 1.7B, no al 360M**. Además, la familia está principalmente orientada al inglés. ([Hugging Face][6])

---

# Lo interesante de Qwen3-0.6B

Tu arquitectura sería algo así:

```text
                   ┌───────────────────┐
                   │      Usuario      │
                   │  "reserva hora..."│
                   └─────────┬─────────┘
                             │
                             ▼
                   ┌───────────────────┐
                   │     RAG/context   │
                   │                   │
                   │ contexto relevante│
                   └─────────┬─────────┘
                             │
                             ▼
                 ┌───────────────────────┐
                 │     Qwen3-0.6B        │
                 │                       │
                 │ "¿qué debo hacer?"    │
                 └───────────┬───────────┘
                             │
                  ┌──────────┴──────────┐
                  │                     │
               no tool                tool
                  │                     │
                  ▼                     ▼
               respuesta            MCP call
                                      │
                                      ▼
                                 resultado
                                      │
                                      ▼
                               Qwen3-0.6B
                                      │
                                      ▼
                                  respuesta
```

Y esto es importante:

### No necesitas que el modelo "sepa" nada.

Puedes hacer que el modelo sea básicamente un **router/agente lingüístico**.

El RAG aporta:

```text
qué sabe el sistema
```

MCP aporta:

```text
qué puede hacer el sistema
```

El pequeño LLM aporta:

```text
qué quiere el usuario
+
qué herramienta corresponde
+
qué argumentos necesita
+
si debe llamar alguna herramienta
```

Eso reduce muchísimo la exigencia del modelo.

---

# Y aquí Qwen3 tiene una ventaja interesante

Qwen3-0.6B tiene modo de razonamiento y modo sin razonamiento.

Para tu caso yo **no utilizaría reasoning continuamente**.

Usaría:

```text
enable_thinking = false
```

para las operaciones normales.

Porque quieres:

```text
usuario
 ↓
interpretar intención
 ↓
seleccionar herramienta
 ↓
argumentos
 ↓
MCP
```

y no:

```text
usuario
 ↓
<think>
...
100 tokens de razonamiento
...
</think>
 ↓
tool
```

El propio template de Qwen3 contempla explícitamente el modo sin thinking. ([Hugging Face][1])

---

# Pero hay algo todavía más interesante

Tú mencionas:

> "puede complementarse con un modelo RLCD para toma de decisiones"

Ahí creo que tu arquitectura puede ser **mucho mejor que intentar encontrar un LLM de 1B que haga todo**.

Yo separaría las responsabilidades:

```text
             ┌───────────────────────┐
             │       Usuario         │
             └───────────┬───────────┘
                         │
                         ▼
              ┌─────────────────────┐
              │     Qwen3-0.6B      │
              │                     │
              │ comprensión        │
              │ intención           │
              │ argumentos          │
              │ tool candidates     │
              └──────────┬──────────┘
                         │
                         ▼
              ┌─────────────────────┐
              │   Decision model    │
              │      / RLCD         │
              │                     │
              │ ¿usar herramienta?  │
              │ ¿cuál?              │
              │ ¿es seguro?         │
              │ ¿faltan argumentos? │
              └──────────┬──────────┘
                         │
                         ▼
                    MCP Tool
```

Eso tiene una ventaja enorme:

**el LLM no tiene que ser el responsable final de tomar decisiones.**

---

# Hay evidencia de que esto importa

El benchmark que encontré es especialmente relevante para tu problema porque no mide solamente:

> "¿generó correctamente el JSON?"

Mide:

> **¿entendió que debía utilizar una herramienta?**

Y eso es bastante más difícil.

Por ejemplo, el benchmark prueba casos donde:

* aparece una palabra relacionada con una herramienta pero **no debe utilizarse**;
* el usuario explícitamente dice que **no consulte** una herramienta;
* la información necesaria ya está disponible;
* hay que distinguir entre una petición que requiere acción y una conversación normal.

En esa evaluación, Qwen3-0.6B obtuvo 0,880, mientras que FunctionGemma 270M, pese a estar especializado en function calling, presentó problemas con este tipo de decisiones. ([GitHub][3])

Esto es precisamente la diferencia entre:

```text
function calling
```

y

```text
tool-use reasoning
```

---

# FunctionGemma tiene otra gran ventaja

No lo descartaría.

De hecho, para tu proyecto **haría un experimento con ambos**.

Google reconoce explícitamente que existen dos habilidades diferentes:

> conocimiento mecánico de cómo usar una herramienta
> vs.
> comprensión de por qué y cuándo utilizarla.

Y señala que los modelos pequeños necesitan fine-tuning para mejorar especialmente la segunda. ([Google AI for Developers][7])

Esto encaja casi perfectamente con tu idea.

Podrías tener:

### Modelo A — Qwen3-0.6B

```text
comprensión de lenguaje
español
intención
contexto
tool selection
```

### Modelo B — FunctionGemma 270M

```text
validación de tool call
selección especializada
argument extraction
```

Pero inicialmente **no complicaría tanto el sistema**.

---

# Hay un tercer candidato que me llamó mucho la atención

**Granite 350M**.

Encontré una evaluación independiente muy reciente que compara específicamente tool calling entre:

* Qwen3-0.6B
* Granite 350M
* FunctionGemma 270M

sobre **30 prompts en 7 idiomas**.

El resultado reportado fue:

```text
Granite 350M Q8       27/30
Qwen3-0.6B Q4_K_M     25/30
Granite 350M Q4_K_M   25/30
```

Y una característica interesante: Granite no necesita el modo thinking para conseguir esos resultados. ([GitHub][8])

Esto merece una prueba real para tu caso, especialmente porque tú quieres **simplicidad y baja latencia**.

---

# Mi selección para tu proyecto

Si tuviera que reducirlo a tres candidatos:

### 🥇 Qwen3-0.6B

**Primera opción para desarrollar.**

Porque reúne:

* <1B
* ~600M
* tool calling nativo
* español/multilingüe
* buen instruction following
* razonamiento opcional
* formato estructurado
* WASM viable
* ya existe experiencia real ejecutándolo en navegador
* buen resultado en benchmark específico de tool-use. ([GitHub][3])

### 🥈 Granite 350M

**El experimento que haría inmediatamente después.**

Especialmente si descubres que Qwen3 consume demasiado o genera demasiados tokens.

El resultado 27/30 en el benchmark de 30 casos es suficientemente interesante como para no ignorarlo. ([GitHub][8])

### 🥉 FunctionGemma 270M

**El candidato para especializar/fine-tunear.**

No necesariamente el mejor modelo "general", pero posiblemente el más interesante si posteriormente quieres crear:

```text
TuToolModel-270M
```

entrenado específicamente para:

```text
español
+
tus schemas MCP
+
tus reglas
+
tus herramientas
+
tus casos de no-tool
```

Google incluso recomienda precisamente **distillation + fine-tuning** para especializar FunctionGemma en flujos de trabajo concretos. ([Google AI for Developers][9])

---

## Y haría una cosa diferente con tu RAG

No le pasaría al modelo **todo el MCP**.

En lugar de:

```text
150 herramientas
        ↓
Qwen3-0.6B
```

haría:

```text
Usuario
   ↓
RAG / router semántico
   ↓
3–8 herramientas candidatas
   ↓
Qwen3-0.6B
   ↓
decisión
```

Eso puede ser **muchísimo más importante que pasar de 600M a 900M parámetros**.

Por ejemplo:

```text
"Necesito saber si el doctor tiene horas disponibles mañana"

RAG/tool retrieval:

calendar.search_availability
doctor.get_schedule
appointment.create
```

El LLM sólo tiene que decidir entre esas tres.

No necesita comprender 100 herramientas.

---

# Mi arquitectura objetivo

Para lo que estás construyendo, yo probaría inicialmente:

```text
                    BROWSER
                       │
                       ▼
              ┌─────────────────┐
              │   Embeddings    │
              │   384 dim       │
              └────────┬────────┘
                       │
                 tool retrieval
                       │
                       ▼
              ┌─────────────────┐
              │   3–8 tools     │
              │   candidates    │
              └────────┬────────┘
                       │
                       ▼
              ┌─────────────────┐
              │   Qwen3-0.6B    │
              │   no-thinking   │
              └────────┬────────┘
                       │
                 tool_call
                       │
                       ▼
                     MCP
                       │
                       ▼
                  tool result
                       │
                       ▼
              ┌─────────────────┐
              │   Qwen3-0.6B    │
              └────────┬────────┘
                       │
                       ▼
                    Usuario
```

Y posteriormente:

```text
                  Qwen3-0.6B
                       │
                       ▼
                ┌──────────────┐
                │ Decision/RL  │
                │    model     │
                └──────┬───────┘
                       │
                 approve/reject
                       │
                       ▼
                     MCP
```

**Eso me parece más prometedor que buscar un LLM sub-1B que sea simultáneamente chatbot + razonador + knowledge base + agente + tool caller.**

### En resumen

Para tu objetivo concreto, **yo empezaría con Qwen3-0.6B**, no con SmolLM2 ni directamente con FunctionGemma. Qwen3 tiene el equilibrio más interesante entre tamaño, español, instruction following, tool calling, razonamiento opcional y viabilidad WASM. ([Hugging Face][1])

Pero **mantendría FunctionGemma 270M como candidato serio para una segunda fase de fine-tuning especializado**, porque su propósito coincide casi exactamente con el componente que quieres construir. ([Google AI for Developers][4])

Y **Granite 350M es el tercer modelo que pondría en una prueba A/B real**, porque las evaluaciones recientes sugieren que los modelos sub-400M pueden ser sorprendentemente competentes en tool selection. ([GitHub][8])

[1]: https://huggingface.co/Qwen/Qwen3-0.6B/blob/c916fa4defd319b7d4e4da17604ca7338f4d99f5/tokenizer_config.json?utm_source=chatgpt.com "tokenizer_config.json · Qwen/Qwen3-0.6B at c916fa4defd319b7d4e4da17604ca7338f4d99f5"
[2]: https://github.com/hackur/llama-cpp-wasm-qwen3?utm_source=chatgpt.com "GitHub - hackur/llama-cpp-wasm-qwen3 · GitHub"
[3]: https://github.com/MikeVeerman/tool-calling-benchmark?utm_source=chatgpt.com "GitHub - MikeVeerman/tool-calling-benchmark · GitHub"
[4]: https://ai.google.dev/gemma/docs/functiongemma?utm_source=chatgpt.com "FunctionGemma model overview  |  Google AI for Developers"
[5]: https://ai.google.dev/gemma/docs/functiongemma/model_card?utm_source=chatgpt.com "FunctionGemma model card  |  Google AI for Developers"
[6]: https://huggingface.co/HuggingFaceTB/SmolLM2-360M-Instruct/blob/main/README.md?utm_source=chatgpt.com "README.md · HuggingFaceTB/SmolLM2-360M-Instruct at main"
[7]: https://ai.google.dev/gemma/docs/functiongemma/finetuning-with-functiongemma?hl=es-419&utm_source=chatgpt.com "Ajuste con FunctionGemma  |  Google AI for Developers"
[8]: https://github.com/mynah-org/mynah-slm/blob/main/docs/models.md?utm_source=chatgpt.com "mynah-slm/docs/models.md at main · mynah-org/mynah-slm · GitHub"
[9]: https://ai.google.dev/gemma/docs/functiongemma/finetuning-with-functiongemma?utm_source=chatgpt.com "Fine-tuning with FunctionGemma  |  Google AI for Developers"
