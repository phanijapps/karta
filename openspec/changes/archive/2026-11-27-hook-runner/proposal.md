# Proposal

## Why

Hooks are declared end to end but never run: `~/.zbot/hooks/*.yaml` manifests
load and validate (`registry/hooks.go`: six lifecycle events, a compiling
`matcher`, a required `command`, `timeout_ms`), `domain.HookRunner` and the
`hook_decision` timeline kind exist in the domain and the contract, the
compaction policy accepts a runner — and `backend/AGENTS.md` states the truth:
nothing executes hooks. Every seam built since (the spine's tool wrapper, the
compaction policy, the delegation fold-back) is waiting on this one missing
piece. The final gate of `engine-adk-spine` recorded it (G5 R2-1, G8 F3) as
the follow-up.

## What Changes

- **New kernel package `internal/hooks`** implementing `domain.HookRunner`
  by the constitution's exit-code contract: the hook's `command` runs
  through `sh -c` with the `HookEvent` as JSON on stdin; exit 0 = allow,
  exit 2 = block (stderr is the reason, surfaced to the agent as a
  recoverable error), any other non-zero = warn-and-continue; a timeout
  (the manifest's `timeout_ms`, default 10 s, max 60 s) is a warn. Output
  is bounded; decisions from several matching hooks combine block > warn >
  allow with per-hook reasons. The `matcher` applies to the event's subject
  — the tool name for Pre/PostToolUse, the agent name for SubagentStop —
  and is ignored for subject-less events.
- **Engine seams** (`internal/engine/eino`, behind `SetHookRunner`):
  - PreToolUse before every tool call: block = not executed, the model sees
    `error: blocked by hook: <reason>`; PostToolUse after: block = the
    model sees the reason, the durable row keeps the real output.
  - SessionStart at the root run's start and SessionEnd at every terminal
    (status in the payload) — advisory (block reads as warn).
  - SubagentStop when a delegated child returns — advisory: block/warn
    append the reason to the fold-back so the root reads it.
  - PreCompact through the compaction policy (already honored; the daemon
    now wires the runner).
- **Observable**: every decision is a durable-first `hook_decision` event
  with the decision payload (`context.hooks` names the hooks that ran),
  attributed like the run's other events; a child's decisions surface in
  the parent's stream as delegation-kind events.
- **Docs**: `backend/AGENTS.md` hook rule moves from "not implemented" to
  the contract as built; `internal/hooks/AGENTS.md`; engine AGENTS.md.
- **Out of scope**: rendering hook decisions in the UI (the kind is already
  subscribed; a Work-tab line is a follow-up), new hook events, and any
  contract change (none — the `HookDecision` schema is used as is).

## Capabilities

### Added Capabilities
- `hooks`: the runner's exit-code contract, matching, timeouts, combining,
  and observability.

### Modified Capabilities
- `agent-engine`: tool calls pass through Pre/PostToolUse; the root run's
  edges emit SessionStart/SessionEnd (advisory).
- `engine-delegation`: SubagentStop is advisory on the fold-back.
- `config-registry`: the matcher's subject rule (one scenario added; every
  original scenario kept).

## Impact

Contract: none (`hook_decision` and `HookDecision` exist). Backend:
`internal/hooks` (new), `internal/engine/eino` (tool wrapper, run edges,
fold-back, emission), `internal/compaction` (unchanged; wired),
`cmd/zbotd` (wiring), `backend/AGENTS.md`. Frontend: none. Verification:
`task verify`; a live check with a `~/.zbot/hooks/*.yaml` manifest that
blocks one tool.
