# Findings — intent-analyst-agent

Review ledger for the change. Phase gates: `zbot-reviewer` with
`gpt-6.1-terra` (backend) and `gpt-6.1-sol` (frontend); final pass:
`zbot-reviewer` (default model). Every P0/P1 is fixed before archive;
P2s are fixed or recorded here with the reason.

## Backend phase gate (gpt-6.1-terra) — round 1

| # | Sev | Finding | Resolution |
|---|---|---|---|
| 1 | P1 | The daemon installed the wrapper only when `intent_analysis.enabled` was on, so the disabled-config `analysis_bypassed` branch was unreachable and the ward orchestrator model override vanished with the kill-switch. | **Fixed**: the wrapper is ALWAYS installed (`wireIntentMiddleware`); the kill-switch only disables analysis. Test: `TestDisabledKeepsOrchestratorAndEmitsBypass`. |
| 2 | P1 | Failed decisions returned `Decision{}` — the failed event, the execution row, and the Test probe reported zero tokens/attempts even after billed attempts. | **Fixed**: every failure after initialization returns the accumulated `Decision`. Test: `TestFailedDecisionKeepsSpentUsage`. |
| 3 | P1 | "Default packet pruning occurs below the valve threshold" (20/20/20/64 caps, 512-char descriptions, 32k request bound applied before measurement). | **Disputed — no change.** The plan's own §3.4 derived-knobs table says *"catalog caps stay 20/20/20/64 + the request echo (32k chars)"* and makes the valve the only thing that shrinks BELOW them; the operator's "no pruning" decision is about not budgeting below the model's window (the pre-existing ranked/capped packet shape from intent-context-packet is retained by the plan). The 20/20/20/64 caps are the packet, not a token budget. Recorded here for the operator; if the caps should go, that is a plan change (S2 reworks the packet anyway). |
| 4 | P1 | The valve could still send an oversized prompt: a window ≤ 4k disabled it (`Budget() <= 0`), the floor path enforced a 256-char request minimum without re-measuring, and correction feedback was added after composition. | **Fixed**: a known window ≤ reserve is a typed failure before any call; a rubric that alone exceeds the budget is a typed failure; the correction block's size is reserved up front; every attempt re-measures and degrades typed rather than sending over budget. Tests: `TestValveTinyWindowTypedFailure`, `TestValveReservesCorrectionRoom`. |
| 5 | P1 | PUT accepted `deadline_seconds`/`max_attempts`/string lengths outside the contract's declared bounds; the test endpoint's 400 was not in the contract. | **Fixed**: `validateIntentSettings` enforces the schema bounds (`invalid_settings`); the contract declares 400 on `POST /api/intent-analysis/test` and documents the bounds on PUT. Test: `PUT enforces the declared bounds`. |
| 6 | P2 | An agent-named run whose request named no model analyzed on nothing while the engine resolved the agent's model. | **Fixed**: the chain's last fallback is the named agent's own `model`. Test: `TestAnalysisTargetNamedAgentModel`. |
| 7 | P2 | The returned run channel forwards the engine's first event before the terminal analysis event and never forwards `analysis_started`; the initial receive is not cancellation-aware. | **Recorded, no change in S1.** The channel is the best-effort path (callers drain it; the SSE surface reads the durable store + bus, which ARE ordered started → terminal → run steps — the spec's scenario). The empty-session started-event omission is design D4. The initial `<-events` matches the engine's contract (it closes on cancellation). Follow-up candidate for S2's Plan events (the same channel gains `plan_step_*`). |
| 8 | P2 | Every 4xx became `ErrRequestRejected` (401/429 too); `MaxTokensDropped` was not visible on the event. | **Fixed**: only a 400 is typed as the rejection; the completed event's text records "provider rejected max_tokens; ran without it". |
| — | Note | The 400 → retry-without-`max_tokens` guard departs from "output = the model's own limit". | **Deliberate, documented (design D5 + Risks).** It is not an input-pruning valve: the same attempt re-runs once without an output parameter when a provider whose output cap sits below the window refuses the request. Without it, every model on the 32k fallback with a smaller output cap (e.g. DeepSeek's 8k) would lose analysis entirely. Operator may veto; the guard is one line to remove. |
| — | Note | The reviewer could not run tests/grep (read-only session) and did not complete a line-by-line pass of the whole diff. | Compensated by the mechanical gates (`task verify`: codegen drift, lint, -race suites, budget) and the final reviewer pass. |

Pre-existing drift noticed while here (not introduced by this change):
`openspec/specs/model-catalog/spec.md` said the catalog was in-memory
only while the code persisted it since the base engine layer — the
model-catalog delta in this change now states the real behavior
(persisted, restored at startup).

## Frontend phase gate (gpt-6.1-sol) — round 1

No P0/P1. Seven P2s, all fixed:

| # | Finding | Resolution |
|---|---|---|
| 1 | The four new `workRow` cases in Chat.svelte were dead code — the Work tab renders from ContextPanel's own timeline subscription (ward transitions only). | **Fixed**: a pure `entities/timeline-event/analysisText.ts` formatter; ContextPanel renders a "Task analysis" section (started → terminal, with cost); the dead `workRow` removed. Playwright asserts the rows. |
| 2 | A second run kept showing the previous decision — no pending state; durable replay could re-apply the old terminal event. | **Fixed**: `analysisPending` from send start until THIS run's terminal event; events handled once (id set) so a replayed old terminal event cannot release the pending state. Playwright: "a second run in an analyzed session shows Analyzing…". |
| 3 | Test probed the SAVED model while the picker showed a dirty draft. | **Fixed**: Test disabled while dirty/saving; copy says "saved selection · Save changes before testing"; the result clears when the selection changes. |
| 4 | Popover Cost/Window rows interpolated missing fields and hid a real zero usage. | **Fixed**: presence-based `costText` (shared formatter); zero counts render. |
| 5 | "Inherits the run model" copy contradicted the precedence chain. | **Fixed**: badge "Uses model defaults"; the derivation names the chain and the ward override; the saved note qualifies the ward override. |
| 6 | A failed reload left a stale selection in the collapsed card beside "Daemon unreachable". | **Fixed**: the summary shows only a daemon-confirmed, non-dirty selection; Test/save gated on `connected`. |
| 7 | The four new API types were hand-written duplicates of contract schemas. | **Fixed**: `components["schemas"][...]` aliases in client.ts. |

## Final reviewer pass

Pending.
