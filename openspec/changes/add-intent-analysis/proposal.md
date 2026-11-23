# Proposal

## Why

Goal runs start blind: the model sees the goal and its tools but not
WHAT the task is, WHICH resources exist, or HOW hard it is. The
operator's agentzero deployment proves the value (its tuned prompt set
at `~/Documents/zbot/config/` is the reference bar): a cheap
pre-execution step that classifies the task, recommends the right
slice of the resource index, gates the execution approach, and — with
wards — picks the domain workspace. Slice 2 of ward-based research
(backlog Phase 2.5); delivers value on its own: every goal run gets a
Task Analysis, complexity-based step budgets, and MCP/skill/agent
recommendations.

## What Changes

- **Pre-execution middleware** for ROOT goal runs only (children and
  chat stay lean): a router (trivial bypass — greetings/smalltalk skip
  analysis entirely), then an **intent agent** — one small-model call
  with the flat JSON contract, no tools during analysis.
- **The contract** (typed domain type): primary_intent, hidden_intents,
  solution_path (seeds the run's plan), complexity S/M/L/XL → step
  budget (S=4, M=8, L=16, XL=24 engine steps), recommended
  skills/agents/mcp_tools (from the indexed resources ONLY — hallucinated
  names are dropped with a warning event), ward_recommendation
  {action: use_existing|create_new, name, archetype, reason} (name must
  match the ward index exactly; the reusable-domain naming rule rides in
  the prompt), execution_strategy {approach: simple|graph}, explanation.
- **Semantic resource index**: skills (name+description), MCP tools
  (prober output per enabled server), agents (description) embedded as
  chitta records of a reserved kind via the existing ONNX seam;
  recalled by EmbedQuery of the user goal (listing fallback for small
  rosters / cold cache). Index refreshes on registry watch events.
- **Prompt injection**: the analysis renders as a `## Task Analysis`
  section in the run's system prompt (root only) — the model follows
  recommendations; fast-path (approach=simple) instructions ride along.
- **Observability**: IntentAnalysisStarted/Complete events (streamed +
  persisted); analysis result on the execution record; failures are
  non-fatal (the run proceeds without analysis, event says why).
- **Ward recommendation is advisory in this slice**: the field is
  analyzed and emitted, but ward hosting (slice 3) consumes it. When
  slice 1 is absent (no vault), the field is still produced (create_new
  with the default archetype) so the contract is stable.
- NOT in scope: ward tools/sticky ward (slice 3), spec→plan→tasks skills
  (slice 4), the graph/planner pipeline (slice 4's skills own it — this
  slice only gates approach).

## Capabilities

### New Capabilities

- `intent-analysis`: pre-execution classification of goal runs — the
  JSON contract, resource recommendations from the semantic index,
  complexity budgets, approach gating, events, non-fatal degradation.
- `system-prompt`: the composed system prompt — static registry
  sections (role/soul/os/tools/custom, os=auto per platform) +
  per-surface dynamic sections (Task Analysis, Skills, Context),
  runtime skill provisioning (no load_skill tool), typed load
  failures, seeded default persona. Serves chat (static-only this
  change; its Context recall is a recorded follow-up) and goal runs
  (full stack).

### Modified Capabilities

- `agent-engine`: root runs carry a Task Analysis prompt section and a
  complexity-derived step budget (conformance: analysis is present,
  absent, or failed — all valid states).
- `chitta` (or new `resource-index` capability if the shape diverges):
  reserved-kind records for skills/MCP tools/agents.

## Impact

`internal/domain` (contract type + events), `internal/engine/eino`
(middleware hook in RunGoal root path, prompt section, step budget),
`internal/chitta` or a thin `internal/resourceindex` on the embedder
seam, `internal/registry` (watch → index refresh), `contracts/openapi.yaml`
(analysis surfaced on the execution/session read if cheap), codegen.
Prompts seeded as config-tree files (user-editable; the operator's
`~/Documents/zbot/config/` set as the starting point). Follow-up
awareness: engine-plane run-ownership items untouched.
