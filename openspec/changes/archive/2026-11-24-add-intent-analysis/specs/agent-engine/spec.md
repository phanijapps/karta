# Spec Delta

## MODIFIED Requirements

### Requirement: The engine loop is bounded and tool-calling

The engine SHALL drive an OpenAI-compatible chat loop with tool calling over the compaction-aware history window for the run's execution scope, bounded by a maximum number of steps per run. Tools SHALL execute through the tool-provider seam only; the engine SHALL persist a tool-call intent before execution and a tool result row after, emitting `tool_call` events. The loop terminates on a final assistant answer, the step bound, or cancellation. ROOT runs MAY carry a pre-executed intent analysis (prompt section + complexity-derived step budget replacing the fixed bound); analysis absence, bypass, and failure are all valid run states with no effect on the loop's semantics. The conformance suite SHALL accept engines without analysis (the middleware is daemon-layer, not an engine obligation).

#### Scenario: A run uses a tool

- **WHEN** the model requests a tool during a run
- **THEN** intent and result rows are persisted, a tool_call event is emitted, and the loop continues to a final answer

#### Scenario: The step bound stops runaway loops

- **WHEN** a run exceeds the maximum steps
- **THEN** it terminates with a recorded outcome rather than looping forever

#### Scenario: Budget override on analyzed runs

- **WHEN** a root run arrives with an analysis carrying complexity L
- **THEN** the loop's step bound is 16 for that run
