# Spec Delta

## MODIFIED Requirements

### Requirement: Runs stream durable-first timeline events

Every run step SHALL emit timeline events (agent steps, tool calls, session lifecycle) for the run's session, in order, each ATTEMPTED durable-first: the durable row is written before the live notification, and a failed write does not suppress the live event (the engine's recorded posture, pinned by its characterization tests and owned by the follow-up backend-engine-run-ownership). Every event — durable row and live frame alike — SHALL carry the acting agent ("root" or the sub-agent's name) and the execution it belongs to, so a replayed timeline is as attributed as the live one.

#### Scenario: Event order matches execution

- **WHEN** a run completes with a tool round
- **THEN** durable replay yields the lifecycle, step, and tool events in execution order

#### Scenario: Rows are attributed on both paths

- **WHEN** a delegated child makes a tool call
- **THEN** the durable row and the live frame both name the child's agent and execution, and a replay after restart shows the same attribution

#### Scenario: Legacy rows are attributed through their execution

- **WHEN** the stream replays a row persisted before agents rode the payload
- **THEN** its agent comes from the execution it belongs to (root or the delegated agent)

### Requirement: Delegation executes as child runs

The engine SHALL offer a delegation tool to runs whose depth grant allows it, and executing it SHALL run the named agent as a CHILD run of the same session: its own execution record under the parent's, its own bounded step budget, its agent's inventory, delegation events in the parent's stream, and its final answer folded back as the tool result. Each delegation SHALL emit a started event (agent, task, execution) when the child begins and a completed event (agent, execution, token usage, duration, terminal status) when it ends, so a delegation is a first-class Timeline node. The conformance suite SHALL carry delegation cases (fold-back, unknown target, depth refusal, cancellation propagation) every implementation must pass.

#### Scenario: Conformance covers delegation

- **WHEN** the shared conformance suite runs against an engine
- **THEN** delegate-and-fold-back, unknown-agent, depth-refusal, and cancellation-propagation cases all hold

#### Scenario: A delegation has a lifecycle with its cost

- **WHEN** a child run completes
- **THEN** the parent's stream carries `delegation_started` before the child's steps and `delegation_completed` after them, the latter with the child's execution id, summed tokens, duration, and status

## ADDED Requirements

### Requirement: Research-run assistant rows persist reasoning

The engine SHALL persist the model's reasoning text (`reasoning_content` / `reasoning`) on every assistant row it writes, exactly as the chat path does, so the transcript carries what the model thought for each step.

#### Scenario: Reasoning survives a research run

- **WHEN** a root run's final step comes from a reasoning model
- **THEN** the final assistant row carries that step's reasoning text, and replay after restart returns it intact
