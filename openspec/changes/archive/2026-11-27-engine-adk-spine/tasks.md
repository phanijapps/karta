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

- [x] 2.1 `reduction`: byte cap (`compaction.tool_result_cap`, default
  12 KB) → `<persisted-output>` head + tail + path; the ORIGINAL bytes
  offloaded to a per-run `os.Root` (canonical, kernel-bounded, removed
  with the run); `karta_read_file` serves BYTE ranges confined to that
  root. `delegate_*`/`karta_skill` exempt from both phases. Verify:
  `TestADKToolResultCapOffloadsAndRereads` (a 30 KB single-line dump; the
  durable row keeps the whole dump; the offload goes away),
  `TestADKOffloadRootIsCanonicalAndConfined`, `TestClampRangeEdges`,
  `TestADKFoldBackIsNeverCapped`. (The ward `read` tool's own ranged
  params were superseded: the cap offloads every oversized result
  regardless of tool, and the reader serves all of them.)
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
- [x] 2.3b CHILD runs get in-run compaction, progressive skills and the
  effort — they run on the spine since slice 2 (`runChild` → `runAgent`).
- [x] 2.4 Reasoning effort (implementation): `reasoning_effort` on the
  agent spec (strict, per-file error; `low|medium|high|none`) rides every
  model call via `WrapModel` (roots since slice 1, children since slice
  2); builder seed `low` with a once-per-home upgrade. Verify:
  `TestAgentSpecReasoningEffort`, `TestADKReasoningEffortRidesTheCalls`,
  `TestSeedEffortUpgrade`.
- [ ] 2.5 Operator: confirm Ollama cloud honors `reasoning_effort` per
  pinned model (docs: ≥ 0.31 does; 0.34.2 installed) and record the
  completion-token drop on the builder path; then the solar measurement
  run (step-1 prompt, final context, completion split, skill loads,
  truncations, compaction events) recorded in this change.
## 3. Slice 2 — delegation on the spine

- [x] 3.1 Per-agent task tools `delegate_<agent>` `{task}` (D1) sharing
  `delegateTool.Exec`: child execution creation, `delegation_started/
  completed` (usage from the store, stored-status wins), deliverable
  receipts on fold-back, ward guard in descriptions; the dispatch name
  stays callable as an alias. Verify: `TestPerAgentDelegationToolParamsExposed`,
  `TestContractSuiteADK` delegation group, the legacy delegation tests
  (now running on the spine): depth refusal invisible, unknown target
  typed, system agent refused, receipts, fold-back.
- [x] 3.2 Children run as their own spine agents (`runChild`): steps
  surface as delegation-kind events with child attribution; rows persist
  under the child execution (the legacy delegation tests on the spine:
  `TestDelegateAndFoldBack`, `TestToolFreeFirstAnswerNudgedOnce` — the
  nudge continuation under the child execution). No `EmitInternalEvents`
  needed (recorded in design.md).
- [x] 3.3 Depth grant where the inventory is assembled (hold unchanged);
  system-role agents never offered; the nudge works for children.
  Verify: `TestDelegationDepthGrantAndCap`, `TestContractSuiteADK/
  Delegation/DepthRefused`, the nudge test above.
- [x] 3.4 `engine_loop` default → `adk` (config + `Default` + `New`);
  `TestContractSuite` runs the legacy path explicitly. UI: the
  `delegate_` prefix recognized (`delegation.ts`; svelte-check + build +
  the full e2e suite 109 green incl. the new `delegation-tools.spec.ts`).
  **Live**: a real research run on the dev stack through the browser
  (`tests/_live/karta-live.mjs`) — per-agent tool, child on the spine,
  lifecycle + receipts, attributed frames, pills/slideout/Work tab
  (findings.md "Live verification").

## 4. Slice 3 — soak, measurement, archive

- [x] 4.1 Measurement record: the live run's numbers (64.2k prompt / 930
  completion / 17 s / 1 delegation; per-execution usage) in findings.md
  "Live verification"; the re-measurement protocol and the lever table in
  `run-quality-research.md` §8. The SOLAR re-measurement itself is the
  operator's (2.5) — the signoff records whether the archive waits for it.
- [x] 4.2 Docs: `engine/eino/AGENTS.md` (the spine, slice by slice),
  `internal/compaction/AGENTS.md`, `backend/AGENTS.md` Compaction Rule,
  intent/registry/config/frontend AGENTS.md; design.md mapping rows
  marked LANDED; `docs/SESSION-HANDOFF.md` refreshed for the next session.
- [x] 4.3 Final zbot-reviewer pass (G8): P1 F2 fixed with a test; P2s
  F4–F6 fixed; F1/F3 are operator decisions (signoff.md §6). `task verify`
  exit 0 on the final tree (log `/tmp/task-verify-final.log`, 140 s:
  10 steps, 29 backend packages under -race, LadybugDB suite, 109 e2e,
  0 FAIL, no drift). Screenshots:
  `/tmp/karta-signoff-{1-thread-work,2-slideout,3-context,4-settings-agents}.png`.
- [ ] 4.4 Archive (`openspec archive -y` + the scenario audit) — after the
  operator's signoff (signoff.md).
