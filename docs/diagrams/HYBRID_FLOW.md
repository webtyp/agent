# Flujo híbrido de un turno (propuesta)

Un turno del agente cuando lo conduce un modelo de decisión (`llm.Decider`, decider-0.8b) y el
texto lo escriben plantillas o un redactor pequeño (LFM2.5-350M). El modelo de decisión nunca
escribe texto libre. El código llama a las tools; ningún modelo lo hace. Ver
[HYBRID_DESIGN.md](../HYBRID_DESIGN.md).

```mermaid
flowchart TD
    M[mensaje del funcionario] --> G{decider: ¿intenta cambiar<br/>las reglas o el rol?}
    G -- sí, conf ≥ 0.8 --> R1[respuesta fija:<br/>no puedo hacer eso]
    G -- no --> P[ToolIndex: tools candidatas<br/>para el mensaje]
    P --> RT{decider: ¿qué tool?<br/>candidatas + ninguna}
    RT -- conf menor a 0.8 --> AQ[pregunta al funcionario:<br/>¿quisiste decir A o B?]
    RT -- ninguna --> SM[respuesta de conversación<br/>plantilla o redactor]
    RT -- tool --> AR[argumentos:<br/>código para fechas y RUT,<br/>decider para enums,<br/>mensaje como consulta de búsqueda]
    AR --> MOD{¿la tool modifica datos?}
    MOD -- sí --> CF[Reply.Pending:<br/>el funcionario confirma]
    MOD -- no --> EX[código ejecuta la tool]
    CF -- confirma --> EX
    EX --> AN{¿hay plantilla<br/>para esta tool?}
    AN -- sí --> TP[plantilla con los datos<br/>el código calcula días y horas]
    AN -- no --> WR[redactor LFM2.5-350M<br/>con los datos en español]
    WR --> CR{decider crítico:<br/>¿afirma algo no respaldado?}
    CR -- sí --> TP2[respuesta segura:<br/>datos en lista, sin redactar]
    CR -- no --> OUT[respuesta]
    TP --> OUT
```
