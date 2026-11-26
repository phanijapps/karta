# Proposal

## Why

A Playwright + axe review of the live INR session (pre-attribution
transcript, two delegations issued in ONE root step) found defects in
the freshly shipped delegation surfaces: the thread split mid-run (a
child's report landed between a delegate intent and its result, so the
next child context started a new turn), the 26 KB research report was
tagged "The answer", a delegation pill showed "running" forever, the
delegation cards printed the agent twice, the task text (starting with
the ward preamble) was shown as a clipped one-liner everywhere, "100.0k"
tokens, the Work tab's scroll region was not keyboard-focusable (axe),
and the Context tab's Agents list printed a 3.4 KB task as a
description.

## What Changes

- Turn building tracks OPEN delegations: while a delegate result is
  pending, a user row is the child's context (absorbed) and a tool-free
  assistant row is a report — never an answer and never a turn break.
- Delegation cards name the agent once (status in its own live region);
  task gists drop the ward preamble and clamp to one line with the full
  text on demand (slideout, Work tab step labels, Context tab agents).
- `fmtTokens` drops a trailing `.0`; the Work tabpanel is focusable.

## Capabilities

None — defect fixes within the shipped specs (`skip_specs: true`).

## Impact

`frontend/src/features/chat-stream/{Chat.svelte,steps.ts,DelegationSlideout.svelte,WorkSteps.svelte}`,
`frontend/src/app/ContextPanel.svelte`,
`frontend/src/entities/timeline-event/analysisText.ts`, Playwright.
