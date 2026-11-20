# Proposal

## Why

Karta has providers, a model catalog, a persistent chat store in flight,
and a constitutionally-defined `domain.AgentEngine` seam — but no engine.
Everything the roadmap promises (research subagents, skills, MCP tools,
memory) hangs off that seam. This change builds the **base engine
layer**: the first real `AgentEngine` implementation behind the eino
isolation rule, with the tool-provider and agent-definition seams that
research/skills/MCP later plug into.

## What Changes

- **`internal/engine/eino/` (new) — the only eino importer.** Implements
  `domain.AgentEngine`: `RunGoal` starts a goal run as a store-backed
  session + root execution, drives a bounded agent loop (eino
  OpenAI-compatible chat model — spike-verified — with tool calling over
  the compaction-aware history window), streams `TimelineEvent`s as it
  goes (durable-first), and completes the execution; `Cancel` stops a
  run; `Resume` remains a typed not-yet (its waiting-for-input semantics
  arrive with interactive goals). Loop choice (eino's agent primitives
  vs a thin loop over the verified `Generate`) is a preflight task —
  either stays inside this package.
- **Tool provider seam (the foundation piece).** `internal/engine`
  defines `ToolProvider` (assemble tool infos + execute by name); this
  change wires exactly one provider: the builtins (`clock.now`, `echo`)
  from the chat change. Skills, MCP servers, and subagent tools
  implement the same seam later — no engine changes needed to add them.
- **Agent definitions come from the registry.** RunGoal's `agent` field
  selects a `config/agents` spec (system prompt, optional model
  override, tool allowlist). With no agent named and none configured, a
  minimal wiring prompt is used (recorded: wiring, not a hardcoded
  agent). Model precedence: request > agent spec > refuse with guidance.
- **Contract goes live (server-side only).** `POST /api/goals` (additive
  optional qualified `model` on GoalRequest) creates the run; the session
  list/detail/cancel endpoints answer from the store; and
  `GET /api/sessions/{id}/events` becomes a real SSE stream — replay
  from durable `timeline_events`, then live from the bus. `resume`
  answers 501 with a typed not-yet body.
- **No frontend in this change.** Goal runs are exercised via
  contract/tests/curl; the research UI arrives with the subagents change.
- **Docs.** Package AGENTS.md files; engine-loop preflight record;
  constitution untouched (eino isolation already encoded).

**Out of scope (recorded):** subagent delegation, skills, MCP tools,
interactive/waiting goals and resume, compaction logic, memory/ladybugdb
(`docs/architecture/memory-graph-ladybugdb.md` records that future),
frontend surfaces, streaming chat.

**Dependencies:** applies after `add-simple-chat` (store, executions,
timeline events, builtins, chat-completion patterns).

## Capabilities

### New Capabilities

- `agent-engine`: The base engine layer — AgentEngine semantics over the
  store (goal runs as executions, bounded tool loop, event streaming),
  the tool-provider seam, registry agent definitions, and cancellation.

### Modified Capabilities

- `api-contract`: Goal/session endpoints leave the 501 era — create with
  optional model, list/detail/cancel real, session events as SSE
  (replay-then-live); `resume` typed not-yet.
- `domain-model`: `GoalRequest` gains the optional qualified model
  (additive); no seam break — engines that ignore it keep compiling.

## Impact

- **Backend:** new `internal/engine` (seams + tool providers) and
  `internal/engine/eino` (adapter, sole eino/eino-ext importer);
  `internal/store` gains session/execution run support;
  `internal/server` implements goal/session/SSE handlers; `cmd/zbotd`
  wiring. go.mod: eino + eino-ext pinned (spike versions).
- **Contract & codegen:** additive `model` field + SSE behavior; TS
  client regenerates.
- **Docs:** AGENTS.md (engine, engine/eino), engine-loop preflight
  record, product-context note that goal runs are contract-real.
- **Verification:** hermetic (fake chat model + in-memory store);
  manual E2E vs local ollama (goal run with a tool round, SSE observed
  via curl, cancel mid-run).
