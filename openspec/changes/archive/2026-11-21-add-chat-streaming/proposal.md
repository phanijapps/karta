# Proposal

## Why

Quick chat today is a synchronous POST: the user watches a spinner for the
entire exchange, then the full reply appears at once (the contract says so
explicitly: "Synchronous; streaming is a future change"). Worse, thinking
tokens are silently discarded — the provider client decodes neither
`message.reasoning_content` nor any streaming shape, the store has no
reasoning column, and the UI has nowhere to show them. Reasoning models
already pay for those tokens on every reply; Karta neither shows nor keeps
them.

## What Changes

- **Streaming send (contract-first):** new SSE operation — `POST
  /api/chat/send` returning `text/event-stream` with `chat_delta` events
  (`kind: reasoning | content`) and a terminal `chat_exchange` event carrying
  the persisted rows + cursor (same shape as today's synchronous response).
  The existing `sendChatMessage` stays, unchanged, for compatibility.
- **Provider streaming:** `ChatCompletionStream` — `"stream": true`, parses
  OpenAI-compatible SSE chunks (`delta.reasoning_content` — the Z.ai/DeepSeek/
  Qwen/vLLM de-facto standard — plus `delta.reasoning`), `[DONE]` sentinel,
  usage from the final chunk, same typed-failure taxonomy. The non-streaming
  path additionally decodes `message.reasoning_content` so it stops dropping
  thinking tokens too.
- **Chat service:** `SendStream` — identical durable-first semantics (tool
  intent persisted before execution; the assistant row persists ONCE at
  completion; F-15 characterizations hold). Chunks are volatile: written
  directly to the SSE response, never to the Timeline Tree; coalesced
  (~100ms) `chat_delta` events also ride the eventbus as ephemeral
  notifications so other subscribed clients (second window, future channels)
  see typing live.
- **Reasoning persistence:** the final reasoning text persists on the
  assistant message row (new nullable `reasoning` column; guarded ALTER
  migration). Rationale: it is part of the model's paid output and exactly
  what Timeline auditability is for.
- **Frontend:** progressive render — live collapsible Thinking block
  (auto-expanded while reasoning streams, auto-collapses when content
  begins, expandable after), streaming content render, terminal event
  reconciles against the persisted transcript (store remains the truth).
- Engine-plane (goal runs) thinking display and per-request thinking-effort
  control are explicitly out of scope (noted in design as follow-ups).

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `simple-chat`: gains streaming-send and reasoning-capture requirements;
  the durable-events requirement's "live SSE delivery is a separate
  streaming change" deferral is fulfilled.
- `api-contract`: new chat streaming send endpoint (additive, schema-disciplined).

## Impact

`contracts/openapi.yaml` + codegen (new endpoint, delta/exchange schemas,
reasoning field on the chat message view) · `internal/providers` (streaming
client + tests with an httptest SSE server) · `internal/chat` (SendStream +
emit-path extension, F-15 tests extended to the streaming path) ·
`internal/store` (reasoning column + guarded migration + replay surface) ·
`internal/server` (SSE handler) · `frontend` chat feature (streaming
consumer + Thinking UI). Local-first, boundaries, and eventbus rules
unchanged; every new emission path keeps durable-before-volatile.
