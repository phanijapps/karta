# chitta-binding Specification

## Purpose

How chitta's seven-tool surface binds into the agent tool plane: the
provider composition agents see, the provenance stamped on every call,
and the degradation when chitta is unavailable.

## Requirements

### Requirement: Chitta tools on the agent tool plane

The daemon SHALL expose the seven chitta tools (`memory_remember`,
`memory_recall`, `memory_revise`, `memory_history`,
`knowledge_connect`, `knowledge_find_entities`, `knowledge_about`)
on the agent tool surface alongside the builtin tools, with the
schemas chitta already publishes. Agents SHALL invoke them by name
with JSON arguments; results SHALL be the tools' output transfer
objects; failures SHALL surface the chitta typed error text to the
agent without crashing the run.

#### Scenario: Agent calls memory_remember mid-conversation
- **WHEN** an agent invokes `memory_remember` with a valid observation batch through the tool plane
- **THEN** the observation is persisted with host-stamped provenance and the agent receives the per-observation dispositions

#### Scenario: Invalid chitta tool input is a tool error, not a crash
- **WHEN** an agent invokes a chitta tool with malformed or invalid arguments
- **THEN** the tool result carries the `invalid_input` message and the agent run continues

### Requirement: One merged provider at the composition root

The composition root SHALL compose the builtin tools and the chitta
binding into one tool provider; tool names SHALL be unique across the
merge (a collision is a startup error), and unknown names SHALL return
the existing typed unknown-tool error.

#### Scenario: Merged surface enumerates all tools
- **WHEN** the composed provider lists its tools
- **THEN** the builtin tools and the seven chitta tools all appear exactly once

### Requirement: Provenance stamped from the execution context

Every chitta tool invocation SHALL stamp its `ChittaHostContext` from
context values supplied by the calling layer (session id, execution id,
agent id); the binding SHALL NOT accept provenance from tool arguments.
When the calling context carries no identity, the binding SHALL use
its configured fallback (anonymous agent provenance), never fabricate a
session.

#### Scenario: Session identity flows to provenance
- **WHEN** the calling layer stamps session S and execution E into the context and an agent calls a chitta tool
- **THEN** the persisted provenance names S and E with source `agent`

#### Scenario: Anonymous fallback
- **WHEN** the context carries no identity
- **THEN** the call still succeeds with the fallback provenance and one observable event

### Requirement: Unavailable chitta degrades visibly

When the chitta system is closed or unavailable, chitta tool
invocations SHALL return a typed unavailable error to the agent; the
rest of the tool plane (builtin tools) SHALL keep working.

#### Scenario: Closed chitta does not take the tool plane down
- **WHEN** chitta is closed and an agent calls `clock.now` then `memory_recall`
- **THEN** `clock.now` succeeds and `memory_recall` returns the unavailable error text
