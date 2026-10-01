# Flujo híbrido de un turno

Un turno del agente: el código revisa el mensaje, un modelo de decisión (`llm.Decider`,
decider-0.8b) elige, y el texto lo escriben plantillas o un redactor pequeño (LFM2.5-350M). El
modelo de decisión nunca escribe texto libre. El código llama a las tools; ningún modelo lo hace.
Ver [HYBRID_DESIGN.md](../HYBRID_DESIGN.md), sección "La especificación de v1".

```mermaid
flowchart TD
    M[mensaje del funcionario] --> G{código: limpia y revisa<br/>largo, marcadores, roles, frases}
    G -- marcado o muy largo --> R1[respuesta fija:<br/>Texts.Refused o Texts.TooLong]
    G -- limpio --> P[ToolIndex: tools candidatas]
    P --> RT{decider: ¿qué tool?<br/>candidatas + ninguna}
    RT -- ninguna --> SM[Texts.NoTool]
    RT -- confianza menor a 0.8 --> AQ[Texts.Clarify:<br/>las dos tools más probables]
    RT -- tool --> AR[argumentos:<br/>enum por el decider,<br/>texto = el mensaje]
    AR --> MOD{¿la tool modifica datos?}
    MOD -- sí --> IJ1{decider: ¿intenta cambiar<br/>las reglas o el rol?}
    IJ1 -- sí --> R1
    IJ1 -- no --> CF[Reply.Pending:<br/>el funcionario confirma]
    CF -- confirma --> EX
    MOD -- no --> EX[código ejecuta la tool]
    EX --> YN{decider: ¿es una pregunta<br/>de sí o no?}
    YN -- sí --> FA{decider: según los datos,<br/>¿la respuesta es sí?}
    FA -- confianza ≥ 0.8 --> YS[Texts.Yes o Texts.No]
    FA -- dudoso --> AN
    YN -- no --> AN{¿la plantilla de la tool<br/>responde?}
    AN -- sí --> TP[plantilla de la aplicación]
    AN -- no, hay redactor --> IJ2{decider: ¿intenta cambiar<br/>las reglas o el rol?}
    IJ2 -- sí --> R1
    IJ2 -- no --> WR[redactor con los datos]
    WR --> CR{decider crítico:<br/>¿afirma algo no respaldado?}
    CR -- sí --> FD[Texts.Found + los datos tal cual]
    CR -- no --> OUT[respuesta]
    AN -- no, sin redactor --> FD
    TP --> OUT
    YS --> OUT
    FD --> OUT
```
