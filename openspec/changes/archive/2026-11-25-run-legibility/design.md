# Design

## Context

The engine attributes child events on the live bus only (`Agent`, kind
`delegation`), persists `{text}` / `{tool, output}` payloads without the
agent, and the SSE writer drops `Agent`. `ChatMessage` has no
agent/execution. `messages.reasoning` is written by the chat path only.
`DelegationPayload` has child_agent/task but no execution or cost. The
operator's decisions (mockup approved): report cards in the thread,
per-step collapsed Thinking, the root's last assistant row is the answer,
the duplicate report stays stored and is hidden in the UI.

## Goals / Non-Goals

**Goals** — attributed events and rows end to end; reports vs answers;
real reasoning; delegation lifecycle events with cost; a run footer.
**Non-Goals** — S2's contract v2 / Plan events (the mockup's Plan row),
MCP scoping, streaming research runs, transcript dedupe.

## Decisions

- **D1 Attribution rule.** `agent` is the ACTING agent name: `"root"`
  for root runs (named root runs use the agent's name), the sub-agent's
  name for children, `"intent-analyst"` for the analyst's events.
  Provider/model is not an agent (the bus previously carried
  `provider/model` there — dropped). Durable payloads gain `agent`; the
  `timeline_events.execution_id` column already exists and now rides the
  frame. Legacy rows: `dbSessionEvents` left-joins `executions` on
  `execution_id` and uses `payload.agent` when present, else the
  execution's `delegation` column (`root` → `"root"`).
- **D2 Thread roles.** Transcript rows gain `agent` + `execution_id`
  (new `messages.agent` column, guarded migration like `reasoning`;
  the engine writes `in.agentName` or `"root"`, children their name; the
  chat path leaves it empty and `chatView` renders empty as `"root"` —
  so legacy and chat rows arrive as root on the wire). Rule in the thread: `role === 'assistant' && !tool_calls`
  → `agent === 'root'` (or no agent = legacy) is an answer; any other
  agent is a report card (`<details>`: agent · size). The last root
  answer of the turn is THE answer (styled as such).
- **D3 Reasoning + roles.** The adapter writes `out.ReasoningContent`
  to `messages.reasoning`. The answer block shows the final row's
  reasoning under a collapsed "Thinking" toggle and the previous root
  assistant row's content as "Narration". The Work tab's step detail
  already renders `reasoning` when present — it lights up for free.
- **D4 Delegation events.** `delegation_started{agent, task,
  execution_id}` when the child's execution row exists (before its first
  step) and `delegation_completed{agent, execution_id, tokens_in,
  tokens_out, duration_ms, status}` after `runLoop` returns — the usage
  read back from the execution row (`store.ExecutionUsage`), so
  `runLoop`'s signature stays. Payload = the contract's
  `DelegationPayload` extended (optional fields). The existing `delegation`
  kind (child steps on the live bus) is unchanged.
- **D5 Run cost.** `SessionState.executions[]` from the store (id,
  agent, role, status, tokens, started/finished) — the footer reads the
  state surface (durable; refreshed by `reconcile` after the run);
  live `delegation_completed` events update the cards in flight.
- **D6 The duplicate report stays.** The child's final row (its own
  transcript) and the `delegate_agent` tool result (the root's context)
  both remain; the UI never renders the tool result as an answer.
  Dedupe needs reference expansion in the root's context builder — a
  follow-up with S3's context budget.
- **D7 The bus `Agent` for root runs** changes from `provider/model` to
  `"root"` (or the named agent) — no consumer read the old value.

## Risks / Trade-offs

- A schema migration (`messages.agent`) — guarded, idempotent, the
  house pattern; legacy rows read as root.
- Two attribution sources (payload, executions join) — the join is the
  fallback only; a test pins both.
