# Spec Delta

## ADDED Requirements

### Requirement: Chat streaming send endpoint

`POST /api/chat/send` SHALL accept the same body as the synchronous send and
return `text/event-stream`: zero or more `chat_delta` events (data carries
`kind` — `reasoning` or `content` — and a `text` fragment), at most one typed
`chat_error` event on failure, and exactly one terminal `chat_exchange` event
whose payload matches the synchronous response schema (rows, end cursor,
optional error) before the stream closes. The event schemas SHALL be strict
(additionalProperties: false), the endpoint additive, and the generated
clients typed against them.

#### Scenario: Happy stream shape
- **WHEN** a streamed send succeeds
- **THEN** the stream contains only chat_delta events followed by exactly one chat_exchange, then closes

#### Scenario: Bad request fails before streaming
- **WHEN** the body is empty text or an invalid model reference
- **THEN** the endpoint responds with the same typed 4xx JSON error as the synchronous endpoint, without opening a stream

#### Scenario: Client disconnect mid-stream
- **WHEN** the client disconnects after deltas began
- **THEN** the daemon completes or fails the exchange per its own semantics (the user row persists), emits the durable per-row events, and does not leak the stream
