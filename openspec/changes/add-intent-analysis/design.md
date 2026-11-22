# Design

## Context

Slice 1 (`add-ward-vault`, proposal ready) provides the ward index this
analysis recommends into. Karta has: the registry (skills/agents/MCPs
with hot-reload), the MCP prober (per-server tool discovery), chitta's
local ONNX embedder seam (EmbedQuery/EmbedPassages, proven in
recalleval), and the engine's delegation. The operator's tuned prompt
set at `~/Documents/zbot/config/` (intent-analysis-prompt.md,
first-turn-protocol.md, INSTRUCTIONS.md, OS.md) is the quality bar —
its ward-naming discipline, skills-vs-agents separation, and
simple/graph gating are adopted; agentzero's implementation (intent
middleware in gateway-execution) is the architecture reference.

## Goals / Non-Goals

**Goals:** every root goal run classified cheaply (one small-model
call); recommendations that are real (index-validated) and discoverable
at scale (semantic recall); complexity-driven budgets; approach gating;
full observability; non-fatal degradation everywhere.

**Non-Goals:** ward hosting/tools (slice 3); the graph/planner pipeline
(slice 4's skills); chat-surface analysis (later — same mechanism,
gated by whether it pays for itself); auto-loading recommended
resources (recommendations are advisory; slice 3 wires loading).

## Decisions

1. **Daemon-layer middleware, not engine-core.** The analysis runs in
   the daemon's goal path BEFORE RunGoal (a wrapper around the engine
   call), not inside the adapter's loop. The engine receives only
   (a) an extra system-prompt section and (b) a step-budget parameter.
   Rationale: the engine stays framework-agnostic machinery; analysis
   is an orchestration concern; conformance suite unchanged (agents
   don't need to know analysis exists). Alternative — engine
   middleware chain (agentzero's shape) — rejected: heavier seam, and
   Karta's engine seam is deliberately thin.
2. **The intent agent is a plain completion, not a tool loop.** One
   small-model call (the configured provider's cheap model or the same
   model with a low budget), flat JSON out, no tools — exactly the
   operator's prompt discipline. Parse failures retry once, then
   degrade (run proceeds, event says parse_failed). Structured-output
   modes are NOT used (agentzero's finding: they break on some
   providers); plain-text JSON + strict decode.
3. **Prompt as a config-tree file** (`~/.zbot/config/prompts/intent-
   analysis.md`), user-editable, seeded from the operator's version
   with Karta deltas (ward naming rules kept; complexity +
   solution_path restored from agentzero's contract; mcp_tools added).
   The runtime injects the resource hits + ward index summary into the
   prompt's resource slots. OS.md-style platform info is NOT needed
   (analysis never executes anything).
4. **Resource index = chitta reserved kinds.** Records of kind
   `resource_skill` / `resource_mcp_tool` / `resource_agent` (content
   = "name — description"; subject = the source registry id) written
   through the trusted-host seam on registry watch events; recall by
   EmbedQuery(goal) filtered to resource kinds, limit ~12. Listing
   fallback when the embedder is off (cold cache) or the index is
   empty. Deletions remove the record (the watcher knows the diff).
   F-30 caveat recorded: vector discrimination must be re-verified for
   this corpus in the eval harness; the listing fallback caps the
   damage if recall is weak.
5. **Contract is a domain type with strict validation.**
   `domain.IntentAnalysis{...}` — unknown fields rejected at decode;
   recommendations validated against the live index (drop + warn);
   ward names validated against the ward index (use_existing requires
   exact match; create_new names must satisfy the reusable-domain
   pattern — the prompt carries the rule, validation enforces the
   shape, not the semantics).
6. **Step budget plumbed as a RunGoal input.** The engine's runInput
   gains `maxSteps int` (default MaxSteps=24 unchanged); the daemon
   wrapper passes the complexity mapping. Conformance stays valid
   (budget override is a scenario, not an obligation).
7. **Fast-path + graph instructions ride in the injected section**
   (the operator's first-turn-protocol, trimmed to what exists in
   Karta today: approach=simple → "answer directly, no delegation
   needed"; approach=graph → the delegation hint "consider
   delegate_agent and ward hosting (when available)"). Full graph
   first-actions arrive with slice 3/4 — the prompt file evolves, no
   code change.

## Risks / Trade-offs

- [Analysis latency on every goal run] — small model, one call,
  parallel with EnsureSession (the run's session boot overlaps);
  bypass router kills the trivial case entirely. Measured budget: p95
  ≤ 2s; a config kill-switch (`intent_analysis.enabled: false`) ships
  day one.
- [Recall quality unknown for this corpus (F-30)] — listing fallback;
  the eval harness re-verifies with resource-corpus fixtures before
  the semantic path becomes default-on.
- [Prompt drift vs the config file] — the seeded file is user-owned;
  version-skew tolerance: the runtime validates the file's contract
  section exists, degrades to the embedded default on malformed.
- [Index staleness] — registry watcher events drive upserts; a
  reconcile-on-boot pass re-syncs (the agentzero lesson: never trust
  incremental-only state).

## Migration Plan

Additive: no analysis today → analysis (or its absence) tomorrow. The
kill-switch config defaults ON; flip to OFF restores today's behavior
exactly. No contract break (the analysis rides events + the execution
record's existing payload surface).

## Open Questions

- Surface the analysis in the UI (a Task Analysis card on the run's
  first event?) — leaning yes, cheap, high trust value; decide in
  tasks.
- Whether chat ALSO gets analysis (the mechanism is surface-agnostic)
  — recorded as follow-up knob, off by default v1.
- MCP tool recall granularity: per-tool records vs per-server records
  (per-tool matches the prober output; per-server halves the index
  size). Leaning per-tool; measure.
