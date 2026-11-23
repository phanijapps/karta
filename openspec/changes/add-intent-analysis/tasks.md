# Tasks

## 1. Contract + router + prompts

- [x] 1.1 `domain.IntentAnalysis` (strict decode, complexity enum,
  ward_recommendation, execution_strategy) + the S/M/L/XL→step-budget
  mapping + validation helpers (index-validated recommendations, exact
  ward-name match, reusable-domain name pattern). Verify: table tests
  (decode, unknown fields rejected, validation drop/warn rules).
- [x] 1.2 The router (greeting/smalltalk bypass — deterministic, no
  model call) + the analysis completion call (plain JSON, no tools,
  one retry, then degrade). Verify: fake-completer tests (full
  contract, malformed JSON, model failure → typed degrade).
- [x] 1.3 The intent prompt seeded to
  `~/.zbot/config/prompts/intent-analysis.md` (operator's version with
  the design's adaptation table applied: Karta tool names
  (delegate_agent, no load_skill — skills are runtime provisions),
  complexity + solution_path + mcp_tools restored/added,
  subdirectory/structure/graph fields dropped, no 'coding' skill —
  graph runs recommend a builder/orchestrator agent, resource lists as
  template slots {{.Skills}}/{{.Agents}}/{{.Wards}}); runtime
  validates the file's shape, falls back to the embedded default.
  Verify: seeded-file load + malformed-file fallback tests.

- [x] 1.4 `system-prompt` capability (specs/system-prompt delta):
  config surface (`system_prompt.role/soul/os/tools/custom`, os=auto by
  GOOS), strict typed resolution (missing role/unresolvable section =
  load error naming file+section; optional missing = skipped),
  back-compat (absent key = defaults), persona seeds (role, soul,
  os-linux/windows/darwin, tools) write-once under the roster marker.
  Verify: config tests (surface, back-compat, typed errors) + seed
  tests (fresh/seeds, edit survives) + assembly table tests (order,
  auto-OS pick, optional skip).

## 2. Resource index

- [x] 2.1 Reserved-kind chitta records (resource_skill /
  resource_mcp_tool / resource_agent): Sync reconciles skills+agents to
  the snapshot (new/changed/tombstoned-on-removal, revived-on-return;
  MCP tools reconciled only by IndexTools); watch/boot WIRING is task
  3.1's daemon work. Verify: OpenInMemory tests — lifecycle incl.
  pagination beyond the History clamp (>10 records), batches beyond
  the 64-observation cap, tombstone no-op re-sync, revival, tool
  lifecycle (changed/reduced/empty probe), registry Sync preserving
  tools, agent-tool invisibility + write rejection (chitta side).
- [x] 2.2 Semantic recall for the intent context: EmbedQuery(goal) →
  kind-filtered Recall limit ~12; listing fallback when the embedder is
  off or the index is empty. Verify: deterministic-embedder tests
  (recall hits surface the right resource; fallback path; F-30 note:
  vector discrimination fixtures in the eval harness).

## 3. Middleware + engine plumbing

- [x] 3.1 The daemon-layer wrapper: router → analysis (resource context
  in, contract out) → inject `## Task Analysis` (with fast-path/graph
  instructions per approach) → RunGoal with the complexity step budget;
  children and chat untouched. Verify: wrapper tests against the fake
  engine (bypass, analyzed, degraded — prompt section + budget asserted).
- [x] 3.2 Engine: `maxSteps` run input (default unchanged); the
  conformance suite gains the budget-override scenario (advisory).
  Verify: adapter tests (budget respected) + suite green.
- [x] 3.3 Events + persistence: IntentAnalysisStarted/Complete/Bypass/
  Failed emitted and the result on the execution record. Verify: event
  tests (all four paths; rule 4 holds).

- [x] 3.4 Composition wired into both surfaces: chat prepends the
  composed static sections as the system message (no dynamic sections
  in chat this change); the goal wrapper composes static + `## Context`
  (chitta recall: memory + knowledge graph, fused) + `## Task Analysis`
  + `## Skills` (validated bodies inlined; no load_skill tool — the
  drop-and-warn path emits events). Verify: chat tests assert the
  system message; wrapper tests assert section order + budget.
## 4. Config + UI (light)

- [x] 4.1 Config: `intent_analysis.enabled` (default true) +
  optional `model` ("provider/model"; empty = the run's model) +
  validation. Verify: config tests (off restores exact current
  behavior — the wrapper tests assert the disabled path).
- [x] 4.2 UI DISPOSITION: the Task Analysis card lands with the
  Timeline Tree surface (the Context panel's Timeline tab is a
  placeholder — no run-event UI exists to host a card). The CARD's
  data pipe is proven server-side instead: the intent payload
  (complexity, approach, ward, drops) survives durable replay AND live
  SSE (TestIntentAnalysisPayloadStreams). Verify: SSE payload test
  green; card tracked for the Timeline UI change.

## 5. Review and hardening

- [ ] 5.1 zbot-reviewer read-only over the change; fix P0/P1, P2s fix
  or record. Verify: reviewer report zero open P0/P1.
- [ ] 5.2 task verify green. Verify: exits zero.
