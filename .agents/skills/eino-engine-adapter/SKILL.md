---
name: eino-engine-adapter
description: Implement or modify the CloudWeGo Eino agent engine adapter for z-Bot — the ONLY package allowed to import github.com/cloudwego/eino. Use when working in backend/internal/engine/eino/, adding middleware adapters, wiring ADK/DeepAgents/TaskTool/Skill middleware, or swapping to a different agent engine.
---

# Eino Engine Adapter (backend/internal/engine/eino/)

Authority: root `AGENTS.md` and `backend/AGENTS.md`. This skill operationalizes
their engine rules.

## The Isolation Boundary

- `backend/internal/engine/eino/` is the **only** package that may import
  `github.com/cloudwego/eino` or `eino-ext` modules.
- Everything outside depends on `domain.AgentEngine`:

```go
type AgentEngine interface {
    Name() string
    RunGoal(ctx context.Context, req GoalRequest) (<-chan TimelineEvent, error)
    Resume(ctx context.Context, sessionID string, input UserInput) (<-chan TimelineEvent, error)
    Cancel(ctx context.Context, sessionID string) error
}
```

- Before merging, verify no other package grew an eino import:
  `grep -rn "cloudwego/eino" backend/ --include="*.go" | grep -v "internal/engine/eino/"`
  must return nothing.

## What the Adapter Owns

Build the engine on Eino ADK primitives — `adk.Runner`, `deep.New`
(DeepAgents), `WriteTodos`, `TaskTool` sub-agents, `eino-ext` MCP tools, and
the `Skill` middleware — all behind `domain.AgentEngine`:

- **Goal runs and resume** stream `domain.TimelineEvent`s to the eventbus.
- **Tools** resolve from the registry: Ward-scoped FS/Shell backends
  (`internal/tools/`), MCP servers from `~/.zbot/config/mcps/*.yaml`, skills
  from `~/.zbot/config/skills/**`.
- **Middleware adapters** (each its own file, thin, no policy):
  - `middleware_compaction.go` — bridges `domain.Compactor` into Eino's
    `ChatModelAgentMiddleware` `BeforeModel` hook (see the compaction rule
    in `backend/AGENTS.md`).
  - Hook middleware — routes tool calls through `domain.HookRunner`
    (`PreToolUse`/`PostToolUse`).

## Engine Swap Procedure

Swapping Eino for another framework (Google ADK Go, custom ReAct loop, …):

1. Create `backend/internal/engine/<new-engine>/` implementing
   `domain.AgentEngine` from scratch — do not copy eino internals.
2. Wire it into the same engine registry/factory the server uses.
3. Flip `engine: <new-engine>` in `~/.zbot/config.yaml`.
4. Memory, compaction, hooks, wards, Timeline Tree, server, and all clients
   stay untouched. If a swap requires touching any of those, the seam is
   leaking — stop and fix the seam instead.

## Checklist Before Finishing

- [ ] No eino imports outside `internal/engine/eino/`
- [ ] Adapter contains no compaction policy and no hook policy (delegates to
      `domain.Compactor` / `domain.HookRunner`)
- [ ] Every step, delegation, tool call, and decision emits a TimelineEvent
- [ ] Table-driven tests with a fake LLM; never a live API call
