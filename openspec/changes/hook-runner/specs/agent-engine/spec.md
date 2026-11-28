# agent-engine

## ADDED Requirements

### Requirement: Tool calls pass through hooks
On the ADK path every tool call SHALL raise PreToolUse before execution —
on the live plane, the dispatch alias and middleware-injected tools alike
— and PostToolUse after it with the output in the payload. A PreToolUse
block SHALL NOT execute the call: the model sees the hook's reason as a
recoverable tool error, recorded like a refusal. A PostToolUse block SHALL
replace the model-facing result with the reason while the durable row keeps
the real output. A warn SHALL proceed. The root run SHALL raise
SessionStart at its start and SessionEnd at every terminal with the status
in the payload; both are advisory (a block reads as warn).

#### Scenario: A blocked tool is never executed
- **WHEN** a PreToolUse hook blocks a call to a tool
- **THEN** the tool is not executed, the tool result the model sees is the hook's reason, and the `tool_call` row and event record the blocked text

#### Scenario: A post-hook hides the output from the model only
- **WHEN** a PostToolUse hook blocks after a tool ran
- **THEN** the model sees the hook's reason, the durable tool row keeps the real output, and the run continues

#### Scenario: The run's edges raise session hooks
- **WHEN** a root run starts and later reaches any terminal state
- **THEN** SessionStart fires at the start and SessionEnd fires once at the terminal with the status in its payload
