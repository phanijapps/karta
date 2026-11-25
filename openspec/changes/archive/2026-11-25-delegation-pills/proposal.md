# Proposal

## Why

The thread's sub-agent reports render as collapsed cards only when the
transcript rows carry attribution — the operator's existing sessions
(pre-attribution) show nothing, and a card is not what they asked for:
"a simple pill, clickable, that slides out to show me that sub-agent's
calls".

## What Changes

- Every delegation in a turn renders as a **pill** in the thread
  (`👥 <agent>`, the task as its tooltip) — derived from the root's
  `delegate_agent` intents, so legacy sessions get them too.
- Clicking a pill opens a **slideout** (dialog drawer): the agent and
  task, its cost when known, the agent's calls (the same friendly step
  tree the Work tab renders) and the report it returned. Escape and the
  close button return focus to the pill.
- The collapsed report cards are replaced by the pills (the report is
  in the slideout).

## Capabilities

### Modified Capabilities

- `chat-modes`: sub-agent reports surface as delegation pills + a
  slideout instead of collapsed cards.

## Impact

`frontend/src/features/chat-stream/{Chat.svelte,steps.ts}`, a new
`DelegationSlideout.svelte`, Playwright.
