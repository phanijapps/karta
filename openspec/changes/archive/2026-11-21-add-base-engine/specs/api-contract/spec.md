# Spec Delta

## MODIFIED Requirements

### Requirement: Goal endpoints

The contract SHALL define endpoints to start a goal run for a session, cancel a session's active run, and resume a session with user input. Create SHALL accept an optional qualified `model` (`provider/model-id`) selecting the run's model, returning 201 with the session; refusing with typed guidance when no model is resolvable (request or agent). Cancel SHALL stop the active run. Resume SHALL answer 501 with a typed not-yet body until interactive goals land.

#### Scenario: Start a goal run

- **WHEN** the goal endpoint is called with a goal and a model
- **THEN** it returns 201 with the session identity and status for the started run

#### Scenario: No model resolvable

- **WHEN** the goal endpoint is called with no request model and no agent model
- **THEN** it returns a typed error with guidance to select a model

#### Scenario: Cancel and resume

- **WHEN** cancel or resume is called for a session
- **THEN** cancel stops the active run (200), and resume answers 501 typed not-yet

### Requirement: Session read endpoints

The session list endpoint SHALL return bounded, explicitly truncated results from the store, and the session detail endpoint SHALL return identity and status.

#### Scenario: List is bounded and truncation is explicit

- **WHEN** the session list endpoint is called
- **THEN** the response bounds the number of entries and signals truncation when more exist

#### Scenario: Detail by identifier

- **WHEN** the session detail endpoint is called with an identifier
- **THEN** it returns 200 with the session's identity and status, or 404 with a typed error body for an unknown session

### Requirement: Timeline SSE stream

The session events endpoint SHALL stream that session's timeline events as SSE: replay of durable events first, then live events as they occur, ordered and gap-free, until the client disconnects. Events SHALL use the contract's TimelineEvent schema.

#### Scenario: Subscribe to a session timeline

- **WHEN** a client opens the session events endpoint
- **THEN** it first receives the session's durable history, then live events as the run progresses

#### Scenario: Event payload mirrors the domain

- **WHEN** the stream event schema is compared with the domain TimelineEvent kinds
- **THEN** every domain kind (agent step, delegation, tool call, hook decision, CompactionTriggered) is representable in the contract schema
