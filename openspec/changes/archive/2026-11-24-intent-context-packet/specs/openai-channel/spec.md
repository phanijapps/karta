# Spec Delta

## ADDED Requirements

### Requirement: OpenAI-compatible chat endpoint

The daemon SHALL expose an OpenAI-compatible endpoint pair — `GET
/v1/models` and `POST /v1/chat/completions` (non-streaming) — that
maps OpenAI-client conversations (OpenWebUI first) onto Karta
sessions: the model name selects the session type (`karta-research`
→ research, `karta-chat` → chat), and the conversation's first user
message derives a stable session id so follow-up turns continue the
same session. The reply SHALL be the exchange's final assistant
message content; failures SHALL map to the OpenAI error shape
(including the research session-busy conflict).

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
