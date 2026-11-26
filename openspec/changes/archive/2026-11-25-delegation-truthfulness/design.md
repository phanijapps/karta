# Design

## Context

`adapter.go:352` mounts MCP tools on root runs only; `:389` accepts the
first tool-free completion as the final answer; the delegate tool folds
the child's text back verbatim; `DefaultDepthCap` is a constant (2);
every seed grants `max_depth: 0`, so nesting is off only by convention.
Usage is tracked per execution, not per step.

## Goals / Non-Goals

**Goals** — children share the root's plane; one nudge against
narrated tool-use (opt-in); deliverable receipts on fold-back; the
depth cap as a knob defaulting to 1; per-step usage. **Non-Goals** —
context budget (Q2b/S3), nested delegation itself.

## Decisions

- **D1 Shared plane**: drop the root-only guard on MCP; the agent's
  `tools:` allowlist keeps filtering the whole plane as today.
- **D2 The nudge**: `runInput.requireToolUse` (from the spec) — when
  step 0 returns no tool calls, the answer is kept in the conversation
  with a user-message nudge appended (the progress tracker's nudge
  channel), one time, then any answer is final. Emitted as an agent-step
  event ("engine: … answered without tools — nudged once").
- **D3 Receipts**: the delegate tool extracts path-like tokens from the
  task (`dir/name.ext`), stats each under the active ward (no ward → no
  receipts), and appends "Deliverables check (host): path ✓ / ✗ not on
  disk" to the tool result only when the task named any.
- **D4 Depth knob**: `delegation.max_depth` (config, default 1, ≥ 1) →
  `SetDepthCap`; the per-agent `max_depth` grant still applies within it.
- **D5 Per-step usage**: `out.ResponseMeta.Usage` → the assistant row's
  prompt/completion columns (already on the schema; the chat path fills
  them the same way).
- **D6 Seeds**: `require_tool_use: true` + the honesty clause on
  research-agent and builder-agent; the upgrade embeds the pre-honesty
  bodies and replaces byte-identical copies once under
  `.agent-seeds-honesty`.

## Risks / Trade-offs

- The nudge costs one completion for a genuinely tool-free task on an
  opted-in agent — acceptable; the operator opts agents in.
- Receipts only cover paths the task names; a child that invents
  unnamed deliverables is caught only by the root's judgement (as today).
- MCP for children means more tool schemas per child step (~1–2k
  tokens) — the context-budget slice trims schemas per role.
