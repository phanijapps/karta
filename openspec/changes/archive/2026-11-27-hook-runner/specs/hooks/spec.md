# hooks

## ADDED Requirements

### Requirement: Hook commands run by the exit-code contract
The hook runner SHALL implement `domain.HookRunner` over the registry's hook
manifests: for an event, every manifest bound to that event whose matcher
(when present) matches the event's subject runs its `command` through a
shell with the `HookEvent` as JSON on stdin, within the manifest's timeout
(default 10 s, clamped to 60 s). Exit 0 SHALL be allow; exit 2 SHALL be
block with stderr as the reason; any other non-zero exit or a timeout SHALL
be warn-and-continue. Decisions SHALL combine block over warn over allow
with per-hook reasons, and a hook failure SHALL never fail the run.

#### Scenario: Allow, block and warn by exit code
- **WHEN** three hooks match an event and exit 0, 2 and 3 respectively
- **THEN** the decision is block, its reason names the exit-2 hook's stderr, and the context names all three hooks

#### Scenario: A timeout is a warning, not a failure
- **WHEN** a matching hook exceeds its timeout
- **THEN** it is killed, the decision carries a timed-out warning naming the hook, and the run continues

#### Scenario: The matcher selects by subject
- **WHEN** a PreToolUse manifest declares a matcher and the event's tool name does not match it
- **THEN** that hook does not run; for SubagentStop the subject is the agent name, and a matcher on a subject-less event is ignored

#### Scenario: The hook reads the event
- **WHEN** a hook command inspects its standard input
- **THEN** it finds the `HookEvent` as JSON (event, session, tool name and arguments when present)

### Requirement: Every hook decision is observable
Every hook decision — allow, block or warn — SHALL be emitted to the
Timeline as a durable-first `hook_decision` event carrying the decision
(`action`, `reason`, `context` naming the hooks that ran) with the run's
attribution; a child run's decisions SHALL surface in the parent's stream
as delegation-kind events while the durable row keeps `hook_decision`.

#### Scenario: An allow is on the record
- **WHEN** a hook allows a tool call
- **THEN** a `hook_decision` event with action allow is persisted, published and streamed before the tool executes
