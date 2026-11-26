# Proposal

## Why

The INR run's research agent produced an 8k-token fabricated report
with zero tool calls: it narrated reads and searches it never made,
regurgitated the ward packet as "evidence", and the engine accepted the
first tool-free completion as its answer (`docs/architecture/
run-quality-research.md` §2). Children also had no MCP plane, so the
one delegation meant to research could not search. Operator decisions:
sub-agents share the root's tool plane (MCP included); nested
delegation stays off behind a config knob; a first-answer nudge ships
opt-in per agent; the honesty clause upgrades unmodified seed copies.

## What Changes

- **Engine**: children get the MCP plane (the root's tool plane; the
  agent's `tools:` allowlist still applies). A run whose agent sets
  `require_tool_use: true` that answers without tools on its first step
  gets ONE bounded nudge before the answer is accepted. The delegate
  tool's fold-back appends host-verified **deliverable receipts** for
  the paths the task named (✓ on disk / ✗ missing). Assistant rows
  persist per-step prompt/completion usage.
- **Delegation depth is a config knob**: `delegation.max_depth`
  (default 1 — the root delegates, children never), replacing the
  constant cap.
- **Agent spec**: optional `require_tool_use` (strict frontmatter
  decode extended).
- **Seeds**: research-agent and builder-agent gain `require_tool_use:
  true` and an honesty contract ("never describe a step you did not
  take; every claim cites a path you read or a command you ran this
  run; report gaps, do not infer"); existing unmodified copies upgrade
  once under their own marker.

## Capabilities

### Modified Capabilities

- `engine-delegation`: shared tool plane; the first-answer nudge;
  deliverable receipts; the configurable depth cap.
- `agent-engine`: per-step usage on assistant rows.
- `config-registry`: `require_tool_use` on the agent spec.
- `config-plane`: the honesty upgrade of unmodified research/builder
  seeds.

## Impact

Contract: none (no API change). Backend: config, registry, engine
adapter + delegation, config seeds; `cmd/zbotd` wiring; tests.
