La gestión eficiente del contexto se logra mediante la **inyección dinámica y la modularidad**. El prompt deja de ser un texto estático gigantesco y se convierte en una plantilla (template) que un orquestador ensambla en tiempo real, inyectando solo lo estrictamente necesario para resolver el turno actual.

## **Estrategias de Orquestación (RAG, Memoria y Tools)**

Para mantener el prompt siempre pequeño mientras cambian las opciones y se retiene la identidad, los frameworks actuales (como LangChain, LlamaIndex o AutoGen) utilizan una arquitectura de capas:

> 1. **Gestión de Herramientas Dinámicas (Tool Retrieval):** En lugar de pasar las descripciones de 50 herramientas en el prompt, pasas solo dos herramientas fijas, tal como sugieres:  
   * ejecutar\_herramienta(nombre, parametros): Para usar la herramienta.  
   * buscar\_herramientas(intencion): Consulta una base de datos vectorial que devuelve solo los esquemas de las herramientas relevantes para ese turno. El agente usa esta primero, el orquestador inyecta temporalmente el esquema de la herramienta encontrada en el contexto, y luego el agente la ejecuta.  
> 2. **Memoria Híbrida (Corto y Largo Plazo):**  
   * **Corto plazo (Sliding Window / Summarization):** El prompt solo contiene los últimos *N* mensajes exactos. Cuando la ventana se llena, un proceso en segundo plano resume los mensajes antiguos y los convierte en un único bloque de "Contexto previo".  
   * **Largo plazo (RAG):** Las interacciones pasadas o los datos del usuario se guardan en una base de datos vectorial. Cuando el usuario hace una pregunta, el orquestador busca fragmentos relevantes y los inyecta.

## **El Caso del Navegador: Procesamiento de Documentos**

Si estás ejecutando un agente en un contexto de navegador (por ejemplo, usando WebAssembly y una base de datos local como IndexedDB) y el usuario sube un documento de 100 páginas, **jamás se inyecta el documento en el prompt**.  
El flujo correcto es:

> 1. **Procesamiento (Background):** El documento se extrae, se divide en fragmentos (chunks) semánticos y se vectoriza localmente.  
> 2. **Lo que ve el Prompt:** Al agente solo se le pasa una notificación de estado.  
>    Plaintext  
>    \[Sistema\]: El usuario acaba de cargar el documento "reporte\_financiero.pdf". No tienes el contenido del documento en tu contexto. Para responder preguntas sobre él, DEBES usar la herramienta \`buscar\_en\_documento(query)\`.

> 3. **Ejecución:** Si el usuario pregunta "¿Cuál es el balance neto?", el agente llama a la herramienta, el motor RAG recupera solo los 2 párrafos que hablan del balance neto, y el orquestador inyecta *esos dos párrafos* en el siguiente turno del prompt.

## **Estructura de un Prompt Dinámico**

Un prompt moderno orquestado sigue una estructura estandarizada de bloques inyectables (usualmente en formato JSON/ChatML por debajo):

> 1. **Instrucciones del Sistema (Identidad \- Estático):** "Eres un asistente experto. Tus respuestas son concisas."  
> 2. **Directivas Temporales (Estado \- Dinámico):** "Hoy es 27 de Septiembre. El usuario acaba de subir un archivo."  
> 3. **Memoria Recuperada (RAG \- Dinámico):** "Contexto relevante encontrado en interacciones previas: El usuario prefiere código en Python."  
> 4. **Esquemas de Herramientas (Dinámico):** Solo las firmas (nombres y parámetros) de las herramientas habilitadas para este turno.  
> 5. **Historial de Conversación (Recortado/Resumido):** Últimos 3-4 turnos.  
> 6. **Input del Usuario:** La consulta actual.

## **Paradigmas y Estándares Actuales**

Actualmente **sí existe un estándar de facto** en la industria tanto a nivel de estructura de mensajes como de razonamiento del agente.

### **1\. Estándar de Estructura: ChatML y System/User/Assistant**

Casi todos los modelos (OpenAI, Anthropic, Gemini, Llama) han convergido en estructurar el contexto no como texto plano, sino como un array de mensajes tipados.

* **¿Por qué?** Porque evita ataques de inyección de prompts (Prompt Injection) y permite a la red neuronal distinguir matemáticamente qué instrucciones son reglas inquebrantables (System), qué es petición (User) y qué es salida de herramientas (Tool/Function).

### **2\. Estándar de Arquitectura de Razonamiento: ReAct (Reason \+ Act)**

Es el patrón fundamental para agentes que usan herramientas. El prompt obliga al LLM a estructurar su salida en un bucle:

* **Thought (Pensamiento):** "El usuario quiere saber sobre X. Necesito buscar herramientas para esto."  
* **Action (Acción):** Llama a buscar\_herramientas(query="X").  
* **Observation (Observación):** El orquestador pausa la generación, ejecuta la función, e inyecta el resultado de vuelta.  
* **¿Por qué?** Se demostró que forzar al modelo a "pensar en voz alta" antes de actuar reduce significativamente las alucinaciones y mejora la selección correcta de parámetros.

### **3\. Estándar de Tareas Complejas: Plan-and-Execute**

Para evitar que un agente consuma todo su contexto iterando a ciegas, este patrón divide el agente en dos.

* **Planificador:** Recibe la meta, crea un plan de 5 pasos y se apaga.  
* **Ejecutor:** Recibe un prompt minúsculo que solo contiene *el paso actual* y las herramientas necesarias para ese paso.  
* **Justificación:** Es la forma más eficiente de aislar el contexto. El Ejecutor nunca necesita saber el plan completo de 20 páginas; solo necesita el contexto para resolver la tarea de ese segundo.