# Implementation Details — `webtyp/agent`

## File Organization

The repository follows a flat layout in the root directory for library code and isolates all tests in `tests/`.

```
├── agent.go            Constructor (New) & agent struct
├── arguments.go        Tool argument extraction from InputSchema
├── clock.go            Clock interface and MachineClock implementation
├── decide.go           Decision model helper questions
├── errors.go           Unexported error constants
├── guard.go            Code guard (sanitization, length, role lines, forbidden phrases)
├── interfaces.go       Memory, tool, clock, and MCP interfaces
├── mcp_client.go       HTTP MCP client implementation
├── mcp_json.go         JSON decoding helpers for MCP protocol
├── mcp_registry.go     Registry for local tools, MCP handlers, and MCP servers
├── mem_memory.go       In-memory reference implementation of MemoryStore
├── mem_tool_index.go   In-memory keyword-based implementation of ToolIndex
├── texts.go            Texts, Template, and Data structs
├── turn.go             Run, Confirm, Decline execution flow
├── types.go            Public Reply, Agent, Knowledge, ToolLog, and Config structs
├── conformance/        Conformance test suite for MemoryStore implementations
└── tests/              Public API unit tests
```

## Testing

Run tests with `go test ./...`. All tests consume only the public exported API of `webtyp.com/agent`.
