---
PLAN: "refactor!: agent consumes llm and agentcontext; feat: tool search (search_tools + ToolIndex)"
TAG: v0.6.0
EXECUTOR: jules
REVIEWER: none
STATUS: review
SESSION: 8612478597620030459
PR: https://github.com/webtyp/agent/pull/13
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.
>
> Phase 3 of [`AGENT_ECOSYSTEM_MASTER_PLAN.md`](AGENT_ECOSYSTEM_MASTER_PLAN.md).
> **Blocked until `webtyp.com/agentcontext` v0.1.0 is published** (`webtyp.com/llm` v0.1.0 is).

# PLAN — execution queue for `webtyp/agent`

> If you were told to "execute the plan described in docs/PLAN.md", execute
> **ALL the plans below, in order (top to bottom)**. Each plan is
> self-contained; finish one (its acceptance criteria green) before starting
> the next. Never mix changes from one plan into another.

| Order | Plan | Subject |
|-------|------|---------|
| 1 | [PLAN_LLM_AGENTCONTEXT.md](PLAN_LLM_AGENTCONTEXT.md) | consume `llm` + `agentcontext`; delete `context_window.go` and the duplicated types; llama-server integration test |
| 2 | [PLAN_TOOL_SEARCH.md](PLAN_TOOL_SEARCH.md) | the model is offered only `search_tools`; discovered tools become callable; `ToolIndex` port + keyword reference index |

After completing all plans, run `gotest ./...` and `gotest -tinygo` one final time: everything green.
