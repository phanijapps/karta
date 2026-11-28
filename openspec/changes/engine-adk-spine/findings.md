# Findings — engine-adk-spine

Reviewers are read-only (diffs written to `/tmp/*.diff`). Severity per the
playbook: P0/P1 fixed before the next phase; P2 fixed or recorded here.

## G1 — slice 0 phase gate (zbot-reviewer on gpt-6.1-terra, 2026-11-27)

Diff: `/tmp/engine-adk-spine-slice0.diff` (20 files, +1378/−192). Verdict
on the first pass: FAIL (four P1). All fixed; re-gated below.

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| F1 | P1 | A successful agent run was accepted as completion without checking `ctx.Err()` — a model answer landing after Cancel would be recorded as success (legacy checks right after Generate). | **Fixed**: the driver routes through `failRun` when `ctx.Err() != nil` after a successful run; `TestADKCancelDuringModelCallNeverCompletes` (barrier fake that answers successfully after Cancel → status cancelled, no assistant row, cancellation on the record). |
| F2 | P1 | The nudge continuation created a fresh agent with the full `MaxIterations` — a nudged run could make one call more than the legacy ceiling. | **Fixed**: `MaxIterations = steps − calls`; `TestADKNudgedRunSharesTheCeiling` (5 calls total with `MaxSteps: 1`, ends at the step bound). |
| F3 | P1 | Refusal rows paired with the LAST call ID of a tool name — two hallucinated calls to one name in a response mispaired (replay-unsafe transcript). | **Fixed**: per-name FIFO of call IDs (`runState.pendingID`; sequential execution makes the order deterministic); `TestADKRefusalsPairWithTheirOwnCallID` (two same-name calls, each row pairs with its own ID). |
| F4 | P1 | Inventory and delegate snapshot frozen at run start; the legacy loop rebuilt them per step (a mid-run registry hot-reload or ward entry was ignored; a dropped agent stayed invocable). | **Fixed for existing tools/agents**: `delegateTool.Exec` re-reads the registry; the model-facing inventory (incl. the delegate description's ward guard) is re-derived every call. `TestADKDelegateToolReadsTheLiveRegistry`, `TestADKInventoryFollowsTheRegistry`. **Recorded delta**: a tool that appears mid-run (a newly probed MCP server) waits for the next run on the ADK path — the executable node is compiled at run start. Legacy picked it up on the next step (accidental rather than designed). |
| F5 | P2 | `tracker.Nudge` ran even when tools were suppressed; legacy only inside its tools-offered branch — `Nudge` flips the warning flags `ShouldStop` reads, so a stuck run could die at a different step near the ceiling. | **Fixed**: nudges only while tools are offered; asserted in `TestADKStepBoundReached`. |
| F6 | P2 | The continuation run retained the `require_tool_use` nudge in state for every later call; the legacy root sent it on the immediately following call only. | **Fixed** — and the test caught the first fix stripping the WRONG message (an input-relative index vs. the state's instruction prefix): matched by content now. `TestADKNudgeRidesOneCallOnARoot`. |
| F7 | P2 | The suite's cancellation case cancels immediately (no proof of cancel-mid-model-call on ADK); the parity test counted durable events only after completion. | **Fixed** (the half that matters): the barrier-driven cancel test above. Persist-before-stream is by construction (the state hooks run inside the model call, before the event reaches the driver) — not separately instrumented. |
| — | drift | design.md described final detection via `AfterAgent` and all assistant rows via the middleware. | **Fixed**: mapping rows now say the driver persists final/nudge rows; `AfterAgent` unused (never fires on error termination). |

## G2 — slice 0 re-gate (zbot-reviewer on gpt-6.1-terra, second pass)

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R2-1 | P1 | Cancellation arriving between the post-run `ctx.Err()` check and the nudge condition's own `ctx.Err() == nil` fell through to completion. | **Fixed**: the nudge condition no longer consults the context; cancellation is handled only by the explicit check (the remaining window — cancel between that check and the row append — is the legacy loop's own posture). |
| R2-2 | P1 | Execution stayed bound to the compiled tool objects: a removed MCP tool stayed invocable, a same-name replacement advertised its new schema while running the old code. | **Fixed** (and the G1 "mid-run additions wait for the next run" delta CLOSED): every call executes against the live plane (`persistenceMW.execute` → `execOffered` over a fresh inventory; unknown names route through the same round). `TestADKExecutionFollowsTheLiveInventory` (removed / replaced / added). |
| R2-3 | P2 | Content-matching could strip a USER message equal to the nudge text. | **Fixed**: removed by pointer identity (`runState.stripMsg`). |
| R2-4 | P2 | Persist-before-stream was argued by construction, not observed. | **Fixed**: `TestADKEventsAreDurableBeforePublication` checks, at bus delivery time, that every event's durable row exists. |
| T1 | P1 (test-found) | A run that STARTED with an empty plane kept `noTools` for the whole run — tools appearing later were never advertised (legacy: per step). | **Fixed**: "no tools" is a per-call fact of the live inventory; the placeholder only serves the start-empty case. Covered by the "added" case above. |
| T2 | P2 (test-found, pre-existing on BOTH paths) | `emitToolCall` minted one id for the durable `tool_call` row and another for the published/streamed event — an SSE tool_call could not be correlated with the stored event. | **Fixed** in the shared helper (the legacy path benefits too): one identity per event. |

## G3 — slice 0 third pass (zbot-reviewer on gpt-6.1-terra): PASS-WITH-FIXES

R2-1..R2-4 and T1/T2 verified. One P2, **recorded**:

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R3-1 | P2 | `TestADKEventsAreDurableBeforePublication` checks the store when the consumer READS a buffered event, not at the publication boundary — a wrong ordering could pass if the append landed between publish and the read. | **Recorded, not fixed**: a sound proof needs a test-only publication hook in production code (`Adapter` takes the concrete `*eventbus.Bus`). The ordering is by construction — `AppendEvent` precedes `Publish` in both emit paths (`runsetup.go` emit, `runhelpers.go` emitToolCall) — and the test still catches a gross violation (publish with no row at receipt). |

## G4 — slice 1 phase gate (zbot-reviewer on gpt-6.1-terra, first pass: FAIL → fixed)

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| F1 | P1 | In-run summarization used Eino's middleware directly in the adapter — a compaction policy hardcoded where the backend constitution's Compaction Rule forbids it. | **Fixed structurally**: new kernel package `internal/compaction` implementing `domain.Compactor` (`Service`: instruction kept, old messages summarized, tail verbatim + token-bounded, never starting on a tool result, previous summaries folded, PreCompact hooks honored); the adapter has only the thin bridge `mw_compaction.go`; the daemon wires the summarizer model (`compaction.summarizer` → D4's cheap model, else the run's own). `backend/AGENTS.md` Compaction Rule updated to say "including a framework's own summarization middleware". |
| F2 | P1 | Offloads were line-wrapped, so a ranged read could not reproduce the original bytes. | **Fixed**: the original bytes are stored unchanged; `karta_read_file` serves BYTE ranges (offset/limit, rune-boundary clamped) with a continuation hint. Tested on a 30 KB single-line dump (exact bytes 10000–10030). |
| F3 | P1 | Reader/writer confinement was lexical only (symlinks, traversal, TOCTOU). | **Fixed**: root resolved via `EvalSymlinks`; reads and writes open with `O_NOFOLLOW`; writes reject paths outside the root. `TestADKReadFileIsConfinedToTheOffloadRoot` (escapes, a planted symlink, traversal, a write outside). |
| F4 | P1 | Engine extras (`read_file`, `skill`) ignored `tools: []`. | **Fixed**: an explicit no-tools inventory gets neither. `TestADKNoSkillToolWithoutAnalysisOrWithNoTools`. |
| F5 | P1 | Tasks claimed child compaction/effort while children still run the legacy loop. | **Recorded**: tasks 2.3b split out as open (slice 2); 2.3/2.4 say ROOT. |
| F6 | P1 | Reduction's clear phase could clear fold-back receipts and loaded skills. | **Fixed**: `ClearExcludeTools` mirrors the truncation exemptions. |
| F7 | P2 | The effort seam was a package global reassigned by a parallel test (a race). | **Fixed**: the option is the adapter's `effortOpt` field; tests substitute on their own adapter. |
| F8 | P2 | Summarizer usage not booked on the execution; the summarizer cache key omitted BaseURL. | **Moot/recorded**: the summarizer now goes through the kernel `Completer` (the daemon's `llmCaller`, no per-call usage seam) — not booked; recorded as a follow-up for the run-ownership change. No engine-side summarizer cache exists anymore. |
| F9 | P2 | `reasoning_effort: none` was forced on the summarizer regardless of provider support. | **Fixed**: not forced; D5's `none` on the summarizer waits for the per-model verification (task 2.4) and would be the policy's knob. |
| F10 | P2 | `skill`/`read_file` were not reserved against user/MCP names. | **Fixed**: `karta_skill` / `karta_read_file`; the `karta_` prefix documented as reserved. |
| F11 | P2 | A malformed `compaction.summarizer` passed config loading. | **Fixed**: validated as `provider/model-id` at load (`TestContextBudgetKnobs`). |
| F12 | P2 | Spec drift: skills scenario scope, cap exemptions, registry `none`. | **Fixed** in the three deltas; a new scenario pins "in-run compaction is the kernel policy". |
| F13 | P2 | Compaction token counts were labeled as the middleware's trigger count. | **Fixed**: the event text says "chars/4 estimate"; the stats come from the policy's own counter. |
| T3 | P1 (test-found) | With the token bound, a tail could shrink to a lone tool RESULT (orphaned from its intent — a provider error). | **Fixed** in the policy: the cut moves back to the exchange start; `TestCompactNeverOrphansToolResults`. |
| T4 | P2 (test-found) | Successive passes stacked summary system messages. | **Fixed**: previous summaries fold into the next (`IsInstruction`); `TestCompactFoldsPreviousSummary`. |

Decisions surfaced by the gate (operator): analysis ABSENT/bypassed now yields NO
skill tool (nil `GoalRequest.Skills`), while an analysis that recommends none
offers every provisioned skill (D3's fallback) — implemented that way;
`compaction.summarizer` ships EMPTY (the run's own model) because a model id
cannot be a cross-install default — set it in config.yaml for D4.

## G5 — slice 1 re-gate (zbot-reviewer on gpt-6.1-terra, second pass)

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R2-1 | P1 | The production `Service` has no `Hooks`: configured `PreCompact` hooks never run. | **Recorded, not fixable here**: the daemon has NO `domain.HookRunner` implementation yet (only manifest validation in `registry/hooks.go`; `PreToolUse`/`PostToolUse` are equally unwired). The policy accepts a runner and honors block/error (`TestCompactHooks`, `TestADKInRunCompactionBlockedByHook`); wiring lands with the hook runner (follow-up: hook-runner). |
| R2-2 | P1 | `withinRoot` compared a resolved root with an unresolved path — a temp root under a symlinked ancestor (macOS /tmp) would break every offload. | **Fixed**: `newOffloadDir` returns the CANONICAL root (symlinks resolved) used for the middleware, the writer and the reader; `TestADKOffloadRootIsCanonicalAndDirSymlinksRefused`. |
| R2-3 | P2 | The `karta_` reservation was documented, not enforced. | **Fixed**: user-plane tools with the prefix are dropped at the tool-plane boundary and the drop is reported once on the stream; `TestADKReservedToolNamesAreNotOffered`. |
| R2-4 | P2 | A limit shorter than the next rune could yield an empty range forever; offset+limit could overflow. | **Fixed**: the range always advances by at least one decoded rune; the end is computed bounded by the content; negative offsets normalize to 0; `TestClampRangeEdges`. |
| R2-5 | P2 | The tail bounds were documented as absolute. | **Fixed (docs)**: both are targets, best-effort — the cut never orphans a tool result, so one oversized final exchange can exceed them; `TestCompactKeepsAnOversizedFinalExchange` (which also caught the halving loop not running when the tail covered everything). |
| R2-6 | P2 | The fallback test passed if the skill tool was absent. | **Fixed**: asserts the tool was found. |

## G6 — slice 1 third pass (zbot-reviewer on gpt-6.1-terra)

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| R3-1 | P1 | Confinement was still a path-NAME check after `open` (a directory swap could win); the writer followed symlinked parents. | **Fixed with `os.Root`**: the run's offload dir is created canonical and held as an `*os.Root`; writer and reader go through `Root.MkdirAll/OpenFile/Open`, which can only reach files BENEATH the root (kernel-bounded on Linux; symlinks out of the tree refused). No path-name races remain by construction. `TestADKOffloadRootIsCanonicalAndConfined` (escapes, the root itself, a symlinked directory inside, traversal writes, the legitimate path). |
| R3-2 | P2 | `rawInventory` duplicated the assembly (drift risk) and omitted the delegate tool. | **Fixed**: `inventory()` assembles ONCE and returns kept + dropped; `offeredTools` wraps it. |
| R3-3 | P2 | The reserved-name test asserted the tool's OUTPUT, not its name. | **Fixed**: asserts `karta_skill` is absent from the advertised user inventory. |
| R3-4 | P2 | `backend/AGENTS.md` claimed `internal/hooks/` implements `domain.HookRunner`. | **Fixed**: the entry now states the runner is not implemented (seams exist; nothing executes hooks) — consistent with G5 R2-1. |

## G7 — slice 2 phase gates (terra: backend; sol: frontend)

| ID | Sev | Finding | Disposition |
|---|---|---|---|
| T-F1 | P1 (terra) | Per-agent wire names were not collision-free (`a.b` / `a_b` → one name; an agent named `agent` silently dropped). | **Fixed**: colliding agents get no tool of their own, are reported once on the stream and stay reachable through the dispatch alias (`inventory` returns `collided`); `TestADKDelegationToolNamesCollideSafely`. |
| T-F2 | P1 (terra) | Reduction exempted only `delegate_agent`; per-agent fold-backs (receipts at their END) could be capped or cleared. | **Fixed**: the exemption list is derived from the plane (every `delegate_*` + the alias + the skill tool); `TestADKFoldBackIsNeverCapped` (a 28 KB child answer reaches the parent whole). |
| T-F3 | P2 (terra) | A hallucinated `delegate_<unknown>` fell to the generic refusal (the spec promises "lists configured agents"). | **Fixed**: the refusal names the delegation tools offered; `TestADKHallucinatedDelegationToolListsOffered`. |
| T-F4 | P2 (terra) | tasks.md lost 2.4/2.5 in a slice rewrite. | **Fixed**: 2.4 (implementation) checked; 2.5 (operator verification + measurement) restored as open. |
| S-F1 | P1 (sol) | The Work panel closed delegations only on `delegate_agent` results — after a per-agent result, later user turns were absorbed into the old turn. | **Fixed**: `ContextPanel` uses `isDelegationTool`; pinned by the new e2e spec (two Work turns). |
| S-F2 | P2 (sol) | The Context tab's Agents roster keyed on `delegate_agent` + `args.agent`. | **Fixed**: derives both shapes (`delegationTarget`); pinned. |
| S-F3 | P2 (sol) | `toAgent` matched the step's representative tool, not each call — a row with two delegations nested only the first agent's rows. | **Fixed**: per-call matching; pinned (the builder's rows nest under the shared step). |
| S-F4 | P2 (sol) | Suffix extraction would mis-map names the engine rewrites. | **Moot by construction**: the engine offers no tool for a colliding name (T-F1), so every offered suffix is the registry name verbatim; documented in `delegation.ts` and frontend/AGENTS.md. |
| S-F5 | P2 (sol) | WorkSteps' expanded details kept two legacy-only branches (raw task JSON; duplicated report text). | **Fixed**: both branches use the helpers. |
| S-F6 | P2 (sol) | No e2e fixture exercised a per-agent call. | **Fixed**: `tests/delegation-tools.spec.ts` — two per-agent calls in one root row (pills, slideout, Work nesting + turn boundary, Context roster); the full suite is 109 green. |
