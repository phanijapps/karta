# Spec Delta

## ADDED Requirements

### Requirement: Streaming sends deliver deltas live

A send SHALL be available as an SSE stream: reasoning deltas (kind
`reasoning`) stream as the model thinks, content deltas (kind `content`)
stream as it writes, and the stream SHALL terminate with one `chat_exchange`
event carrying the persisted rows in order plus the end cursor — identical
truth to the synchronous response. Deltas SHALL be volatile: never written
to the timeline, never required for replay. The synchronous endpoint SHALL
remain and return the same rows.

#### Scenario: Plain streamed round trip
- **WHEN** the user sends text via the streaming endpoint with a reachable reasoning model
- **THEN** reasoning deltas stream first, content deltas follow, and the terminal exchange event carries the user and assistant rows with the assistant's reasoning attached

#### Scenario: Non-reasoning model streams content only
- **WHEN** the selected model returns no reasoning field
- **THEN** only `content` deltas stream, and the terminal exchange is identical to the synchronous response

#### Scenario: Typed failure mid-stream
- **WHEN** the provider fails after deltas began
- **THEN** the stream emits a typed failure event, keeps the user row (and any committed tool rows), appends no final-answer row, and closes

#### Scenario: Other subscribed clients see typing
- **WHEN** a second client holds the session events stream open during a streamed send
- **THEN** it receives coalesced chat-delta notifications without any timeline persistence

### Requirement: Reasoning output is captured

The daemon SHALL decode provider reasoning fields on both the streaming and
non-streaming paths (`reasoning_content`, and the `reasoning` spelling), and
SHALL persist the final reasoning text on the assistant message row. The
message views (sync response, streaming terminal event, session reads)
SHALL carry the reasoning when present.

#### Scenario: Reasoning survives restart
- **WHEN** a send from a reasoning model completes and the daemon restarts
- **THEN** replaying the session returns the assistant row with its reasoning text intact

#### Scenario: Synchronous sends keep reasoning too
- **WHEN** the user sends via the synchronous endpoint with a reasoning model
- **THEN** the response's assistant row carries the reasoning text

## MODIFIED Requirements

### Requirement: Chat emits durable timeline events

Every appended message SHALL emit a `session` timeline event and every tool
execution a `tool_call` event, each persisted durably BEFORE any volatile
notification, with replay available by cursor. Streaming deltas are volatile
notifications, not timeline events; the durable contract is unchanged.
Chat is observable and restart-safe; live SSE delivery exists on both the
session events stream (durable events) and the streaming send endpoint
(volatile deltas + terminal truth).

#### Scenario: A send with a tool emits all events durably

- **WHEN** a send completes with one tool round
- **THEN** durable event replay yields session events per appended row and a tool_call event, in order, surviving restart

#### Scenario: Streamed sends do not multiply timeline writes

- **WHEN** a streamed send emits hundreds of deltas
- **THEN** durable event replay contains exactly the per-row events of the equivalent synchronous send
