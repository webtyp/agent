fundamentos técnicos necesarios para diseñar **agentes de IA de nivel producción**, utilizando como ejemplo un sistema de atención al cliente. La premisa central es que un agente es un **bucle de razonamiento** (planificar, actuar, observar, repetir) y no solo un simple prompt.

Los conceptos clave abordados son:

* **El bucle del agente:** La fiabilidad disminuye con cada paso añadido, por lo que es vital gestionar la complejidad (0:51-1:56).
* **Enrutamiento de modelos:** Balancear el uso de modelos pequeños y rápidos para tareas simples, y modelos "frontera" potentes para casos complejos que requieren juicio (1:57-2:55).
* **Herramientas (Tools) y MCP:** El diseño de herramientas debe seguir el modelo de APIs estrictas, separando las de lectura (bajo riesgo) de las de escritura (alto riesgo) (2:56-5:20).
* **Memoria y Estado:** Diferenciar entre el estado de ejecución inmediata y la memoria a largo plazo para evitar la "podredumbre del contexto" (5:21-7:19).
* **RAG (Generación Aumentada por Recuperación):** Vital para gestionar bases de conocimiento sin saturar el prompt, aunque requiere una recuperación precisa para evitar respuestas incorrectas (6:33-7:23).
* **Orquestación:** Decidir entre un pipeline definido o agentes autónomos, priorizando siempre la arquitectura más simple que funcione (7:24-8:16).
* **Evaluaciones (Evals) y Observabilidad:** Dado que los agentes son no deterministas, se requieren tests de regresión específicos y rastreo (tracing) detallado para depurar errores en producción (8:17-9:52).
* **Seguridad y controles:** Implementar validaciones de código para acciones de alto riesgo (como reembolsos) y tratar cualquier entrada del modelo como potencialmente hostil (9:53-10:51).