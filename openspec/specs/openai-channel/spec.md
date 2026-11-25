# openai-channel Specification

## Purpose

The OpenAI-compatible channel: OpenAI-shaped clients (OpenWebUI and
any `POST /v1/chat/completions` speaker) map their conversations onto
Karta sessions — the model name selects the session type, the
conversation's identity (metadata.chat_id, else the first-message
hash) derives a stable session id, and the reply is the exchange's
final assistant message. Typed failures carry the OpenAI error shape;
an optional bearer token (server.api_token) guards the pair.

## Requirements

### Requirement: OpenAI-compatible chat endpoint

*(the endpoint pair is unchanged; streaming joins it)*

#### Scenario: A streaming completion emits OpenAI chunks

- **WHEN** a client requests `stream: true` on a chat session
- **THEN** the response is `text/event-stream` of bare-data
  `chat.completion.chunk` frames (content deltas, then
  `finish_reason: stop`) terminated by `data: [DONE]` AFTER the stop
  frame

#### Scenario: A streaming research run shows its work

- **WHEN** a client requests `stream: true` on a research session
- **THEN** the stream carries the live work (tool calls, delegations,
  steps as prefixed content chunks) while the run executes, then the
  final answer and `stop`; a failed run ends with `finish_reason:
  error`; a busy session answers the typed 409 before any stream opens

#### Scenario: OpenWebUI chats into a research session

- **WHEN** an OpenAI client sends a completion with model
  `karta-research` and a first user message
- **THEN** a research session is created (first-message-hash id),
  the full pipeline runs, and the response carries the final
  assistant answer in the OpenAI completion shape

#### Scenario: Follow-up turns continue the conversation

- **WHEN** the same client sends the conversation again with an
  additional user message
- **THEN** the exchange appends to the SAME session (stable id) and
  the session appears in Karta's sidebar with its analysis surface

#### Scenario: Model list advertises the Karta models

- **WHEN** the client lists models
- **THEN** the response carries `karta-research` and `karta-chat`

#### Scenario: Optional bearer authentication

- **WHEN** `server.api_token` is set in config.yaml
- **THEN** /v1 requests require `Authorization: Bearer <token>`
  (401 otherwise); when unset the loopback posture applies unchanged

#### Scenario: Busy research sessions map to the OpenAI error shape

- **WHEN** a completion targets a research session whose run is live
- **THEN** the response is the OpenAI error body carrying the
  session-busy message (the client sees a typed failure, not a hang)
