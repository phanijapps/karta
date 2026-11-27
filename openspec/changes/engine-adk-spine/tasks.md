# Tasks

## 0. Decisions (operator) — before implementation

- [x] 0.1 D1–D9 decided by the operator (2026-11-26): D1 per-agent tools,
  D2 keep one soak cycle, D3 scoped + fallback, D4 glm-5.3-flash summarizer,
  D5 low|medium|high|none (builder low), D6 plantask out, D7 selected
  model wins the root, D8 Ollama cloud-first, D9 routing as a sibling
  change. Recorded in design.md.

## 1. Slice 0 — the spine swap (behavior-preserving)

- [ ] 1.1 `engine_loop` config (`legacy` default during transition, strict
  decode) + daemon wiring. Verify: config tests; both paths construct.
- [ ] 1.2 `einotool.go`: `tools.Tool` → eino `InvokableTool` adapter
  (reusing `paramsOneOfFromJSON`); tool wrappers (`WrapInvokableToolCall`)
  persist tool rows + emit `tool_call` events + feed the tracker. Verify:
  adapter tests with the fake model; row/event parity vs legacy.
- [ ] 1.3 Middlewares: `toolplane` (`BeforeAgent`: allowlist, ward, MCP,
  delegate offering, `tools: []` semantics), `persistence`
  (`AfterModelRewriteState`: assistant rows with reasoning + per-step
  usage), `budget` (tracker, nudges, stuck kill, extension events,
  near-ceiling tool shrink), `reasoning` (`WrapModel`: usage accumulation +
  `WithReasoningEffort`). Verify: unit tests per middleware.
- [ ] 1.4 The run driver: seeding (`RootWindowFull` + summary), Runner
  construction, event iterator → durable-first timeline translation with
  attribution, terminal states (completed/failed/cancelled/step-bound
  described on the stream), the nudge re-run, `FinishExecution` usage.
  Verify: contracttest Suite green on the `adk` path (incl. delegation
  group via legacy children); characterization tests (F-23/F-24/F-26
  shape) green.
- [ ] 1.5 Phase gate: gpt-6.1-terra over the diff; fix P0/P1; record P2s.

## 2. Slice 1 — the four pay-now middlewares

- [ ] 2.1 `reduction` + ranged reads: truncation with offload Backend
  (session-scoped), `compaction.tool_result_cap`, `offset`/`limit` on the
  ward read tool. Verify: oversize results truncate head+pointer; ranged
  re-read round-trips; existing wardtool tests.
- [ ] 2.2 `skill` middleware + backend over `~/.zbot/skills/**` scoped to
  the analyst's recommended skills; `skills.injection` config; sysprompt
  stops composing bodies for engine runs (chat unchanged). Verify: skill
  listed as a tool; body loads on call; `## Skills` absent from engine-run
  prompts, present in chat.
- [ ] 2.3 `summarization` for roots AND children; `compaction.summarizer`
  override; timeline compaction events from the middleware's events.
  Verify: over-threshold runs compress; compaction visible on the
  timeline; children bounded.
- [ ] 2.4 Reasoning effort: `reasoning_effort` frontmatter (strict decode)
  → the model wrapper; verify Ollama cloud honors it on the openai-compat
  endpoint (operator task; record the finding). Verify: decode tests;
  the option reaches the wrapper.
- [ ] 2.5 Operator measurement run (solar scenario); record per-step
  numbers against §3a in this change. Phase gate: gpt-6.1-terra.

## 3. Slice 2 — delegation on the spine

- [ ] 3.1 Per-agent task tools (per D1) with the lifecycle decorator:
  child execution creation, `delegation_started/completed` (usage from the
  store, stored-status wins), deliverable receipts on fold-back, ward
  guard in descriptions. Verify: contracttest delegation group green on
  the ADK path.
- [ ] 3.2 `EmitInternalEvents` translation: child steps surface as
  delegation-kind events with child attribution; child rows persist under
  the child execution; no double persistence. Verify: delegation_cases
  tests green.
- [ ] 3.3 Depth grant in `BeforeAgent` (`max_depth` hold unchanged);
  system-role agents never offered; `require_tool_use` nudge works for
  children (re-run under the child execution). Verify: depth refusal
  invisible; analyst absent; nudge-once scenario.
- [ ] 3.4 Flip `engine_loop` default to `adk`. Phase gate:
  gpt-6.1-terra.

## 4. Slice 3 — soak, measurement, archive

- [ ] 4.1 Solar-run re-measurement; record the before/after table in this
  change; `run-quality-research.md` addendum pointing at it.
- [ ] 4.2 Docs: `engine/eino/AGENTS.md` rewritten for the spine; mapping
  table in design.md marked landed; SESSION-HANDOFF refreshed.
- [ ] 4.3 Final zbot-reviewer read-only pass; fix all P0/P1; P2s in
  findings.md; `task verify` green (dev stack stopped); `openspec
  archive -y` + scenario audit (MODIFIED deltas keep every original
  scenario).

## Constraints honored throughout

- Only `backend/internal/engine/eino/` imports eino (`task gates:backend`
  constitution check).
- No live API calls in tests — fake models throughout.
- Stage files explicitly (never `git add -A`); `backend/zbotd` never
  committed from a local build.
