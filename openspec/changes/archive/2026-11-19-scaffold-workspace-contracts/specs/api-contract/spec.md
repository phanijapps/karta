# Spec Delta

## Purpose

The initial daemon HTTP/SSE contract in `contracts/openapi.yaml` — the single source of truth every client and handler is written and generated against.

## ADDED Requirements

### Requirement: Contract exists with strict schema discipline

`contracts/openapi.yaml` SHALL be a valid OpenAPI 3.1 document in which every operation has an `operationId`, every object schema sets `additionalProperties: false` with explicit required fields, and identifiers are bounded (length and pattern).

#### Scenario: Contract validates

- **WHEN** the contract is checked with an OpenAPI 3.1 validator
- **THEN** it passes with no errors

#### Scenario: Every operation is callable by name

- **WHEN** the contract is inspected
- **THEN** each operation carries a unique `operationId` usable for client generation

### Requirement: Health endpoint

The contract SHALL define a liveness endpoint returning success when the daemon is up.

#### Scenario: Liveness probe

- **WHEN** the health endpoint is called on a running daemon
- **THEN** it returns 200 with a body identifying the daemon and version

### Requirement: Goal endpoints

The contract SHALL define endpoints to start a goal run for a session, cancel a session's active run, and resume a session with user input, matching the domain engine seam's `RunGoal`, `Cancel`, and `Resume` semantics.

#### Scenario: Start a goal run

- **WHEN** the goal endpoint is called with a goal request
- **THEN** it returns the session identity and status for the started run (201), or a validation error (400)

#### Scenario: Cancel and resume

- **WHEN** cancel or resume is called for a session
- **THEN** the contract defines their success and not-found (404) responses with typed error bodies

### Requirement: Session read endpoints

The contract SHALL define a session list endpoint with bounded, explicitly truncated results and a session detail endpoint returning identity and status.

#### Scenario: List is bounded and truncation is explicit

- **WHEN** the session list endpoint is called
- **THEN** the response bounds the number of entries and signals truncation when more exist

#### Scenario: Detail by identifier

- **WHEN** the session detail endpoint is called with an identifier
- **THEN** it returns 200 with the session's identity and status, or 404 with a typed error body for an unknown session

### Requirement: Timeline SSE stream

The contract SHALL define a per-session SSE stream endpoint whose event payload schema matches the domain `TimelineEvent` (session identity, event kind, timestamp, kind-specific payload) with the `CompactionTriggered` kind present, served as `text/event-stream`.

#### Scenario: Subscribe to a session timeline

- **WHEN** a client opens the session events endpoint
- **THEN** the contract specifies an SSE stream of timeline events for that session

#### Scenario: Event payload mirrors the domain

- **WHEN** the stream event schema is compared with the domain TimelineEvent kinds
- **THEN** every domain kind (agent step, delegation, tool call, hook decision, CompactionTriggered) is representable in the contract schema

### Requirement: Additive contract evolution

Within a contract version, changes SHALL be additive (new optional fields, new operations); removing or renaming fields or changing semantics SHALL bump the major version.

#### Scenario: Additive change keeps the version

- **WHEN** a new endpoint or optional field is added
- **THEN** the minor version increments and existing clients remain valid

#### Scenario: Breaking change bumps major

- **WHEN** a field is removed or renamed
- **THEN** the major version is bumped
