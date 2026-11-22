# Tasks

## 1. Contract and codegen

- [ ] 1.1 Add the streaming send operation to `contracts/openapi.yaml`:
  `POST /api/chat/send` → `text/event-stream` with `chat_delta` (kind:
  reasoning|content, text), `chat_error`, and terminal `chat_exchange`
  schemas (the exchange reuses the synchronous response schema by $ref);
  add optional `reasoning` to the chat message view; strict schemas,
  additive only. Verify: `task codegen` regenerates cleanly with zero
  uncommitted drift beyond the expected new bindings.

## 2. Provider streaming client

- [ ] 2.1 `ChatCompletionStream` in `internal/providers`: `"stream": true`,
  SSE line parser (`data:` payloads, `[DONE]` sentinel), `delta.content`,
  `delta.reasoning_content` (+ `reasoning` alias), usage from the final
  chunk (or absent → zero), same typed-failure taxonomy. Detect a
  non-streaming JSON response and degrade to one synthetic content delta.
  Verify: httptest SSE-server table tests (happy, reasoning+content,
  failure mid-stream, `[DONE]`-less provider, JSON-fallback), zero live
  calls.
- [ ] 2.2 Non-streaming path: decode `message.reasoning_content` into the
  response. Verify: table test with a reasoning-bearing fixture.

## 3. Store and chat service

- [ ] 3.1 `messages.reasoning` nullable column with guarded ALTER migration
  (pragma table_info check) in `internal/store`; replay/view surfaces carry
  it. Verify: migration test (old schema → open → column present, existing
  rows read with null reasoning) + restart round-trip.
- [ ] 3.2 `SendStream` in `internal/chat`: mirrors `Send` (sendMu, tool
  intent-before-execution, bounded rounds, typed failures), consumes the
  streaming client, persists the assistant row ONCE at completion with
  reasoning, emits the same durable per-row events (durable-before-volatile
  unchanged), and publishes coalesced (~100ms / kind-change) `chat_delta`
  events to the bus. Verify: fake-streaming completer tests cover plain,
  tool-round, failure-mid-stream, and the "hundreds of deltas → exactly the
  per-row durable events" invariant.
- [ ] 3.3 Extend the F-15 characterization tests to the streaming emit path
  (closed store/bus behavior on the stream leg). Verify: tests pin both
  paths' semantics.

## 4. Server handler

- [ ] 4.1 SSE handler for the streaming send in `internal/server`:
  no-store + flush per event, pre-stream 4xx for bad bodies/models (same
  typed JSON as sync), terminal exchange, clean close and client-disconnect
  handling (exchange completes per its own semantics; stream does not
  leak). Verify: handler tests via the real HTTP stack asserting per-event
  delivery, error shapes, and disconnect safety.

## 5. Frontend

- [ ] 5.1 Generated client typing for the new SSE operation; chat feature
  consumes the streaming send: optimistic streaming render (Thinking block
  auto-expanded during reasoning, auto-collapse on content start,
  expandable after), terminal exchange REPLACES the optimistic render with
  persisted rows. Verify: Playwright flow with a scripted SSE mock (deltas
  visible mid-stream, reconciled transcript after, thinking collapsible).
- [ ] 5.2 Svelte 5 runes discipline + a11y for the Thinking block (aria
  live region politeness, focus unaffected). Verify: `task check:frontend`
  green; a11y assertions in the Playwright flow.

## 6. Review and hardening

- [ ] 6.1 Run the read-only `zbot-reviewer` subagent over the change scope,
  the change artifacts, and the five constitution boundaries (durable-
  before-volatile on every new emit path especially); fix every P0/P1
  finding, fix P2s or record in the change summary why not. Verify:
  reviewer report shows zero open P0/P1.
- [ ] 6.2 Re-run `task verify` green (with `CONTRACT_CHANGE=1` — this change
  legitimately modifies the contract; codegen drift committed). Verify:
  `task verify` exits zero.
