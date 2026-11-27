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
