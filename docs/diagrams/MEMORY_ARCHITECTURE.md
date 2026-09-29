# Memory architecture

The agent declares **what** it needs to remember as four narrow ports. Where it is stored is
decided by the implementation, which is `webtyp/agentmemory` in production and `mem_memory.go`
in tests. The same implementation code runs in the browser (IndexedDB) and on a server (SQL),
because it is written against `orm` + `ddl`, not against a database.

```mermaid
flowchart TD
    O[agent orchestrator] --> CS[ConversationStore<br/>turns of the conversation]
    O --> SS[SummaryStore<br/>summaries of compacted turns]
    O --> KS[KnowledgeStore<br/>facts and documents, searched by text]
    O --> TS[ToolLogStore<br/>audit of every tool execution]
    CS --> AM[webtyp/agentmemory]
    SS --> AM
    KS --> AM
    TS --> AM
    AM -->|rows| ORM[webtyp/orm + ddl]
    ORM --> IDB[IndexedDB via webtyp/indexdb<br/>browser]
    ORM --> SQL[postgres or sqlt<br/>server]
    AM -->|chunks + vectors| RET[webtyp/retrieval]
    RET --> EMB[webtyp/embed<br/>text to vector, in the browser]
    RET --> VDB[webtyp/vectordb<br/>kNN over a shared arena]
```

| Port | Kind of memory | Read by |
|---|---|---|
| `ConversationStore` | short-term: the recent turns | `agentcontext.Compile` via the orchestrator |
| `SummaryStore` | compacted history | `agentcontext.Compile` |
| `KnowledgeStore` | long-term semantic memory (global or per session) | the agent's knowledge lookups |
| `ToolLogStore` | action memory: what ran, with what, and how it went | audits and self-correction |

`SearchKnowledge` receives text. Embedding it and searching vectors is done inside the
implementation, which is why the agent never imports `embed` or `vectordb`.
