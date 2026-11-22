# simple-chat Specification

## Purpose

The eternal single-session chat: one locally persisted conversation that lives forever, provider-backed replies with bounded built-in tool rounds, durable observability, and honest failure semantics.

## Requirements

### Requirement: One persistent session that lives forever

The daemon SHALL maintain exactly one chat conversation with a fixed identity (`default`), created lazily on first message append and never rotated, reset, archived, or deleted. The transcript SHALL only ever grow by appended messages, in append order, and SHALL survive daemon restarts. It is not a goal-run session of the existing session endpoints: engine runs (a later change) get their own identities and never terminate it.

#### Scenario: First message creates the session

- **WHEN** the first chat message is sent on a fresh database
- **THEN** the conversation exists and the message is its first transcript row

#### Scenario: Transcript survives restart

- **WHEN** the daemon restarts after messages were exchanged
- **THEN** replay returns every previously appended message in order

### Requirement: Messages persist atomically ordered in SQLite

Every appended message SHALL persist under `~/.zbot/data/` with role, content, UTC timestamp, tool fields, provider/model provenance and request token usage when known, an execution linkage, and a per-session `seq` assigned atomically at INSERT. Replay SHALL be cursor-paginated by `seq`; provider history reads SHALL be compaction-aware by construction. Request usage is not content size.

#### Scenario: Append and replay in order

- **WHEN** messages are appended and replayed from `seq` zero
- **THEN** they return in exact append order with all stored fields

#### Scenario: Cursor sync returns only new rows

- **WHEN** replay is requested with `after_seq` at the current end
- **THEN** only messages appended after that cursor are returned

#### Scenario: Rows record their execution and provenance

- **WHEN** an assistant row is written
- **THEN** it carries the execution that produced it and the provider/model that answered, so attribution survives restarts

### Requirement: Sends serialize per conversation

A send SHALL run as one serialized exchange per conversation: while a send is in flight, another send to the same conversation waits (or is refused with a typed busy error); rows from concurrent senders never interleave half-exchanges. No database transaction SHALL span a provider call.

#### Scenario: Two clients send concurrently

- **WHEN** two sends race on the same conversation
- **THEN** they execute one-after-the-other as complete exchanges, never interleaved

### Requirement: Sending exchanges one bounded completion with basic tools

A send SHALL append the user message, then produce the assistant reply via an OpenAI-compatible chat completion over the compaction-aware root-context window. Built-in tools (`clock.now`, `echo`) are offered; requested tools execute (intent persisted BEFORE execution) with a fixed small round cap. A failed send keeps the user row and every committed intermediate row, appends no final-answer row, and reports the typed failure.

#### Scenario: Plain round trip

- **WHEN** the user sends text with a reachable model selected
- **THEN** the user row and one assistant row are appended and returned

#### Scenario: One tool round

- **WHEN** the model requests `clock.now` before answering
- **THEN** the intent row, a tool row recording the result, and the final assistant answer are appended in order

#### Scenario: Tool rounds are bounded

- **WHEN** the model keeps requesting tools past the round cap
- **THEN** the daemon stops offering tools and completes (or fails typed) instead of looping

#### Scenario: Failure after a tool round keeps committed rows

- **WHEN** the provider fails on the follow-up completion after a tool round committed
- **THEN** the user row and the tool-round rows remain, no final-answer row exists, and the typed failure is returned

### Requirement: Provider context windows are exchange-complete

The history window sent to a provider SHALL never start in the middle of a tool exchange: leading tool rows whose assistant call falls outside the window, and incomplete leading assistant tool-call groups, are dropped from the window front. The window is bounded; when the untrimmed suffix exceeds the bound the service signals that compaction is due rather than silently overflowing.

#### Scenario: Cutoff lands inside a tool exchange

- **WHEN** the row bound would cut between an assistant tool-call row and its tool result
- **THEN** the window is trimmed to the exchange boundary so no dangling tool reference is sent

### Requirement: Chat emits durable timeline events

Every appended message SHALL emit a `session` timeline event and every tool execution a `tool_call` event, each persisted durably BEFORE any volatile notification, with replay available by cursor. Chat is observable and restart-safe from day one; live SSE delivery is a separate streaming change.

#### Scenario: A send with a tool emits all events durably

- **WHEN** a send completes with one tool round
- **THEN** durable event replay yields session events per appended row and a tool_call event, in order, surviving restart

### Requirement: Model selection is explicit

A send SHALL name the provider and model (the picker's qualified selection). With no model selected the daemon SHALL refuse the send with a typed error telling the user to pick one; no default model is invented.

#### Scenario: No model selected

- **WHEN** a send arrives without a selected model
- **THEN** it is refused with guidance to choose a model in the header picker

### Requirement: Chat renders safely and honestly

Displayed content SHALL render as safe text without executing HTML; tool activity is always visible as tool activity; pending sends show a pending state, never fabricated answers; after EVERY send outcome the surface reconciles its transcript from its last contiguously delivered cursor so committed intermediate and failure rows always become visible; unimplemented actions are omitted or visibly unavailable.

#### Scenario: Untrusted message text

- **WHEN** a message contains HTML or script-like markup
- **THEN** it appears as inert text rather than executable content

#### Scenario: Pending send

- **WHEN** a send is awaiting the assistant reply
- **THEN** the surface shows a pending state and the composer stays honest about it

#### Scenario: Composer interaction rules

- **WHEN** the user presses Shift+Enter, submits blank input, or presses Enter during IME composition
- **THEN** a newline is inserted, submission is refused, or nothing is sent, respectively; focus returns to the composer after sending

#### Scenario: Reconciliation after a failed send

- **WHEN** a send fails after its user row was persisted
- **THEN** the transcript reconciles to show that row plus the typed failure, without duplicating or losing rows
