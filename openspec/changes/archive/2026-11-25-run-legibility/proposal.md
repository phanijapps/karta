# Proposal

## Why

The INR research run (review: `docs/architecture/run-legibility-plan.md`)
delegated three times and succeeded, but the surface could not show its
shape: child events lose their agent on the durable row and the SSE
frame, `ChatMessage` carries no agent/execution so the thread renders a
26 KB sub-agent report exactly like the final answer, the engine never
persists the model's reasoning, a delegation has no events of its own,
and the run's cost (≈1.3M prompt tokens) lives only in the `executions`
table. The operator: "I don't know which is thought, sub-agent response
and final response."

## What Changes

- **Attribution on the wire (L1)**: every timeline row and SSE frame
  carries `agent` ("root" or the sub-agent's name) and `execution_id`;
  `ChatMessage` gains `agent` + `execution_id`. Thread rule: the root's
  assistant rows without tool calls are answers; any other agent's are
  **sub-agent reports** — collapsed cards (agent · size), never the
  answer style. Legacy rows are attributed through the executions join.
- **Reasoning + roles (L2)**: the engine persists `reasoning_content` on
  assistant rows (the chat path already does); the answer block shows
  the model's real **Thinking** (collapsed) and the last **Narration**
  before it; the Work tab's step detail shows per-step reasoning.
- **Delegation + cost events (L3)**: `delegation_started{agent, task,
  execution_id}` / `delegation_completed{agent, execution_id, tokens,
  duration, status}` as first-class Timeline events; `SessionState`
  carries the session's executions (agent, role, status, tokens,
  duration) so the Work tab shows one card per delegation and a run
  footer (tokens · delegations · time).
- The Work tab labels every step with its agent.

## Capabilities

### Modified Capabilities

- `agent-engine`: durable rows are attributed; research-run assistant
  rows persist reasoning; delegation lifecycle events.
- `engine-delegation`: child work is attributed on the wire and a
  delegation has started/completed events with cost.
- `simple-chat`: reasoning capture covers engine runs too.
- `chat-modes`: the research thread separates reports from the answer;
  the context panel shows delegations and the run cost.
- `api-contract`: SSE frames carry `agent`/`execution_id`; transcript
  rows carry `agent`/`execution_id`; session state carries executions;
  two new event kinds.

## Impact

Contract (additive, `CONTRACT_CHANGE=1`): `TimelineEvent.execution_id`,
`ChatMessage.agent/execution_id`, `SessionState.executions[]`,
`DelegationPayload` cost fields, kinds `delegation_started/completed`.
Backend: store (`messages.agent` migration, `ExecutionUsage`), engine
adapter (attribution, reasoning, delegation events), server (SSE
frames, replay join, DTO, state). Frontend: timeline store, thread
cards + answer block, Work tab attribution + delegation cards + footer.
Mockup: `docs/architecture/run-legibility-mockup.html`. The Plan row of
the mockup lands with S2 (plan events).
