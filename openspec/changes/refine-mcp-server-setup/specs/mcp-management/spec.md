# Spec Delta

## ADDED Requirements

### Requirement: MCP enablement

An MCP manifest MAY carry `enabled` (boolean; absent means enabled).
Toggling SHALL persist to the manifest file and apply immediately.
Disabled servers SHALL stay registered, listed, and toggleable, and
SHALL NOT be hidden from management; the connection probe SHALL still
verify a disabled server's configuration. Future runtime wiring (agent
tool calls) SHALL skip disabled servers.

#### Scenario: Toggle persists

- **WHEN** a server is disabled and the daemon restarts
- **THEN** the server is still listed as disabled and its manifest file
  carries the disabled state

#### Scenario: Disabled servers stay managed

- **WHEN** a server is disabled
- **THEN** it remains in the list (marked disabled), can be re-enabled,
  edited, deleted, and its connection can still be tested
