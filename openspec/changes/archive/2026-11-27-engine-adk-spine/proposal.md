# Proposal

## Why

The engine hand-rolls its ReAct loop (`backend/internal/engine/eino/adapter.go`
— tool offering, progress tracker, budget/extensions, cancellation, per-step
usage, all bespoke) while eino v0.9.23 already ships the ADK:
`ChatModelAgentMiddleware` hooks, the `adk.Runner` event stream, and built-in
middlewares (summarization, skill, reduction, plantask, filesystem,
dynamictool, agentsmd, patchtoolcalls). Every open cost lever measured in the
solar run (`docs/architecture/run-quality-research.md` §3a — 13.6k fixed
overhead/step, ~350k of re-sent shell dumps, 80% of completion tokens being
reasoning) is a middleware on that spine. Hand-rolling replacements for each
one duplicates what the framework now ships and keeps the engine off the ADK
path (the original adapter design in the `eino-engine-adapter` skill intended
`adk.Runner` from the start).

## What Changes

- **The engine's run path moves onto the ADK spine**: every run (root and
  delegated child) executes as an `adk.ChatModelAgent` under an `adk.Runner`;
  our concerns become `ChatModelAgentMiddleware` implementations and tool
  wrappers. The `domain.AgentEngine` seam is unchanged; the
  `internal/engine/contracttest` suite is the swap's gate.
- **Revertible**: the legacy hand-rolled loop stays selectable behind
  `engine_loop: legacy` for one cycle (default `adk`); both paths must pass
  the full conformance suite. The legacy path is deleted in a follow-up
  change after soak.
- **The four pay-now middlewares** (the measured levers):
  - `reduction` — tool-result caps by size with offload + a pointer
    (head + "re-read with a range"), plus `offset`/`limit` on the ward read
    tool.
  - `skill` — progressive disclosure: skills become a tool (names +
    descriptions in the tool description; the SKILL.md body loads on demand)
    instead of 18.8 KB of injected bodies per step; scoped to the analyst's
    recommended skills.
  - `summarization` — in-run compaction for roots AND children (children
    today have none), with timeline-visible compaction activity.
  - `WrapModel` — per-agent reasoning effort (Ollama `reasoning_effort`)
    and per-step usage accumulation.
- **Delegation moves to agent-as-tool**: each delegatable registry agent is
  offered as a task tool (child = its own ChatModelAgent); the depth grant
  (`delegation.max_depth`, default 1 — the hold stays) is decided in
  `BeforeAgent`, so depth refusal stays invisible by construction. Delegation
  lifecycle events, deliverable receipts, and the first-answer nudge
  (`require_tool_use`) are preserved verbatim as tool wrappers / run-driver
  behavior.
- **Config**: `engine_loop`, `skills.injection` (progressive | bodies),
  `compaction.summarizer` (optional model override) and
  `compaction.tool_result_cap`, agent frontmatter `reasoning_effort`.
- **Out of scope**: `plantask`/plan events (S2 `intent-contract-v2` owns
  them), the filesystem middleware (not ward-aware; our ward tools stay),
  `agentsmd` (the ward packet stays in the instruction this change), chat /
  quick-chat paths (untouched), any contract change (none — no API/SSE
  change; frontend is not touched).

## Capabilities

### Modified Capabilities

- `agent-engine`: runs execute on the ADK spine behind the engine seam; the
  loop is the framework's ReAct loop driven by our middlewares; the run path
  is selectable; context budget levers (caps, progressive skills, in-run
  compaction, reasoning effort) become engine requirements.
- `engine-delegation`: the delegate surface becomes per-agent task tools on
  the spine; depth-grant, receipts, shared tool plane, and nudge semantics
  are unchanged; child runs get in-run compaction.
- `config-plane`: new `engine_loop`, `skills.injection`,
  `compaction.summarizer` knobs (strict decode).
- `config-registry`: `reasoning_effort` on the agent spec.

## Impact

Contract: none (no HTTP/SSE change; `GoalRequest.SystemPrompt` composition is
daemon-internal). Backend: `internal/engine/eino/` (the bulk — new run
driver, middlewares, tool adapters, skill backend), `internal/wardtools`
(ranged reads), `internal/sysprompt` + `internal/intent` (skills seam),
`internal/config`, seeds unchanged, `cmd/zbotd` wiring. Frontend: none.
Verification: `task test:backend`, `task lint:backend`, `task gates:backend`,
`task budget:backend`, `task verify`; measurement against the solar run's
per-step numbers recorded in the change before archive.
