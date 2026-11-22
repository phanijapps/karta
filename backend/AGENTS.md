# backend/ — Go Constitution

Scope rules for all Go work in `backend/` (daemon `zbotd` + Bubbletea CLI
`zbot`), on top of the root constitution. Target stack: Go 1.26+,
`net/http` + `chi` + SSE; `zbot.db` (SQLite) for sessions/Timeline; chitta (LadybugDB: memory + knowledge graph) at `~/.zbot/data/chitta/`; local ONNX embeddings.

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

The tree has two zones (the add-chitta change made this explicit):

- **Kernel** — everything under `backend/internal/`: domain seams,
  config, registry, eventbus, store, server, mcpclient, chat, tools,
  skillhub. Kernel packages never import the plugin zone; the
  composition root (`cmd/zbotd`) wires plugins.
- **Plugin zone** — capability packages directly under `backend/`, e.g.
  `backend/chitta` (memory + knowledge graph over its own LadybugDB at
  `~/.zbot/data/chitta/`). A plugin imports kernel seams downward and
  nothing imports it back. Relocating `internal/engine` into the zone
  (as `backend/engine`) is a planned follow-up; until then the eino
  adapter stays at `internal/engine/eino`.

```
domain                    (pure types & interfaces ONLY — zero internal imports)
  → config, registry, eventbus
    → compaction, hooks, tools, wards
      → server, channels

plugin zone (outside internal/):
chitta → domain, config, eventbus, tools  (kernel never imports chitta)
distiller → domain, chitta (public surface) + kernel seams via cmd adapters
cmd/* → server + plugins                   (composition root)

Zone rule extension (add-chitta-distillation): a plugin-zone composition
component (distiller) may import other plugins' public surfaces and
kernel seams; nothing imports it but cmd. Kernel stays clean.
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
