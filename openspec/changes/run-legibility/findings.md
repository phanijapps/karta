# Findings — run-legibility

## Backend phase gate (gpt-6.1-terra)

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P1 | Analyst events and ward events carried no agent/execution — "every frame" was not true. | **Fixed**: analysis events carry `intent-analyst` + the analyst execution (a bypass: root, the root execution id); ward transitions are attributed through the per-session `runAttr` map set at every run's start; both durable payloads carry `agent`. Tests: `TestWrapperEmitsStartedThenTerminal` (attribution), bypass attribution, `TestSSEFramesAttributed`. |
| 2 | P1 | The lifecycle emitter claimed durable-first but published on append failure. | **Recorded + spec corrected**: the engine's posture is ATTEMPTED durable-first (F-15, pinned by characterization tests, owned by backend-engine-run-ownership); the delta text now says so instead of overpromising. |
| 3 | P1 | Root executions showed agent `default` (schema default); named roots lost their name. | **Fixed**: `SetExecutionAgent` at run start; children write their agent on creation; legacy `default` maps to `root` in the state. Test: `TestSessionState`. |
| 4 | P2 | A cancelled child read `failed`. | **Fixed**: the stored terminal status wins; `failed` only when no reliable status exists. |
| 5 | P2 | State query errors were swallowed; the list was unbounded. | **Fixed**: typed 500; `LIMIT 256` (the contract's maxItems). |
| 6 | P2 | Coverage gaps (ordering, nonzero usage, restart, independent frames, two children). | **Fixed**: lifecycle ordering + usage assertions (the fake now reports usage), reasoning + agent read back after a real reopen, per-frame decoding in the SSE test, two children in the state test. Cancellation lifecycle: recorded — the conformance suite covers cancellation propagation; a lifecycle-status cancellation test would need a deterministic cancel in the fake (follow-up). |

## Frontend phase gate (gpt-6.1-sol)

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P1 | Step nesting still depended on order (a late child step after a second delegation attached to the wrong one; a root step could nest). | **Fixed**: attributed child steps bind to their delegation by EXECUTION (nearest unclaimed delegation to that agent, the binding sticks); root-attributed steps never nest; legacy rows keep adjacency; child context rows absorb into the turn by attribution. Playwright: "interleaved children nest under their own delegation by execution". |
| 2 | P1 | Executions could regress to an older state response (assigned before the generation check). | **Fixed**: a separate `stateGeneration` guards the cost read (live analysis still guards the analysis). Playwright: "a stale state response never overwrites newer run totals" (fails without the guard). |
| 3 | P2 | The stored duplicate report was still readable as the delegate tool result in the Work tab. | **Fixed**: a reference line replaces the duplicate text (errors still show) — the approved stored-but-hidden decision. |
| 4 | P2 | Delegation cards lacked the tool mix; the footer lacked a per-execution breakdown (both in the delta). | **Fixed**: tool mix per child execution from attributed rows; a collapsible per-execution cost list under the footer. |
| 5 | P2 | Live narration still rendered under "Thinking". | **Fixed**: labelled Narration; Thinking is reasoning only. |
| 6 | P2 | Status changes on delegation cards had no announcement semantics. | **Fixed**: decorative pulse `aria-hidden`; a polite `role="status"` per card announcing "agent: status" once on change. |
| 7 | P2 | Disclosure tests could pass with the reasoning hidden. | **Fixed**: the report's nested Thinking and the final Thinking are opened and asserted visible; `14m 0s` → `14m` (zero parts drop). |

## Final reviewer pass

Pending.
