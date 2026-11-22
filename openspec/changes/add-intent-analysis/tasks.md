# Tasks

## 1. Contract + router + prompts

- [ ] 1.1 `domain.IntentAnalysis` (strict decode, complexity enum,
  ward_recommendation, execution_strategy) + the S/M/L/XL→step-budget
  mapping + validation helpers (index-validated recommendations, exact
  ward-name match, reusable-domain name pattern). Verify: table tests
  (decode, unknown fields rejected, validation drop/warn rules).
- [ ] 1.2 The router (greeting/smalltalk bypass — deterministic, no
  model call) + the analysis completion call (plain JSON, no tools,
  one retry, then degrade). Verify: fake-completer tests (full
  contract, malformed JSON, model failure → typed degrade).
- [ ] 1.3 The intent prompt seeded to
  `~/.zbot/config/prompts/intent-analysis.md` (operator's version +
  Karta deltas: complexity, solution_path, mcp_tools, resource-slot
  markers); runtime validates the file's shape, falls back to the
  embedded default. Verify: seeded-file load + malformed-file fallback
  tests.

## 2. Resource index

- [ ] 2.1 Reserved-kind chitta records (resource_skill /
  resource_mcp_tool / resource_agent): upsert on registry watch diff,
  delete on removal, reconcile pass on boot. Verify: fake-system tests
  (watch event → indexed, removal → deleted, boot re-sync).
- [ ] 2.2 Semantic recall for the intent context: EmbedQuery(goal) →
  kind-filtered Recall limit ~12; listing fallback when the embedder is
  off or the index is empty. Verify: deterministic-embedder tests
  (recall hits surface the right resource; fallback path; F-30 note:
  vector discrimination fixtures in the eval harness).

## 3. Middleware + engine plumbing

- [ ] 3.1 The daemon-layer wrapper: router → analysis (resource context
  in, contract out) → inject `## Task Analysis` (with fast-path/graph
  instructions per approach) → RunGoal with the complexity step budget;
  children and chat untouched. Verify: wrapper tests against the fake
  engine (bypass, analyzed, degraded — prompt section + budget asserted).
- [ ] 3.2 Engine: `maxSteps` run input (default unchanged); the
  conformance suite gains the budget-override scenario (advisory).
  Verify: adapter tests (budget respected) + suite green.
- [ ] 3.3 Events + persistence: IntentAnalysisStarted/Complete/Bypass/
  Failed emitted and the result on the execution record. Verify: event
  tests (all four paths; rule 4 holds).

## 4. Config + UI (light)

- [ ] 4.1 Config: `intent_analysis.enabled` (default true) +
  validation. Verify: config tests (off restores exact current
  behavior).
- [ ] 4.2 UI: a Task Analysis card rendering on the run's first event
  (approach badge, complexity, recommendations, ward recommendation)
  — the Timeline already streams it. Verify: Playwright flow with a
  mocked analysis event.

## 5. Review and hardening

- [ ] 5.1 zbot-reviewer read-only over the change; fix P0/P1, P2s fix
  or record. Verify: reviewer report zero open P0/P1.
- [ ] 5.2 task verify green. Verify: exits zero.
