# System context

The agent and every library it composes. Solid arrows are version 1 (text). Dotted arrows are
version 2 (voice), wired by the application around `agent.Run`. Everything runs in the browser,
in Go compiled with TinyGo, inside one Web Worker. MCP servers can be remote.

```mermaid
flowchart TD
    User[User] -->|text| App[application<br/>page + Web Worker]
    User -. voice v2 .-> Media[webtyp/media<br/>microphone]
    Media -. audio.PCM .-> STT[webtyp/stt]
    STT -. text .-> App
    App -->|Run| Agent[webtyp/agent<br/>orchestrator + FSM]
    Agent -->|compile request| Ctx[webtyp/agentcontext]
    Agent -->|Generate / CountTokens| LLM[webtyp/llm contract<br/>in-browser model runtime]
    Agent -->|memory ports| Mem[webtyp/agentmemory]
    Mem -->|orm + ddl| Store[IndexedDB in the browser<br/>SQL on a server]
    Mem -->|knowledge search| Ret[webtyp/retrieval]
    Ret --> Emb[webtyp/embed + vectordb]
    Agent -->|JSON-RPC 2.0| MCP[MCP servers]
    Agent -->|direct call| Local[local Go tools]
    App -. answer text .-> TTS[webtyp/tts]
    TTS -. audio.PCM .-> Play[playback]
```
