# Spec Delta

## ADDED Requirements

### Requirement: MCP import endpoint

The contract SHALL define an MCP import endpoint accepting the pasted document as RAW TEXT plus an optional `name` (single-server pastes) and an optional `replaceName` (edit-as-JSON: exactly one entry, replacing that server's transports wholesale — its name unchanged; a multi-entry document with `replaceName`, or an entry that would skip or fail, rejects or reports without touching the target). It SHALL return 200 with one typed outcome per entry in paste order — created (stored name, warnings), skipped (duplicate, disabled, other-OS), or failed (shape, field, name, capacity, `io_error`) — each naming its original key; individual entry failures never fail the request. It SHALL return 400 with a typed error for every document-level rejection: malformed JSON, duplicate keys, trailing content, BOM or non-UTF-8 text, JSON non-object, unclassifiable shape, no recognizable servers, single-server paste without `name`, payload over 256 KiB, or more than 32 entries. The request body's document field SHALL be a string; the daemon is the only parser.

#### Scenario: Mixed paste returns per-entry outcomes

- **WHEN** the import endpoint receives a paste with one valid entry, one duplicate, and one malformed entry
- **THEN** it returns 200 with three outcome rows — created, skipped-duplicate, failed — in paste order, each naming its original key and reason

#### Scenario: Document-level rejection is a 400 creating nothing

- **WHEN** the paste has trailing content, a duplicated key, or 33 entries
- **THEN** it returns 400 with a typed error body and no manifest is written

#### Scenario: Edit-as-JSON replaces one server

- **WHEN** the endpoint receives a single-entry document with `replaceName` naming an existing server
- **THEN** that server's manifest is replaced wholesale (name unchanged) and the outcome reports it created-at-replace; a second entry in the document rejects the request without touching the target

## ADDED Requirements

### Requirement: MCP enablement field

MCP responses SHALL carry `enabled` (boolean). MCP create and edit
bodies MAY carry `enabled` (absent means enabled); edit is full-replace,
so an absent field re-enables the server.

#### Scenario: Toggle round-trips

- **WHEN** a server is edited with `enabled: false` and then listed
- **THEN** the response reports `enabled: false` and the manifest file
  persists the state
