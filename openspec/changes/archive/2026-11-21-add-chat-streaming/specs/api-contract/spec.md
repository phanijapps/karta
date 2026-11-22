# Spec Delta

## ADDED Requirements

### Requirement: Chat streaming send endpoint

`POST /api/chat/send` SHALL accept the same body as the synchronous send and
return `text/event-stream`: zero or more `chat_delta` events (data carries
`kind` — `reasoning` or `content` — and a `text` fragment) and exactly one
terminal `chat_exchange` event whose payload matches the synchronous
response schema (rows, end cursor, optional typed error — mid-stream
provider failures surface in the terminal exchange's error field, the same
single truth as the synchronous path) before the stream closes. The event
schemas SHALL be strict (additionalProperties: false), the endpoint
additive, and the generated clients typed against them.

#### Scenario: Happy stream shape
- **WHEN** a streamed send succeeds
- **THEN** the stream contains only chat_delta events followed by exactly one chat_exchange, then closes

#### Scenario: Bad request fails before streaming
- **WHEN** the body is empty text or an invalid model reference
- **THEN** the endpoint responds with the same typed 4xx JSON error as the synchronous endpoint, without opening a stream

#### Scenario: Typed failure mid-stream
- **WHEN** the provider fails after deltas began
- **THEN** the terminal exchange carries the typed error, the user row (and any committed tool rows) persist, no final-answer row exists, and the stream closes
