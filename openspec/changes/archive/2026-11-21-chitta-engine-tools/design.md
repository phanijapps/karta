# Design

## Context

Kernel seam: `internal/tools` defines `Tool{Def, Exec}` and
`Provider{Tools}`; chat + engine receive a Provider at construction
(`cmd/zbotd/main.go:150-153` currently passes `tools.BuiltinProvider{}`).
Chitta (plugin zone) already has `System.Dispatch(ctx, tool, args,
host)` with schemas and one-event-per-call observability. The
constitution forbids kernel→plugin imports, so the binding must be
chitta implementing the kernel interface (downward), composed at the
root.

## Goals / Non-Goals

**Goals:** agents can call all seven chitta tools; provenance comes
from the execution context (host-stamped, never arguments); merged
tool surface with unique names; degradation visible, never fatal.

**Non-Goals:** distillation (next change), briefing middleware, prompt
tuning for tool-use habits, new tools, MCP provider, engine adapter
changes.

## Decisions

### D1: Binding lives in chitta, implements the kernel interface

`backend/chitta/engine.go`:

```go
func (s *System) ToolProvider(fallback domain.ChittaHostContext) tools.Provider
```

Returns a provider whose `Tools()` maps chitta's `ToolDef` list to
`tools.Def` (name, description, schema JSON) and whose execution wraps
each tool in a `chittaTool` implementing `tools.Tool`; `Exec` decodes
argsJSON (raw pass-through to Dispatch) and returns
`tools.Result{Output | Err}`. Direction check: chitta (plugin) imports
`internal/tools` + `internal/domain` (kernel) — legal downward.

### D2: Host context from context values + configured fallback

Kernel convention: the calling layer may stamp
`chitta.HostContextKey` (a typed context key exported from the chitta
package) carrying a partial `ChittaHostContext`. The binding fills
missing fields from the fallback (default: `source: agent`,
`agent_id: "root"`, empty session — never fabricated). Tool arguments
never carry provenance (Dispatch already rejects reserved fields).

### D3: `tools.Multi` in the kernel

`internal/tools.Multi(ps ...Provider) Provider` merges Tool slices;
duplicate names panic at composition time (startup error, per spec).
Small, obviously-correct kernel addition; no chitta knowledge.

### D4: zbotd composition

```go
chittaTools := chittaSys.ToolProvider(domain.ChittaHostContext{Source: domain.SourceAgent, AgentID: "root"})
all := tools.Multi(tools.BuiltinProvider{}, chittaTools)
```

passed to both `chat.NewStoreBacked` and `einoengine.New`. Identity
stamping: the chat layer does not yet stamp session identity into
tool-execution contexts — v1 runs with the fallback; the briefing/
distillation changes (or an engine-middleware tweak) add stamping
without touching this binding (the context key is the contract).

### D5: Events unchanged

`Dispatch` already emits exactly one `ChittaToolInvoked` per call —
the binding adds no emission. Engine-level `tool_call` events are the
engine's existing behavior; no double-chitta events.

## Risks / Trade-offs

- [Agents may over/under-use memory tools] — prompt tuning is
  explicitly out of scope; observe via Timeline events first.
- [Fallback provenance (anonymous) until identity stamping lands] —
  recorded; contributions from unstamped calls carry `agent_id:
  "root"`, no session — still distinct evidence per content+time.
- [Name collision panic at startup] — deliberate: fail fast at
  composition, not mid-run.

## Migration Plan

None (additive composition; no persisted data, no contract change).

## Open Questions

- Whether the chat layer should stamp the context key in this change
  (small) or with briefing (next) — default: next, keep this change
  minimal.
