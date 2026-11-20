# backend/ — Go Constitution

Scope rules for all Go work in `backend/` (daemon `zbotd` + Bubbletea CLI
`zbot`), on top of the root constitution. Target stack: Go 1.26+,
`net/http` + `chi` + SSE, SQLite + sqlite-vec + FTS5, local ONNX.

## Engine Isolation Rule

**Only `internal/engine/eino/` may import `github.com/cloudwego/eino`** (and
its `eino-ext` modules). Every other package depends exclusively on the
`domain.AgentEngine` interface. Swapping engines means adding
`internal/engine/<new-engine>/` implementing `domain.AgentEngine` and flipping
`engine:` in `~/.zbot/config.yaml` — nothing else changes.

## Compaction Rule

Compaction logic lives in `internal/compaction/` and implements
`domain.Compactor` (`ShouldCompact`, `Compact`). It operates purely on
`domain.Message` and `domain.TokenBudget`. It is NEVER hardcoded inside an
engine adapter; engines integrate it via a thin middleware adapter (Eino:
`ChatModelAgentMiddleware` `BeforeModel` hook) that fires `PreCompact` hooks
and emits a `CompactionTriggered` event (tokens before/after, strategy).

## Downward Dependency Graph

Packages may only import downward:

```
domain                    (pure types & interfaces ONLY — zero internal imports)
  → config, registry, eventbus
    → compaction, hooks, memory, tools, wards
      → engine/*          (eino adapter lives here; only eino importer)
        → server, channels
```

`cmd/zbotd` and `cmd/zbot` wire everything; they import `server` and below,
never bypassing the graph. A new package's placement in this graph determines
what it may import.

## Testing Rule

Table-driven unit tests. In-memory SQLite (`:memory:`) for store tests. Fake
LLM/engine stubs for engine and middleware tests. **Never hit live APIs in
unit tests.**

## Other

- Domain layer (`internal/domain/`) stays pure: types and interfaces only,
  zero imports from other internal packages.
- The registry (`internal/registry/`) loads `~/.zbot/config/**` with `fsnotify`
  hot-reload; zero hardcoded agents, prompts, skills, hooks, or MCPs.
- Hook execution (`internal/hooks/`, implementing `domain.HookRunner`) follows
  the exit-code contract: 0 allow, 2 block (stderr → agent as recoverable
  error), other non-zero warn-and-continue.
