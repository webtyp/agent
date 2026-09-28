## PREGUNTA:

cual es la forma de manejar un promt en una converzacion sin superar el liminte de su ventana de contexto? ejeplo como pasar simpre casi el mismo contenido pero las opciones van cambiando.. .por ejemplo tener 2 herramietaa fijas una para ejecutar y otra para buscar tools.  que formato debria tener el promp para que sea dinamico. segun el proposito que se requiere?..si ya se cuneta con las tools y el rag como orquestar esto para que el promt que se pase siempre sea pequeño.. mantenitendo todo en el rag (memoria) de largo a corto plazo. identidad..cuales son las estrategias de los arneces actueles para manejar esto de foram mas efciente posible . por ejemplo estando en el contexto del navegador que es donde se ejecuara el agente, le paso un codumente el cual por debajo sera procesado y ene el promt solo deberia mostrar que? ..como se deberia estructurar?. cunado tipos de estructuras existes ? y por que? hay algun estandar acteulmente? justiifca@Busca en la web 

## RESPUESTA:
Sí. Para el agente que describes —**ejecutándose en el navegador, con RAG/memoria, MCP/tools y un LLM pequeño orientado a tool-calling**— yo no diseñaría un “prompt dinámico” que contenga progresivamente toda la conversación.

Diseñaría un **context compiler**: tienes un estado grande fuera del modelo y, para cada inferencia, construyes una pequeña *vista de contexto* con solamente lo que el modelo necesita en ese momento.

Esto coincide bastante con la dirección actual de los sistemas de agentes: Anthropic lo llama **context engineering**, Google plantea el contexto como una *compiled view* sobre estado persistente, y OpenAI recomienda preservar prefijos estables, cargar herramientas bajo demanda y compactar el historial. ([Anthropic][1])

---

# 1. La idea fundamental

No pienses:

```text
CONVERSACIÓN
+ MEMORIA
+ DOCUMENTO
+ TODAS LAS TOOLS
+ IDENTIDAD
+ INSTRUCCIONES
+ RAG
----------------
       ↓
     PROMPT
```

Eso termina creciendo indefinidamente.

Piensa:

```text
                 ┌──────────────┐
                 │    STATE     │
                 │              │
                 │ conversation │
                 │ memory       │
                 │ documents    │
                 │ tools        │
                 │ identity     │
                 │ task         │
                 └──────┬───────┘
                        │
                 context compiler
                        │
                        ▼
              ┌──────────────────┐
              │ WORKING CONTEXT  │
              │                  │
              │ identity         │
              │ task             │
              │ relevant memory  │
              │ relevant tools   │
              │ current input    │
              └────────┬─────────┘
                       │
                       ▼
                     LLM
```

La diferencia es enorme.

**El RAG no es el prompt.**

**La memoria no es el prompt.**

**Los documentos no son el prompt.**

Son fuentes desde las cuales construyes el contexto de trabajo.

Google describe precisamente esta separación entre *working context*, *session*, *memory* y *artifacts*. ([Google Developers Blog][2])

---

# 2. En tu caso, yo tendría solamente 2 tools permanentes

Tu idea es buena:

```text
tool.search
tool.execute
```

Y evitaría registrar inicialmente:

```text
calendar.create
calendar.delete
github.create
github.search
weather.get
database.query
...
```

Todas esas herramientas pueden estar fuera del contexto.

El modelo ve:

```text
TOOLS

search_tools(query)
execute_tool(name, arguments)
```

Y nada más.

Entonces:

```text
Usuario:
"Busca mis reservas de mañana y cancela la de las 15:00"
```

El LLM decide:

```text
search_tools("buscar reservas y cancelar reserva")
```

El runtime devuelve solamente:

```json
[
  {
    "name": "calendar.search",
    "description": "Busca eventos...",
    "input": {...}
  },
  {
    "name": "calendar.cancel",
    "description": "Cancela un evento...",
    "input": {...}
  }
]
```

Ahora el modelo puede:

```text
execute_tool(
    "calendar.search",
    {"date":"tomorrow"}
)
```

y posteriormente:

```text
execute_tool(
    "calendar.cancel",
    {"id":"evt_9281"}
)
```

Esto es prácticamente el patrón que Anthropic está utilizando con **Tool Search / deferred loading**: las herramientas no necesarias no se introducen inicialmente en el contexto y se incorporan sólo cuando son descubiertas. ([Anthropic][3])

---

# 3. El prompt debería ser casi estático

Yo separaría tu contexto en **5 capas**:

```text
SYSTEM
│
├── IDENTITY
├── BEHAVIOR
├── TOOL PROTOCOL
├── CONTEXT PROTOCOL
└── OUTPUT PROTOCOL
```

Después:

```text
STATE
│
├── TASK
├── CURRENT TURN
├── RELEVANT MEMORY
├── RELEVANT DOCUMENTS
└── DISCOVERED TOOLS
```

Y solamente la segunda parte cambia.

Por ejemplo:

```text
You are an assistant.

IDENTITY
You are ...

RULES
- Follow the user's request.
- Use tools when external information or actions are required.
- Never invent tool results.
- Ask when required information is missing.

TOOLS
- search_tools(query)
- execute_tool(name, arguments)

CONTEXT
Relevant memory:
...

Relevant documents:
...

Current task:
...

User:
...
```

Eso podría ser tu **plantilla base**.

---

# 4. Pero hay algo todavía mejor

Yo no pondría toda la identidad cada vez si el runtime/modelo permite mantener un prefijo estable.

Tendrías:

```text
[STATIC PREFIX]

AGENT
version: 1

IDENTITY
...

RULES
...

TOOL PROTOCOL
...

OUTPUT PROTOCOL
...

AVAILABLE CORE TOOLS
...
```

Y después:

```text
[DYNAMIC SUFFIX]

TASK
...

MEMORY
...

DOCUMENTS
...

USER
...
```

Esto es importante por **prompt caching**.

OpenAI recomienda explícitamente colocar instrucciones estables y definiciones de herramientas primero, mantenerlas sin cambios y agregar el contenido dinámico después. ([OpenAI Developers][4])

Por lo tanto:

```text
┌───────────────────────────────┐
│ STATIC                        │
│                               │
│ identity                      │
│ rules                         │
│ tool protocol                 │
│ output protocol               │
│ core tools                    │
│                               │
│ ← CACHEABLE                   │
├───────────────────────────────┤
│ DYNAMIC                       │
│                               │
│ task                          │
│ memory                        │
│ retrieved docs                │
│ current turn                 │
│                               │
│ ← cambia                      │
└───────────────────────────────┘
```

---

# 5. ¿Y qué hago con un documento enorme?

Aquí está uno de los errores más comunes:

> "El usuario entregó un PDF, entonces debo meter el PDF en el prompt."

No.

El navegador debería registrar:

```text
artifact:
  id: doc_381
  type: pdf
  name: contrato.pdf
  size: 4.8 MB
```

Y procesarlo fuera del contexto.

Después:

```text
RAG

doc_381
 ├── chunk 001
 ├── chunk 002
 ├── chunk 003
 ├── ...
 └── chunk 847
```

Incluso puedes generar:

```text
document summary
document topics
document entities
document metadata
```

El LLM inicialmente recibe:

```text
DOCUMENTS

contracto.pdf
id: doc_381
summary: Contrato de prestación de servicios...
```

Nada más.

Cuando pregunta:

> ¿Cuál es la duración del contrato?

El RAG recupera:

```text
chunk_183
```

y entonces el modelo recibe solamente:

```text
RETRIEVED CONTEXT

[source=doc_381#183]
...
```

Google describe este patrón como **Artifacts + handles**: el documento grande permanece fuera del contexto y sólo se carga temporalmente cuando el agente lo necesita. ([Google Developers Blog][2])

---

# 6. Esto también aplica a la memoria

Yo separaría memoria en niveles.

### L0 — conversación inmediata

```text
últimos 2–6 turnos
```

Muy barata.

### L1 — estado de tarea

```json
{
  "goal": "...",
  "status": "waiting_tool",
  "entities": {},
  "constraints": [],
  "last_action": "..."
}
```

Esto es **mucho más importante que conservar toda la conversación**.

### L2 — memoria episódica

```text
"El usuario anteriormente decidió utilizar PostgreSQL."
```

RAG.

### L3 — memoria semántica

```text
preferencias
hechos
conocimiento
```

RAG/vector/keyword.

### L4 — artefactos

```text
PDF
imagen
CSV
documentos
resultados grandes
```

Fuera del contexto.

---

# 7. Entonces el prompt de una iteración podría ser diminuto

Imagina:

```text
IDENTITY
assistant

TASK
Reservar una hora con el médico.

STATE
doctor = Juan Pérez
date = mañana
time = 15:00

TOOLS
search_tools
execute_tool

USER
Reserva la hora.
```

Eso es suficiente.

No necesitas:

```text
los últimos 50 mensajes
+
el PDF completo
+
las 300 tools
+
toda la memoria
+
todos los resultados anteriores
```

---

# 8. El estado debería ser estructurado

Para tu proyecto yo **no usaría texto libre para todo**.

Tendría un `AgentState` conceptual:

```json
{
  "session": "...",
  "task": {
    "goal": "...",
    "status": "..."
  },
  "user": {
    "id": "...",
    "locale": "es-CL"
  },
  "memory": [
    "..."
  ],
  "artifacts": [
    {
      "id": "...",
      "summary": "..."
    }
  ],
  "tools": {
    "available": [],
    "loaded": []
  },
  "turn": {
    "input": "..."
  }
}
```

Y el **context compiler** convierte eso en:

```text
SYSTEM
...

TASK
...

MEMORY
...

TOOLS
...

USER
...
```

Esto es conceptualmente muy parecido a la arquitectura de Google ADK: el *working context* se reconstruye desde session + memory + artifacts en cada invocación. ([Google Developers Blog][2])

---

# 9. Hay una distinción importantísima: estado ≠ contexto

Esta es probablemente la idea más importante para tu arquitectura.

```text
STATE
```

puede ser enorme.

```text
CONTEXT
```

debe ser pequeño.

Por ejemplo:

```text
STATE
────────────────────
10.000 mensajes
3.000 memorias
100 documentos
500 herramientas
2 GB de artifacts
────────────────────

CONTEXT
────────────────────
identity
task
5 memories
2 documents chunks
3 tools
current message
────────────────────
```

El contexto es una **proyección** del estado.

---

# 10. ¿Qué estructuras existen actualmente?

No hay un estándar único que diga:

> "Este es el formato universal de contexto de agentes."

Pero sí existe una convergencia bastante clara.

## A. Chat/messages

```json
[
  {"role":"system","content":"..."},
  {"role":"user","content":"..."},
  {"role":"assistant","content":"..."}
]
```

Es el formato tradicional.

Bueno para conversaciones.

Malo para estados complejos.

---

## B. Tool calling estructurado

```json
{
  "name": "calendar.search",
  "arguments": {
    "date": "2026-09-28"
  }
}
```

Actualmente es fundamental para agentes.

---

## C. MCP

MCP separa explícitamente:

```text
Prompts
Resources
Tools
```

y les asigna diferentes responsabilidades. ([Model Context Protocol][5])

La separación es muy interesante para tu arquitectura:

```text
PROMPT
  ↓
cómo comportarse

RESOURCE
  ↓
información

TOOL
  ↓
acción
```

Eso encaja casi perfectamente con tu idea.

---

# 11. MCP no significa "meter todas las tools en el prompt"

Esto es especialmente relevante para tu proyecto.

MCP define mecanismos para descubrir tools y recursos, pero el cliente puede controlar cómo expone esas capacidades al modelo. ([GitHub][6])

Además, las implementaciones modernas están incorporando mecanismos de cache y carga diferida; la especificación reciente incluso recomienda orden determinista en `tools/list` para facilitar caching. ([Model Context Protocol][7])

Por tanto:

```text
MCP SERVER
     │
     ├── 500 tools
     ├── 100 resources
     └── 20 prompts
          │
          ▼
       AGENT
          │
          ├── tool search
          │
          ▼
       2-4 tools
          │
          ▼
        LLM
```

Eso es mucho más escalable.

---

# 12. Un patrón que me parece especialmente adecuado para tu navegador

Yo implementaría:

```text
                    ┌───────────────┐
                    │     USER      │
                    └───────┬───────┘
                            │
                            ▼
                 ┌─────────────────────┐
                 │    AGENT RUNTIME    │
                 │      browser       │
                 └─────────┬───────────┘
                           │
             ┌─────────────┼──────────────┐
             │             │              │
             ▼             ▼              ▼
          Memory         RAG          Artifacts
             │             │              │
             └─────────────┼──────────────┘
                           │
                           ▼
                 ┌──────────────────┐
                 │ CONTEXT COMPILER │
                 └────────┬─────────┘
                          │
                          ▼
                  ┌───────────────┐
                  │      LLM      │
                  └───────┬───────┘
                          │
                 ┌────────┴─────────┐
                 ▼                  ▼
           search_tools       execute_tool
```

El **runtime**, no el LLM, decide cuánto contexto cargar.

---

# 13. Y hay una optimización todavía más interesante

No necesariamente necesitas que el LLM diga:

```text
"necesito buscar tools"
```

en lenguaje natural.

Puedes darle una interfaz extremadamente pequeña:

```text
search(query)
execute(tool, args)
```

Y entrenar/ajustar el modelo para producir estructuradamente:

```json
{
  "action": "search",
  "query": "reservar hora médica"
}
```

o:

```json
{
  "action": "execute",
  "tool": "calendar.book",
  "arguments": {
    "doctor": "123",
    "date": "2026-09-28",
    "time": "15:00"
  }
}
```

Eso reduce muchísimo la cantidad de texto necesario.

Para un modelo sub-1B orientado específicamente a **tool orchestration**, esto es especialmente interesante.

---

# 14. Incluso puedes evitar describir herramientas en lenguaje natural

Tu `search_tools` puede trabajar contra:

```text
tool registry
```

con:

```text
name
description
keywords
embedding
parameters
permissions
```

Por ejemplo:

```json
{
  "name": "calendar.book",
  "description": "Reserva una hora",
  "tags": ["calendar", "appointment", "booking"],
  "embedding": "...",
  "schema": "..."
}
```

El modelo solamente pregunta:

```text
search("reservar hora")
```

El **RAG de tools** encuentra:

```text
calendar.book
calendar.availability
calendar.cancel
```

y devuelve las definiciones.

Anthropic está utilizando precisamente la idea de **tool discovery + carga bajo demanda** para evitar que cientos de herramientas ocupen el contexto desde el principio. ([Anthropic][3])

---

# 15. Tu "prompt dinámico" realmente debería ser un DSL

Yo incluso iría un paso más allá.

No diseñaría el sistema alrededor de Markdown.

Diseñaría internamente algo como:

```text
Context
├── identity
├── policy
├── task
├── state
├── memory[]
├── resources[]
├── tools[]
└── input
```

Y después:

```text
Context → Renderer → Model format
```

Por ejemplo:

```text
                 AgentState
                     │
                     ▼
              Context Compiler
                     │
          ┌──────────┴──────────┐
          │                     │
       llama format          chat format
          │                     │
          ▼                     ▼
        model A               model B
```

Esto es muy importante si tu objetivo es **WASM/browser**.

No quieres que todo tu agente dependa de un formato de prompt específico.

---

# 16. Yo utilizaría "progressive disclosure"

Esta es probablemente la estrategia que más encaja con tu proyecto:

```text
NIVEL 0
────────────────
identity
rules
2 core tools
────────────────

NIVEL 1
────────────────
task state
short memory
────────────────

NIVEL 2
────────────────
tool search results
────────────────

NIVEL 3
────────────────
tool schema
────────────────

NIVEL 4
────────────────
tool result
────────────────

NIVEL 5
────────────────
RAG chunk específico
────────────────
```

Cada nivel aparece **solamente cuando hace falta**.

MCP está evolucionando precisamente hacia esta separación entre recursos, herramientas y prompts, y su extensión de Skills utiliza explícitamente un modelo de *progressive disclosure*. ([MCP Skills Extension][8])

---

# 17. ¿Qué haría con el historial?

No intentaría mantenerlo indefinidamente.

Por ejemplo:

```text
TURN 1
TURN 2
TURN 3
...
TURN 20
```

Cuando supera cierto presupuesto:

```text
TURN 1..15
       ↓
   summarize
       ↓
TASK STATE
```

y conservas:

```text
SUMMARY
+
LAST 3 TURNS
+
CURRENT STATE
```

Pero **no resumiría indiscriminadamente todo**.

Separaría:

```text
conversation summary
task state
facts
decisions
user preferences
tool history
```

porque un resumen único mezcla cosas que tienen distinta vida útil.

OpenAI actualmente ofrece *compaction* precisamente para reducir conversaciones largas conservando el estado necesario; su documentación también advierte que la compactación puede cambiar el prefijo cacheable, por lo que conviene mantener estable la parte reutilizable. ([OpenAI Developers][9])

---

# 18. Arquitectura final que te recomendaría

Para **tu agente browser/WASM**, yo lo organizaría así:

```text
                   AGENT
                     │
     ┌───────────────┼────────────────┐
     │               │                │
     ▼               ▼                ▼
  Identity         Session          Memory
  static           short-term       long-term
                     │                │
                     │                ▼
                     │               RAG
                     │
                     ▼
                  Task State
                     │
       ┌─────────────┴─────────────┐
       │                           │
       ▼                           ▼
    Artifacts                  Tool Registry
       │                           │
       │                      tool-search
       │                           │
       └─────────────┬─────────────┘
                     ▼
              CONTEXT COMPILER
                     │
                     ▼
                  LLM <1B
                     │
             ┌───────┴───────┐
             ▼               ▼
        search_tools     execute_tool
```

Y el modelo **nunca necesita conocer todo el mundo**.

Solamente:

> **qué soy + qué estoy intentando hacer + qué sé ahora + qué puedo hacer ahora.**

---

# 19. El formato que yo probaría inicialmente

Algo extremadamente pequeño:

```text
<identity>
assistant
</identity>

<task>
{goal}
</task>

<state>
{minimal_state}
</state>

<memory>
{relevant_memory}
</memory>

<tools>
search_tools
execute_tool
</tools>

<input>
{user_message}
</input>
```

Pero incluso esto es una **representación para el modelo**, no tu estructura interna.

Internamente:

```go
type Context struct {
    Identity  Identity
    Task      Task
    State     State
    Memory    []Memory
    Tools     []Tool
    Input     string
}
```

Luego:

```text
Context
   ↓
budget/relevance filter
   ↓
prompt renderer
   ↓
LLM
```

Y cada sección tiene un presupuesto:

```text
identity       100 tokens
task           100
state          200
memory         300
tools          500
input          variable
──────────────────────
~1.2k tokens
```

En vez de:

```text
20k
50k
100k
...
```

---

# 20. La conclusión importante

Lo que estás describiendo ya no es realmente **prompt engineering**.

Es **context engineering**.

Y actualmente hay una convergencia bastante clara hacia:

**1. Estado externo al LLM**

**2. Memoria recuperable, no permanentemente inyectada**

**3. Artifacts externos**

**4. Tools descubiertas bajo demanda**

**5. Contexto de trabajo reconstruido para cada inferencia**

**6. Prefijo estable + sufijo dinámico**

**7. Compaction/summarization cuando la sesión crece**

**8. Tool results pequeños y estructurados**

**9. Progressive disclosure**

**10. Caching del contexto estable**

Eso es exactamente lo que hace que un agente pueda tener **muchísima más información disponible que la que realmente ocupa su ventana de contexto**. Anthropic, OpenAI y Google están convergiendo en estas mismas ideas, aunque con implementaciones diferentes. ([Anthropic][1])

Y para **tu caso concreto**, yo reduciría el problema a una arquitectura de **`State → Retrieve → Compile → Infer → Update State`**, donde `search_tools` y `execute_tool` sean prácticamente las únicas capacidades que el LLM vea permanentemente.

Ese diseño además es particularmente apropiado para un **LLM <1B ejecutándose dentro de WASM**, porque desplaza la complejidad desde el modelo hacia el runtime/RAG, que es justamente lo que necesitas si tu objetivo es que el modelo sea bueno **decidiendo qué hacer**, no almacenando todo el conocimiento del agente.

[1]: https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents?utm_source=chatgpt.com "Effective context engineering for AI agents \ Anthropic"
[2]: https://developers.googleblog.com/architecting-efficient-context-aware-multi-agent-framework-for-production/?utm_source=chatgpt.com "Architecting efficient context-aware multi-agent framework for production - Google Developers Blog"
[3]: https://www.anthropic.com/engineering/advanced-tool-use?utm_source=chatgpt.com "Introducing advanced tool use on the Claude Developer Platform \ Anthropic"
[4]: https://developers.openai.com/api/docs/guides/prompt-caching?utm_source=chatgpt.com "Prompt caching | OpenAI API"
[5]: https://modelcontextprotocol.io/specification/draft/server/index?utm_source=chatgpt.com "Overview - Model Context Protocol"
[6]: https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-06-18/server/tools.mdx?utm_source=chatgpt.com "modelcontextprotocol/docs/specification/2025-06-18/server/tools.mdx at main · modelcontextprotocol/modelcontextprotocol · GitHub"
[7]: https://modelcontextprotocol.io/specification/draft/changelog?utm_source=chatgpt.com "Key Changes - Model Context Protocol"
[8]: https://skills.extensions.modelcontextprotocol.io/specification/stable/skills?utm_source=chatgpt.com "Skills | MCP Skills Extension"
[9]: https://developers.openai.com/api/docs/guides/compaction?utm_source=chatgpt.com "Compaction | OpenAI API"


