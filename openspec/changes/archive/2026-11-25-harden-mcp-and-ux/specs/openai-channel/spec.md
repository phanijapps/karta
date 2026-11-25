# Spec Delta

## MODIFIED Requirements

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
