# Tasks

## 0. Decisions (operator) — before implementation

- [x] 0.1 D1–D9 decided by the operator (2026-11-26): D1 per-agent tools,
  D2 keep one soak cycle, D3 scoped + fallback, D4 glm-5.3-flash summarizer,
  D5 low|medium|high|none (builder low), D6 plantask out, D7 selected
  model wins the root, D8 Ollama cloud-first, D9 routing as a sibling
  change. Recorded in design.md.

## 1. Slice 0 — the spine swap (behavior-preserving)

- [x] 1.1 `engine_loop` config (`legacy` default during transition, strict
  decode) + daemon wiring (`SetLoop`). Verify: `TestEngineLoop`,
  `TestSetLoopRejectsUnknown`.
- [x] 1.2 `einotool.go`: `tools.Tool` → eino `InvokableTool` adapter
  (reusing `paramsOneOfFromJSON`); the tool wrapper (`WrapInvokableToolCall`)
  persists tool rows + emits `tool_call` events + feeds the tracker;
  hallucinated/suppressed/tool-less calls refused as typed text, recorded
  and emitted like executed calls; `placeholderTool` keeps tool-less runs
  on the ReAct path. Verify: `TestADKToolRoundParity`,
  `TestADKWrapperRefusesSuppressedCalls`, `TestADKStepBoundReached`.
- [x] 1.3 Middlewares: tool plane at agent construction (`offeredTools`),
  `mw_persistence.go` (assistant intent rows with reasoning + per-step
  usage), `mw_budget.go` (tracker, transient nudges, stuck kill,
  extension events, near-ceiling tool shrink), `mw_usage.go` (`WrapModel`:
  usage accumulation). Verify: unit + parity tests above.
- [x] 1.4 The run driver (`adkrun.go`): seeding (`RootWindowFull` +
  summary), Runner construction, event iterator → durable-first timeline
  translation with attribution, terminal states (completed/failed/
  cancelled/stuck/step-bound described on the stream), the nudge
  continuation run, `FinishExecution` usage. Verify: `TestContractSuiteADK`
  green (incl. the delegation group via legacy children);
  `TestADKRootNudgeOnce`, `TestADKStuckKill`; legacy suite unchanged.
- [x] 1.5 Phase gate: gpt-6.1-terra over the diff (findings.md G1):
  four P1s fixed with tests (F1 late answer after cancel, F2 continuation
  budget, F3 refusal call IDs, F4 live registry/inventory); P2s F5–F7
  fixed (nudges only while tools are offered, the nudge rides one call,
  barrier-driven cancel test). The test for F6 caught the first fix
  stripping the wrong message (input-relative index vs. the state's
  instruction prefix) — matched by identity now. Second pass (G2): the
  remaining P1s (a cancel falling through the nudge branch; execution
  bound to the compiled tool objects) fixed — every call executes against
  the live plane; two more bugs found by the new tests (a tool-less START
  was sticky for the run; `tool_call` durable rows minted a second id) —
  both fixed.

## 2. Slice 1 — the four pay-now middlewares

- [x] 2.1 `reduction` + ranged reads: byte cap (`compaction.tool_result_cap`,
  default 12 KB) → `<persisted-output>` head + tail + path; offload to a
  per-run temp dir (lines wrapped at 1000 bytes); `read_file` (offset/
  limit) restricted to that root. `delegate_agent`/`skill` exempt. Verify:
  `TestADKToolResultCapOffloadsAndRereads` (a 30 KB single-line dump;
  durable row keeps the whole dump; offload removed with the run),
  `TestADKReadFileRefusesPathsOutsideOffload`. (The ward `read` tool's
  own ranged params were superseded: the cap offloads every oversized
  result regardless of tool, and the reader serves all of them.)
- [x] 2.2 `skill` middleware over the registry's provisioned skills,
  scoped to `GoalRequest.Skills` (the wrapper sets it from the analysis
  when `skills.injection: progressive` on the ADK loop; empty = all);
  foreign-tool passthrough. Verify: `TestADKSkillToolLoadsOnDemand`
  (scoped description, body on call, out-of-scope = typed tool error,
  observable), `TestWrapperProgressiveSkills` (names not bodies; chat and
  legacy unchanged).
- [x] 2.3 In-run compaction for ROOT runs through the kernel policy —
  `internal/compaction` behind `domain.Compactor` (gate G4 F1: the
  constitution's Compaction Rule, not Eino's summarization middleware),
  bridged by `mw_compaction.go`; budget `max_context_tokens − reserve`;
  summarizer = `compaction.summarizer` (cheap, D4) or the run's model;
  PreCompact hooks honored; each pass → one durable `compaction_triggered`
  event with before/after stats. Verify: `compaction` package tests,
  `TestADKInRunCompactionEmitsCompaction`, `TestADKInRunCompactionBlockedByHook`.
- [ ] 2.3b CHILD runs get in-run compaction, progressive skills and the
  effort when they move to the spine (slice 2) — children still run the
  legacy loop today (gate G4 F5: recorded, not claimed).
- [x] 2.4 Reasoning effort: `reasoning_effort` on the agent spec (strict,
  per-file error; `low|medium|high|none`) rides every ROOT call via
  `WrapModel` (children: slice 2); builder seed `low` with a
  once-per-home upgrade. Verify: `TestAgentSpecReasoningEffort`,
  `TestADKReasoningEffortRidesTheCalls`, `TestSeedEffortUpgrade`.
  **Operator task remains**: confirm Ollama cloud honors `reasoning_effort`
  per pinned model (docs say ≥ 0.31 does; 0.34.2 installed) and record the
  completion-token drop on the builder path.
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
