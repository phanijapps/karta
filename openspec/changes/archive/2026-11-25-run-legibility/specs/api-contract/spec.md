# Spec Delta

## MODIFIED Requirements

### Requirement: Session read endpoints

The session list endpoint SHALL return bounded, explicitly truncated results from the store, and the session detail endpoint SHALL return identity and status. The session STATE endpoint SHALL additionally carry the session's executions — id, agent, role (root / analyst / the delegated agent), status, prompt and completion tokens, started and finished times — so clients can show a run's cost without a new endpoint.

#### Scenario: List is bounded and truncation is explicit
- **WHEN** the session list endpoint is called
- **THEN** the response bounds the number of entries and signals truncation when more exist

#### Scenario: Detail by identifier
- **WHEN** the session detail endpoint is called with an identifier
- **THEN** it returns 200 with the session's identity and status, or 404 with a typed error body for an unknown session

#### Scenario: State carries the run's executions
- **WHEN** a research session's state is read after a run with two delegations
- **THEN** the response lists the root, the analyst and both child executions with their tokens and terminal status

### Requirement: Timeline SSE stream

The session events endpoint SHALL stream that session's timeline events as SSE: replay of durable events first, then live events as they occur, ordered and gap-free, until the client disconnects. Events SHALL use the contract's TimelineEvent schema, and every frame — replayed or live — SHALL carry the acting `agent` and its `execution_id`.

#### Scenario: Subscribe to a session timeline
- **WHEN** a client opens the session events endpoint
- **THEN** it first receives the session's durable history, then live events as the run progresses

#### Scenario: Event payload mirrors the domain
- **WHEN** the stream event schema is compared with the domain TimelineEvent kinds
- **THEN** every domain kind (agent step, delegation, tool call, hook decision, CompactionTriggered, delegation started/completed) is representable in the contract schema

#### Scenario: Frames are attributed
- **WHEN** a child's tool call is streamed and later replayed
- **THEN** both frames carry the child's agent name and execution id

### Requirement: Chat transcript sync endpoint

The contract SHALL define `GET /api/chat/messages` returning transcript rows ordered by `seq` with bounded pages, a `next_cursor` that is the LAST DELIVERED sequence (a page cursor, never the database tail), and `truncated` indicating whether more replay rows remain. Clients page until `truncated` is false. Rows SHALL carry the acting `agent` ("root" or the sub-agent's name) and their `execution_id` when known.

#### Scenario: Full replay pages to the end
- **WHEN** the endpoint is called with no cursor on a multi-page transcript
- **THEN** paging with each returned `next_cursor` yields every row exactly once, in order

#### Scenario: Incremental sync
- **WHEN** the endpoint is called with the last delivered cursor
- **THEN** it returns only newer rows and the new page cursor

#### Scenario: Rows name their author
- **WHEN** a delegated child's report row is synced
- **THEN** it carries the child's agent name and execution id; the root's rows carry "root"
