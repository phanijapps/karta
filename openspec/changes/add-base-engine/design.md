# Design

## Context

The constitution places CloudWeGo Eino behind `domain.AgentEngine`
(`internal/engine/eino/` is the only eino importer); the spike
(`docs/architecture/eino-verification.md`) verified eino v0.9.23 +
eino-ext's OpenAI-compatible model against our provider plane (custom
base URL, full tool-call round, per-request usage) and proved eino has
no history/memory — the store owns conversations. `add-simple-chat`
(in flight) delivers the store (sessions/executions/messages/
timeline_events), builtins, and the durable-first event contract. The
roadmap (research subagents, skills, MCP, memory via LadybugDB —
`docs/architecture/memory-graph-ladybugdb.md`) hangs off the engine
seam. This change builds the base layer those plug into.

## Goals / Non-Goals

**Goals:** a real AgentEngine behind the isolation rule; a ToolProvider
seam future skills/MCP/subagents implement; registry-driven agent
definitions; goal/session/SSE endpoints live server-side; hermetic
tests; zero frontend.

**Non-Goals:** delegation/subagents, skills, MCP tools, interactive
goals/resume (typed 501), compaction logic, memory/LadybugDB, streaming
chat, any goal-run UI.

## Decisions

### D1: Engine shape — `internal/engine` seams + `internal/engine/eino` adapter

`internal/engine` (framework-free) defines `ToolProvider` (List() tool
defs; Exec(ctx, name, args) → result) and run orchestration helpers;
`internal/engine/eino` implements `domain.AgentEngine` and is the sole
importer of `github.com/cloudwego/eino` + `eino-ext` (pinned to the
spike versions). The adapter constructs the chat model from the
provider plane (verified `ChatModelConfig{BaseURL, APIKey, Model}`),
maps store rows ↔ `schema.Message` (1:1 per the verification), and
emits events through the store's durable-first path. Swapping engines
remains config-only per the constitution.

### D2: Agent loop — preflight decides framework depth

Preflight task: evaluate eino v0.9.23's agent primitives (ReAct agent /
flow compositions) against the bounded-loop requirement; adopt one if it
fits the seam cleanly, else drive a thin loop over the verified
`Generate` + `model.WithTools` (the spike already demonstrated the full
mechanics). Either way the loop lives inside `engine/eino`, is bounded
(max steps per run, configurable, default ~24), uses the store's
exchange-complete `HistoryWindow` for the run's execution scope, and
persists tool intent before execution and result rows after (same
discipline as chat). Recorded in the preflight note — not a design
fork left open at apply time.

### D3: Model resolution — explicit, never invented

Precedence: request `model` (qualified `provider/model-id`, split
daemon-side) → registry agent's `model` field → refuse with typed
guidance. Mirrors the chat change's no-default stance. Credentials
resolve through the existing SecretStore seam (keyring/env); a
keyring-unavailable run fails typed before any provider call.

### D4: Agent definitions from the registry

RunGoal's `agent` selects a registry `AgentSpec` (snapshot from the
existing `internal/registry`): system prompt (literal or prompt-ref),
model override, tool allowlist (filter over ToolProvider output). No
agent named → minimal wiring prompt ("work toward the goal; tools
available") — recorded as wiring, not a hardcoded agent. Registry misses
→ typed 404-style refusal naming the configured agents.

### D5: Store integration — runs are executions

RunGoal: `EnsureSession(id?)` → create root execution (delegation=root)
→ append goal as user row → loop (assistant/tool rows in the execution)
→ terminal status + execution token rollups. Cancel: per-session cancel
context; the loop checks at safe points (before model call, before tool
exec); cancelled status, committed rows kept. All events durable-first;
the in-process registry of active runs lives in the engine adapter
(map[sessionID]cancelFunc under a mutex — single-daemon, no
cross-process coordination needed).

### D6: SSE — replay-then-live, gap-free

`GET /api/sessions/{id}/events`: stream durable events from
`timeline_events` (ordered by insertion), then subscribe to the volatile
bus filtered by session — the durable-first write order guarantees no
gap at the seam (an event is visible to late subscribers only after its
durable write; the live tail starts at the last replayed id). Heartbeat
comment every ~20s; client disconnect ends the subscription. The
contract's TimelineEvent schema is already generated. `resume` → 501
typed `not_yet`. Session list/detail/cancel read the store directly.

### D7: Hermetic tests + manual E2E

Fake `ChatModel` interface in the adapter tests (scripted completions:
plain, tool round, runaway→step-bound, slow→deadline, fail→execution
failed); fake ToolProvider alongside the builtins; in-memory store.
Server tests: goal 201/SSE replay+live via httptest recorder with
flushes; cancel mid-run. Manual E2E vs local ollama: real goal run with
a `clock.now` round observed over `curl -N` SSE, then cancel mid-run on
a slow local model.

## Risks / Trade-offs

- [eino agent primitives may not fit the seam] → preflight decides
  early; the thin-loop fallback is spike-proven — either path is small.
- [Local models exceed short deadlines] → chat/deadline policy from the
  chat change reused (configurable, generous).
- [SSE without frontend consumers] → curl-verifiable now; the research
  UI consumes it later without contract change.
- [Active-run registry is in-process] → single daemon per home is the
  deployment model; cross-process coordination is out of scope.

## Migration Plan

Additive contract (`model` field optional; resume already contracted).
No data migration — new sessions/executions only. The engine flips on
via existing `engine:` config (already validated). Rollback = revert.
