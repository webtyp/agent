# Mapa del ecosistema del agente

Esta página muestra **qué piezas existen, cómo se conectan y en qué estado está cada una**, para
quien necesita entender el proyecto sin haber seguido su construcción. Un *agente* es un programa
que recibe un mensaje, deja que un modelo de lenguaje decida qué herramientas (*tools*) usar, las
ejecuta y devuelve una respuesta. En webtyp todo eso corre **dentro del navegador**, en Go
compilado a WebAssembly (WASM) con TinyGo, sin servidor de inferencia.

Los colores indican el estado de cada repositorio:

- **verde:** publicado y probado;
- **amarillo:** tiene plan escrito, pero no código;
- **gris:** solo documentación;
- **rojo:** hay que realinearlo.

Las secciones 0, 4 y 5 son **propuestas pendientes de decisión**.

## 0. Lo que ve Cote (la aplicación)

Cote es el asistente del consultorio María Josefa. Su código solo debe **configurar**: quién es
Cote (el prompt), con qué servidor de tools habla y qué modelo usa. `agent` no puede elegir por
sí mismo el modelo ni la memoria: es una librería que también debe servir para otras
aplicaciones, otros modelos y servidores. Alguien tiene que crear esas piezas concretas y
entregárselas. Ese "alguien" es una pieza reutilizable que arma el agente dentro de un Web
Worker, y así cada aplicación no repite el mismo armado.

```mermaid
flowchart TD
    JOSE[Cote: config<br/>prompt + reglas + URL MCP de mjosefa-cms] --> AW[agentworker<br/>arma el agente en un Web Worker]
    AW --> AGENT[agent<br/>orquesta: decide, llama tools, responde]
    AW --> MODEL[modelo: qwen]
    AW --> MEMORY[memoria: agentmemory]
    AGENT --> CMS[mjosefa-cms por MCP<br/>horarios, reservas, pacientes<br/>con los permisos del rol de Cote]
    classDef proposal fill:#1565c0,color:#fff
    class AW proposal
```

## 1. Repositorios por capa

Cada flecha va de quien importa a lo importado. Un repositorio **contrato** solo declara
interfaces y tipos. Una **implementación** cumple un contrato. Una **herramienta** corre en la
máquina del desarrollador, nunca en el navegador.

```mermaid
flowchart TD
    APP[Cote<br/>la aplicación, hoy mjosefa-cora] --> AGENT[agent v0.6<br/>orquestador: bucle ReAct, FSM, tools]
    APP --> AM[agentmemory<br/>memoria del agente en IndexedDB]
    APP --> JS[js v0.0.11<br/>Web Worker]
    AGENT --> AC[agentcontext v0.1<br/>compila lo que ve el modelo]
    AGENT --> LLM[llm v0.1<br/>contrato con un modelo]
    AGENT --> MCP[mcp<br/>tools remotas JSON-RPC]
    AM --> RET[retrieval<br/>búsqueda por significado]
    RET --> EMB[embed v0.4<br/>contrato de embeddings]
    RET --> VDB[vectordb v0.2]
    BEKKO[bekko v0.1<br/>modelo de embeddings 8M] --> EMB
    BEKKO --> ENC[encoder v0.2]
    QWEN[qwen<br/>Qwen3.5-0.8B como llm.Client] --> LLM
    QWEN --> TOK[tokenizer v0.3<br/>texto a ids]
    QWEN --> DEC[decoder v0.1<br/>un token por paso]
    DEC --> NN[nn v0.1<br/>operaciones matemáticas]
    ENC --> NN
    DEC --> W[weights v0.2<br/>formato de pesos]
    QWEN --> OPFS[opfs<br/>archivos del navegador]
    OPFS --> FILES[files v0.0.2<br/>contrato de archivos]
    WC[weightsc v0.2<br/>herramienta: convierte pesos] --> W
    classDef done fill:#2e7d32,color:#fff
    classDef plan fill:#f9a825,color:#000
    classDef docs fill:#757575,color:#fff
    classDef stale fill:#c62828,color:#fff
    class AGENT,AC,LLM,MCP,EMB,VDB,BEKKO,ENC,TOK,DEC,NN,W,FILES,WC,JS done
    class AM,QWEN plan
    class RET,OPFS docs
    class APP stale
```

`stt`, `tts`, `audio` y `phoneme` (voz, versión 2) no aparecen en el diagrama porque la versión 1
es solo texto. Sus contratos ya están publicados.

## 2. Qué pasa con un mensaje

Un funcionario le escribe a Cote "¿hasta qué hora atendemos hoy?". La página solo dibuja. El modelo corre en un
*Web Worker*, un hilo aparte del navegador, para que la página no se congele mientras el modelo
piensa.

```mermaid
flowchart TD
    U[funcionario escribe en el chat] --> PAGE[página<br/>wasm pequeño, solo interfaz]
    PAGE -->|js Post| WK[Web Worker<br/>wasm con SIMD o sin SIMD]
    WK --> RUN[agent.Run sesión, mensaje]
    RUN --> MEM[agentmemory: últimos turnos<br/>y resúmenes de la sesión]
    MEM --> COMP[agentcontext.Compile<br/>identidad + resúmenes + turnos + tools<br/>recortado al presupuesto de tokens]
    COMP --> GEN[qwen.Generate]
    GEN --> TPL[plantilla de chat de Qwen]
    TPL --> TK[tokenizer: texto a ids]
    TK --> STEP[decoder.Step por cada token<br/>gramática: solo respuesta o tool válida]
    STEP --> RESP{¿qué devolvió el modelo?}
    RESP -- llamada a tool --> EXEC[agent ejecuta la tool<br/>local en Go o MCP remoto]
    EXEC --> COMP
    RESP -- respuesta --> REFL[agent: reflexión y guardado del turno]
    REFL --> PAGE
```

## 3. De dónde salen los pesos del modelo

Los pesos se convierten **una vez**, en la máquina del desarrollador. El navegador los descarga
una sola vez y los guarda en OPFS, el sistema de archivos privado del sitio. `llama-server` no
forma parte del producto: es la **referencia** con la que se verifica que nuestro código da los
mismos números.

```mermaid
flowchart TD
    HF[Qwen3.5-0.8B original<br/>safetensors bf16] --> WC[weightsc<br/>int8 en bloques de 32]
    WC --> ART[qwen3.5-0.8b.wtypw 851 MB<br/>+ qwen3.5-0.8b.merges]
    ART -->|primera visita| OPFS[OPFS del navegador]
    OPFS --> OPEN[weights.Open]
    OPEN --> DEC[decoder.New]
    GGUF[mismo modelo en GGUF Q8_0] --> LS[llama-server<br/>solo desarrollo]
    LS --> REF[referencia: pruebas de integración<br/>y comparación de resultados]
```

## 4. Propuesta: dónde entra el enrutador de tools

**Estado: pendiente de decisión.** Un *enrutador* (router) decide qué tools le mostramos al
modelo para este mensaje. Hoy el modelo debe pedirlas él mismo con `search_tools`, y un modelo de
0,8B se pierde en ese salto extra. La propuesta separa tres responsabilidades:

- **agent** pregunta, porque es el único que hace entrada/salida;
- **el enrutador** ordena las tools por relevancia;
- **agentcontext** decide cuántas caben.

```mermaid
flowchart TD
    MSG[mensaje del usuario] --> ASK[agent: pregunta al enrutador<br/>antes del primer paso]
    ASK --> ROUTER{implementación del enrutador}
    ROUTER --> KW[por palabras<br/>en agent, para pruebas]
    ROUTER --> SEM[por significado con bekko<br/>en agentmemory]
    ROUTER --> TRAINED[entrenado con ejemplos<br/>repo futuro]
    KW --> RANK[tools ordenadas por relevancia]
    SEM --> RANK
    TRAINED --> RANK
    RANK --> FIT{agentcontext:<br/>¿caben todas en el presupuesto?}
    FIT -- sí --> ALL[se ofrecen todas]
    FIT -- no --> TOP[las k primeras + search_tools]
    ALL --> MODEL[paso del modelo]
    TOP --> MODEL
```

## 5. Propuesta: niveles de prueba

**Estado: pendiente de decisión.** Probar un agente no es igual que probar una función, porque el
modelo no siempre responde lo mismo. Por eso hay cuatro niveles, y solo los dos primeros son
pruebas de pasa/no pasa en cada cambio.

```mermaid
flowchart TD
    L0[Nivel 0: unitarias<br/>gotest con modelo simulado<br/>deterministas, en cada cambio] --> L1
    L1[Nivel 1: referencia numérica<br/>nuestros números = transformers / llama.cpp<br/>deterministas, en cada cambio] --> L2
    L2[Nivel 2: evaluación de comportamiento<br/>escenarios × modelo × N repeticiones<br/>resultado = tasa de éxito, no pasa/no pasa] --> L3
    L3[Nivel 3: laboratorio con interfaz<br/>editar el prompt, correr escenarios,<br/>ver transcripción y contexto compilado]
    L2 --> CHECK[verificadores deterministas<br/>¿llamó list_business_hours? ¿dice 8?]
    L2 --> JUDGE[modelo juez local<br/>tono, no inventar datos]
    L3 --> LABEL[etiquetas humanas<br/>miden si el juez acierta]
    LABEL --> JUDGE
```
