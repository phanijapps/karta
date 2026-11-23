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

## Operator refinement (2026-11-24): composable system prompt

The chat/research system prompt is COMPOSED from markdown section
files in the prompt registry (`~/.zbot/config/prompts/`, existing
kind — hot-reloaded, API-listed). config.yaml selects them:

```yaml
system_prompt:
  role:   role          # REQUIRED — identity, capabilities, bounds
  soul:   soul          # optional — voice, principles
  os:     auto          # auto|linux|windows|darwin|none → os-<name>.md
  tools:  tools         # optional — tool-use discipline
  custom: [house-rules] # optional, ORDERED extras
```

Assembly order: Role, Soul, OS, Tools, Custom — then DYNAMIC sections
appended by machinery at run time, per surface: `## Context` (chitta
recall — chat and goal runs), `## Task Analysis` (this change's
middleware — GOAL RUNS ONLY: simple chat, the single-session
conversation, never goes through intent analysis), `## Skills` (the
provisioned skills — intent-recommended, index-validated bodies
inlined into context at run start; no load_skill tool: the runtime
provides, the agent does not fetch — goal runs only), `## Active Ward`
(slice 3: the ward's OWN constitution — its AGENTS.md + layout
template; the "ward = mini bot with its own constitution" model). `AGENTS.md` is
 deliberately NOT a static section: it is the ward-level constitution
and composes only when a run is hosted. One template serves chat and
research; surface differences ride the dynamic sections. Failure
semantics: missing role or unresolvable section = typed load error
naming file+section; missing optional = skipped. Seeded from the
operator's set: SOUL.md→soul, OS.md→os packs, tooling-skills.md→tools.

## Operator refinement 2 (2026-11-24): the intent-analysis prompt

Checked against the operator's actual file
(`~/Documents/zbot/config/intent-analysis-prompt.md`). Kept verbatim
in spirit: the analyzer identity (no tools, no browsing, no prose,
runtime-supplied schema, flat JSON no envelope), skills-vs-agents
never-mixed discipline, ward naming rules (reusable domain categories,
GOOD/BAD examples), exact-match against the Existing Wards list, and
the simple/graph approach gating with its anti-pattern callout.
Adaptations when seeding `config/prompts/intent-analysis.md`:

| Operator field/rule | Karta v1 disposition |
|---|---|
| `subdirectory`, `structure` in ward_recommendation | DROP from the contract — creation-shape details belong to slice 3's `ward_create` args; v1 keeps `{action, ward_name, reason}` |
| `execution_strategy.graph` (executable graph object, "prefer null") | DROP — Karta's engine is a bounded loop, no planner builds graphs in v1; `approach` + `explanation` stay |
| `complexity`, `solution_path` | RESTORED from agentzero's contract (this change's step budgets 4/8/16/24 + plan seeding) — the operator's prompt dropped them |
| `mcp_tools` | ADDED (this change's MCP-plane recommendations) |
| "Skills = load_skill()", "Agents = delegate_to_agent()" | NO load_skill tool in Karta: the runtime PROVIDES the right skills — intent-recommended, index-validated — by injecting their bodies into the run's context before the loop starts (a dynamic `## Skills` section; eino ships `adk/middlewares/skill` as prior art, but Karta provisions proactively instead of an agent-initiated load call). Agents remain `delegate_agent` — skills are context provisions, agents are delegations; still never mixed |
| "ALWAYS include 'coding' in recommended_skills" (ward structure + task runner) | NO such skill in Karta — v1 rule: graph runs must recommend a builder/orchestrator agent (the seeded builder-agent); the flow-skill form returns with slice 4 |
| "Relevant Skills/Agents" + "Existing Wards" lists | Template slots ({{.Skills}}/{{.Agents}}/{{.Wards}}) — the registry already parses prompts as Go templates, so the seeded file is hot-editable and the runtime injects index hits; Existing Wards = slice 1's Vault.Index (name, archetype, canonical title as scope) |

The composable chat/research prompt (previous refinement) is a
SEPARATE surface: analysis runs on this dedicated prompt alone — no
role/soul/os/tools composition, exactly like the operator's file.

## Open Questions

- Surface the analysis in the UI (a Task Analysis card on the run's
  first event?) — leaning yes, cheap, high trust value; decide in
  tasks.
- ~~Whether chat ALSO gets analysis~~ RESOLVED (operator, 2026-11-24):
  simple chat (the single-session conversation) does NOT go through
  intent analysis — analysis is goal-run/research-mode only. Chat keeps
  the light composable prompt (static sections + Context); no Task
  Analysis, no provisioned Skills section there. If chat ever grows
  research behavior, that traffic belongs in goal runs.
- MCP tool recall granularity: per-tool records vs per-server records
  (per-tool matches the prober output; per-server halves the index
  size). Leaning per-tool; measure.
