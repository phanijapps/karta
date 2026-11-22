# Design

## Context

Quick chat is a synchronous POST over a serialized exchange (`chat.Send`):
user row → bounded completion (intent-before-execution tool rounds) →
assistant row, each row emitting a durable timeline event after commit
(F-15 semantics, pinned by characterization tests). The provider client is
non-streaming (`Accept: application/json`, whole-body decode) and discards
`reasoning_content`. The contract defers streaming explicitly; the session
events SSE endpoint (replay-then-live) exists and is the transport model to
copy. Store schema is `CREATE TABLE IF NOT EXISTS` — no migration framework.

## Goals / Non-Goals

**Goals:**
- Token-level feedback on every send (content and reasoning) with the
  transcript as persisted truth — deltas are a view, never a source.
- Thinking tokens captured everywhere: streaming path, sync path, store,
  views. Zero silent discards.
- Durable-before-volatile holds on every new path (constitution rule 4).
- Other subscribed clients observe typing (ephemeral), because the channel
  work in the backlog will want it.

**Non-Goals:**
- Engine-plane (goal runs) thinking display — step-granular events; a future
  engine event-model change owns it.
- Per-request thinking-effort control (`reasoning_effort`) — one contract
  field + passthrough as a follow-up knob.
- Streaming for tool *results* (tool rows are atomic, as today).

## Decisions

1. **Transport: dedicated SSE on the send endpoint, deltas also ride the
   bus as ephemeral events.** The requesting client reads the response
   stream (lowest latency, no subscription setup); coalesced ~100ms
   `chat_delta` events also publish to the eventbus so open session-events
   subscribers see typing. Alternatives: deltas only on the response
   (simplest, but second clients see nothing — rejected: the backlog's
   channel work needs observers); deltas only via session events (every
   client happy, but the sender waits on a second round-trip and
   correlation gets awkward — rejected).
2. **Chunk volatility with coalescing.** No DB write per token; the bus
   leg coalesces deltas (flush at ~100ms or on kind change) so bounded
   drop-oldest mailboxes never thrash. The assistant row persists once, at
   completion, exactly as today.
3. **Reasoning persists on the assistant row** (`messages.reasoning`,
   nullable). It is paid model output and Timeline-auditability material.
   Chunks of reasoning never persist. Alternative: volatile-only reasoning
   (smaller store) — rejected: restart kills the "why", and local-first
   makes size a non-issue.
4. **Provider parsing standard:** `delta.reasoning_content` /
   `message.reasoning_content` as primary (Z.ai, DeepSeek, Qwen, vLLM — the
   de-facto OpenAI-compatible field), `delta.reasoning` accepted as alias.
   Unknown extra delta fields are ignored (forward-compatible).
5. **`SendStream` mirrors `Send`, it does not replace it.** Same
   serialization (sendMu), same tool-round semantics, same typed failures;
   the sync path becomes the degenerate case internally (a stream consumed
   to completion). F-15 characterization tests extended to the streaming
   emit path so both stay honest.
6. **Guarded column migration:** `ALTER TABLE messages ADD COLUMN reasoning
   TEXT` executed only when the column is absent (pragma table_info check) —
   the house pattern for a schema with no migration framework.
7. **Frontend truth discipline:** stream renders optimistically; the
   terminal `chat_exchange` REPLACES the optimistic render with persisted
   rows (reconciliation, not append) — the store stays the single truth.

## Risks / Trade-offs

- [Proxy/SSE buffering swallows deltas] → handler sets
  `Cache-Control: no-store` + flushes per event; a test drives the stream
  through the real HTTP stack to prove per-event delivery.
- [Coalesced bus deltas still flood a slow subscriber] → bounded
  drop-oldest mailboxes already exist (eventbus contract); the coalescer
  bounds the rate; subscriber overflow counts stay observable.
- [Provider ignores `stream: true` and returns JSON] → detect
  content-type/first-byte shape, fall back to decoding the whole body and
  emit one synthetic content delta — the stream contract still holds.
- [Reasoning column grows unbounded] → local-first: acceptable; a future
  retention change can prune (out of scope, noted).
- [Contract drift between sync and terminal shapes] → terminal event reuses
  the SAME schema object as the synchronous response (single $ref).

## Migration Plan

Additive endpoint + nullable column: old clients unaffected; `task codegen`
regenerates; no data backfill (absent reasoning = null). Rollback = revert;
the column being nullable makes old code ignore it.

## Open Questions

- Should the coalesced bus `chat_delta` events carry a send-correlation id
  (execution id) from day one? (Leaning yes — one field, saves a breaking
  change when channels arrive.) Resolve during spec review, does not alter
  the approach.
