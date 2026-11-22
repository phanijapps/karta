# Proposal

## Why

Chitta is a fully built memory/knowledge core that no agent can reach:
`zbotd` opens it at startup, but the chat service and engine still hand
agents `tools.BuiltinProvider{}` (clock + echo only). This change binds
the seven chitta tools into the agent tool surface — the first point
where an agent actually uses chitta mid-conversation, with host-stamped
provenance from the execution context. First step of the close-the-loop
arc (engine tools → distillation → briefing).

## What Changes

- **Kernel**: `internal/tools` gains `Multi(providers ...Provider) Provider`
  — one merged tool surface (builtin + chitta), unknown names still typed.
- **Chitta binding**: `backend/chitta/engine.go` — the chitta package
  (plugin zone, importing the kernel seam downward) implements
  `tools.Provider`, exposing the seven `memory_*` / `knowledge_*` tools
  with their existing schemas as `tools.Def` entries; `Exec` funnels
  into `System.Dispatch`.
- **Host provenance from context**: the binding derives
  `ChittaHostContext` (session, execution, agent, source) from
  context values stamped by the calling layer; a supplied fallback
  function covers callers that do not stamp (anonymous agent
  provenance, never caller-supplied input).
- **Composition root**: `cmd/zbotd` passes
  `tools.Multi(Builtin, chittaBinding)` to the chat service and the
  engine — agents see nine tools (clock, echo + seven chitta).
- Emission unchanged: exactly one `ChittaToolInvoked` per call from
  `Dispatch` (engine-level `tool_call` events are the engine's own).
- Out of scope: distillation job (next change), briefing middleware,
  prompt/soul tuning for when agents should call memory tools, any new
  chitta tool, MCP provider.

## Capabilities

### New Capabilities
- `chitta-binding`: how chitta's tool surface binds into the agent
  tool plane — provider composition, provenance stamping, degradation.

### Modified Capabilities
- (none — `chitta-tools` wire contracts unchanged; the binding adds no
  tools and changes no schemas)

## Impact

- **Code:** `backend/internal/tools` (+Multi + test),
  `backend/chitta/engine.go` (binding + host-context extraction),
  `backend/cmd/zbotd/main.go` (compose), AGENTS.md notes.
- **Boundaries:** chitta imports `internal/tools` + `internal/domain`
  (downward, legal); no kernel package imports chitta; the engine
  adapter is untouched.
- **Observability:** unchanged — one event per dispatch, already tested.
