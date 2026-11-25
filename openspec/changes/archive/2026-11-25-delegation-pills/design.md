# Design

## Context

Report cards need attributed rows; the operator wants a pill per
delegation that opens the agent's calls. The `delegate_agent` intent
(args: agent, task) and its tool result (the report) exist in every
transcript; the child's rows sit between them (adjacency) or carry the
child's execution id (attribution).

## Goals / Non-Goals

**Goals** — pills from intents (legacy-proof); a slideout with the
agent's calls and report. **Non-Goals** — no backend change.

## Decisions

- **D1** `delegationGroups(rows)` (steps.ts) derives one group per
  `delegate_agent` call: `{callId, agent, task, executionId, rows,
  report}` — the child's rows are those between the intent and the
  call's result (legacy) or carrying the children's execution id
  (attributed); the report is the result row's content.
- **D2** The slideout is a fixed drawer with a backdrop, `role="dialog"`,
  labelled with the agent and task; focus moves in on open and returns
  to the pill on close; Escape closes. Cost comes from the session's
  executions (matched by execution id when known).
- **D3** The step tree reuses `WorkSteps` over the group's rows.

## Risks / Trade-offs

- Adjacency grouping for legacy rows can over-capture if two delegations
  interleave in a legacy transcript — acceptable; attributed rows group
  by execution.
